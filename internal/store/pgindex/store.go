package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

// NewStore now opens PostgreSQL by DSN and runs migrations.
func NewStore(dsn string) (*Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("pg dsn is required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open pg connection: %w", err)
	}

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

	// run module migrations on startup.
	if err := s.RunMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run pgindex migrations: %w", err)
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
