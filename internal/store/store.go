package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type PersistentStore struct {
	db *sql.DB
}

func NewPersistentStore(dbPath, blobDir string) (*PersistentStore, error) {
	// Open the metadata db
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Ping makes sure the file is actually accessible and the DSN is valid
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	store := &PersistentStore{db: db}

	return store, nil
}

func (s *PersistentStore) Close() error {
	return s.db.Close()
}
