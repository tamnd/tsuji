// Package store is the sqlite persistence layer: users, API keys,
// credits, the model catalog cache, and usage rows.
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// Store wraps the sqlite database.
type Store struct {
	db *sql.DB
}

// Open opens (and creates if missing) the database at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_version (
	version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	label TEXT NOT NULL,
	hash TEXT NOT NULL UNIQUE,
	disabled INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS generations (
	id TEXT PRIMARY KEY,
	key_id INTEGER NOT NULL REFERENCES keys(id),
	model_requested TEXT NOT NULL,
	model_served TEXT NOT NULL,
	provider TEXT NOT NULL,
	app_referer TEXT NOT NULL DEFAULT '',
	app_title TEXT NOT NULL DEFAULT '',
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0,
	cost_microcents INTEGER NOT NULL DEFAULT 0,
	latency_ms INTEGER NOT NULL DEFAULT 0,
	streamed INTEGER NOT NULL DEFAULT 0,
	finish_reason TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_generations_key_created ON generations(key_id, created_at);
`)
	return err
}
