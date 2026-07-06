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
`)
	return err
}
