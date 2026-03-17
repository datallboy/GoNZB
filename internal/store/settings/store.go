package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
	_ "modernc.org/sqlite"
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

// load latest runtime settings revision and overlay it onto bootstrap config.
func (s *Store) LoadEffectiveSettings(ctx context.Context, base *config.Config) (*config.Config, error) {
	runtime, err := s.GetRuntimeSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	effective := ApplyToConfig(base, runtime)
	if effective == nil {
		return nil, fmt.Errorf("effective config is nil")
	}

	// CHANGED: validate the overlaid config, not just bootstrap config.
	if err := effective.ValidateEffective(); err != nil {
		return nil, fmt.Errorf("validate effective config: %w", err)
	}

	return effective, nil
}

// return the latest persisted runtime settings revision.
// If no row exists yet, fall back to bootstrap-derived defaults.
func (s *Store) GetRuntimeSettings(ctx context.Context, base ...*config.Config) (*RuntimeSettings, error) {
	var (
		id      int64
		payload string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT id, payload_json
		FROM settings_revision
		ORDER BY id DESC
		LIMIT 1`).Scan(&id, &payload)

	if err == sql.ErrNoRows {
		if len(base) > 0 && base[0] != nil {
			out := FromConfig(base[0])
			out.Revision = 0
			return out, nil
		}
		return &RuntimeSettings{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select latest settings revision: %w", err)
	}

	if payload == "" {
		payload = "{}"
	}

	var runtime RuntimeSettings
	if err := json.Unmarshal([]byte(payload), &runtime); err != nil {
		return nil, fmt.Errorf("unmarshal settings revision %d: %w", id, err)
	}
	runtime.Revision = id

	return &runtime, nil
}

// patch and persist the latest runtime settings state as a new atomic revision.
func (s *Store) UpdateSettings(ctx context.Context, patch any) error {
	settingsPatch, ok := patch.(*RuntimeSettingsPatch)
	if !ok {
		return fmt.Errorf("settings patch must be *settings.RuntimeSettingsPatch")
	}

	current, err := s.GetRuntimeSettings(ctx)
	if err != nil {
		return fmt.Errorf("load current runtime settings: %w", err)
	}

	next := mergeRuntimeSettings(current, settingsPatch)

	b, err := encodeRuntimeSettings(next)
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

// poll settings_revision for new ids and publish a lightweight signal.
// This is intentionally simple for the first live-reload slice.
func (s *Store) WatchSettingsChanges(ctx context.Context) (<-chan struct{}, error) {
	lastSeen, err := s.latestRevisionID(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("read initial settings revision: %w", err)
	}
	if err == sql.ErrNoRows {
		lastSeen = 0
	}

	ch := make(chan struct{}, 1)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, currentErr := s.latestRevisionID(ctx)
				if currentErr != nil {
					continue
				}
				if current > lastSeen {
					lastSeen = current
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	return ch, nil
}

func (s *Store) latestRevisionID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM settings_revision
		ORDER BY id DESC
		LIMIT 1`).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
