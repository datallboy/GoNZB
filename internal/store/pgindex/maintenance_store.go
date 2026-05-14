package pgindex

import (
	"context"
	"fmt"
)

type IndexerMaintenanceResult struct {
	AbandonedStageRuns         int64
	ClearedStageLeases         int64
	AbandonedScrapeRuns        int64
	AbandonedBinaryInspections int64
	PurgedStageRuns            int64
	PurgedScrapeRuns           int64
	PurgedBinaryInspections    int64
	PurgedHeaderPayloads       int64
	PurgedReadinessSummaries   int64
	PurgedOrphanReleases       int64
}

func (s *Store) RunIndexerMaintenance(ctx context.Context) (*IndexerMaintenanceResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	result := &IndexerMaintenanceResult{}

	repair, err := s.RepairIndexerStageRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if repair != nil {
		result.AbandonedStageRuns = repair.AbandonedRuns
		result.ClearedStageLeases = repair.ClearedStaleLeases
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin indexer maintenance tx: %w", err)
	}
	defer rollbackTx(tx)

	if res, err := tx.ExecContext(ctx, `
		UPDATE scrape_runs
		SET status = 'abandoned',
		    error_text = CASE
		    	WHEN error_text = '' THEN 'maintenance marked stale running scrape'
		    	ELSE error_text
		    END,
		    finished_at = NOW()
		WHERE status = 'running'
		  AND finished_at IS NULL
		  AND started_at < NOW() - INTERVAL '1 hour'`); err != nil {
		return nil, fmt.Errorf("abandon stale scrape runs: %w", err)
	} else if result.AbandonedScrapeRuns, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("abandon stale scrape runs rows affected: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		UPDATE binary_inspections
		SET status = 'failed',
		    finished_at = NOW(),
		    error_text = CASE
		    	WHEN error_text = '' THEN 'maintenance marked stale running inspection'
		    	ELSE error_text
		    END,
		    inspection_claimed_by = '',
		    inspection_claimed_until = NULL,
		    updated_at = NOW()
		WHERE status = 'running'
		  AND updated_at < NOW() - INTERVAL '15 minutes'`); err != nil {
		return nil, fmt.Errorf("abandon stale binary inspections: %w", err)
	} else if result.AbandonedBinaryInspections, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("abandon stale binary inspections rows affected: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM indexer_stage_runs
		WHERE (
			status IN ('completed', 'abandoned')
			AND started_at < NOW() - INTERVAL '14 days'
		) OR (
			status = 'failed'
			AND started_at < NOW() - INTERVAL '30 days'
		)`); err != nil {
		return nil, fmt.Errorf("purge old indexer stage runs: %w", err)
	} else if result.PurgedStageRuns, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("purge old indexer stage runs rows affected: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM scrape_runs
		WHERE (
			status IN ('completed', 'abandoned')
			AND started_at < NOW() - INTERVAL '14 days'
		) OR (
			status = 'failed'
			AND started_at < NOW() - INTERVAL '30 days'
		)`); err != nil {
		return nil, fmt.Errorf("purge old scrape runs: %w", err)
	} else if result.PurgedScrapeRuns, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("purge old scrape runs rows affected: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM binary_inspections
		WHERE (
			status = 'completed'
			AND updated_at < NOW() - INTERVAL '14 days'
		) OR (
			status = 'failed'
			AND updated_at < NOW() - INTERVAL '30 days'
		)`); err != nil {
		return nil, fmt.Errorf("purge old binary inspections: %w", err)
	} else if result.PurgedBinaryInspections, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("purge old binary inspections rows affected: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM article_header_ingest_payloads p
		USING article_headers ah
		WHERE ah.id = p.article_header_id
		  AND ah.assembled_at IS NOT NULL
		  AND (
		  	(
		  		ah.assembled_at < NOW() - INTERVAL '1 hour'
		  		AND COALESCE(BTRIM(p.subject_file_name), '') <> ''
		  		AND COALESCE(p.yenc_recovery_missing_count, 0) = 0
		  		AND p.yenc_recovery_retry_after IS NULL
		  	) OR (
		  		ah.assembled_at < NOW() - INTERVAL '24 hours'
		  		AND (
		  			COALESCE(BTRIM(p.subject_file_name), '') = ''
		  			OR COALESCE(p.yenc_recovery_missing_count, 0) > 0
		  			OR p.yenc_recovery_retry_after IS NOT NULL
		  		)
		  	)
		  )`); err != nil {
		return nil, fmt.Errorf("purge old article header payloads: %w", err)
	} else if result.PurgedHeaderPayloads, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("purge old article header payloads rows affected: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries s
		WHERE s.readiness_bucket = $1
		  AND s.updated_at <= COALESCE(s.processed_at, s.updated_at)
		  AND s.updated_at < NOW() - INTERVAL '6 hours'`,
		releaseReadinessPreferBaseStem,
	); err != nil {
		return nil, fmt.Errorf("purge old prefer_base_stem readiness summaries: %w", err)
	} else if rows, rowsErr := res.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("purge old prefer_base_stem readiness summaries rows affected: %w", rowsErr)
	} else {
		result.PurgedReadinessSummaries += rows
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries s
		WHERE s.readiness_bucket IN ($1, $2)
		  AND s.updated_at <= COALESCE(s.processed_at, s.updated_at)
		  AND s.updated_at < NOW() - INTERVAL '24 hours'`,
		releaseReadinessFragmentOnly,
		releaseReadinessStaleCleanupOnly,
	); err != nil {
		return nil, fmt.Errorf("purge old fragment/stale readiness summaries: %w", err)
	} else if rows, rowsErr := res.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("purge old fragment/stale readiness summaries rows affected: %w", rowsErr)
	} else {
		result.PurgedReadinessSummaries += rows
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries s
		WHERE s.readiness_bucket IN ($1, $2, $3)
		  AND s.updated_at <= COALESCE(s.processed_at, s.updated_at)
		  AND s.updated_at < NOW() - INTERVAL '24 hours'
		  AND NOT EXISTS (
		  	SELECT 1
		  	FROM binaries b
		  	WHERE b.provider_id = s.provider_id
		  	  AND b.newsgroup_id = s.newsgroup_id
		  	  AND s.key_kind = 'release_family'
		  	  AND b.release_family_key = s.family_key
		  	  AND LOWER(COALESCE(b.family_kind, '')) IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
		  	  AND b.is_main_payload = TRUE
		  	  AND COALESCE(b.recovered_source, '') <> 'yenc_header'
		  )`,
		releaseReadinessWeakSingle,
		releaseReadinessWeakObfuscated,
		releaseReadinessOvergrouped,
	); err != nil {
		return nil, fmt.Errorf("purge old weak readiness summaries: %w", err)
	} else if rows, rowsErr := res.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("purge old weak readiness summaries rows affected: %w", rowsErr)
	} else {
		result.PurgedReadinessSummaries += rows
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM releases r
		WHERE NOT EXISTS (
			SELECT 1
			FROM release_files rf
			WHERE rf.release_id = r.release_id
		)`); err != nil {
		return nil, fmt.Errorf("purge orphan releases: %w", err)
	} else if result.PurgedOrphanReleases, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("purge orphan releases rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit indexer maintenance tx: %w", err)
	}

	return result, nil
}
