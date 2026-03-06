package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create settings db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite settings db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect sqlite settings db: %w", err)
	}

	s := &Store{db: db}
	if err := s.RunMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("could not migrate settings db: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// milestone-2 scaffold only (effective overlay wiring comes later).
func (s *Store) LoadEffectiveSettings(ctx context.Context) error {
	_ = ctx
	return nil
}

// stores revision payload as JSON; normalization comes in later milestone.
func (s *Store) UpdateSettings(ctx context.Context, patch any) error {
	b, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal settings patch: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings_revision (payload_json)
		VALUES (?)`, string(b))
	if err != nil {
		return fmt.Errorf("insert settings revision: %w", err)
	}
	return nil
}

// watcher scaffold; real pub/sub later.
func (s *Store) WatchSettingsChanges(ctx context.Context) (<-chan struct{}, error) {
	ch := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}
