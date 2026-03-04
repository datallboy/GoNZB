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
	query := `
		INSERT INTO release_cache (
			release_id, present, blob_size, blob_mtime_unix, cached_at_unix, verified_at_unix, last_error
		) VALUES (?, 1, ?, ?, ?, ?, '')
		ON CONFLICT(release_id) DO UPDATE SET
			present = 1,
			blob_size = excluded.blob_size,
			blob_mtime_unix = excluded.blob_mtime_unix,
			cached_at_unix = CASE WHEN release_cache.cached_at_unix = 0 THEN excluded.cached_at_unix ELSE release_cache.cached_at_unix END,
			verified_at_unix = excluded.verified_at_unix,
			last_error = ''`

	_, err := s.db.ExecContext(ctx, query, releaseID, blobSize, blobMtimeUnix, now, now)
	return err
}

func (s *Store) MarkReleaseCacheMissing(ctx context.Context, releaseID, reason string) error {
	now := time.Now().Unix()
	query := `
		INSERT INTO release_cache (
			release_id, present, blob_size, blob_mtime_unix, cached_at_unix, verified_at_unix, last_error
		) VALUES (?, 0, 0, 0, 0, ?, ?)
		ON CONFLICT(release_id) DO UPDATE SET
			present = 0,
			verified_at_unix = excluded.verified_at_unix,
			last_error = excluded.last_error`

	_, err := s.db.ExecContext(ctx, query, releaseID, now, reason)
	return err
}

func (s *Store) ReconcileReleaseCache(ctx context.Context) error {
	pattern := filepath.Join(s.blobDir, "*.nzb")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to scan blob cache: %w", err)
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

	rows, err := s.db.QueryContext(ctx, "SELECT release_id FROM release_cache WHERE present = 1")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, ok := present[id]; !ok {
			if err := s.MarkReleaseCacheMissing(ctx, id, "missing blob on disk"); err != nil {
				return err
			}
		}
	}

	return nil
}
