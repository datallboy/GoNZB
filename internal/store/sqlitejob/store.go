package sqlitejob

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const expectedSchemaVersion = 1

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

	if err := store.ValidateSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("validate sqlitejob schema: %w", err)
	}

	if err := store.ReconcileBlobCacheIndex(context.Background()); err != nil {
		return nil, fmt.Errorf("could not reconcile blob cache index: %w", err)
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlitejob store is not initialized")
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.db.PingContext(pingCtx)
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("sqlitejob store is not initialized")
	}

	var version int
	err := s.db.QueryRowContext(ctx, `
		SELECT version
		FROM module_schema_version
		WHERE module_name = ?`, "sqlitejob").Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("sqlitejob schema version row is missing")
		}
		return 0, fmt.Errorf("read sqlitejob schema version: %w", err)
	}

	return version, nil
}

func (s *Store) ExpectedSchemaVersion() int {
	return expectedSchemaVersion
}

func (s *Store) ValidateSchema(ctx context.Context) error {
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}

	expected := s.ExpectedSchemaVersion()
	switch {
	case version < expected:
		return fmt.Errorf("sqlitejob schema is incomplete: expected version %d, got %d", expected, version)
	case version > expected:
		return fmt.Errorf("sqlitejob schema is newer than supported: expected version %d, got %d", expected, version)
	default:
		return nil
	}
}
