package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *Store) SetReleaseArchivePreview(ctx context.Context, releaseID, objectKey, contentType, sourceKind string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	objectKey = strings.TrimSpace(objectKey)
	contentType = strings.TrimSpace(contentType)
	sourceKind = strings.TrimSpace(sourceKind)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO release_archive_state (
			release_id,
			preview_object_key,
			preview_content_type,
			preview_source_kind,
			preview_updated_at,
			updated_at
		) VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (release_id) DO UPDATE
		SET preview_object_key = EXCLUDED.preview_object_key,
		    preview_content_type = EXCLUDED.preview_content_type,
		    preview_source_kind = EXCLUDED.preview_source_kind,
		    preview_updated_at = EXCLUDED.preview_updated_at,
		    updated_at = NOW()`,
		releaseID,
		objectKey,
		contentType,
		sourceKind,
	)
	if err != nil {
		return fmt.Errorf("set release archive preview %s: %w", releaseID, err)
	}
	return nil
}

func (s *Store) RefreshReleaseArchiveDetailSnapshot(ctx context.Context, releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release archive detail snapshot refresh tx: %w", err)
	}
	defer rollbackTx(tx)

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM release_archive_state
			WHERE release_id = $1
		)`, releaseID).Scan(&exists); err != nil {
		return fmt.Errorf("check release archive state %s: %w", releaseID, err)
	}
	if !exists {
		return tx.Commit()
	}
	if err := syncReleaseCatalogFiles(ctx, tx, releaseID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release archive detail snapshot refresh tx: %w", err)
	}
	return nil
}

func nullableTimeValue(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return v.UTC()
}

func execReleaseMutationAndRefreshArchiveSnapshot(ctx context.Context, db *sql.DB, releaseID string, mutation func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release mutation tx: %w", err)
	}
	defer rollbackTx(tx)

	if err := mutation(tx); err != nil {
		return err
	}
	if err := syncReleaseCatalogFiles(ctx, tx, releaseID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release mutation tx: %w", err)
	}
	return nil
}
