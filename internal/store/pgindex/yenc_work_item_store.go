package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

const yencRecoveryWorkItemSeedLimit = 5000
const yencRecoverySubjectFileNamePredicate = `
			  AND (
			  	p.article_header_id IS NULL OR
			  	p.subject_file_name = '' OR (
			  		LOWER(BTRIM(p.subject_file_name)) LIKE '%.bin' AND
			  		regexp_replace(LOWER(BTRIM(p.subject_file_name)), '\.bin$', '') ~ '^[a-z0-9]{12,}$'
			  	) OR
			  	LOWER(BTRIM(p.subject_file_name)) ~ '\.(dat|tmp|bak|temp)$'
			  )`
const yencRecoveryWeakFamilyEligibilityPredicate = `
			  AND (
			  	(
			  		BTRIM(COALESCE(b.release_family_key, '')) = '' AND
			  		LOWER(BTRIM(COALESCE(b.identity_strength, ''))) IN ('weak', 'provisional')
			  	) OR
			  	COALESCE(s.recover_pending, FALSE) = TRUE OR
			  	COALESCE(s.readiness_bucket, '') IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  )`
const yencRecoveryWorkItemPriorityRankSQL = `
				CASE
					WHEN GREATEST(COALESCE(b.expected_file_count, 0), COALESCE(b.expected_archive_file_count, 0)) > 1
						OR COALESCE(b.file_index, 0) > 0
						OR COALESCE(b.total_parts, 0) > 1
						OR COALESCE(p.subject_file_total, 0) > 1
						OR COALESCE(p.yenc_total_parts, 0) > 1
					THEN 0
					WHEN COALESCE(s.binary_count, 0) > 1
						OR COALESCE(s.complete_binary_count, 0) > 0
						OR COALESCE(s.recover_pending, FALSE) = TRUE
						OR (
							BTRIM(COALESCE(b.release_family_key, '')) = '' AND
							LOWER(BTRIM(COALESCE(b.identity_strength, ''))) IN ('weak', 'provisional')
						)
					THEN 1
					ELSE 2
				END`

func (s *Store) ensureYEncRecoveryWorkItemsSeed(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = yencRecoveryWorkItemSeedLimit
	}
	_, _, err := s.BackfillYEncRecoveryWorkItems(ctx, limit)
	return err
}

func configureYEncRecoveryWorkItemQueryTx(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return fmt.Errorf("yenc recovery work item tx is required")
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
		return fmt.Errorf("disable parallel gather for yenc recovery work items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_parallel_append = off`); err != nil {
		return fmt.Errorf("disable parallel append for yenc recovery work items: %w", err)
	}
	return nil
}

func (s *Store) BackfillYEncRecoveryWorkItems(ctx context.Context, limit int) (int64, int64, error) {
	if limit <= 0 {
		limit = yencRecoveryWorkItemSeedLimit
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin yenc recovery work item backfill tx: %w", err)
	}
	defer rollbackTx(tx)
	if err := configureYEncRecoveryWorkItemQueryTx(ctx, tx); err != nil {
		return 0, 0, err
	}

	binaryIDs, err := s.selectYEncRecoveryBackfillBinaryIDsInTx(ctx, tx, limit)
	if err != nil {
		return 0, 0, err
	}
	if len(binaryIDs) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, 0, fmt.Errorf("commit empty yenc recovery work item backfill tx: %w", err)
		}
		return 0, 0, nil
	}

	upserted, retired, err := s.syncYEncRecoveryWorkItemsForBinariesInTx(ctx, tx, binaryIDs)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit yenc recovery work item backfill tx: %w", err)
	}
	return upserted, retired, nil
}

