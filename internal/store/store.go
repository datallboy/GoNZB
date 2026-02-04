package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"
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
		Scan(&r.ID, &r.Title, &r.Source, &r.DownloadURL, &r.Size, &r.Category, &r.RedirectAllowed)
	return r, err
}

func (s *PersistentStore) GetNZBReader(id string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.blobDir, id+".nzb"))
}

func (s *PersistentStore) CreateNZBWriter(id string) (io.WriteCloser, error) {
	return os.Create(filepath.Join(s.blobDir, id+".nzb"))
}

func (s *PersistentStore) Exists(id string) bool {
	_, err := os.Stat(filepath.Join(s.blobDir, id+".nzb"))
	return err == nil
}

func (s *PersistentStore) Close() error {
	return s.db.Close()
}
