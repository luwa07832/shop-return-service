// Package store owns the SQLite file and every write the service performs.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Statuses a return can be in.
const (
	StatusRequested = "requested"
	StatusApproved  = "approved"
	StatusReceived  = "received"
	StatusInspected = "inspected"
	StatusRefunding = "refunding"
	StatusRefunded  = "refunded"
	StatusRejected  = "rejected"
	StatusClosed    = "closed"
)

// Refund statuses.
const (
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
)

// Sentinel errors callers map onto the published error codes.
var (
	ErrReturnNotFound         = errors.New("return not found")
	ErrReturnAlreadyProcessed = errors.New("return already processed")
	ErrReturnNotApproved      = errors.New("return is not approved")
	ErrRefundExceedsBalance   = errors.New("refund exceeds returnable balance")
	ErrDuplicateRefund        = errors.New("duplicate refund")
	ErrRefundNotFound         = errors.New("refund not found")
)

// Store wraps the SQLite handle so callers never touch database/sql directly.
type Store struct {
	db *sql.DB
}

// Open prepares the database file and the schema this service needs.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection serializes writers so the refund balance checks stay race-free.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Ping reports whether the storage layer is usable.
func (s *Store) Ping() error { return s.db.Ping() }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

func nowTimestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReturn(row rowScanner) (*Return, error) {
	var r Return
	var evidenceJSON string
	var receivedAt sql.NullString
	if err := row.Scan(
		&r.ID, &r.OrderID, &r.Status, &r.Reason, &evidenceJSON,
		&r.RefundableAmount, &r.RefundedAmount, &r.QualityConclusion,
		&receivedAt, &r.RejectReason, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	r.Evidence = []string{}
	if evidenceJSON != "" {
		if err := decodeEvidence(evidenceJSON, &r.Evidence); err != nil {
			return nil, err
		}
	}
	if receivedAt.Valid {
		r.ReceivedAt = &receivedAt.String
	}
	return &r, nil
}

const returnColumns = `
	id, order_id, status, reason, evidence,
	refundable_amount, refunded_amount, quality_conclusion,
	received_at, reject_reason, created_at, updated_at
`

const schema = `
CREATE TABLE IF NOT EXISTS service_metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS returns (
	id                 TEXT PRIMARY KEY,
	order_id           TEXT NOT NULL,
	status             TEXT NOT NULL,
	reason             TEXT NOT NULL DEFAULT '',
	evidence           TEXT NOT NULL DEFAULT '[]',
	refundable_amount  INTEGER NOT NULL DEFAULT 0,
	refunded_amount    INTEGER NOT NULL DEFAULT 0,
	quality_conclusion TEXT NOT NULL DEFAULT '',
	received_at        TEXT,
	reject_reason      TEXT NOT NULL DEFAULT '',
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS return_items (
	return_id  TEXT NOT NULL,
	position   INTEGER NOT NULL,
	product_id TEXT NOT NULL,
	quantity   INTEGER NOT NULL,
	PRIMARY KEY (return_id, position)
);

CREATE TABLE IF NOT EXISTS refunds (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	refund_id       TEXT NOT NULL UNIQUE,
	return_id       TEXT NOT NULL,
	amount          INTEGER NOT NULL,
	status          TEXT NOT NULL,
	failure_reason  TEXT NOT NULL DEFAULT '',
	idempotency_key TEXT NOT NULL,
	created_at      TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_refunds_succeeded_key
	ON refunds(return_id, idempotency_key) WHERE status = 'succeeded';
`
