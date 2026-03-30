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

var migrationNameRE = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

func (s *Store) RunMigrations() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pg store is not initialized")
	}

	ctx := context.Background()

	if err := ensureModuleVersionTable(ctx, s.db); err != nil {
		return err
	}

	current, err := currentVersion(ctx, s.db, "pgindex")
	if err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read pgindex migrations dir: %w", err)
	}

	type mig struct {
		version int
		path    string
	}

	migs := make([]mig, 0, len(entries))
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
			return fmt.Errorf("invalid migration filename: %s", name)
		}

		v, err := strconv.Atoi(m[1])
		if err != nil {
			return fmt.Errorf("parse migration version %s: %w", name, err)
		}

		migs = append(migs, mig{
			version: v,
			path:    filepath.ToSlash(filepath.Join("migrations", name)),
		})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	for _, m := range migs {
		if m.version <= current {
			continue
		}

		sqlBytes, err := migrationFS.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.path, err)
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.path, err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO module_schema_version (module_name, version, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (module_name) DO UPDATE
			SET version = EXCLUDED.version,
			    updated_at = NOW()`,
			"pgindex", m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update module schema version: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		current = m.version
	}

	return nil
}

func ensureModuleVersionTable(ctx context.Context, db *sql.DB) error {
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

func currentVersion(ctx context.Context, db *sql.DB, module string) (int, error) {
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
