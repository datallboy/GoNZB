package store

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type PersistentStore struct {
	db      *sql.DB
	blobDir string
}

func NewPersistentStore(dbPath, blobDir string) (*PersistentStore, error) {

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

	store := &PersistentStore{db: db, blobDir: blobDir}

	if err := store.RunMigrations(); err != nil {
		return nil, fmt.Errorf("could not migrate database: %w", err)
	}

	return store, nil
}

func (s *PersistentStore) GetNZBReader(id string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.blobDir, id+".nzb"))
}

func (s *PersistentStore) CreateNZBWriter(id string) (io.WriteCloser, error) {
	return os.Create(filepath.Join(s.blobDir, id+".nzb"))
}

func (s *PersistentStore) SaveNZBAtomically(id string, data []byte) (err error) {
	finalPath := filepath.Join(s.blobDir, id+".nzb")

	tmpFile, err := os.CreateTemp(s.blobDir, id+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp nzb file: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp nzb file: %w", err)
	}

	if err = tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp nzb file: %w", err)
	}

	if err = tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp nzb file: %w", err)
	}

	if err = os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to commit nzb cache file: %w", err)
	}

	return nil
}

func (s *PersistentStore) Exists(id string) bool {
	_, err := os.Stat(filepath.Join(s.blobDir, id+".nzb"))
	return err == nil
}

func (s *PersistentStore) Close() error {
	return s.db.Close()
}
