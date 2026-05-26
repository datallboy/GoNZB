package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

const yencRecoveryWorkItemSeedLimit = 5000

func (s *Store) ensureYEncRecoveryWorkItemsSeed(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = yencRecoveryWorkItemSeedLimit
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM yenc_recovery_work_items`).Scan(&count); err != nil {
		return fmt.Errorf("count yenc recovery work items: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, _, err := s.BackfillYEncRecoveryWorkItems(ctx, limit)
	return err
}

func (s *Store) BackfillYEncRecoveryWorkItems(ctx context.Context, limit int) (int64, int64, error) {
	if limit <= 0 {
		limit = yencRecoveryWorkItemSeedLimit
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH candidate_binaries AS (
			SELECT
				b.id AS binary_id
			FROM binaries b
			JOIN release_family_readiness_summaries s
			  ON s.provider_id = b.provider_id
			 AND s.newsgroup_id = b.newsgroup_id
			 AND s.key_kind = 'release_family'
			 AND s.family_key = b.release_family_key
			LEFT JOIN yenc_recovery_work_items wi
			  ON wi.binary_id = b.id
			WHERE s.readiness_bucket IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  AND b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
			  AND (
			  	wi.binary_id IS NULL
			  	OR wi.updated_at < b.updated_at
			  	OR wi.status <> 'ready'
			  )
			ORDER BY b.updated_at DESC, b.id
			LIMIT $1
		)
		SELECT binary_id
		FROM candidate_binaries`,
		limit,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("select yenc recovery work item backfill binaries: %w", err)
	}
	defer rows.Close()

	binaryIDs := make([]int64, 0, limit)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			return 0, 0, fmt.Errorf("scan yenc recovery backfill binary: %w", err)
		}
		binaryIDs = append(binaryIDs, binaryID)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate yenc recovery backfill binaries: %w", err)
	}
	if len(binaryIDs) == 0 {
		return 0, 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin yenc recovery work item backfill tx: %w", err)
	}
	defer rollbackTx(tx)

	upserted, retired, err := s.syncYEncRecoveryWorkItemsForBinariesInTx(ctx, tx, binaryIDs)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit yenc recovery work item backfill tx: %w", err)
	}
	return upserted, retired, nil
}

func (s *Store) syncYEncRecoveryWorkItemsForBinariesInTx(ctx context.Context, tx *sql.Tx, binaryIDs []int64) (int64, int64, error) {
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
				CASE
					WHEN COALESCE(p.yenc_total_parts, 0) > 1 THEN 0
					WHEN ah.message_id ~* 'part[0-9]{1,6}of[0-9]{1,6}' THEN 1
					ELSE 2
				END AS priority_rank,
				COALESCE(p.yenc_recovery_retry_after, NOW()) AS ready_at,
				COALESCE(p.yenc_recovery_missing_count, 0) AS missing_count,
				b.binary_key,
				b.release_family_key,
				b.base_stem,
				COALESCE(s.readiness_bucket, '') AS readiness_bucket,
				COALESCE((b.grouping_evidence_json -> 'summary' ->> 'fallback_used')::boolean, false) AS structured_identity_binary_matched
			FROM requested r
			JOIN binaries b ON b.id = r.binary_id
			JOIN release_family_readiness_summaries s
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
			JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
			WHERE s.readiness_bucket IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  AND b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
			  AND p.subject_file_name = ''
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
			JOIN release_family_readiness_summaries s
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
			JOIN article_header_ingest_payloads p ON p.article_header_id = bp.article_header_id
			WHERE s.readiness_bucket IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set')
			  AND b.family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND b.is_main_payload = TRUE
			  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
			  AND p.subject_file_name = ''
		),
		retired AS (
			UPDATE yenc_recovery_work_items wi
			SET status = CASE
				WHEN EXISTS (
					SELECT 1
					FROM binaries b
					WHERE b.id = wi.binary_id
					  AND COALESCE(b.recovered_source, '') = 'yenc_header'
				) THEN 'done'
				ELSE 'stale'
			END,
			    updated_at = NOW()
			WHERE wi.binary_id IN (SELECT binary_id FROM requested)
			  AND wi.binary_id NOT IN (SELECT binary_id FROM eligible)
			  AND wi.status <> CASE
			  	WHEN EXISTS (
					SELECT 1
					FROM binaries b
					WHERE b.id = wi.binary_id
					  AND COALESCE(b.recovered_source, '') = 'yenc_header'
				) THEN 'done'
				ELSE 'stale'
			  END
			RETURNING 1
		)
		SELECT COUNT(*) FROM retired`,
		args...,
	).Scan(&retired); err != nil {
		return 0, 0, fmt.Errorf("retire yenc recovery work items: %w", err)
	}

	return upserted, retired, nil
}
