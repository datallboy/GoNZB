package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *Store) ResetReleaseInspectionState(ctx context.Context, releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release inspection reset tx: %w", err)
	}
	defer rollbackTx(tx)

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM releases WHERE release_id = $1)`, releaseID).Scan(&exists); err != nil {
		return fmt.Errorf("check release inspection reset target %s: %w", releaseID, err)
	}
	if !exists {
		return sql.ErrNoRows
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_inspections
		WHERE release_id = $1
		   OR binary_id IN (
				SELECT rf.binary_id
				FROM release_files rf
				WHERE rf.release_id = $1
		   )`, releaseID); err != nil {
		return fmt.Errorf("delete binary inspections for %s: %w", releaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE releases
		SET updated_at = NOW()
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("touch release inspection reset target %s: %w", releaseID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release inspection reset %s: %w", releaseID, err)
	}
	return nil
}

func (s *Store) ResetReleaseEnrichmentState(ctx context.Context, releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release enrichment reset tx: %w", err)
	}
	defer rollbackTx(tx)

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM releases WHERE release_id = $1)`, releaseID).Scan(&exists); err != nil {
		return fmt.Errorf("check release enrichment reset target %s: %w", releaseID, err)
	}
	if !exists {
		return sql.ErrNoRows
	}

	for _, tableName := range []string{"release_predb_matches", "release_tmdb_matches", "release_tvdb_matches"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE release_id = $1`, releaseID); err != nil {
			return fmt.Errorf("delete %s rows for %s: %w", tableName, releaseID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE releases
		SET
			title_source = CASE
				WHEN title_source = '' THEN title_source
				ELSE 'source'
			END,
			title_confidence = 0,
			matched_media_title = '',
			original_media_title = '',
			tmdb_id = 0,
			tvdb_id = 0,
			external_media_type = '',
			external_year = 0,
			season_number = 0,
			episode_number = 0,
			season_episode_source = '',
			season_episode_confidence = 0,
			metadata_updated_at = NOW(),
			updated_at = NOW()
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("reset enrichment fields for %s: %w", releaseID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release enrichment reset %s: %w", releaseID, err)
	}
	return nil
}
