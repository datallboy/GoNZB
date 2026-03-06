package sqlitejob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Store) MarkReleaseCached(ctx context.Context, releaseID string, blobSize int64, blobMtimeUnix int64) error {
	now := time.Now().Unix()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO blob_cache_index (
			key, size_bytes, mtime_unix, last_verified_unix, last_error
		) VALUES (?, ?, ?, ?, '')
		ON CONFLICT(key) DO UPDATE SET
			size_bytes = excluded.size_bytes,
			mtime_unix = excluded.mtime_unix,
			last_verified_unix = excluded.last_verified_unix,
			last_error = ''`,
		releaseID, blobSize, blobMtimeUnix, now,
	)
	if err != nil {
		return fmt.Errorf("upsert blob_cache_index cached row: %w", err)
	}

	return nil
}

func (s *Store) MarkReleaseCacheMissing(ctx context.Context, releaseID, reason string) error {
	now := time.Now().Unix()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO blob_cache_index (
			key, size_bytes, mtime_unix, last_verified_unix, last_error
		) VALUES (?, 0, 0, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			size_bytes = 0,
			mtime_unix = 0,
			last_verified_unix = excluded.last_verified_unix,
			last_error = excluded.last_error`,
		releaseID, now, reason,
	)
	if err != nil {
		return fmt.Errorf("upsert blob_cache_index missing row: %w", err)
	}

	return nil
}

func (s *Store) ReconcileBlobCacheIndex(ctx context.Context) error {
	pattern := filepath.Join(s.blobDir, "*.nzb")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to scan blob cache dir: %w", err)
	}

	present := make(map[string]struct{}, len(matches))
	for _, p := range matches {
		base := filepath.Base(p)
		id := strings.TrimSuffix(base, ".nzb")
		if id == "" {
			continue
		}

		info, statErr := os.Stat(p)
		if statErr != nil {
			continue
		}

		present[id] = struct{}{}
		if err := s.MarkReleaseCached(ctx, id, info.Size(), info.ModTime().Unix()); err != nil {
			return err
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT key FROM blob_cache_index`)
	if err != nil {
		return fmt.Errorf("query blob_cache_index keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return fmt.Errorf("scan blob_cache_index key: %w", err)
		}

		if _, ok := present[key]; !ok {
			if err := s.MarkReleaseCacheMissing(ctx, key, "missing blob on disk"); err != nil {
				return err
			}
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate blob_cache_index keys: %w", err)
	}

	return nil
}

func (s *Store) ReconcileReleaseCache(ctx context.Context) error {
	return s.ReconcileBlobCacheIndex(ctx)
}
