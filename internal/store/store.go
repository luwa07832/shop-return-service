// Package store owns the SQLite file and every write the service performs.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
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
	// SQLite only allows a single writer; keep one connection so concurrent requests
	// never see "database is locked" and every transaction is serialized.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
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

const schema = `
CREATE TABLE IF NOT EXISTS service_metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS returns (
	id                 TEXT PRIMARY KEY,
	order_id           TEXT NOT NULL,
	status             TEXT NOT NULL,
	reason             TEXT NOT NULL,
	evidence           TEXT NOT NULL,
	quality_conclusion TEXT NOT NULL DEFAULT '',
	received_at        TEXT,
	refundable_amount  INTEGER NOT NULL DEFAULT 0,
	reject_reason      TEXT NOT NULL DEFAULT '',
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS return_items (
	return_id  TEXT NOT NULL REFERENCES returns(id) ON DELETE CASCADE,
	position   INTEGER NOT NULL,
	product_id TEXT NOT NULL,
	quantity   INTEGER NOT NULL,
	PRIMARY KEY (return_id, position)
);

CREATE TABLE IF NOT EXISTS refunds (
	id              TEXT PRIMARY KEY,
	return_id       TEXT NOT NULL REFERENCES returns(id) ON DELETE CASCADE,
	amount          INTEGER NOT NULL,
	reason          TEXT NOT NULL,
	evidence        TEXT NOT NULL,
	status          TEXT NOT NULL,
	failure_reason  TEXT NOT NULL DEFAULT '',
	idempotency_key TEXT NOT NULL,
	created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_refunds_return ON refunds(return_id);

-- Only one successful refund may exist per idempotency key; failed attempts may
-- be retried with the same key and therefore stay out of this index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_refunds_succeeded_key
	ON refunds(return_id, idempotency_key)
	WHERE status = 'succeeded';
`
