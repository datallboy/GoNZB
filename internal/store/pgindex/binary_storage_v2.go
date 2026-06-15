package pgindex

import (
	"context"
	"fmt"
)

func upsertBinaryStorageV2FromStagedBinaries(ctx context.Context, runner sqlExecQueryer) error {
	if runner == nil {
		return fmt.Errorf("binary storage v2 runner is required")
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_core (
			binary_id,
			provider_id,
			newsgroup_id,
			poster_id,
			binary_key,
			original_binary_name,
			updated_at
		)
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.poster_id,
			b.binary_key,
			b.binary_name,
			NOW()
		FROM tmp_upsert_binaries r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.binary_key = r.binary_key
		ON CONFLICT (binary_id) DO UPDATE
		SET poster_id = EXCLUDED.poster_id,
		    binary_key = EXCLUDED.binary_key,
		    original_binary_name = CASE
		    	WHEN binary_core.original_binary_name = '' THEN EXCLUDED.original_binary_name
		    	ELSE binary_core.original_binary_name
		    END,
		    updated_at = NOW()`); err != nil {
		return fmt.Errorf("upsert binary_core v2: %w", err)
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_observation_stats (
			binary_id,
			provider_id,
			newsgroup_id,
			total_parts,
			observed_parts,
			total_bytes,
			first_article_number,
			last_article_number,
			posted_at,
			refreshed_at,
			updated_at
		)
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.total_parts,
			b.observed_parts,
			b.total_bytes,
			b.first_article_number,
			b.last_article_number,
			b.posted_at,
			NOW(),
			NOW()
		FROM tmp_upsert_binaries r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.binary_key = r.binary_key
		ON CONFLICT (binary_id) DO UPDATE
		SET total_parts = GREATEST(binary_observation_stats.total_parts, EXCLUDED.total_parts),
		    observed_parts = EXCLUDED.observed_parts,
		    total_bytes = EXCLUDED.total_bytes,
		    first_article_number = EXCLUDED.first_article_number,
		    last_article_number = EXCLUDED.last_article_number,
		    posted_at = COALESCE(binary_observation_stats.posted_at, EXCLUDED.posted_at),
		    refreshed_at = NOW(),
		    updated_at = NOW()`); err != nil {
		return fmt.Errorf("upsert binary_observation_stats v2: %w", err)
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_identity_current (
			binary_id,
			provider_id,
			newsgroup_id,
			source_release_key,
			release_family_key,
			file_set_key,
			file_family_key,
			identity_strength,
			identity_reason,
			subject_set_token,
			subject_set_kind,
			family_kind,
			base_stem,
			release_key,
			release_name,
			binary_name,
			file_name,
			file_index,
			expected_file_count,
			expected_archive_file_count,
			is_auxiliary,
			is_main_payload,
			match_confidence,
			match_status,
			grouping_summary_kind,
			grouping_summary_status,
			grouping_summary_fallback_used,
			updated_at
		)
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.source_release_key,
			b.release_family_key,
			b.file_set_key,
			b.file_family_key,
			b.identity_strength,
			b.identity_reason,
			b.subject_set_token,
			b.subject_set_kind,
			b.family_kind,
			b.base_stem,
			b.release_key,
			b.release_name,
			b.binary_name,
			b.file_name,
			b.file_index,
			b.expected_file_count,
			b.expected_archive_file_count,
			b.is_auxiliary,
			b.is_main_payload,
			b.match_confidence,
			b.match_status,
			b.grouping_summary_kind,
			b.grouping_summary_status,
			b.grouping_summary_fallback_used,
			NOW()
		FROM tmp_upsert_binaries r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.binary_key = r.binary_key
		ON CONFLICT (binary_id) DO UPDATE
		SET source_release_key = EXCLUDED.source_release_key,
		    release_family_key = EXCLUDED.release_family_key,
		    file_set_key = EXCLUDED.file_set_key,
		    file_family_key = EXCLUDED.file_family_key,
		    identity_strength = EXCLUDED.identity_strength,
		    identity_reason = EXCLUDED.identity_reason,
		    subject_set_token = EXCLUDED.subject_set_token,
		    subject_set_kind = EXCLUDED.subject_set_kind,
		    family_kind = EXCLUDED.family_kind,
		    base_stem = EXCLUDED.base_stem,
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_name = EXCLUDED.binary_name,
		    file_name = EXCLUDED.file_name,
		    file_index = EXCLUDED.file_index,
		    expected_file_count = GREATEST(binary_identity_current.expected_file_count, EXCLUDED.expected_file_count),
		    expected_archive_file_count = GREATEST(binary_identity_current.expected_archive_file_count, EXCLUDED.expected_archive_file_count),
		    is_auxiliary = EXCLUDED.is_auxiliary,
		    is_main_payload = EXCLUDED.is_main_payload,
		    match_confidence = GREATEST(binary_identity_current.match_confidence, EXCLUDED.match_confidence),
		    match_status = EXCLUDED.match_status,
		    grouping_summary_kind = EXCLUDED.grouping_summary_kind,
		    grouping_summary_status = EXCLUDED.grouping_summary_status,
		    grouping_summary_fallback_used = EXCLUDED.grouping_summary_fallback_used,
		    updated_at = NOW()`); err != nil {
		return fmt.Errorf("upsert binary_identity_current v2: %w", err)
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_recovery_current (
			binary_id,
			provider_id,
			newsgroup_id,
			recovered_kind,
			recovered_extension,
			recovered_source,
			recovered_confidence,
			recovered_file_name,
			recovered_at,
			updated_at
		)
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.recovered_kind,
			b.recovered_extension,
			b.recovered_source,
			b.recovered_confidence,
			b.file_name,
			b.recovered_at,
			NOW()
		FROM tmp_upsert_binaries r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.binary_key = r.binary_key
		ON CONFLICT (binary_id) DO UPDATE
		SET recovered_kind = EXCLUDED.recovered_kind,
		    recovered_extension = EXCLUDED.recovered_extension,
		    recovered_source = EXCLUDED.recovered_source,
		    recovered_confidence = GREATEST(binary_recovery_current.recovered_confidence, EXCLUDED.recovered_confidence),
		    recovered_file_name = EXCLUDED.recovered_file_name,
		    recovered_at = COALESCE(EXCLUDED.recovered_at, binary_recovery_current.recovered_at),
		    updated_at = NOW()`); err != nil {
		return fmt.Errorf("upsert binary_recovery_current v2: %w", err)
	}

	if _, err := runner.ExecContext(ctx, `
		INSERT INTO binary_lifecycle (
			binary_id,
			provider_id,
			newsgroup_id,
			lifecycle_status,
			updated_at
		)
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			'active',
			NOW()
		FROM tmp_upsert_binaries r
		JOIN binaries b
		  ON b.provider_id = r.provider_id
		 AND b.newsgroup_id = r.newsgroup_id
		 AND b.binary_key = r.binary_key
		ON CONFLICT (binary_id) DO NOTHING`); err != nil {
		return fmt.Errorf("upsert binary_lifecycle v2: %w", err)
	}

	return nil
}

