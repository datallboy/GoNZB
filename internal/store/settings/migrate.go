package settings

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/datallboy/gonzb/internal/store/sqlitemigrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const preV080SettingsSchemaVersion = 8

func (s *Store) RunMigrations() error {
	ctx := context.Background()
	if err := rebasePreV080SettingsVersion(ctx, s.db); err != nil {
		return err
	}
	return sqlitemigrate.RunModuleMigrations(ctx, s.db, "settings", migrationFiles, "migrations")
}

// The v0.8 release replaced the original 001-008 chain with an equivalent
// baseline numbered 001. Preserve databases that completed the old chain by
// rebasing only the known legacy schema fingerprint before applying newer
// migrations. Unknown/newer schemas remain untouched and fail validation.
func rebasePreV080SettingsVersion(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS module_schema_version (
			module_name TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("ensure settings module version table: %w", err)
	}

	var version int
	err := db.QueryRowContext(ctx, `
		SELECT version FROM module_schema_version WHERE module_name = ?`, "settings").Scan(&version)
	if err == sql.ErrNoRows || version != preV080SettingsSchemaVersion {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read settings schema version for legacy rebase: %w", err)
	}

	var legacyFingerprint bool
	if err := db.QueryRowContext(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'settings_revision')
			AND EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'settings_nntp_servers')
			AND EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'auth_users')
			AND EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'settings_download')
			AND NOT EXISTS (SELECT 1 FROM pragma_table_info('settings_indexers') WHERE name = 'allow_private_addresses')
			AND NOT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'settings_download_clients')`,
	).Scan(&legacyFingerprint); err != nil {
		return fmt.Errorf("identify pre-v0.8 settings schema: %w", err)
	}
	if !legacyFingerprint {
		return nil
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE module_schema_version
		SET version = 1, updated_at = CURRENT_TIMESTAMP
		WHERE module_name = ? AND version = ?`, "settings", preV080SettingsSchemaVersion); err != nil {
		return fmt.Errorf("rebase pre-v0.8 settings schema version: %w", err)
	}
	return nil
}
