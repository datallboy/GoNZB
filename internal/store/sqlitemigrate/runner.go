package sqlitemigrate

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

var migrationNameRE = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

func RunModuleMigrations(ctx context.Context, db *sql.DB, moduleName string, fsys embed.FS, dir string) error {
	if err := ensureModuleVersionTable(ctx, db); err != nil {
		return err
	}

	current, err := currentVersion(ctx, db, moduleName)
	if err != nil {
		return err
	}

	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
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
		migs = append(migs, mig{version: v, path: filepath.ToSlash(filepath.Join(dir, name))})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	for _, m := range migs {
		if m.version <= current {
			continue
		}

		sqlBytes, err := fsys.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.path, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.path, err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO module_schema_version (module_name, version)
			VALUES (?, ?)
			ON CONFLICT(module_name) DO UPDATE SET
				version = excluded.version,
				updated_at = CURRENT_TIMESTAMP`, moduleName, m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update module version %s: %w", moduleName, err)
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
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
	return err
}

func currentVersion(ctx context.Context, db *sql.DB, moduleName string) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, `
		SELECT version FROM module_schema_version WHERE module_name = ?`, moduleName).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}
