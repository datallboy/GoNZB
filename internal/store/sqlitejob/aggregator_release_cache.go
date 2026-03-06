package sqlitejob

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

// UpsertAggregatorReleaseCache stores/replaces release cache entries for Aggregator queries.
func (s *Store) UpsertAggregatorReleaseCache(ctx context.Context, releases []*domain.Release) error {
	if len(releases) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, rel := range releases {
		if rel == nil || rel.ID == "" {
			continue
		}

		publishUnix := int64(0)
		if !rel.PublishDate.IsZero() {
			publishUnix = rel.PublishDate.Unix()
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO aggregator_release_cache (
				release_id, title, size_bytes, source, category, guid, publish_date_unix, nzb_cached, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(release_id) DO UPDATE SET
				title = excluded.title,
				size_bytes = excluded.size_bytes,
				source = excluded.source,
				category = excluded.category,
				guid = excluded.guid,
				publish_date_unix = excluded.publish_date_unix,
				nzb_cached = CASE
					WHEN excluded.nzb_cached THEN 1
					ELSE aggregator_release_cache.nzb_cached
				END,
				updated_at = CURRENT_TIMESTAMP`,
			rel.ID,
			rel.Title,
			rel.Size,
			rel.Source,
			rel.Category,
			rel.GUID,
			publishUnix,
			rel.CachePresent,
		)
		if err != nil {
			return fmt.Errorf("upsert aggregator_release_cache for %s: %w", rel.ID, err)
		}
	}

	return tx.Commit()
}

// SearchAggregatorReleaseCache searches cached releases by title.
func (s *Store) SearchAggregatorReleaseCache(ctx context.Context, query string, limit int) ([]*domain.Release, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			release_id,
			title,
			size_bytes,
			source,
			category,
			guid,
			publish_date_unix,
			nzb_cached
		FROM aggregator_release_cache
		WHERE title LIKE ?
		ORDER BY publish_date_unix DESC, updated_at DESC
		LIMIT ?`,
		"%"+query+"%",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query aggregator_release_cache: %w", err)
	}
	defer rows.Close()

	results := make([]*domain.Release, 0)
	for rows.Next() {
		rel, scanErr := scanAggregatorReleaseCacheRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregator_release_cache rows: %w", err)
	}

	return results, nil
}

// GetAggregatorReleaseCacheByID returns one cached release by release id.
func (s *Store) GetAggregatorReleaseCacheByID(ctx context.Context, id string) (*domain.Release, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			release_id,
			title,
			size_bytes,
			source,
			category,
			guid,
			publish_date_unix,
			nzb_cached
		FROM aggregator_release_cache
		WHERE release_id = ?
		LIMIT 1`, id)

	rel, err := scanAggregatorReleaseCacheRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return rel, nil
}

type aggregatorReleaseCacheRowScanner interface {
	Scan(dest ...any) error
}

func scanAggregatorReleaseCacheRow(scanner aggregatorReleaseCacheRowScanner) (*domain.Release, error) {
	var (
		releaseID       string
		title           string
		sizeBytes       int64
		source          string
		category        string
		guid            string
		publishDateUnix int64
		nzbCached       bool
	)

	if err := scanner.Scan(
		&releaseID,
		&title,
		&sizeBytes,
		&source,
		&category,
		&guid,
		&publishDateUnix,
		&nzbCached,
	); err != nil {
		return nil, fmt.Errorf("scan aggregator_release_cache row: %w", err)
	}

	rel := &domain.Release{
		ID:           releaseID,
		Title:        title,
		Size:         sizeBytes,
		Source:       source,
		Category:     category,
		GUID:         guid,
		CachePresent: nzbCached,
	}

	if publishDateUnix > 0 {
		rel.PublishDate = time.Unix(publishDateUnix, 0).UTC()
	}

	return rel, nil
}