func (s *Store) selectYEncRecoveryBackfillBinaryIDsInTx(ctx context.Context, tx *sql.Tx, limit int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}

	perBranchLimit := limit * 4
	if perBranchLimit < limit {
		perBranchLimit = limit
	}

	queries := []string{
		`
			SELECT b.id
			FROM binaries b
			WHERE b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
			  AND (
			  	GREATEST(COALESCE(b.expected_file_count, 0), COALESCE(b.expected_archive_file_count, 0)) > 1
			  	OR COALESCE(b.file_index, 0) > 0
			  	OR COALESCE(b.total_parts, 0) > 1
			  )
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
			  	WHERE wi.binary_id = b.id
			  	  AND wi.status = 'ready'
			  	  AND wi.updated_at >= b.updated_at
			  )
			ORDER BY b.updated_at DESC, b.id
			LIMIT $1
		`,
		`
			SELECT b.id
			FROM binaries b
			WHERE b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
			  AND BTRIM(COALESCE(b.release_family_key, '')) = ''
			  AND LOWER(BTRIM(COALESCE(b.identity_strength, ''))) IN ('weak', 'provisional')
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
			  	WHERE wi.binary_id = b.id
			  	  AND wi.status = 'ready'
			  	  AND wi.updated_at >= b.updated_at
			  )
			ORDER BY b.updated_at DESC, b.id
			LIMIT $1
		`,
		`
			SELECT b.id
			FROM binaries b
			JOIN release_family_readiness_summaries s
			  ON s.provider_id = b.provider_id
			 AND s.newsgroup_id = b.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = b.release_family_key
			WHERE b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
			  AND BTRIM(COALESCE(b.release_family_key, '')) <> ''
			  AND (
			  	COALESCE(s.recover_pending, FALSE) = TRUE
			  	OR COALESCE(s.readiness_bucket, '') IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  )
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
			  	WHERE wi.binary_id = b.id
			  	  AND wi.status = 'ready'
			  	  AND wi.updated_at >= b.updated_at
			  )
			ORDER BY b.updated_at DESC, b.id
			LIMIT $1
		`,
	}

	binaryIDs := make([]int64, 0, limit)
	seen := make(map[int64]struct{}, limit)
	for _, query := range queries {
		if len(binaryIDs) >= limit {
			break
		}
		rows, err := tx.QueryContext(ctx, query, perBranchLimit)
		if err != nil {
			return nil, fmt.Errorf("select yenc recovery work item backfill binaries: %w", err)
		}
		for rows.Next() {
			var binaryID int64
			if err := rows.Scan(&binaryID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan yenc recovery backfill binary: %w", err)
			}
			if _, ok := seen[binaryID]; ok {
				continue
			}
			seen[binaryID] = struct{}{}
			binaryIDs = append(binaryIDs, binaryID)
			if len(binaryIDs) >= limit {
				break
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate yenc recovery backfill binaries: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close yenc recovery backfill rows: %w", err)
		}
	}

	return binaryIDs, nil
}

