package sqlitejob

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	db      *sql.DB
	blobDir string
}

func NewStore(dbPath, blobDir string) (*Store, error) {

	dbDir := filepath.Dir(dbPath)

	// Ensure the database directory exists
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Ensure the blob directory exist
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create blob directory: %w", err)
	}

	// Open the metadata db
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Ping makes sure the file is actually accessible and the DSN is valid
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	store := &Store{db: db, blobDir: blobDir}

	if err := store.RunMigrations(); err != nil {
		return nil, fmt.Errorf("could not migrate database: %w", err)
	}

	if err := store.ReconcileReleaseCache(context.Background()); err != nil {
		return nil, fmt.Errorf("could not reconcile release cache: %w", err)
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
