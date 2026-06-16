package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type DatabaseStorageStatus struct {
	DatabaseBytes         int64
	DataDirectory         string
	FilesystemFreeBytes   int64
	FilesystemTotalBytes  int64
	FilesystemFreePercent float64
	FilesystemVisible     bool
}

type DatabaseStorageGuardConfig struct {
	Enabled        bool
	MinFreeBytes   int64
	MinFreePercent float64
}

type DatabaseStorageGuardEvaluation struct {
	Blocked bool
	Reason  string
	Status  DatabaseStorageStatus
}

func (s *Store) DatabaseStorageStatus(ctx context.Context) (*DatabaseStorageStatus, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	status := &DatabaseStorageStatus{}
	if err := s.db.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&status.DatabaseBytes); err != nil {
		return nil, fmt.Errorf("read current database size: %w", err)
	}

	var isSuperuser bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT rolsuper
		FROM pg_roles
		WHERE rolname = current_user`,
	).Scan(&isSuperuser); err != nil {
		return nil, fmt.Errorf("check postgres storage guard role capability: %w", err)
	}
	if !isSuperuser {
		status.FilesystemVisible = false
		return status, nil
	}

	if err := s.db.QueryRowContext(ctx, `SELECT current_setting('data_directory')`).Scan(&status.DataDirectory); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("postgres data directory is unavailable")
		}
		return nil, fmt.Errorf("read postgres data directory: %w", err)
	}

	dataDir := filepath.Clean(status.DataDirectory)
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dataDir, &fs); err != nil {
		if os.IsNotExist(err) || err == syscall.ENOENT || err == syscall.EPERM || err == syscall.EACCES {
			status.DataDirectory = dataDir
			status.FilesystemVisible = false
			return status, nil
		}
		return nil, fmt.Errorf("stat postgres data directory %s: %w", dataDir, err)
	}
	blockSize := uint64(fs.Bsize)
	status.FilesystemFreeBytes = int64(fs.Bavail * blockSize)
	status.FilesystemTotalBytes = int64(fs.Blocks * blockSize)
	if status.FilesystemTotalBytes > 0 {
		status.FilesystemFreePercent = (float64(status.FilesystemFreeBytes) / float64(status.FilesystemTotalBytes)) * 100
	}
	status.DataDirectory = dataDir
	status.FilesystemVisible = true
	return status, nil
}

func EvaluateDatabaseStorageGuard(status DatabaseStorageStatus, cfg DatabaseStorageGuardConfig) DatabaseStorageGuardEvaluation {
	evaluation := DatabaseStorageGuardEvaluation{Status: status}
	if !cfg.Enabled {
		return evaluation
	}
	if !status.FilesystemVisible {
		return evaluation
	}

	if cfg.MinFreeBytes > 0 && status.FilesystemFreeBytes < cfg.MinFreeBytes {
		evaluation.Blocked = true
		evaluation.Reason = fmt.Sprintf(
			"postgres data directory low on space: free_bytes=%d threshold=%d free_percent=%.2f",
			status.FilesystemFreeBytes,
			cfg.MinFreeBytes,
			status.FilesystemFreePercent,
		)
		return evaluation
	}
	if cfg.MinFreePercent > 0 && status.FilesystemFreePercent < cfg.MinFreePercent {
		evaluation.Blocked = true
		evaluation.Reason = fmt.Sprintf(
			"postgres data directory low on space: free_percent=%.2f threshold=%.2f free_bytes=%d",
			status.FilesystemFreePercent,
			cfg.MinFreePercent,
			status.FilesystemFreeBytes,
		)
	}
	return evaluation
}
