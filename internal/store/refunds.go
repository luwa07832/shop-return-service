package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// rowQuerier is satisfied by both *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// CreateRefund appends a refund record and updates the return balance atomically.
//
// The refund history is append-only: over-limit attempts are stored as failed
// records with a failure reason and the return status/balance is never rewritten
// for them. A key that already has a succeeded record rejects the request.
func (s *Store) CreateRefund(ctx context.Context, returnID string, in RefundInput) (*Return, *Refund, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin refund: %w", err)
	}
	defer tx.Rollback()

	r, err := s.loadReturn(ctx, tx, returnID)
	if err != nil {
		return nil, nil, err
	}

	timestamp := nowTimestamp()

	var existing sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT refund_id FROM refunds WHERE return_id = ? AND idempotency_key = ? AND status = ?
		 ORDER BY id LIMIT 1`,
		returnID, in.IdempotencyKey, RefundSucceeded,
	).Scan(&existing)
	switch {
	case err == nil:
		return nil, nil, ErrDuplicateRefund
	case !errors.Is(err, sql.ErrNoRows):
		return nil, nil, fmt.Errorf("lookup refund key: %w", err)
	}

	// Only post-approval returns can initiate refunds. refunded returns pass the
	// gate so the zero-balance check below reports the precise conflict.
	if r.Status != StatusApproved && r.Status != StatusRefunding && r.Status != StatusRefunded {
		return nil, nil, ErrReturnNotApproved
	}

	remaining := r.RefundableAmount - r.RefundedAmount
	if in.Amount > remaining {
		refundID := newID("ref_")
		failureReason := "refund amount exceeds the returnable balance"
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO refunds (
				refund_id, return_id, amount, status, failure_reason, idempotency_key, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			refundID, returnID, in.Amount, RefundFailed, failureReason, in.IdempotencyKey, timestamp,
		); err != nil {
			return nil, nil, fmt.Errorf("insert failed refund: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, fmt.Errorf("commit failed refund: %w", err)
		}
		failed := &Refund{
			RefundID:       refundID,
			ReturnID:       returnID,
			Amount:         in.Amount,
			Status:         RefundFailed,
			FailureReason:  failureReason,
			IdempotencyKey: in.IdempotencyKey,
			CreatedAt:      timestamp,
		}
		return r, failed, ErrRefundExceedsBalance
	}

	newRefunded := r.RefundedAmount + in.Amount
	newStatus := StatusRefunding
	if newRefunded >= r.RefundableAmount {
		newStatus = StatusRefunded
	}
	refundID := newID("ref_")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO refunds (
			refund_id, return_id, amount, status, failure_reason, idempotency_key, created_at
		) VALUES (?, ?, ?, ?, '', ?, ?)`,
		refundID, returnID, in.Amount, RefundSucceeded, in.IdempotencyKey, timestamp,
	); err != nil {
		return nil, nil, fmt.Errorf("insert refund: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE returns
		   SET refunded_amount = ?, status = ?, updated_at = ?
		 WHERE id = ?`,
		newRefunded, newStatus, timestamp, returnID,
	); err != nil {
		return nil, nil, fmt.Errorf("update refunded amount: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit refund: %w", err)
	}

	updated, err := s.GetReturn(ctx, returnID)
	if err != nil {
		return nil, nil, err
	}
	refund := &Refund{
		RefundID:       refundID,
		ReturnID:       returnID,
		Amount:         in.Amount,
		Status:         RefundSucceeded,
		IdempotencyKey: in.IdempotencyKey,
		CreatedAt:      timestamp,
	}
	return updated, refund, nil
}

// ListRefunds returns every refund of a return, oldest first.
func (s *Store) ListRefunds(ctx context.Context, returnID string) ([]Refund, error) {
	if _, err := s.GetReturn(ctx, returnID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT refund_id, return_id, amount, status, failure_reason, idempotency_key, created_at
		  FROM refunds
		 WHERE return_id = ?
		 ORDER BY created_at ASC, id ASC`, returnID)
	if err != nil {
		return nil, fmt.Errorf("list refunds: %w", err)
	}
	defer rows.Close()

	refunds := []Refund{}
	for rows.Next() {
		refund, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		refunds = append(refunds, *refund)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refunds, nil
}

// GetRefund reads one refund record.
func (s *Store) GetRefund(ctx context.Context, returnID, refundID string) (*Refund, error) {
	if _, err := s.GetReturn(ctx, returnID); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT refund_id, return_id, amount, status, failure_reason, idempotency_key, created_at
		  FROM refunds
		 WHERE return_id = ? AND refund_id = ?`, returnID, refundID)
	refund, err := scanRefund(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRefundNotFound
	}
	if err != nil {
		return nil, err
	}
	return refund, nil
}

func scanRefund(row rowScanner) (*Refund, error) {
	var refund Refund
	if err := row.Scan(
		&refund.RefundID, &refund.ReturnID, &refund.Amount, &refund.Status,
		&refund.FailureReason, &refund.IdempotencyKey, &refund.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &refund, nil
}
