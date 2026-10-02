package store

import (
	"context"
	"database/sql"
)

// CreateRefund appends a successful refund record. The idempotency check,
// status guard, balance computation, insert and return-status update all run
// in one transaction so the accumulated successful amount can never overdraw
// the refundable total.
func (s *Store) CreateRefund(ctx context.Context, returnID string, input NewRefundInput) (*Refund, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	record, err := s.getReturn(ctx, tx, returnID)
	if err != nil {
		return nil, err
	}

	// Any successful record with the same key wins forever; failed records
	// leave the key free for a retry that appends a new record.
	var succeeded int
	err = tx.QueryRowContext(ctx, `
SELECT 1 FROM refunds
WHERE return_id = ? AND idempotency_key = ? AND status = ?
LIMIT 1`, returnID, input.IdempotencyKey, RefundSucceeded).Scan(&succeeded)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		return nil, ErrDuplicateRefund
	}

	// The approval outcome opens the refund window; refunding keeps it open for
	// further partial refunds and refunded leaves a zero balance, so attempts
	// after full settlement fall through to the balance check. Returns that
	// never passed review stay refused.
	switch record.Status {
	case StatusApproved, StatusRefunding, StatusRefunded:
	default:
		return nil, ErrReturnNotApproved
	}

	var refunded int64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(amount), 0) FROM refunds
WHERE return_id = ? AND status = ?`, returnID, RefundSucceeded).Scan(&refunded); err != nil {
		return nil, err
	}
	if input.Amount > record.RefundableAmount-refunded {
		return nil, ErrRefundExceedsBalance
	}

	id := newID()
	createdAt := nowString()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO refunds (id, return_id, amount, reason, evidence, status, failure_reason, idempotency_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)`,
		id, returnID, input.Amount, input.Reason, encodeStrings(input.Evidence),
		RefundSucceeded, input.IdempotencyKey, createdAt); err != nil {
		return nil, err
	}

	newStatus := StatusRefunding
	if refunded+input.Amount == record.RefundableAmount {
		newStatus = StatusRefunded
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE returns SET status = ?, updated_at = ? WHERE id = ?`,
		newStatus, createdAt, returnID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Refund{
		ID:             id,
		ReturnID:       returnID,
		Amount:         input.Amount,
		Reason:         input.Reason,
		Evidence:       append([]string{}, input.Evidence...),
		Status:         RefundSucceeded,
		FailureReason:  "",
		IdempotencyKey: input.IdempotencyKey,
		CreatedAt:      createdAt,
	}, nil
}

// ListRefunds returns every refund of a return in creation order (oldest first).
func (s *Store) ListRefunds(ctx context.Context, returnID string) ([]Refund, error) {
	if _, err := s.GetReturn(ctx, returnID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, return_id, amount, reason, evidence, status, failure_reason, idempotency_key, created_at
FROM refunds WHERE return_id = ? ORDER BY rowid ASC`, returnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRefunds(rows)
}

// GetRefund loads a single refund that belongs to the given return.
func (s *Store) GetRefund(ctx context.Context, returnID, refundID string) (*Refund, error) {
	if _, err := s.GetReturn(ctx, returnID); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, return_id, amount, reason, evidence, status, failure_reason, idempotency_key, created_at
FROM refunds WHERE id = ? AND return_id = ?`, refundID, returnID)
	refund, err := scanRefund(row)
	if err == sql.ErrNoRows {
		return nil, ErrRefundNotFound
	}
	return refund, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRefund(row rowScanner) (*Refund, error) {
	refund := &Refund{}
	var evidence string
	if err := row.Scan(
		&refund.ID,
		&refund.ReturnID,
		&refund.Amount,
		&refund.Reason,
		&evidence,
		&refund.Status,
		&refund.FailureReason,
		&refund.IdempotencyKey,
		&refund.CreatedAt,
	); err != nil {
		return nil, err
	}
	refund.Evidence = decodeStrings(evidence)
	return refund, nil
}

func scanRefunds(rows *sql.Rows) ([]Refund, error) {
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

// RecordFailedRefund appends a failed refund attempt without changing the
// return status or consuming the refundable balance. The failure reason is
// retained and the idempotency key stays free for retry.
func (s *Store) RecordFailedRefund(ctx context.Context, returnID string, input NewRefundInput, failureReason string) (*Refund, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := s.getReturn(ctx, tx, returnID); err != nil {
		return nil, err
	}

	id := newID()
	createdAt := nowString()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO refunds (id, return_id, amount, reason, evidence, status, failure_reason, idempotency_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, returnID, input.Amount, input.Reason, encodeStrings(input.Evidence),
		RefundFailed, failureReason, input.IdempotencyKey, createdAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Refund{
		ID:             id,
		ReturnID:       returnID,
		Amount:         input.Amount,
		Reason:         input.Reason,
		Evidence:       append([]string{}, input.Evidence...),
		Status:         RefundFailed,
		FailureReason:  failureReason,
		IdempotencyKey: input.IdempotencyKey,
		CreatedAt:      createdAt,
	}, nil
}
