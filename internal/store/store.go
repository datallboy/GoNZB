package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/datallboy/gonzb/internal/indexer"
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
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal+mode(WAL)&_progma=synchronous=(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Ping makes sure the file is actually accessible and the DSN is valid
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	// Initialize Schema
	schema := `CREATE TABLE IF NOT EXISTS releases (
		id TEXT PRIMARY KEY,
		title TEXT,
		source TEXT,
		download_url TEXT,
		size INTEGER,
		category TEXT,
		redirect_allowed INTEGER, -- 0 for false, 1 for true
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	return &PersistentStore{db: db, blobDir: blobDir}, nil
}

// Satisfies app.Store for metadata
func (s *PersistentStore) SaveReleases(ctx context.Context, results []indexer.SearchResult) error {
	if len(results) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO releases (id, title, source, download_url, size, category, redirect_allowed) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range results {
		_, err := stmt.ExecContext(ctx, r.ID, r.Title, r.Source, r.DownloadURL, r.Size, r.Category, r.RedirectAllowed)
		if err != nil {
			return fmt.Errorf("failed to insert release %s: %w", r.ID, err)
		}
	}

	return tx.Commit()
}

func (s *PersistentStore) GetRelease(ctx context.Context, id string) (indexer.SearchResult, error) {
	var r indexer.SearchResult
	err := s.db.QueryRowContext(ctx, "SELECT id, title, source, download_url, size, category, redirect_allowed FROM releases WHERE id = ?", id).
		Scan(&r.ID, &r.Title, &r.Source, &r.DownloadURL, &r.Size)
	return r, err
}

func (s *PersistentStore) GetNZB(id string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.blobDir, id+".nzb"))
}

func (s *PersistentStore) PutNZB(id string, data []byte) error {
	_ = os.MkdirAll(s.blobDir, 0755)
	return os.WriteFile(filepath.Join(s.blobDir, id+".nzb"), data, 0644)
}

func (s *PersistentStore) Exists(id string) bool {
	_, err := os.Stat(filepath.Join(s.blobDir, id+".nzb"))
	return err == nil
}
