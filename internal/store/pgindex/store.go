package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB

	manifestCacheMu       sync.RWMutex
	manifestCacheMaxBytes int64
	manifestCacheTTLDays  int

	yencSeedScanMu               sync.Mutex
	yencSeedScanBackoffUntil     time.Time
	yencSeedScanConsecutiveEmpty int

	yencSelectionMu        sync.Mutex
	yencLastSelectionStats YEncRecoverySelectionStats

	yencApplyMu        sync.Mutex
	yencLastApplyStats YEncRecoveryApplyStats
}

// SetGoNZBNetManifestCachePolicy applies the runtime limits used by the
// federation manifest cache. A non-positive value disables that constraint.
func (s *Store) SetGoNZBNetManifestCachePolicy(maxBytes int64, ttlDays int) {
	if s == nil {
		return
	}
	s.manifestCacheMu.Lock()
	s.manifestCacheMaxBytes = maxBytes
	s.manifestCacheTTLDays = ttlDays
	s.manifestCacheMu.Unlock()
}

func (s *Store) manifestCachePolicy() (int64, int) {
	s.manifestCacheMu.RLock()
	defer s.manifestCacheMu.RUnlock()
	return s.manifestCacheMaxBytes, s.manifestCacheTTLDays
}

// NewStore opens PostgreSQL by DSN and runs application-owned migrations.
func NewStore(dsn string) (*Store, error) {
	return newStore(dsn, true)
}

// NewMaintenanceStore opens PostgreSQL by DSN for privileged maintenance-only
// commands. It validates the existing schema but intentionally does not run
// migrations; runtime migrations belong to the normal application role.
func NewMaintenanceStore(dsn string) (*Store, error) {
	return newStore(dsn, false)
}

func newStore(dsn string, runMigrations bool) (*Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("pg dsn is required")
	}

	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse pg connection config: %w", err)
	}
	if connConfig.RuntimeParams == nil {
		connConfig.RuntimeParams = make(map[string]string)
	}
	// These indexing selectors are small enough after refinement that PostgreSQL JIT
	// startup cost dominates one-shot stage runs. Disable it for this workload.
	connConfig.RuntimeParams["jit"] = "off"

	db := stdlib.OpenDB(*connConfig)

	// Reasonable pool defaults for initial indexing workloads.
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping pg connection: %w", err)
	}

	s := &Store{db: db}

	if runMigrations {
		// run module migrations on startup.
		if err := s.RunMigrations(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("run pgindex migrations: %w", err)
		}
	}

	if err := s.ValidateSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("validate pgindex schema: %w", err)
	}

	return s, nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.db.PingContext(pingCtx)
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}

	var version int
	err := s.db.QueryRowContext(ctx, `
		SELECT version
		FROM module_schema_version
		WHERE module_name = $1`, "pgindex").Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("pgindex schema version row is missing")
		}
		return 0, fmt.Errorf("read pgindex schema version: %w", err)
	}

	return version, nil
}

func (s *Store) ExpectedSchemaVersion() int {
	return expectedMigrationVersion()
}

func (s *Store) ValidateSchema(ctx context.Context) error {
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}

	expected := s.ExpectedSchemaVersion()
	switch {
	case version < expected:
		return fmt.Errorf("pgindex schema is incomplete: expected version %d, got %d", expected, version)
	case version > expected:
		return fmt.Errorf("pgindex schema is newer than supported: expected version %d, got %d", expected, version)
	default:
		return nil
	}
}
