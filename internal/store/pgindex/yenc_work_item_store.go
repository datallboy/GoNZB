package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

const (
	yencRecoveryWorkItemSeedLimit = 5000
	yencRecoveryWorkItemSyncChunk = 10000
)
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
                    BTRIM(COALESCE(bic.release_family_key, '')) = '' AND
                    LOWER(BTRIM(COALESCE(bic.identity_strength, ''))) IN ('weak', 'provisional')
			  	) OR
			  	COALESCE(s.recover_pending, FALSE) = TRUE OR
			  	COALESCE(s.readiness_bucket, '') IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  )`
const yencRecoveryWorkItemPriorityRankSQL = `
				CASE
					WHEN GREATEST(COALESCE(bic.expected_file_count, 0), COALESCE(bic.expected_archive_file_count, 0)) > 1
						OR COALESCE(bic.file_index, 0) > 0
						OR COALESCE(bos.total_parts, 0) > 1
						OR COALESCE(p.subject_file_total, 0) > 1
						OR COALESCE(p.yenc_total_parts, 0) > 1
					THEN 0
					WHEN COALESCE(s.binary_count, 0) > 1
						OR COALESCE(s.complete_binary_count, 0) > 0
						OR COALESCE(s.recover_pending, FALSE) = TRUE
						OR (
							BTRIM(COALESCE(bic.release_family_key, '')) = '' AND
							LOWER(BTRIM(COALESCE(bic.identity_strength, ''))) IN ('weak', 'provisional')
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
			SELECT bc.binary_id
			FROM binary_core bc
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = bc.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			WHERE bic.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND bic.is_main_payload = TRUE
			  AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
			  AND (
                    GREATEST(COALESCE(bic.expected_file_count, 0), COALESCE(bic.expected_archive_file_count, 0)) > 1
                    OR COALESCE(bic.file_index, 0) > 0
                    OR COALESCE(bos.total_parts, 0) > 1
			  )
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
                    WHERE wi.binary_id = bc.binary_id
			  	  AND wi.status = 'ready'
                      AND wi.updated_at >= GREATEST(bic.updated_at, bos.updated_at, COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch'))
			  )
			ORDER BY GREATEST(bic.updated_at, bos.updated_at, COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')) DESC, bc.binary_id
			LIMIT $1
		`,
		`
			SELECT bc.binary_id
			FROM binary_core bc
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = bc.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			WHERE bic.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND bic.is_main_payload = TRUE
			  AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
			  AND BTRIM(COALESCE(bic.release_family_key, '')) = ''
			  AND LOWER(BTRIM(COALESCE(bic.identity_strength, ''))) IN ('weak', 'provisional')
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
                    WHERE wi.binary_id = bc.binary_id
			  	  AND wi.status = 'ready'
                      AND wi.updated_at >= GREATEST(bic.updated_at, bos.updated_at, COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch'))
			  )
			ORDER BY GREATEST(bic.updated_at, bos.updated_at, COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')) DESC, bc.binary_id
			LIMIT $1
		`,
		`
			SELECT bc.binary_id
			FROM binary_core bc
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = bc.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			JOIN release_family_readiness_summaries s
			  ON s.provider_id = bc.provider_id
			 AND s.newsgroup_id = bc.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = bic.release_family_key
			WHERE bic.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND bic.is_main_payload = TRUE
			  AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
			  AND BTRIM(COALESCE(bic.release_family_key, '')) <> ''
			  AND (
			  	COALESCE(s.recover_pending, FALSE) = TRUE
			  	OR COALESCE(s.readiness_bucket, '') IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  )
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
                    WHERE wi.binary_id = bc.binary_id
			  	  AND wi.status = 'ready'
                      AND wi.updated_at >= GREATEST(bic.updated_at, bos.updated_at, COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch'))
			  )
			ORDER BY GREATEST(bic.updated_at, bos.updated_at, COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')) DESC, bc.binary_id
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

	admission, err := s.RefreshYEncRecoveryAdmissionSnapshot(ctx)
	if err != nil {
		return 0, 0, err
	}
	remainingToHard := admission.RemainingToHard
	overSoftCap := admission.OpenTotal >= admission.SoftCap
	overHardCap := admission.OpenTotal >= admission.HardCap
	if overHardCap {
		overflowCap, err := s.yEncPriority0OverflowCap(ctx, tx)
		if err != nil {
			return 0, 0, err
		}
		var priority0Open int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM yenc_recovery_work_items
			WHERE status IN ('ready', 'running')
			  AND priority_rank = 0`).Scan(&priority0Open); err != nil {
			return 0, 0, fmt.Errorf("count open priority0 yenc recovery work items: %w", err)
		}
		remainingToHard = overflowCap - priority0Open
		if remainingToHard < 0 {
			remainingToHard = 0
		}
	}

	var totalUpserted int64
	var totalRetired int64
	for start := 0; start < len(unique); start += yencRecoveryWorkItemSyncChunk {
		if remainingToHard <= 0 {
			break
		}
		end := start + yencRecoveryWorkItemSyncChunk
		if end > len(unique) {
			end = len(unique)
		}
		upserted, retired, err := s.syncYEncRecoveryWorkItemsChunkInTx(ctx, tx, unique[start:end], remainingToHard, overSoftCap, overHardCap)
		if err != nil {
			return 0, 0, err
		}
		totalUpserted += upserted
		totalRetired += retired
		remainingToHard -= upserted
		if remainingToHard < 0 {
			remainingToHard = 0
		}
	}
	return totalUpserted, totalRetired, nil
}

func (s *Store) syncYEncRecoveryWorkItemsChunkInTx(ctx context.Context, tx *sql.Tx, unique []int64, remainingToHard int64, overSoftCap bool, overHardCap bool) (int64, int64, error) {
	var upserted int64
	if err := tx.QueryRowContext(ctx, `
		WITH requested(binary_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		),
		eligible AS (
			SELECT
				bc.binary_id,
				ah.id AS article_header_id,
				ah.provider_id,
				ah.newsgroup_id,
				ng.group_name AS newsgroup_name,
				ah.article_number,
				ah.message_id,
				COALESCE(NULLIF(p.subject, ''), NULLIF(bic.binary_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.release_name, ''), '') AS subject,
				COALESCE(p.poster, '') AS poster,
				ah.date_utc,
				ah.bytes AS article_bytes,
				ah.lines AS article_lines,
				COALESCE(p.xref, '') AS xref,
				COALESCE(NULLIF(p.subject_file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') AS subject_file_name,
				CASE
					WHEN COALESCE(p.subject_file_index, 0) > 0 THEN p.subject_file_index
					WHEN COALESCE(bic.file_index, 0) > 0 THEN bic.file_index
					ELSE 0
				END AS subject_file_index,
				GREATEST(
					COALESCE(p.subject_file_total, 0),
					COALESCE(bic.expected_file_count, 0),
					COALESCE(bic.expected_archive_file_count, 0)
				) AS subject_file_total,
				COALESCE(p.yenc_part_number, 0) AS yenc_part_number,
				GREATEST(COALESCE(p.yenc_total_parts, 0), COALESCE(bos.total_parts, 0)) AS yenc_total_parts,
				COALESCE(p.yenc_file_size, 0) AS yenc_file_size,
`+yencRecoveryWorkItemPriorityRankSQL+` AS priority_rank,
				NOW() AS ready_at,
				0 AS missing_count,
				bc.binary_key,
				bic.release_family_key,
				bic.base_stem,
				COALESCE(s.readiness_bucket, '') AS readiness_bucket,
				COALESCE(bic.grouping_summary_fallback_used, false) AS structured_identity_binary_matched,
				COALESCE(NULLIF(igp.tier_override, ''), NULLIF(igp.tier, ''), 'warm') AS group_tier
			FROM requested r
			JOIN binary_core bc ON bc.binary_id = r.binary_id
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = bc.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			LEFT JOIN release_family_readiness_summaries s
			  ON s.provider_id = bc.provider_id
			 AND s.newsgroup_id = bc.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = bic.release_family_key
			LEFT JOIN indexer_group_profiles igp
			  ON igp.provider_id = bc.provider_id
			 AND igp.newsgroup_id = bc.newsgroup_id
			JOIN LATERAL (
				SELECT bp.source_posted_at, bp.article_header_id
				FROM binary_parts bp
				WHERE bp.binary_id = bc.binary_id
				ORDER BY bp.part_number, bp.id
				LIMIT 1
			) bp ON true
			JOIN article_headers ah
			  ON ah.source_posted_at = bp.source_posted_at
			 AND ah.id = bp.article_header_id
			LEFT JOIN article_header_ingest_payloads p
			  ON p.source_posted_at = ah.source_posted_at
			 AND p.article_header_id = ah.id
			JOIN newsgroups ng ON ng.id = ah.newsgroup_id
			WHERE bic.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND bic.is_main_payload = TRUE
			  AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
`+yencRecoveryWeakFamilyEligibilityPredicate+`
`+yencRecoverySubjectFileNamePredicate+`
			  AND (
			  	$3::boolean = FALSE
			  	OR `+yencRecoveryWorkItemPriorityRankSQL+` = 0
			  )
			  AND (
			  	$4::boolean = FALSE
			  	OR `+yencRecoveryWorkItemPriorityRankSQL+` = 0
			  	OR COALESCE(NULLIF(igp.tier_override, ''), NULLIF(igp.tier, ''), 'warm') = 'hot'
			  )
			  AND NOT EXISTS (
			    SELECT 1
			    FROM yenc_recovery_work_items existing_article
			    WHERE existing_article.source_posted_at = ah.source_posted_at
			      AND existing_article.article_header_id = ah.id
			      AND existing_article.binary_id <> bc.binary_id
			  )
			ORDER BY `+yencRecoveryWorkItemPriorityRankSQL+`, COALESCE(ah.date_utc, NOW()) DESC, bc.binary_id
			LIMIT CASE
				WHEN $3::boolean THEN LEAST($2::bigint, 10000)
				WHEN $2::bigint <= 0 THEN 0
				ELSE LEAST($2::bigint, 10000)
			END
		),
		upserted AS (
			INSERT INTO yenc_recovery_work_items (
				binary_id,
				article_header_id,
				provider_id,
				newsgroup_id,
				newsgroup_name,
				article_number,
				message_id,
				subject,
				poster,
				date_utc,
				article_bytes,
				article_lines,
				xref,
				subject_file_name,
				subject_file_index,
				subject_file_total,
				yenc_part_number,
				yenc_total_parts,
				yenc_file_size,
				status,
				ready_at,
				priority_rank,
				missing_count,
				current_binary_key,
				current_release_family_key,
				current_base_stem,
				current_readiness_bucket,
				structured_identity_binary_matched,
				group_tier,
				admission_reason,
				admission_score,
				source_posted_at,
				partition_day,
				updated_at
			)
			SELECT
				e.binary_id,
				e.article_header_id,
				e.provider_id,
				e.newsgroup_id,
				e.newsgroup_name,
				e.article_number,
				e.message_id,
				e.subject,
				e.poster,
				e.date_utc,
				e.article_bytes,
				e.article_lines,
				e.xref,
				e.subject_file_name,
				e.subject_file_index,
				e.subject_file_total,
				e.yenc_part_number,
				e.yenc_total_parts,
				e.yenc_file_size,
				'ready',
				e.ready_at,
				e.priority_rank,
				e.missing_count,
				e.binary_key,
				e.release_family_key,
				e.base_stem,
				e.readiness_bucket,
				e.structured_identity_binary_matched,
				e.group_tier,
				CASE
					WHEN e.priority_rank = 0 THEN 'near_complete_or_multipart'
					ELSE 'bounded_admission'
				END,
				CASE
					WHEN e.group_tier = 'hot' THEN 100
					WHEN e.group_tier = 'warm' THEN 50
					WHEN e.group_tier = 'cold' THEN 10
					ELSE 0
				END + GREATEST(0, 10 - e.priority_rank),
				COALESCE(e.date_utc, NOW()),
				COALESCE(e.date_utc, NOW())::date,
				NOW()
			FROM eligible e
			ON CONFLICT (source_posted_at, binary_id) DO UPDATE
			SET article_header_id = EXCLUDED.article_header_id,
			    provider_id = EXCLUDED.provider_id,
			    newsgroup_id = EXCLUDED.newsgroup_id,
			    newsgroup_name = EXCLUDED.newsgroup_name,
			    article_number = EXCLUDED.article_number,
			    message_id = EXCLUDED.message_id,
			    subject = EXCLUDED.subject,
			    poster = EXCLUDED.poster,
			    date_utc = EXCLUDED.date_utc,
			    article_bytes = EXCLUDED.article_bytes,
			    article_lines = EXCLUDED.article_lines,
			    xref = EXCLUDED.xref,
			    subject_file_name = EXCLUDED.subject_file_name,
			    subject_file_index = EXCLUDED.subject_file_index,
			    subject_file_total = EXCLUDED.subject_file_total,
			    yenc_part_number = EXCLUDED.yenc_part_number,
			    yenc_total_parts = EXCLUDED.yenc_total_parts,
			    yenc_file_size = EXCLUDED.yenc_file_size,
			    status = 'ready',
			    ready_at = EXCLUDED.ready_at,
			    priority_rank = EXCLUDED.priority_rank,
			    missing_count = EXCLUDED.missing_count,
			    current_binary_key = EXCLUDED.current_binary_key,
			    current_release_family_key = EXCLUDED.current_release_family_key,
			    current_base_stem = EXCLUDED.current_base_stem,
			    current_readiness_bucket = EXCLUDED.current_readiness_bucket,
			    structured_identity_binary_matched = EXCLUDED.structured_identity_binary_matched,
			    group_tier = EXCLUDED.group_tier,
			    admission_reason = EXCLUDED.admission_reason,
			    admission_score = EXCLUDED.admission_score,
			    source_posted_at = EXCLUDED.source_posted_at,
			    partition_day = EXCLUDED.partition_day,
			    lease_owner = '',
			    lease_expires_at = NULL,
			    updated_at = NOW()
			WHERE NOT EXISTS (
				SELECT 1
				FROM yenc_recovery_work_items existing_article
				WHERE existing_article.article_header_id = EXCLUDED.article_header_id
				  AND existing_article.source_posted_at = EXCLUDED.source_posted_at
				  AND existing_article.binary_id <> yenc_recovery_work_items.binary_id
			)
			RETURNING 1
		)
		SELECT COUNT(*) FROM upserted`,
		unique,
		remainingToHard,
		overHardCap,
		overSoftCap,
	).Scan(&upserted); err != nil {
		return 0, 0, fmt.Errorf("upsert yenc recovery work items: %w", err)
	}

	var retired int64
	if err := tx.QueryRowContext(ctx, `
		WITH requested(binary_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		),
		eligible AS (
			SELECT DISTINCT bc.binary_id
			FROM requested r
			JOIN binary_core bc ON bc.binary_id = r.binary_id
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = bc.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			LEFT JOIN release_family_readiness_summaries s
			  ON s.provider_id = bc.provider_id
			 AND s.newsgroup_id = bc.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = bic.release_family_key
			JOIN LATERAL (
				SELECT bp.source_posted_at, bp.article_header_id
				FROM binary_parts bp
				WHERE bp.binary_id = bc.binary_id
				ORDER BY bp.part_number, bp.id
				LIMIT 1
			) bp ON true
			LEFT JOIN article_header_ingest_payloads p
			  ON p.source_posted_at = bp.source_posted_at
			 AND p.article_header_id = bp.article_header_id
			WHERE bic.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND bic.is_main_payload = TRUE
			  AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
`+yencRecoveryWeakFamilyEligibilityPredicate+`
`+yencRecoverySubjectFileNamePredicate+`
		),
		retire_candidates AS (
			SELECT
				wi.binary_id,
				CASE
					WHEN COALESCE(brc.recovered_source, '') = 'yenc_header' THEN 'done'
					ELSE 'stale'
				END AS next_status
			FROM requested r
			JOIN yenc_recovery_work_items wi ON wi.binary_id = r.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = wi.source_posted_at
			 AND brc.binary_id = wi.binary_id
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
		unique,
	).Scan(&retired); err != nil {
		return 0, 0, fmt.Errorf("retire yenc recovery work items: %w", err)
	}

	return upserted, retired, nil
}
