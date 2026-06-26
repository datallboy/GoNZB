package pgindex

import (
	"context"
	"database/sql"
	"fmt"
)

type SubjectMultipartRegroupResult struct {
	Groups                int64 `json:"groups"`
	TargetBinaries        int64 `json:"target_binaries"`
	SourceBinaries        int64 `json:"source_binaries"`
	PartsMoved            int64 `json:"parts_moved"`
	DuplicatePartsDeleted int64 `json:"duplicate_parts_deleted"`
}

// RegroupSubjectMultipartBinaries repairs binaries that were previously split
// by contextual fallback even though the NNTP Subject carried complete file and
// part counters. It is intentionally conservative and only touches binary-owned
// projection tables.
func (s *Store) RegroupSubjectMultipartBinaries(ctx context.Context, limit int) (*SubjectMultipartRegroupResult, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	result := &SubjectMultipartRegroupResult{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin subject multipart regroup tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_regroup_groups`); err != nil {
		return nil, fmt.Errorf("drop subject multipart groups temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_regroup_sources`); err != nil {
		return nil, fmt.Errorf("drop subject multipart sources temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_regroup_candidates`); err != nil {
		return nil, fmt.Errorf("drop subject multipart candidates temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_regroup_key_groups`); err != nil {
		return nil, fmt.Errorf("drop subject multipart key groups temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_regroup_source_binaries`); err != nil {
		return nil, fmt.Errorf("drop subject multipart source binaries temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_existing_targets`); err != nil {
		return nil, fmt.Errorf("drop subject multipart existing targets temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS tmp_subject_multipart_regroup_part_winners`); err != nil {
		return nil, fmt.Errorf("drop subject multipart part winners temp table: %w", err)
	}

	statsOnly, err := refreshStaleSubjectMultipartObservationStats(ctx, tx, limit)
	if err != nil {
		return nil, err
	}
	result.TargetBinaries += statsOnly

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_regroup_key_groups ON COMMIT DROP AS
		SELECT
			bc.provider_id,
			bc.newsgroup_id,
			bic.file_name,
			lower(btrim(bic.file_name)) AS normalized_lookup_file_name,
			COUNT(*) AS binary_count,
			MAX(COALESCE(bos.total_parts, 0)) AS max_total_parts,
			MAX(COALESCE(bic.expected_file_count, 0)) AS max_expected_file_count
		FROM binary_core bc
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = bc.source_posted_at
		 AND bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bc.source_posted_at
		 AND bos.binary_id = bc.binary_id
		LEFT JOIN binary_lifecycle bl
		  ON bl.binary_id = bc.binary_id
		 AND bl.source_posted_at = bc.source_posted_at
		WHERE COALESCE(bl.lifecycle_status, 'active') <> 'superseded'
		  AND bic.family_kind = 'contextual_obfuscated'
		  AND bic.identity_reason = 'contextual_fallback'
		  AND bos.observed_parts <= 2
		  AND btrim(COALESCE(bic.file_name, '')) <> ''
		  AND lower(bic.file_name) !~ '(\.7z\.[0-9]+|\.part[0-9]+\.rar|\.r[0-9]{2,3}|\.rar|\.par2|\.vol[0-9]+\+[0-9]+\.par2)$'
		GROUP BY bc.provider_id, bc.newsgroup_id, bic.file_name, lower(btrim(bic.file_name))
		HAVING COUNT(*) > 1
		ORDER BY COUNT(*) DESC, MAX(COALESCE(bos.total_parts, 0)) DESC
		LIMIT $1`, limit); err != nil {
		return nil, fmt.Errorf("stage subject multipart regroup key groups: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE INDEX tmp_subject_multipart_regroup_key_groups_lookup_idx
		ON tmp_subject_multipart_regroup_key_groups (provider_id, newsgroup_id, normalized_lookup_file_name)`); err != nil {
		return nil, fmt.Errorf("index subject multipart regroup key groups: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_regroup_source_binaries ON COMMIT DROP AS
		SELECT
			bc.binary_id,
			bc.source_posted_at AS binary_source_posted_at,
			bc.provider_id,
			bc.newsgroup_id,
			bc.binary_key,
			bic.release_family_key,
			bic.file_name AS identity_file_name
		FROM tmp_subject_multipart_regroup_key_groups kg
		JOIN binary_identity_current bic
		  ON bic.provider_id = kg.provider_id
		 AND bic.newsgroup_id = kg.newsgroup_id
		 AND lower(btrim(bic.file_name)) = kg.normalized_lookup_file_name
		JOIN binary_core bc
		  ON bc.source_posted_at = bic.source_posted_at
		 AND bc.binary_id = bic.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = bic.source_posted_at
		 AND bos.binary_id = bic.binary_id
		LEFT JOIN binary_lifecycle bl
		  ON bl.binary_id = bic.binary_id
		 AND bl.source_posted_at = bic.source_posted_at
		WHERE COALESCE(bl.lifecycle_status, 'active') <> 'superseded'
		  AND bic.family_kind = 'contextual_obfuscated'
		  AND bic.identity_reason = 'contextual_fallback'
		  AND bos.observed_parts <= 2`); err != nil {
		return nil, fmt.Errorf("stage subject multipart regroup source binaries: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE UNIQUE INDEX tmp_subject_multipart_regroup_source_binaries_idx
		ON tmp_subject_multipart_regroup_source_binaries (binary_id)`); err != nil {
		return nil, fmt.Errorf("index subject multipart regroup source binaries: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_regroup_candidates ON COMMIT DROP AS
		SELECT
			sb.binary_id,
			sb.binary_source_posted_at,
			sb.provider_id,
			sb.newsgroup_id,
			sb.binary_key,
			sb.release_family_key,
			p.subject_file_name AS file_name,
			btrim(regexp_replace(lower(p.subject_file_name), '[^a-z0-9]+', ' ', 'g')) AS normalized_file_name,
			btrim(regexp_replace(regexp_replace(lower(p.subject_file_name), '\.[^.]+$', ''), '[^a-z0-9]+', ' ', 'g')) AS base_stem,
			p.subject_file_total AS expected_file_count,
			p.yenc_total_parts AS total_parts,
			p.yenc_part_number AS part_number,
			bp.segment_bytes
		FROM tmp_subject_multipart_regroup_source_binaries sb
		JOIN binary_parts bp
		  ON bp.binary_id = sb.binary_id
		JOIN article_header_ingest_payloads p
		  ON p.article_header_id = bp.article_header_id
		 AND p.source_posted_at = bp.source_posted_at
		WHERE btrim(p.subject_file_name) <> ''
		  AND lower(btrim(p.subject_file_name)) = lower(btrim(sb.identity_file_name))
		  AND p.subject_file_index > 0
		  AND p.subject_file_total > 0
		  AND p.yenc_part_number > 0
		  AND p.yenc_total_parts > 1
		  AND lower(p.subject_file_name) !~ '(\.7z\.[0-9]+|\.part[0-9]+\.rar|\.r[0-9]{2,3}|\.rar|\.par2|\.vol[0-9]+\+[0-9]+\.par2)$'`); err != nil {
		return nil, fmt.Errorf("stage subject multipart regroup candidates: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE INDEX tmp_subject_multipart_regroup_candidates_group_idx
		ON tmp_subject_multipart_regroup_candidates (provider_id, newsgroup_id, file_name, expected_file_count, total_parts)`); err != nil {
		return nil, fmt.Errorf("index subject multipart regroup candidates: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_regroup_groups ON COMMIT DROP AS
		WITH
		grouped AS (
			SELECT
				provider_id,
				newsgroup_id,
				file_name,
				normalized_file_name,
				base_stem,
				base_stem || '::' || normalized_file_name AS canonical_binary_key,
				expected_file_count,
				total_parts,
				MIN(binary_source_posted_at) AS min_source_posted_at,
				MIN(binary_id) AS fallback_target_binary_id,
				COUNT(DISTINCT binary_id) AS source_binary_count,
				COUNT(DISTINCT part_number) AS distinct_part_count,
				COUNT(*) AS part_row_count,
				SUM(segment_bytes) AS total_segment_bytes
			FROM tmp_subject_multipart_regroup_candidates
			WHERE normalized_file_name <> ''
			  AND base_stem <> ''
			GROUP BY provider_id, newsgroup_id, file_name, normalized_file_name, base_stem, expected_file_count, total_parts
			HAVING COUNT(DISTINCT binary_id) > 1
			   AND COUNT(DISTINCT part_number) > 1
		),
		limited AS (
			SELECT *
			FROM grouped
			ORDER BY part_row_count DESC, total_segment_bytes DESC, fallback_target_binary_id
			LIMIT $1
		)
		SELECT
			l.provider_id,
			l.newsgroup_id,
			l.file_name,
			l.normalized_file_name,
			l.base_stem,
			l.canonical_binary_key,
			l.expected_file_count,
			l.total_parts,
			COALESCE(canon.binary_id, l.fallback_target_binary_id) AS target_binary_id,
			COALESCE(canon.source_posted_at, fallback.source_posted_at, l.min_source_posted_at) AS target_source_posted_at,
			l.source_binary_count,
			l.distinct_part_count
		FROM limited l
		LEFT JOIN binary_core fallback
		  ON fallback.binary_id = l.fallback_target_binary_id
		LEFT JOIN LATERAL (
			SELECT bc.binary_id, COALESCE(bc.source_posted_at, l.min_source_posted_at) AS source_posted_at
			FROM binary_core bc
			LEFT JOIN binary_lifecycle bl
			  ON bl.binary_id = bc.binary_id
			 AND bl.source_posted_at = COALESCE(bc.source_posted_at, l.min_source_posted_at)
			WHERE bc.provider_id = l.provider_id
			  AND bc.newsgroup_id = l.newsgroup_id
			  AND bc.binary_key = l.canonical_binary_key
			  AND COALESCE(bl.lifecycle_status, 'active') <> 'superseded'
			ORDER BY bc.binary_id
			LIMIT 1
		) canon ON true`, limit); err != nil {
		return nil, fmt.Errorf("stage subject multipart regroup groups: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_existing_targets ON COMMIT DROP AS
		WITH target_counts AS (
			SELECT
				bss.target_binary_id,
				COUNT(DISTINCT bss.source_binary_id) AS source_binary_count
			FROM binary_superseded_sources bss
			WHERE bss.superseded_reason = 'subject_multipart_regroup'
			GROUP BY bss.target_binary_id
			ORDER BY COUNT(DISTINCT bss.source_binary_id) DESC, bss.target_binary_id
			LIMIT $1
		)
		SELECT DISTINCT
			tc.target_binary_id,
			target_bc.source_posted_at AS target_source_posted_at,
			tc.source_binary_count,
			COALESCE(part_counts.distinct_part_count, 0) AS distinct_part_count
		FROM target_counts tc
		JOIN binary_core target_bc
		  ON target_bc.binary_id = tc.target_binary_id
		JOIN binary_identity_current target_bic
		  ON target_bic.source_posted_at = target_bc.source_posted_at
		 AND target_bic.binary_id = target_bc.binary_id
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT bp.part_number) AS distinct_part_count
			FROM binary_parts bp
			WHERE bp.binary_id = tc.target_binary_id
		) part_counts ON true
		WHERE target_bic.identity_reason <> 'subject_multipart_obfuscated'
		  AND COALESCE(part_counts.distinct_part_count, 0) > 1`, limit); err != nil {
		return nil, fmt.Errorf("stage existing subject multipart regroup targets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE UNIQUE INDEX tmp_subject_multipart_existing_targets_idx
		ON tmp_subject_multipart_existing_targets (target_binary_id)`); err != nil {
		return nil, fmt.Errorf("index existing subject multipart regroup targets: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_subject_multipart_regroup_groups (
			provider_id,
			newsgroup_id,
			file_name,
			normalized_file_name,
			base_stem,
			canonical_binary_key,
			expected_file_count,
			total_parts,
			target_binary_id,
			target_source_posted_at,
			source_binary_count,
			distinct_part_count
		)
		SELECT
			bss.provider_id,
			bss.newsgroup_id,
			MIN(src_bic.file_name) AS file_name,
			btrim(regexp_replace(lower(MIN(src_bic.file_name)), '[^a-z0-9]+', ' ', 'g')) AS normalized_file_name,
			btrim(regexp_replace(regexp_replace(lower(MIN(src_bic.file_name)), '\.[^.]+$', ''), '[^a-z0-9]+', ' ', 'g')) AS base_stem,
			COALESCE(NULLIF(MAX(bss.target_binary_key), ''), btrim(regexp_replace(regexp_replace(lower(MIN(src_bic.file_name)), '\.[^.]+$', ''), '[^a-z0-9]+', ' ', 'g')) || '::' || btrim(regexp_replace(lower(MIN(src_bic.file_name)), '[^a-z0-9]+', ' ', 'g'))) AS canonical_binary_key,
			MAX(GREATEST(COALESCE(src_bic.expected_file_count, 0), 1)) AS expected_file_count,
			MAX(GREATEST(COALESCE(src_bos.total_parts, 0), 1)) AS total_parts,
			bss.target_binary_id,
			et.target_source_posted_at,
			MAX(et.source_binary_count) AS source_binary_count,
			MAX(et.distinct_part_count) AS distinct_part_count
		FROM tmp_subject_multipart_existing_targets et
		JOIN binary_superseded_sources bss
		  ON bss.target_binary_id = et.target_binary_id
		 AND bss.superseded_reason = 'subject_multipart_regroup'
		JOIN binary_identity_current src_bic
		  ON src_bic.source_posted_at = bss.source_posted_at
		 AND src_bic.binary_id = bss.source_binary_id
		LEFT JOIN binary_observation_stats src_bos
		  ON src_bos.source_posted_at = src_bic.source_posted_at
		 AND src_bos.binary_id = src_bic.binary_id
		WHERE btrim(COALESCE(src_bic.file_name, '')) <> ''
		GROUP BY bss.provider_id, bss.newsgroup_id, bss.target_binary_id, et.target_source_posted_at
		HAVING COUNT(DISTINCT bss.source_binary_id) > 0
		   AND MAX(et.distinct_part_count) > 1
		LIMIT $1`, limit); err != nil {
		return nil, fmt.Errorf("stage existing subject multipart regroup groups: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tmp_subject_multipart_regroup_groups`).Scan(&result.Groups); err != nil {
		return nil, fmt.Errorf("count subject multipart regroup groups: %w", err)
	}
	if result.Groups == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty subject multipart regroup tx: %w", err)
		}
		return result, nil
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_regroup_sources ON COMMIT DROP AS
		SELECT
			c.binary_id AS source_binary_id,
			c.binary_source_posted_at AS source_posted_at,
			c.binary_key AS source_binary_key,
			c.release_family_key AS source_release_family_key,
			g.target_binary_id,
			g.target_source_posted_at,
			g.provider_id,
			g.newsgroup_id,
			g.file_name,
			g.base_stem,
			g.canonical_binary_key,
			g.expected_file_count,
			g.total_parts
		FROM tmp_subject_multipart_regroup_groups g
		JOIN tmp_subject_multipart_regroup_candidates c
		  ON c.provider_id = g.provider_id
		 AND c.newsgroup_id = g.newsgroup_id
		 AND lower(btrim(c.file_name)) = lower(btrim(g.file_name))
		 AND c.expected_file_count = g.expected_file_count
		 AND c.total_parts = g.total_parts
		WHERE c.binary_id <> g.target_binary_id
		GROUP BY
			c.binary_id,
			c.binary_source_posted_at,
			c.binary_key,
			c.release_family_key,
			g.target_binary_id,
			g.target_source_posted_at,
			g.provider_id,
			g.newsgroup_id,
			g.file_name,
			g.base_stem,
			g.canonical_binary_key,
			g.expected_file_count,
			g.total_parts`); err != nil {
		return nil, fmt.Errorf("stage subject multipart regroup sources: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tmp_subject_multipart_regroup_sources (
			source_binary_id,
			source_posted_at,
			source_binary_key,
			source_release_family_key,
			target_binary_id,
			target_source_posted_at,
			provider_id,
			newsgroup_id,
			file_name,
			base_stem,
			canonical_binary_key,
			expected_file_count,
			total_parts
		)
		SELECT
			bss.source_binary_id,
			bss.source_posted_at,
			bss.source_binary_key,
			bss.release_family_key,
			g.target_binary_id,
			g.target_source_posted_at,
			g.provider_id,
			g.newsgroup_id,
			g.file_name,
			g.base_stem,
			g.canonical_binary_key,
			g.expected_file_count,
			g.total_parts
		FROM tmp_subject_multipart_regroup_groups g
		JOIN binary_superseded_sources bss
		  ON bss.target_binary_id = g.target_binary_id
		 AND bss.superseded_reason = 'subject_multipart_regroup'
		WHERE NOT EXISTS (
			SELECT 1
			FROM tmp_subject_multipart_regroup_sources existing
			WHERE existing.source_binary_id = bss.source_binary_id
			  AND existing.source_posted_at = bss.source_posted_at
		)`); err != nil {
		return nil, fmt.Errorf("stage existing subject multipart regroup sources: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tmp_subject_multipart_regroup_sources`).Scan(&result.SourceBinaries); err != nil {
		return nil, fmt.Errorf("count subject multipart regroup sources: %w", err)
	}
	if result.SourceBinaries == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit subject multipart regroup without sources: %w", err)
		}
		return result, nil
	}

	targetIDs, err := loadSubjectMultipartRegroupTargetIDs(ctx, tx)
	if err != nil {
		return nil, err
	}
	result.TargetBinaries = int64(len(targetIDs))
	if err := lockSubjectMultipartRegroupBinaries(ctx, tx); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_core bc
		SET binary_key = g.canonical_binary_key,
		    original_binary_name = CASE WHEN btrim(bc.original_binary_name) = '' THEN g.file_name ELSE bc.original_binary_name END,
		    updated_at = NOW()
		FROM tmp_subject_multipart_regroup_groups g
		WHERE bc.binary_id = g.target_binary_id
		  AND bc.binary_key <> g.canonical_binary_key`); err != nil {
		return nil, fmt.Errorf("canonicalize subject multipart target binary keys: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_identity_current bic
		SET source_release_key = g.base_stem,
		    release_family_key = g.base_stem,
		    file_set_key = g.base_stem || ' files ' || g.expected_file_count::text,
		    file_family_key = g.base_stem,
		    identity_strength = 'probable',
		    identity_reason = 'subject_multipart_obfuscated',
		    subject_set_token = '',
		    subject_set_kind = '',
		    family_kind = 'subject_multipart_obfuscated',
		    base_stem = g.base_stem,
		    is_auxiliary = false,
		    is_main_payload = true,
		    release_key = g.base_stem,
		    release_name = g.base_stem,
		    binary_name = g.file_name,
		    file_name = g.file_name,
		    file_index = 1,
		    expected_file_count = GREATEST(bic.expected_file_count, g.expected_file_count),
		    match_confidence = GREATEST(bic.match_confidence, 0.86),
		    match_status = 'matched',
		    grouping_summary_kind = '',
		    grouping_summary_status = 'matched',
		    grouping_summary_fallback_used = false,
		    updated_at = NOW()
		FROM tmp_subject_multipart_regroup_groups g
		WHERE bic.binary_id = g.target_binary_id
		  AND bic.source_posted_at = g.target_source_posted_at`); err != nil {
		return nil, fmt.Errorf("update subject multipart target identity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_observation_stats bos
		SET total_parts = GREATEST(bos.total_parts, g.total_parts),
		    updated_at = NOW()
		FROM tmp_subject_multipart_regroup_groups g
		WHERE bos.binary_id = g.target_binary_id
		  AND bos.source_posted_at = g.target_source_posted_at`); err != nil {
		return nil, fmt.Errorf("update subject multipart target stats seed: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_subject_multipart_regroup_part_winners ON COMMIT DROP AS
		SELECT
			source_posted_at,
			id,
			binary_id,
			target_binary_id,
			part_number,
			ROW_NUMBER() OVER (
				PARTITION BY target_binary_id, part_number
				ORDER BY segment_bytes DESC, id
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
			JOIN tmp_subject_multipart_regroup_sources s
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
			JOIN tmp_subject_multipart_regroup_groups g
			  ON g.target_binary_id = bp.binary_id
		) all_parts`); err != nil {
		return nil, fmt.Errorf("stage subject multipart part winners: %w", err)
	}

	duplicateDelete, err := tx.ExecContext(ctx, `
		DELETE FROM binary_parts bp
		USING tmp_subject_multipart_regroup_part_winners w
		WHERE bp.source_posted_at = w.source_posted_at
		  AND bp.id = w.id
		  AND w.keep_rank > 1`)
	if err != nil {
		return nil, fmt.Errorf("delete duplicate subject multipart parts: %w", err)
	}
	result.DuplicatePartsDeleted = rowsAffected(duplicateDelete)

	moveResult, err := tx.ExecContext(ctx, `
		UPDATE binary_parts bp
		SET binary_id = s.target_binary_id,
		    file_name = s.file_name,
		    total_parts = GREATEST(bp.total_parts, s.total_parts),
		    updated_at = NOW()
		FROM tmp_subject_multipart_regroup_sources s
		WHERE bp.binary_id = s.source_binary_id`)
	if err != nil {
		return nil, fmt.Errorf("move subject multipart parts: %w", err)
	}
	result.PartsMoved = rowsAffected(moveResult)

	if _, err := tx.ExecContext(ctx, `
		WITH agg AS (
			SELECT
				g.target_binary_id,
				g.target_source_posted_at,
				COUNT(bp.*)::integer AS observed_parts,
				COALESCE(SUM(bp.segment_bytes), 0)::bigint AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::bigint AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::bigint AS last_article_number,
				MIN(ah.date_utc) AS posted_at
			FROM tmp_subject_multipart_regroup_groups g
			JOIN binary_parts bp
			  ON bp.binary_id = g.target_binary_id
			JOIN article_headers ah
			  ON ah.source_posted_at = bp.source_posted_at
			 AND ah.id = bp.article_header_id
			GROUP BY g.target_binary_id, g.target_source_posted_at
		)
		UPDATE binary_observation_stats bos
		SET observed_parts = agg.observed_parts,
		    total_bytes = agg.total_bytes,
		    first_article_number = agg.first_article_number,
		    last_article_number = agg.last_article_number,
		    posted_at = COALESCE(agg.posted_at, bos.posted_at),
		    refreshed_at = NOW(),
		    updated_at = NOW()
		FROM agg
		WHERE bos.binary_id = agg.target_binary_id
		  AND bos.source_posted_at = agg.target_source_posted_at`); err != nil {
		return nil, fmt.Errorf("refresh subject multipart target observation stats: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE release_files rf
		SET binary_id = s.target_binary_id,
		    file_name = s.file_name
		FROM tmp_subject_multipart_regroup_sources s
		WHERE rf.binary_id = s.source_binary_id`); err != nil {
		return nil, fmt.Errorf("move subject multipart release files: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_completion_keys bck
		USING tmp_subject_multipart_regroup_sources s
		WHERE bck.binary_id = s.source_binary_id`); err != nil {
		return nil, fmt.Errorf("delete source binary completion keys: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE yenc_recovery_work_items wi
		SET status = 'done',
		    lease_owner = '',
		    lease_expires_at = NULL,
		    updated_at = NOW()
		FROM tmp_subject_multipart_regroup_sources s
		WHERE wi.binary_id = s.source_binary_id`); err != nil {
		return nil, fmt.Errorf("retire source yenc recovery work items: %w", err)
	}

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
			s.base_stem,
			s.source_binary_key,
			s.canonical_binary_key,
			'subject_multipart_regroup',
			NOW()
		FROM tmp_subject_multipart_regroup_sources s
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
		return nil, fmt.Errorf("record subject multipart superseded sources: %w", err)
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
		FROM tmp_subject_multipart_regroup_sources s
		ON CONFLICT (source_posted_at, binary_id) DO UPDATE
		SET provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    lifecycle_status = 'superseded',
		    updated_at = NOW()`); err != nil {
		return nil, fmt.Errorf("mark subject multipart source binaries superseded: %w", err)
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
		FROM tmp_subject_multipart_regroup_groups g
		ON CONFLICT (source_posted_at, binary_id) DO UPDATE
		SET provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    lifecycle_status = CASE
		    	WHEN binary_lifecycle.lifecycle_status = 'superseded' THEN 'active'
		    	ELSE binary_lifecycle.lifecycle_status
		    END,
		    updated_at = NOW()`); err != nil {
		return nil, fmt.Errorf("mark subject multipart target binaries active: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit subject multipart regroup tx: %w", err)
	}

	return result, nil
}

func loadSubjectMultipartRegroupTargetIDs(ctx context.Context, tx queryer) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT target_binary_id
		FROM tmp_subject_multipart_regroup_groups
		ORDER BY target_binary_id`)
	if err != nil {
		return nil, fmt.Errorf("load subject multipart target ids: %w", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan subject multipart target id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subject multipart target ids: %w", err)
	}
	return out, nil
}

func refreshStaleSubjectMultipartObservationStats(ctx context.Context, tx *sql.Tx, limit int) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("subject multipart stats tx is required")
	}
	if limit <= 0 {
		limit = 100
	}
	result, err := tx.ExecContext(ctx, `
		WITH stale AS (
			SELECT
				bic.binary_id,
				bic.source_posted_at
			FROM binary_identity_current bic
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bic.source_posted_at
			 AND bos.binary_id = bic.binary_id
			JOIN LATERAL (
				SELECT COUNT(*)::integer AS observed_parts
				FROM binary_parts bp
				WHERE bp.binary_id = bic.binary_id
			) part_counts ON true
			WHERE bic.identity_reason = 'subject_multipart_obfuscated'
			  AND part_counts.observed_parts > bos.observed_parts
			ORDER BY part_counts.observed_parts DESC, bic.binary_id
			LIMIT $1
		),
		agg AS (
			SELECT
				stale.binary_id,
				stale.source_posted_at,
				COUNT(bp.*)::integer AS observed_parts,
				COALESCE(SUM(bp.segment_bytes), 0)::bigint AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::bigint AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::bigint AS last_article_number,
				MIN(ah.date_utc) AS posted_at
			FROM stale
			JOIN binary_parts bp
			  ON bp.binary_id = stale.binary_id
			JOIN article_headers ah
			  ON ah.source_posted_at = bp.source_posted_at
			 AND ah.id = bp.article_header_id
			GROUP BY stale.binary_id, stale.source_posted_at
		)
		UPDATE binary_observation_stats bos
		SET observed_parts = agg.observed_parts,
		    total_bytes = agg.total_bytes,
		    first_article_number = agg.first_article_number,
		    last_article_number = agg.last_article_number,
		    posted_at = COALESCE(agg.posted_at, bos.posted_at),
		    refreshed_at = NOW(),
		    updated_at = NOW()
		FROM agg
		WHERE bos.binary_id = agg.binary_id
		  AND bos.source_posted_at = agg.source_posted_at`, limit)
	if err != nil {
		return 0, fmt.Errorf("refresh stale subject multipart observation stats: %w", err)
	}
	return rowsAffected(result), nil
}

func lockSubjectMultipartRegroupBinaries(ctx context.Context, tx queryer) error {
	rows, err := tx.QueryContext(ctx, `
		WITH ids AS (
			SELECT source_binary_id AS binary_id FROM tmp_subject_multipart_regroup_sources
			UNION
			SELECT target_binary_id AS binary_id FROM tmp_subject_multipart_regroup_groups
		)
		SELECT bc.binary_id
		FROM binary_core bc
		JOIN ids ON ids.binary_id = bc.binary_id
		ORDER BY bc.binary_id
		FOR UPDATE OF bc`)
	if err != nil {
		return fmt.Errorf("lock subject multipart regroup binaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan subject multipart regroup binary lock: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate subject multipart regroup binary locks: %w", err)
	}
	return nil
}

func rowsAffected(result interface{ RowsAffected() (int64, error) }) int64 {
	if result == nil {
		return 0
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0
	}
	return rows
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}
