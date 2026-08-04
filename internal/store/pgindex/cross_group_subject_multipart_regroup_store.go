package pgindex

import (
	"context"
	"fmt"
	"time"
)

type CrossGroupSubjectMultipartRegroupRequest struct {
	Limit             int
	TargetWindowStart *time.Time
	TargetWindowEnd   *time.Time
}

func (r CrossGroupSubjectMultipartRegroupRequest) HasTargetWindow() bool {
	return r.TargetWindowStart != nil &&
		r.TargetWindowEnd != nil &&
		r.TargetWindowStart.Before(*r.TargetWindowEnd)
}

type CrossGroupSubjectMultipartRegroupResult struct {
	Groups                int64   `json:"groups"`
	TargetBinaries        int64   `json:"target_binaries"`
	SourceBinaries        int64   `json:"source_binaries"`
	PartsMoved            int64   `json:"parts_moved"`
	DuplicatePartsDeleted int64   `json:"duplicate_parts_deleted"`
	TargetBinaryIDs       []int64 `json:"-"`
}

// RegroupCrossGroupSubjectMultipartBinaries combines a strongly identified
// multipart file whose segments were distributed across multiple newsgroups.
// It deliberately excludes weak identities, published binaries, old matches,
// and sets that do not gain any unique part numbers by being combined.
func (s *Store) RegroupCrossGroupSubjectMultipartBinaries(ctx context.Context, req CrossGroupSubjectMultipartRegroupRequest) (*CrossGroupSubjectMultipartRegroupResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	result := &CrossGroupSubjectMultipartRegroupResult{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin cross-group multipart regroup tx: %w", err)
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '30s'`); err != nil {
		return nil, fmt.Errorf("set cross-group multipart regroup statement timeout: %w", err)
	}

	candidateLimit := limit * 100
	if candidateLimit < 1000 {
		candidateLimit = 1000
	}
	if candidateLimit > 10_000 {
		candidateLimit = 10_000
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_cross_group_multipart_candidates ON COMMIT DROP AS
		SELECT
			bc.binary_id,
			bc.source_posted_at,
			bc.provider_id,
			bc.newsgroup_id,
			bc.binary_key,
			bic.release_family_key,
			lower(btrim(bic.file_name)) AS normalized_file_name,
			bic.file_name,
			bic.file_index,
			bic.expected_file_count,
			bos.observed_parts,
			bos.total_parts
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		JOIN binary_lifecycle bl
		  ON bl.source_posted_at = bc.source_posted_at
		 AND bl.binary_id = bc.binary_id
		WHERE (
		    (
		      NOT $2
		      AND bc.source_posted_at >= NOW() - INTERVAL '7 days'
		    )
		    OR (
		      $2
		      AND bc.source_posted_at >= $3
		      AND bc.source_posted_at < $4
		    )
		  )
		  AND (
		    (
		      bic.family_kind = 'subject_multipart_obfuscated'
		      AND bic.identity_reason = 'subject_multipart_obfuscated'
		    )
		    OR (
		      bic.family_kind = 'readable_title'
		      AND bic.identity_reason = 'readable_archive_filename'
		      AND bic.identity_strength = 'strong'
		    )
		  )
		  AND bic.identity_strength IN ('probable', 'strong')
		  AND btrim(COALESCE(bic.release_family_key, '')) <> ''
		  AND btrim(COALESCE(bic.file_name, '')) <> ''
		  AND bic.file_index > 0
		  AND bic.expected_file_count > 0
		  AND bos.total_parts > 1
		  AND bos.observed_parts < bos.total_parts
		  AND bl.lifecycle_status = 'active'
		  AND btrim(COALESCE(bl.release_id, '')) = ''
		  AND NOT EXISTS (
		    SELECT 1
		    FROM binary_superseded_sources bss
		    WHERE bss.source_posted_at = bc.source_posted_at
		      AND bss.source_binary_id = bc.binary_id
		  )
		ORDER BY bc.source_posted_at DESC, bc.binary_id DESC
		LIMIT $1`,
		candidateLimit,
		req.HasTargetWindow(),
		req.TargetWindowStart,
		req.TargetWindowEnd,
	); err != nil {
		return nil, fmt.Errorf("stage cross-group multipart candidates: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE UNIQUE INDEX tmp_cross_group_multipart_candidates_id_idx
		ON tmp_cross_group_multipart_candidates (binary_id)`); err != nil {
		return nil, fmt.Errorf("index cross-group multipart candidates: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_cross_group_multipart_groups ON COMMIT DROP AS
		WITH grouped AS (
			SELECT
				c.provider_id,
				c.release_family_key,
				c.normalized_file_name,
				MIN(c.file_name) AS file_name,
				c.file_index,
				c.expected_file_count,
				c.total_parts,
				MIN(c.newsgroup_id) AS anchor_newsgroup_id,
				COUNT(DISTINCT c.newsgroup_id) AS newsgroup_count,
				COUNT(DISTINCT c.binary_id) AS binary_count,
				COUNT(DISTINCT bp.part_number) AS combined_parts,
				MAX(c.observed_parts) AS largest_source_parts,
				MIN(c.source_posted_at) AS first_posted_at,
				MAX(c.source_posted_at) AS last_posted_at
			FROM tmp_cross_group_multipart_candidates c
			JOIN binary_parts bp ON bp.binary_id = c.binary_id
			GROUP BY
				c.provider_id,
				c.release_family_key,
				c.normalized_file_name,
				c.file_index,
				c.expected_file_count,
				c.total_parts
			HAVING COUNT(DISTINCT c.newsgroup_id) > 1
			   AND COUNT(DISTINCT c.binary_id) > 1
			   AND COUNT(DISTINCT bp.part_number) > MAX(c.observed_parts)
			   AND MAX(c.source_posted_at) - MIN(c.source_posted_at) <= INTERVAL '6 hours'
			ORDER BY COUNT(DISTINCT bp.part_number) DESC
			LIMIT $1
		)
		SELECT
			g.provider_id,
			g.release_family_key,
			g.normalized_file_name,
			g.file_name,
			g.file_index,
			g.expected_file_count,
			g.total_parts,
			target.newsgroup_id,
			target.binary_id AS target_binary_id,
			target.source_posted_at AS target_source_posted_at,
			target.binary_key AS target_binary_key,
			g.newsgroup_count,
			g.binary_count,
			g.combined_parts
		FROM grouped g
		JOIN LATERAL (
			SELECT
				c.binary_id,
				c.source_posted_at,
				c.newsgroup_id,
				c.binary_key
			FROM tmp_cross_group_multipart_candidates c
			WHERE c.provider_id = g.provider_id
			  AND c.release_family_key = g.release_family_key
			  AND c.normalized_file_name = g.normalized_file_name
			  AND c.file_index = g.file_index
			  AND c.expected_file_count = g.expected_file_count
			  AND c.total_parts = g.total_parts
			ORDER BY
				(c.newsgroup_id = g.anchor_newsgroup_id) DESC,
				c.binary_id
			LIMIT 1
		) target ON true`, limit); err != nil {
		return nil, fmt.Errorf("stage cross-group multipart groups: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tmp_cross_group_multipart_groups`).Scan(&result.Groups); err != nil {
		return nil, fmt.Errorf("count cross-group multipart groups: %w", err)
	}
	if result.Groups == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty cross-group multipart regroup tx: %w", err)
		}
		return result, nil
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_cross_group_multipart_sources ON COMMIT DROP AS
		SELECT
			c.binary_id AS source_binary_id,
			c.source_posted_at,
			c.provider_id,
			c.newsgroup_id,
			c.binary_key AS source_binary_key,
			c.release_family_key,
			g.target_binary_id,
			g.target_source_posted_at,
			g.target_binary_key,
			g.file_name,
			g.total_parts
		FROM tmp_cross_group_multipart_groups g
		JOIN tmp_cross_group_multipart_candidates c
		  ON c.provider_id = g.provider_id
		 AND c.release_family_key = g.release_family_key
		 AND c.normalized_file_name = g.normalized_file_name
		 AND c.file_index = g.file_index
		 AND c.expected_file_count = g.expected_file_count
		 AND c.total_parts = g.total_parts
		WHERE c.binary_id <> g.target_binary_id`); err != nil {
		return nil, fmt.Errorf("stage cross-group multipart sources: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tmp_cross_group_multipart_sources`).Scan(&result.SourceBinaries); err != nil {
		return nil, fmt.Errorf("count cross-group multipart sources: %w", err)
	}
	if result.SourceBinaries == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit cross-group multipart regroup without sources: %w", err)
		}
		return result, nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT target_binary_id
		FROM tmp_cross_group_multipart_groups
		ORDER BY target_binary_id`)
	if err != nil {
		return nil, fmt.Errorf("load cross-group multipart target ids: %w", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan cross-group multipart target id: %w", err)
		}
		result.TargetBinaryIDs = append(result.TargetBinaryIDs, id)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close cross-group multipart target ids: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cross-group multipart target ids: %w", err)
	}
	result.TargetBinaries = int64(len(result.TargetBinaryIDs))

	lockRows, err := tx.QueryContext(ctx, `
		WITH ids AS (
			SELECT target_binary_id AS binary_id FROM tmp_cross_group_multipart_groups
			UNION
			SELECT source_binary_id AS binary_id FROM tmp_cross_group_multipart_sources
		)
		SELECT bc.binary_id
		FROM binary_core bc
		JOIN ids ON ids.binary_id = bc.binary_id
		ORDER BY bc.binary_id
		FOR UPDATE OF bc`)
	if err != nil {
		return nil, fmt.Errorf("lock cross-group multipart binaries: %w", err)
	}
	for lockRows.Next() {
		var id int64
		if err := lockRows.Scan(&id); err != nil {
			lockRows.Close()
			return nil, fmt.Errorf("scan cross-group multipart binary lock: %w", err)
		}
	}
	if err := lockRows.Close(); err != nil {
		return nil, fmt.Errorf("close cross-group multipart binary locks: %w", err)
	}
	if err := lockRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cross-group multipart binary locks: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_cross_group_multipart_part_winners ON COMMIT DROP AS
		SELECT
			source_posted_at,
			id,
			binary_id,
			target_binary_id,
			part_number,
			ROW_NUMBER() OVER (
				PARTITION BY target_binary_id, part_number
				ORDER BY segment_bytes DESC, source_posted_at, id
			) AS keep_rank
		FROM (
			SELECT
				bp.source_posted_at,
				bp.id,
				bp.binary_id,
				s.target_binary_id,
				bp.part_number,
				bp.segment_bytes
			FROM binary_parts bp
			JOIN tmp_cross_group_multipart_sources s
			  ON s.source_binary_id = bp.binary_id
			UNION ALL
			SELECT
				bp.source_posted_at,
				bp.id,
				bp.binary_id,
				g.target_binary_id,
				bp.part_number,
				bp.segment_bytes
			FROM binary_parts bp
			JOIN tmp_cross_group_multipart_groups g
			  ON g.target_binary_id = bp.binary_id
		) all_parts`); err != nil {
		return nil, fmt.Errorf("stage cross-group multipart part winners: %w", err)
	}
	duplicateDelete, err := tx.ExecContext(ctx, `
		DELETE FROM binary_parts bp
		USING tmp_cross_group_multipart_part_winners w
		WHERE bp.source_posted_at = w.source_posted_at
		  AND bp.id = w.id
		  AND w.keep_rank > 1`)
	if err != nil {
		return nil, fmt.Errorf("delete duplicate cross-group multipart parts: %w", err)
	}
	result.DuplicatePartsDeleted = rowsAffected(duplicateDelete)

	moveResult, err := tx.ExecContext(ctx, `
		UPDATE binary_parts bp
		SET binary_id = s.target_binary_id,
		    file_name = s.file_name,
		    total_parts = GREATEST(bp.total_parts, s.total_parts),
		    updated_at = NOW()
		FROM tmp_cross_group_multipart_sources s
		WHERE bp.binary_id = s.source_binary_id`)
	if err != nil {
		return nil, fmt.Errorf("move cross-group multipart parts: %w", err)
	}
	result.PartsMoved = rowsAffected(moveResult)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binary_superseded_sources (
			source_binary_id,
			source_posted_at,
			target_binary_id,
			provider_id,
			newsgroup_id,
			release_family_key,
			source_binary_key,
			target_binary_key,
			superseded_reason,
			superseded_at
		)
		SELECT
			s.source_binary_id,
			s.source_posted_at,
			s.target_binary_id,
			s.provider_id,
			s.newsgroup_id,
			s.release_family_key,
			s.source_binary_key,
			s.target_binary_key,
			'cross_group_subject_multipart_regroup',
			NOW()
		FROM tmp_cross_group_multipart_sources s
		ON CONFLICT (source_posted_at, source_binary_id) DO UPDATE
		SET target_binary_id = EXCLUDED.target_binary_id,
		    provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    release_family_key = EXCLUDED.release_family_key,
		    source_binary_key = EXCLUDED.source_binary_key,
		    target_binary_key = EXCLUDED.target_binary_key,
		    superseded_reason = EXCLUDED.superseded_reason,
		    superseded_at = EXCLUDED.superseded_at,
		    purged_at = NULL`); err != nil {
		return nil, fmt.Errorf("record cross-group multipart superseded sources: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binary_lifecycle (
			binary_id,
			source_posted_at,
			provider_id,
			newsgroup_id,
			lifecycle_status,
			updated_at
		)
		SELECT
			s.source_binary_id,
			s.source_posted_at,
			s.provider_id,
			s.newsgroup_id,
			'superseded',
			NOW()
		FROM tmp_cross_group_multipart_sources s
		ON CONFLICT (source_posted_at, binary_id) DO UPDATE
		SET lifecycle_status = 'superseded',
		    updated_at = NOW()`); err != nil {
		return nil, fmt.Errorf("mark cross-group multipart source binaries superseded: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binary_lifecycle (
			binary_id,
			source_posted_at,
			provider_id,
			newsgroup_id,
			lifecycle_status,
			updated_at
		)
		SELECT
			g.target_binary_id,
			g.target_source_posted_at,
			g.provider_id,
			g.newsgroup_id,
			'active',
			NOW()
		FROM tmp_cross_group_multipart_groups g
		ON CONFLICT (source_posted_at, binary_id) DO UPDATE
		SET lifecycle_status = 'active',
		    updated_at = NOW()`); err != nil {
		return nil, fmt.Errorf("mark cross-group multipart target binaries active: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO release_family_summary_refresh_queue (
			provider_id,
			newsgroup_id,
			key_kind,
			family_key,
			queued_at
		)
		SELECT DISTINCT
			g.provider_id,
			g.newsgroup_id,
			'release_family',
			g.release_family_key,
			NOW()
		FROM tmp_cross_group_multipart_groups g
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET queued_at = EXCLUDED.queued_at`); err != nil {
		return nil, fmt.Errorf("queue cross-group multipart release summaries: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cross-group multipart regroup tx: %w", err)
	}
	return result, nil
}
