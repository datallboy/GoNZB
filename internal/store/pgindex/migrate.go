package pgindex

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

//go:embed baselines/036_v0_9_0_baseline.up.sql
var baselineFS embed.FS

const (
	v090BaselineVersion = 36
	v090BaselinePath    = "baselines/036_v0_9_0_baseline.up.sql"
	migrationLockName   = "gonzb:pgindex:migrations"
)

var migrationNameRE = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

func (s *Store) RunMigrations() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pg store is not initialized")
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve PostgreSQL migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, migrationLockName); err != nil {
		return fmt.Errorf("acquire PostgreSQL migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, migrationLockName)
	}()

	if err := ensureModuleVersionTable(ctx, conn); err != nil {
		return err
	}

	current, err := currentVersion(ctx, conn, "pgindex")
	if err != nil {
		return err
	}

	migs, err := loadEmbeddedMigrations()
	if err != nil {
		return err
	}
	expected := expectedMigrationVersion()
	if current > expected {
		return fmt.Errorf(
			"pgindex schema version %d is newer than this GoNZB build supports (latest %d)",
			current,
			expected,
		)
	}

	// New v0.9 installations start from the canonical schema instead of
	// replaying the v0.8-to-v0.9 development history. Existing versioned v0.8
	// databases continue through the original migrations below.
	if current == 0 {
		empty, err := applicationSchemaIsEmpty(ctx, conn)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf(
				"pgindex database contains unversioned public objects; refusing to apply the v0.9 baseline over an unknown schema",
			)
		}

		baseline, err := baselineFS.ReadFile(v090BaselinePath)
		if err != nil {
			return fmt.Errorf("read v0.9 baseline: %w", err)
		}
		if err := applyMigrationSQL(ctx, conn, "v0.9 baseline", v090BaselineVersion, baseline); err != nil {
			return err
		}
		current = v090BaselineVersion
	}

	for _, m := range migs {
		if m.version <= current {
			continue
		}

		sqlBytes, err := migrationFS.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.path, err)
		}

		if err := applyMigrationSQL(ctx, conn, m.path, m.version, sqlBytes); err != nil {
			return err
		}

		current = m.version
	}

	return nil
}

func expectedMigrationVersion() int {
	migs, err := loadEmbeddedMigrations()
	if err != nil {
		panic(err)
	}
	if len(migs) == 0 {
		return v090BaselineVersion
	}
	return max(v090BaselineVersion, migs[len(migs)-1].version)
}

type migrationDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func applyMigrationSQL(ctx context.Context, db migrationDB, label string, version int, sqlBytes []byte) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", label, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO module_schema_version (module_name, version, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (module_name) DO UPDATE
		SET version = EXCLUDED.version,
		    updated_at = NOW()`,
		"pgindex", version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update module schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func applicationSchemaIsEmpty(ctx context.Context, db migrationDB) (bool, error) {
	var empty bool
	err := db.QueryRowContext(ctx, `
		SELECT NOT (
			EXISTS (
				SELECT 1
				FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = 'public'
				  AND c.relname <> 'module_schema_version'
				  AND c.relkind IN ('r', 'p', 'S', 'v', 'm', 'f')
				  AND NOT EXISTS (
					  SELECT 1
					  FROM pg_depend d
					  WHERE d.classid = 'pg_class'::regclass
					    AND d.objid = c.oid
					    AND d.deptype = 'e'
				  )
			)
			OR EXISTS (
				SELECT 1
				FROM pg_proc p
				JOIN pg_namespace n ON n.oid = p.pronamespace
				WHERE n.nspname = 'public'
				  AND NOT EXISTS (
					  SELECT 1
					  FROM pg_depend d
					  WHERE d.classid = 'pg_proc'::regclass
					    AND d.objid = p.oid
					    AND d.deptype = 'e'
				  )
			)
			OR EXISTS (
				SELECT 1
				FROM pg_type t
				JOIN pg_namespace n ON n.oid = t.typnamespace
				WHERE n.nspname = 'public'
				  AND t.typtype IN ('d', 'e', 'r')
				  AND NOT EXISTS (
					  SELECT 1
					  FROM pg_depend d
					  WHERE d.classid = 'pg_type'::regclass
					    AND d.objid = t.oid
					    AND d.deptype = 'e'
				  )
			)
		)`).Scan(&empty)
	if err != nil {
		return false, fmt.Errorf("inspect PostgreSQL schema before v0.9 baseline: %w", err)
	}
	return empty, nil
}

type embeddedMigration struct {
	version int
	path    string
}

func loadEmbeddedMigrations() ([]embeddedMigration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read pgindex migrations dir: %w", err)
	}

	migs := make([]embeddedMigration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}

		m := migrationNameRE.FindStringSubmatch(name)
		if len(m) != 2 {
			return nil, fmt.Errorf("invalid migration filename: %s", name)
		}

		v, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %s: %w", name, err)
		}

		migs = append(migs, embeddedMigration{
			version: v,
			path:    filepath.ToSlash(filepath.Join("migrations", name)),
		})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

func ensureModuleVersionTable(ctx context.Context, db migrationDB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS module_schema_version (
			module_name TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("ensure module_schema_version: %w", err)
	}
	return nil
}

func currentVersion(ctx context.Context, db migrationDB, module string) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, `
		SELECT version
		FROM module_schema_version
		WHERE module_name = $1`, module).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read module version for %s: %w", module, err)
	}
	return v, nil
}
