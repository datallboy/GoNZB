package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/datallboy/gonzb/internal/domain"
)

// UpsertReleases handles bulk search results or manual additions.
func (s *PersistentStore) UpsertReleases(ctx context.Context, results []*domain.Release) error {
	if len(results) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Reuse a single DBO instance for efficiency
	var dbo releaseDBO

	for _, rel := range results {
		// 1. Get/Create Poster ID
		var posterID sql.NullInt64
		if rel.Poster != "" {
			var id int64
			err := tx.QueryRowContext(ctx, `
				INSERT INTO posters (name) VALUES (?) 
				ON CONFLICT(name) DO UPDATE SET name=name 
				RETURNING id`, rel.Poster).Scan(&id)
			if err != nil {
				return fmt.Errorf("failed to upsert poster %s: %w", rel.Poster, err)
			}
			posterID = sql.NullInt64{Int64: id, Valid: true}
		}

		// 2. Map domain to the reusable DBO instance
		dbo.FromDomain(rel, posterID)

		// 3. Upsert into Releases
		_, err = tx.ExecContext(ctx, `
			INSERT INTO releases (
				id, file_hash, poster_id, title, size, password, 
				guid, source, download_url, publish_date, category, redirect_allowed
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				file_hash = CASE WHEN excluded.file_hash != '' THEN excluded.file_hash ELSE releases.file_hash END,
				poster_id = COALESCE(excluded.poster_id, releases.poster_id),
				size = CASE WHEN excluded.size > 0 THEN excluded.size ELSE releases.size END,
				category = COALESCE(excluded.category, releases.category)`,
			dbo.ID, dbo.FileHash, dbo.PosterID, dbo.Title, dbo.Size, dbo.Password,
			dbo.GUID, dbo.Source, dbo.DownloadURL, dbo.PublishDate, dbo.Category, dbo.RedirectAllowed,
		)
		if err != nil {
			return fmt.Errorf("failed to upsert release %s: %w", rel.ID, err)
		}
	}

	return tx.Commit()
}

// GetRelease fetches a single release
func (s *PersistentStore) GetRelease(ctx context.Context, id string) (*domain.Release, error) {
	query := `
		SELECT 
			r.id, r.file_hash, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed,
			p.name as poster_name
		FROM releases r
		LEFT JOIN posters p ON r.poster_id = p.id
		WHERE r.id = ? LIMIT 1`

	var dbo releaseDBO
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&dbo.ID, &dbo.FileHash, &dbo.Title, &dbo.Size, &dbo.Password, &dbo.GUID,
		&dbo.Source, &dbo.DownloadURL, &dbo.PublishDate, &dbo.Category, &dbo.RedirectAllowed,
		&dbo.PosterName,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return dbo.ToDomain(), nil
}

// UpdateReleaseHash is used when an indexer result finally downloads its NZB.
func (s *PersistentStore) UpdateReleaseHash(ctx context.Context, id string, hash string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE releases SET file_hash = ? WHERE id = ?", hash, id)
	return err
}

// GetReleaseByHash is used to check for duplicates before creating a new manual upload.
func (s *PersistentStore) GetReleaseByHash(ctx context.Context, hash string) (*domain.Release, error) {
	query := `
		SELECT 
			r.id, r.file_hash, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed,
			p.name as poster_name
		FROM releases r
		LEFT JOIN posters p ON r.poster_id = p.id
		WHERE r.file_hash = ? LIMIT 1`

	var dbo releaseDBO
	err := s.db.QueryRowContext(ctx, query, hash).Scan(
		&dbo.ID, &dbo.FileHash, &dbo.Title, &dbo.Size, &dbo.Password, &dbo.GUID,
		&dbo.Source, &dbo.DownloadURL, &dbo.PublishDate, &dbo.Category, &dbo.RedirectAllowed,
		&dbo.PosterName,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return dbo.ToDomain(), nil
}
