package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failures are not recoverable in a request.
		panic(fmt.Sprintf("read random bytes: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return prefix + hex.EncodeToString(b[:])
}

// CreateReturn persists a return request and its item lines.
func (s *Store) CreateReturn(ctx context.Context, in NewReturnInput) (*Return, error) {
	timestamp := nowTimestamp()
	id := newID("ret_")
	evidenceJSON, err := encodeEvidence(in.Evidence)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create return: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO returns (
			id, order_id, status, reason, evidence,
			refundable_amount, refunded_amount, quality_conclusion,
			received_at, reject_reason, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 0, 0, '', NULL, '', ?, ?)`,
		id, in.OrderID, StatusRequested, in.Reason, evidenceJSON, timestamp, timestamp,
	); err != nil {
		return nil, fmt.Errorf("insert return: %w", err)
	}
	for i, item := range in.Items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO return_items (return_id, position, product_id, quantity) VALUES (?, ?, ?, ?)`,
			id, i, item.ProductID, item.Quantity,
		); err != nil {
			return nil, fmt.Errorf("insert return item: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create return: %w", err)
	}

	return &Return{
		ID:               id,
		OrderID:          in.OrderID,
		Status:           StatusRequested,
		Reason:           in.Reason,
		Evidence:         append([]string{}, in.Evidence...),
		Items:            append([]ReturnItem{}, in.Items...),
		RefundableAmount: 0,
		RefundedAmount:   0,
		CreatedAt:        timestamp,
		UpdatedAt:        timestamp,
	}, nil
}

func (s *Store) loadReturn(ctx context.Context, q rowQuerier, id string) (*Return, error) {
	row := q.QueryRowContext(ctx, `SELECT `+returnColumns+` FROM returns WHERE id = ?`, id)
	r, err := scanReturn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReturnNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := q.QueryContext(ctx,
		`SELECT product_id, quantity FROM return_items WHERE return_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReturnItem{}
	for rows.Next() {
		var item ReturnItem
		if err := rows.Scan(&item.ProductID, &item.Quantity); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r.Items = items
	return r, nil
}

// GetReturn reads one return with its item lines.
func (s *Store) GetReturn(ctx context.Context, id string) (*Return, error) {
	return s.loadReturn(ctx, s.db, id)
}

// ApproveReturn applies a quality conclusion to a still-requested return.
func (s *Store) ApproveReturn(ctx context.Context, id, qualityConclusion, receivedAt string, refundableAmount int64) (*Return, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin approve: %w", err)
	}
	defer tx.Rollback()

	r, err := s.loadReturn(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if r.Status != StatusRequested {
		return nil, ErrReturnAlreadyProcessed
	}

	timestamp := nowTimestamp()
	if _, err := tx.ExecContext(ctx, `
		UPDATE returns
		   SET status = ?, quality_conclusion = ?, received_at = ?,
		       refundable_amount = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		StatusApproved, qualityConclusion, receivedAt, refundableAmount, timestamp, id, StatusRequested,
	); err != nil {
		return nil, fmt.Errorf("update approve: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit approve: %w", err)
	}
	return s.GetReturn(ctx, id)
}

// RejectReturn rejects a still-requested return with a mandatory reason.
func (s *Store) RejectReturn(ctx context.Context, id, reason string) (*Return, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reject: %w", err)
	}
	defer tx.Rollback()

	r, err := s.loadReturn(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if r.Status != StatusRequested {
		return nil, ErrReturnAlreadyProcessed
	}

	timestamp := nowTimestamp()
	if _, err := tx.ExecContext(ctx, `
		UPDATE returns
		   SET status = ?, reject_reason = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		StatusRejected, reason, timestamp, id, StatusRequested,
	); err != nil {
		return nil, fmt.Errorf("update reject: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reject: %w", err)
	}
	return s.GetReturn(ctx, id)
}
