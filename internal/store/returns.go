package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"
)

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand must be available; fail loudly rather than reuse IDs.
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func nowString() string { return time.Now().UTC().Format(time.RFC3339) }

func encodeStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func decodeStrings(encoded string) []string {
	var values []string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return []string{}
	}
	return values
}

// CreateReturn persists a new return and its item lines in one transaction.
func (s *Store) CreateReturn(ctx context.Context, input NewReturnInput) (*Return, error) {
	id := newID()
	createdAt := nowString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
INSERT INTO returns (id, order_id, status, reason, evidence, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, input.OrderID, StatusRequested, input.Reason, encodeStrings(input.Evidence), createdAt, createdAt)
	if err != nil {
		return nil, err
	}

	for position, item := range input.Items {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO return_items (return_id, position, product_id, quantity)
VALUES (?, ?, ?, ?)`, id, position, item.ProductID, item.Quantity); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	items := make([]ReturnItem, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, ReturnItem{ProductID: item.ProductID, Quantity: item.Quantity})
	}
	return &Return{
		ID:               id,
		OrderID:          input.OrderID,
		Status:           StatusRequested,
		Reason:           input.Reason,
		Evidence:         append([]string{}, input.Evidence...),
		RefundableAmount: 0,
		Items:            items,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}, nil
}

// GetReturn loads a return with its item lines.
func (s *Store) GetReturn(ctx context.Context, id string) (*Return, error) {
	return s.getReturn(ctx, s.db, id)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (s *Store) getReturn(ctx context.Context, q rowQuerier, id string) (*Return, error) {
	record := &Return{}
	var receivedAt sql.NullString
	var evidence string
	err := q.QueryRowContext(ctx, `
SELECT id, order_id, status, reason, evidence,
       quality_conclusion, received_at, refundable_amount, reject_reason,
       created_at, updated_at
FROM returns WHERE id = ?`, id).Scan(
		&record.ID,
		&record.OrderID,
		&record.Status,
		&record.Reason,
		&evidence,
		&record.QualityConclusion,
		&receivedAt,
		&record.RefundableAmount,
		&record.RejectReason,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrReturnNotFound
	}
	if err != nil {
		return nil, err
	}
	record.Evidence = decodeStrings(evidence)
	record.ReceivedAt = receivedAt.String

	rows, err := q.QueryContext(ctx, `
SELECT product_id, quantity
FROM return_items
WHERE return_id = ?
ORDER BY position ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	record.Items = []ReturnItem{}
	for rows.Next() {
		var item ReturnItem
		if err := rows.Scan(&item.ProductID, &item.Quantity); err != nil {
			return nil, err
		}
		record.Items = append(record.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return record, nil
}

// ApproveReturn records the quality conclusion and sets a return to approved.
// Only requested returns can be processed.
func (s *Store) ApproveReturn(ctx context.Context, id string, qualityConclusion string, receivedAt string, refundableAmount int64) (*Return, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	record, err := s.getReturnForProcessing(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	updatedAt := nowString()
	if _, err := tx.ExecContext(ctx, `
UPDATE returns
SET quality_conclusion = ?, received_at = ?, refundable_amount = ?, status = ?, updated_at = ?
WHERE id = ?`, qualityConclusion, receivedAt, refundableAmount, StatusApproved, updatedAt, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	record.QualityConclusion = qualityConclusion
	record.ReceivedAt = receivedAt
	record.RefundableAmount = refundableAmount
	record.Status = StatusApproved
	record.UpdatedAt = updatedAt
	return record, nil
}

// RejectReturn records a rejection reason and sets a return to rejected.
// Only requested returns can be processed.
func (s *Store) RejectReturn(ctx context.Context, id string, reason string) (*Return, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	record, err := s.getReturnForProcessing(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	updatedAt := nowString()
	if _, err := tx.ExecContext(ctx, `
UPDATE returns
SET reject_reason = ?, status = ?, updated_at = ?
WHERE id = ?`, reason, StatusRejected, updatedAt, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	record.RejectReason = reason
	record.Status = StatusRejected
	record.UpdatedAt = updatedAt
	return record, nil
}

func (s *Store) getReturnForProcessing(ctx context.Context, tx *sql.Tx, id string) (*Return, error) {
	// Take a write lock so two reviewers cannot both pass the requested check.
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM returns WHERE id = ?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, ErrReturnNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != StatusRequested {
		return nil, ErrAlreadyProcessed
	}
	return s.getReturn(ctx, tx, id)
}