func syncBinaryStorageV2ByIDs(ctx context.Context, runner sqlExecQueryer, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	filter, args := bigintFilter(ids, 1)
	query := `CREATE TEMP TABLE tmp_binary_storage_v2_ids ON COMMIT DROP AS
		SELECT id AS binary_id
		FROM binaries
		WHERE id IN (` + filter + `)`
	if _, err := runner.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("stage binary storage v2 ids: %w", err)
	}
	if _, err := runner.ExecContext(ctx, `
		CREATE TEMP TABLE tmp_upsert_binaries ON COMMIT DROP AS
		SELECT
			row_number() OVER (ORDER BY b.id)::integer - 1 AS ordinal,
			b.provider_id,
			b.newsgroup_id,
			COALESCE(b.poster_id, 0)::bigint AS poster_id,
			b.source_release_key,
			b.release_family_key,
			b.file_set_key,
			b.file_family_key,
			b.identity_strength,
			b.identity_reason,
			b.subject_set_token,
			b.subject_set_kind,
			b.family_kind,
			b.base_stem,
			b.is_auxiliary,
			b.is_main_payload,
			b.release_key,
			b.release_name,
			b.binary_key,
			b.binary_name,
			b.file_name,
			b.file_index,
			b.expected_file_count,
			b.total_parts,
			b.posted_at,
			b.match_confidence,
			b.match_status,
			b.grouping_summary_kind,
			b.grouping_summary_status,
			b.grouping_summary_fallback_used,
			b.grouping_evidence_json AS grouping_evidence_payload
		FROM tmp_binary_storage_v2_ids i
		JOIN binaries b ON b.id = i.binary_id`); err != nil {
		return fmt.Errorf("stage binary storage v2 rows: %w", err)
	}
	return upsertBinaryStorageV2FromStagedBinaries(ctx, runner)
}