func (s *Store) syncYEncRecoveryWorkItemsForBinariesInTx(ctx context.Context, tx *sql.Tx, binaryIDs []int64) (int64, int64, error) {
	if err := configureYEncRecoveryWorkItemQueryTx(ctx, tx); err != nil {
		return 0, 0, err
	}
	unique := make([]int64, 0, len(binaryIDs))
	seen := make(map[int64]struct{}, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		if binaryID <= 0 {
			continue
		}
		if _, ok := seen[binaryID]; ok {
			continue
		}
		seen[binaryID] = struct{}{}
		unique = append(unique, binaryID)
	}
	if len(unique) == 0 {
		return 0, 0, nil
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })

	values := make([]string, 0, len(unique))
	args := make([]any, 0, len(unique))
	for i, binaryID := range unique {
		values = append(values, fmt.Sprintf("($%d::bigint)", i+1))
		args = append(args, binaryID)
	}

	var upserted int64
	if err := tx.QueryRowContext(ctx, `
		WITH requested(binary_id) AS (
			VALUES `+strings.Join(values, ",")+`
		),
		eligible AS (
			SELECT
				b.id AS binary_id,
				ah.id AS article_header_id,
				ah.provider_id,
				ah.newsgroup_id,
				ah.message_id,
`+yencRecoveryWorkItemPriorityRankSQL+` AS priority_rank,
				COALESCE(p.yenc_recovery_retry_after, NOW()) AS ready_at,
				COALESCE(p.yenc_recovery_missing_count, 0) AS missing_count,
				b.binary_key,
				b.release_family_key,
				b.base_stem,
				COALESCE(s.readiness_bucket, '') AS readiness_bucket,
				COALESCE((b.grouping_evidence_json -> 'summary' ->> 'fallback_used')::boolean, false) AS structured_identity_binary_matched
			FROM requested r
			JOIN binaries b ON b.id = r.binary_id
			LEFT JOIN release_family_readiness_summaries s
			  ON s.provider_id = b.provider_id
			 AND s.newsgroup_id = b.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = b.release_family_key
			JOIN LATERAL (
				SELECT bp.article_header_id
				FROM binary_parts bp
				WHERE bp.binary_id = b.id
				ORDER BY bp.part_number, bp.id
				LIMIT 1
			) bp ON true
			JOIN article_headers ah ON ah.id = bp.article_header_id
			LEFT JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
			WHERE b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
`+yencRecoveryWeakFamilyEligibilityPredicate+`
`+yencRecoverySubjectFileNamePredicate+`
		),
		upserted AS (
			INSERT INTO yenc_recovery_work_items (
				binary_id,
				article_header_id,
				provider_id,
				newsgroup_id,
				message_id,
				status,
				ready_at,
				priority_rank,
				missing_count,
				current_binary_key,
				current_release_family_key,
				current_base_stem,
				current_readiness_bucket,
				structured_identity_binary_matched,
				updated_at
			)
			SELECT
				e.binary_id,
				e.article_header_id,
				e.provider_id,
				e.newsgroup_id,
				e.message_id,
				'ready',
				e.ready_at,
				e.priority_rank,
				e.missing_count,
				e.binary_key,
				e.release_family_key,
				e.base_stem,
				e.readiness_bucket,
				e.structured_identity_binary_matched,
				NOW()
			FROM eligible e
			ON CONFLICT (binary_id) DO UPDATE
			SET article_header_id = EXCLUDED.article_header_id,
			    provider_id = EXCLUDED.provider_id,
			    newsgroup_id = EXCLUDED.newsgroup_id,
			    message_id = EXCLUDED.message_id,
			    status = 'ready',
			    ready_at = EXCLUDED.ready_at,
			    priority_rank = EXCLUDED.priority_rank,
			    missing_count = EXCLUDED.missing_count,
			    current_binary_key = EXCLUDED.current_binary_key,
			    current_release_family_key = EXCLUDED.current_release_family_key,
			    current_base_stem = EXCLUDED.current_base_stem,
			    current_readiness_bucket = EXCLUDED.current_readiness_bucket,
			    structured_identity_binary_matched = EXCLUDED.structured_identity_binary_matched,
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM upserted`,
		args...,
	).Scan(&upserted); err != nil {
		return 0, 0, fmt.Errorf("upsert yenc recovery work items: %w", err)
	}

	var retired int64
	if err := tx.QueryRowContext(ctx, `
		WITH requested(binary_id) AS (
			VALUES `+strings.Join(values, ",")+`
		),
		eligible AS (
			SELECT DISTINCT b.id AS binary_id
			FROM requested r
			JOIN binaries b ON b.id = r.binary_id
			LEFT JOIN release_family_readiness_summaries s
			  ON s.provider_id = b.provider_id
			 AND s.newsgroup_id = b.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = b.release_family_key
			JOIN LATERAL (
				SELECT bp.article_header_id
				FROM binary_parts bp
				WHERE bp.binary_id = b.id
				ORDER BY bp.part_number, bp.id
				LIMIT 1
			) bp ON true
			LEFT JOIN article_header_ingest_payloads p ON p.article_header_id = bp.article_header_id
			WHERE b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
`+yencRecoveryWeakFamilyEligibilityPredicate+`
`+yencRecoverySubjectFileNamePredicate+`
		),
		retire_candidates AS (
			SELECT
				wi.binary_id,
				CASE
					WHEN COALESCE(b.recovered_source, '') = 'yenc_header' THEN 'done'
					ELSE 'stale'
				END AS next_status
			FROM requested r
			JOIN yenc_recovery_work_items wi ON wi.binary_id = r.binary_id
			JOIN binaries b ON b.id = wi.binary_id
			LEFT JOIN eligible e ON e.binary_id = wi.binary_id
			WHERE e.binary_id IS NULL
		),
		retired AS (
			UPDATE yenc_recovery_work_items wi
			SET status = rc.next_status,
			    updated_at = NOW()
			FROM retire_candidates rc
			WHERE wi.binary_id = rc.binary_id
			  AND wi.status <> rc.next_status
			RETURNING 1
		)
		SELECT COUNT(*) FROM retired`,
		args...,
	).Scan(&retired); err != nil {
		return 0, 0, fmt.Errorf("retire yenc recovery work items: %w", err)
	}

	return upserted, retired, nil
}
