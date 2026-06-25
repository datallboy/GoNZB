package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func syncReleaseCatalogFiles(ctx context.Context, tx *sql.Tx, releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM release_catalog_files WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("clear release catalog files %s: %w", releaseID, err)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO release_catalog_files (
			release_id,
			file_name,
			size_bytes,
			file_index,
			is_pars,
			subject,
			poster,
			posted_at,
			article_count,
			total_parts,
			observed_parts,
			match_confidence,
			match_status,
			updated_at
		)
		SELECT
			rf.release_id,
			rf.file_name,
			rf.size_bytes,
			rf.file_index,
			rf.is_pars,
			COALESCE(rf.subject, ''),
			COALESCE(NULLIF(rf.poster, ''), raw_meta.poster, ''),
			COALESCE(rf.posted_at, raw_meta.posted_at),
			COUNT(bp.id)::integer AS article_count,
			COALESCE(MAX(bos.total_parts), 0)::integer,
			COALESCE(MAX(bos.observed_parts), 0)::integer,
			COALESCE(MAX(bic.match_confidence), 0),
			COALESCE(MAX(bic.match_status), ''),
			NOW()
		FROM release_files rf
		LEFT JOIN binary_core bc ON bc.binary_id = rf.binary_id
		LEFT JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = rf.binary_id
		LEFT JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = rf.binary_id
		LEFT JOIN binary_parts bp
		  ON bp.source_posted_at = bc.source_posted_at
		 AND bp.binary_id = rf.binary_id
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(NULLIF(p.poster_name, ''), NULLIF(aip.poster, '')) AS poster,
				MIN(ah.date_utc) AS posted_at
			FROM binary_parts rbp
			JOIN article_headers ah
			  ON ah.source_posted_at = rbp.source_posted_at
			 AND ah.id = rbp.article_header_id
			LEFT JOIN article_header_poster_refs apr
			  ON apr.source_posted_at = ah.source_posted_at
			 AND apr.article_header_id = ah.id
			LEFT JOIN posters p ON p.id = apr.poster_id
			LEFT JOIN article_header_ingest_payloads aip
			  ON aip.source_posted_at = ah.source_posted_at
			 AND aip.article_header_id = ah.id
			WHERE rbp.source_posted_at = bc.source_posted_at
			  AND rbp.binary_id = rf.binary_id
			GROUP BY COALESCE(NULLIF(p.poster_name, ''), NULLIF(aip.poster, ''))
			ORDER BY COUNT(*) DESC, COALESCE(NULLIF(p.poster_name, ''), NULLIF(aip.poster, ''))
			LIMIT 1
		) raw_meta ON TRUE
		WHERE rf.release_id = $1
		GROUP BY rf.release_id, rf.file_name, rf.size_bytes, rf.file_index, rf.is_pars, rf.subject, rf.poster, rf.posted_at, raw_meta.poster, raw_meta.posted_at`,
		releaseID,
	)
	if err != nil {
		return fmt.Errorf("seed release catalog files from release files %s: %w", releaseID, err)
	}
	if _, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("read release catalog file sync row count %s: %w", releaseID, rowsErr)
	}

	return nil
}

func (s *Store) BackfillMissingReleaseCatalogFiles(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin release catalog files backfill tx: %w", err)
	}
	defer rollbackTx(tx)

	rows, err := tx.QueryContext(ctx, `
		SELECT r.release_id
		FROM releases r
		LEFT JOIN release_catalog_files cf ON cf.release_id = r.release_id
		WHERE cf.release_id IS NULL
		  AND EXISTS (SELECT 1 FROM release_files rf WHERE rf.release_id = r.release_id)
		ORDER BY r.created_at ASC, r.release_id
		LIMIT $1
		FOR UPDATE OF r`, limit)
	if err != nil {
		return 0, fmt.Errorf("list missing release catalog files: %w", err)
	}
	defer rows.Close()

	releaseIDs := make([]string, 0, limit)
	for rows.Next() {
		var releaseID string
		if err := rows.Scan(&releaseID); err != nil {
			return 0, fmt.Errorf("scan missing release catalog file release id: %w", err)
		}
		releaseIDs = append(releaseIDs, releaseID)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate missing release catalog files: %w", err)
	}

	for _, releaseID := range releaseIDs {
		if err := syncReleaseCatalogFiles(ctx, tx, releaseID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit release catalog files backfill tx: %w", err)
	}
	return int64(len(releaseIDs)), nil
}
