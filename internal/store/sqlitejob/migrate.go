package sqlitejob

import (
	"context"
	"embed"

	"github.com/datallboy/gonzb/internal/store/sqlitemigrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func (s *Store) RunMigrations() error {
	return sqlitemigrate.RunModuleMigrations(context.Background(), s.db, "sqlitejob", migrationFiles, "migrations")
}
