package pgindex

import (
	"context"
	"fmt"
)

const articleHeaderPayloadPurgeWindowSize = 250000
const releaseCatalogFilesBackfillBatchSize = 500
const staleBinaryObservationRepairBatchSize = 1000

type IndexerMaintenanceResult struct {
	AbandonedStageRuns         int64
	ClearedStageLeases         int64
	AbandonedScrapeRuns        int64
	AbandonedBinaryInspections int64
	RepairedBinaryObservations int64
	YEncWorkItemsUpserted      int64
	YEncWorkItemsRetired       int64
	InspectDiscoveryReadyRows  int64
	InspectDiscoveryRetired    int64
	InspectDiscoveryRequeued   int64
	InspectPAR2ReadyRows       int64
	InspectPAR2Retired         int64
	InspectPAR2Requeued        int64
	InspectArchiveReadyRows    int64
	InspectArchiveRetired      int64
	InspectArchiveRequeued     int64
	InspectMediaReadyRows      int64
	InspectMediaRetired        int64
	InspectMediaRequeued       int64
	BackfilledCatalogFiles     int64
	PurgedStageRuns            int64
	PurgedScrapeRuns           int64
	PurgedBinaryInspections    int64
	PurgedHeaderPayloads       int64
	PurgedGroupingEvidence     int64
	PurgedReadinessSummaries   int64
	PurgedOrphanReleases       int64
	SkippedReadinessCleanup    bool
	RefreshQueueBacklog        int64
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

	for {
		backfilled, err := s.BackfillMissingReleaseCatalogFiles(ctx, releaseCatalogFilesBackfillBatchSize)
		if err != nil {
			return nil, err
		}
		result.BackfilledCatalogFiles += backfilled
		if backfilled < releaseCatalogFilesBackfillBatchSize {
			break
		}
	}

	repaired, err := s.RepairStaleBinaryObservationStats(ctx, staleBinaryObservationRepairBatchSize)
	if err != nil {
		return nil, err
	}
	result.RepairedBinaryObservations = repaired

	if err := s.runIndexerMaintenanceMetadataCleanup(ctx, result); err != nil {
		return nil, err
	}

	if err := s.runIndexerMaintenanceDerivedCleanup(ctx, result); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Store) PurgeArticleHeaderPayloads(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}
	return s.purgeArticleHeaderPayloadsInBatches(ctx, articleHeaderPayloadPurgeWindowSize)
}

func (s *Store) runIndexerMaintenanceMetadataCleanup(ctx context.Context, result *IndexerMaintenanceResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin indexer maintenance metadata tx: %w", err)
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
		return fmt.Errorf("abandon stale scrape runs: %w", err)
	} else if result.AbandonedScrapeRuns, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("abandon stale scrape runs rows affected: %w", err)
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
		  AND (
		  	inspection_claimed_until IS NULL OR
		  	inspection_claimed_until < NOW()
		  )
		  AND updated_at < NOW() - INTERVAL '2 minutes'`); err != nil {
		return fmt.Errorf("abandon stale binary inspections: %w", err)
	} else if result.AbandonedBinaryInspections, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("abandon stale binary inspections rows affected: %w", err)
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
		return fmt.Errorf("purge old indexer stage runs: %w", err)
	} else if result.PurgedStageRuns, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("purge old indexer stage runs rows affected: %w", err)
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
		return fmt.Errorf("purge old scrape runs: %w", err)
	} else if result.PurgedScrapeRuns, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("purge old scrape runs rows affected: %w", err)
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
		return fmt.Errorf("purge old binary inspections: %w", err)
	} else if result.PurgedBinaryInspections, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("purge old binary inspections rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit indexer maintenance metadata tx: %w", err)
	}
	return nil
}

func (s *Store) runIndexerMaintenanceDerivedCleanup(ctx context.Context, result *IndexerMaintenanceResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin indexer maintenance derived tx: %w", err)
	}
	defer rollbackTx(tx)

	if res, err := tx.ExecContext(ctx, `
		WITH eligible AS (
			SELECT
				bge.binary_id
			FROM binary_grouping_evidence bge
			JOIN binary_identity_current bic ON bic.binary_id = bge.binary_id
			WHERE bge.updated_at < NOW() - INTERVAL '24 hours'
			  AND bic.match_confidence >= 0.85
			  AND LOWER(COALESCE(bic.identity_strength, '')) NOT IN ('weak', 'provisional')
			  AND LOWER(COALESCE(bic.family_kind, '')) NOT IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			  AND COALESCE((bge.payload_json->'summary'->>'fallback_used')::boolean, false) = false
			  AND bge.payload_json ? 'summary'
		)
		DELETE FROM binary_grouping_evidence bge
		USING eligible e
		WHERE bge.binary_id = e.binary_id`); err != nil {
		return fmt.Errorf("purge old stable grouping evidence: %w", err)
	} else if result.PurgedGroupingEvidence, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("purge old stable grouping evidence rows affected: %w", err)
	}

	var refreshQueueBacklog int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_summary_refresh_queue`).Scan(&refreshQueueBacklog); err != nil {
		return fmt.Errorf("count queued release family summaries before derived cleanup: %w", err)
	}
	result.RefreshQueueBacklog = refreshQueueBacklog
	if refreshQueueBacklog > 0 {
		result.SkippedReadinessCleanup = true
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM release_ready_candidate_acks a
			WHERE NOT EXISTS (
				SELECT 1
				FROM release_ready_candidates c
				WHERE c.provider_id = a.provider_id
				  AND c.newsgroup_id = a.newsgroup_id
				  AND c.key_kind = a.key_kind
				  AND c.family_key = a.family_key
			)`); err != nil {
			return fmt.Errorf("purge orphaned release ready candidate acks while readiness cleanup deferred: %w", err)
		}
		if res, err := tx.ExecContext(ctx, `
			DELETE FROM releases r
			WHERE NOT EXISTS (
				SELECT 1
				FROM release_catalog_files cf
				WHERE cf.release_id = r.release_id
			)
			  AND NOT EXISTS (
				SELECT 1
				FROM release_archive_state ras
				WHERE ras.release_id = r.release_id
				  AND ras.archive_status IN ('purge_pending', 'purged')
			)`); err != nil {
			return fmt.Errorf("purge orphan releases while readiness cleanup deferred: %w", err)
		} else if result.PurgedOrphanReleases, err = res.RowsAffected(); err != nil {
			return fmt.Errorf("purge orphan releases while readiness cleanup deferred rows affected: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit indexer maintenance derived tx with deferred readiness cleanup: %w", err)
		}
		return nil
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries s
		USING release_family_readiness_acks a
		WHERE s.readiness_bucket = $1
		  AND a.provider_id = s.provider_id
		  AND a.newsgroup_id = s.newsgroup_id
		  AND a.key_kind = s.key_kind
		  AND a.family_key = s.family_key
		  AND s.updated_at <= a.processed_at
		  AND s.updated_at < NOW() - INTERVAL '6 hours'`,
		releaseReadinessPreferBaseStem,
	); err != nil {
		return fmt.Errorf("purge old prefer_base_stem readiness summaries: %w", err)
	} else if rows, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("purge old prefer_base_stem readiness summaries rows affected: %w", rowsErr)
	} else {
		result.PurgedReadinessSummaries += rows
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries s
		USING release_family_readiness_acks a
		WHERE s.readiness_bucket IN ($1, $2)
		  AND a.provider_id = s.provider_id
		  AND a.newsgroup_id = s.newsgroup_id
		  AND a.key_kind = s.key_kind
		  AND a.family_key = s.family_key
		  AND s.updated_at <= a.processed_at
		  AND s.updated_at < NOW() - INTERVAL '24 hours'`,
		releaseReadinessFragmentOnly,
		releaseReadinessStaleCleanupOnly,
	); err != nil {
		return fmt.Errorf("purge old fragment/stale readiness summaries: %w", err)
	} else if rows, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("purge old fragment/stale readiness summaries rows affected: %w", rowsErr)
	} else {
		result.PurgedReadinessSummaries += rows
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries s
		USING release_family_readiness_acks a
		WHERE s.readiness_bucket IN ($1, $2, $3)
		  AND a.provider_id = s.provider_id
		  AND a.newsgroup_id = s.newsgroup_id
		  AND a.key_kind = s.key_kind
		  AND a.family_key = s.family_key
		  AND s.updated_at <= a.processed_at
		  AND s.updated_at < NOW() - INTERVAL '24 hours'
		  AND NOT EXISTS (
		  	SELECT 1
			    FROM binary_identity_current bic
			    LEFT JOIN binary_recovery_current brc ON brc.binary_id = bic.binary_id
			    WHERE bic.provider_id = s.provider_id
			      AND bic.newsgroup_id = s.newsgroup_id
		  	  AND s.key_kind = 'release_family'
			      AND bic.release_family_key = s.family_key
			      AND LOWER(COALESCE(bic.family_kind, '')) IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set')
			      AND bic.is_main_payload = TRUE
			      AND COALESCE(brc.recovered_source, '') <> 'yenc_header'
		  )`,
		releaseReadinessWeakSingle,
		releaseReadinessWeakObfuscated,
		releaseReadinessOvergrouped,
	); err != nil {
		return fmt.Errorf("purge old weak readiness summaries: %w", err)
	} else if rows, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("purge old weak readiness summaries rows affected: %w", rowsErr)
	} else {
		result.PurgedReadinessSummaries += rows
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_family_readiness_acks a
		WHERE NOT EXISTS (
			SELECT 1
			FROM release_family_readiness_summaries s
			WHERE s.provider_id = a.provider_id
			  AND s.newsgroup_id = a.newsgroup_id
			  AND s.key_kind = a.key_kind
			  AND s.family_key = a.family_key
		)`); err != nil {
		return fmt.Errorf("purge orphaned release readiness acks: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_ready_candidates c
		WHERE (
			c.key_kind = 'recovered_file_set'
			AND NOT EXISTS (
				SELECT 1
				FROM release_recovered_file_set_candidates r
				WHERE r.provider_id = c.provider_id
				  AND r.representative_newsgroup_id = c.newsgroup_id
				  AND r.file_set_key = c.family_key
				  AND COALESCE(r.readiness_bucket, '') = $1
			)
		) OR (
			c.key_kind <> 'recovered_file_set'
			AND NOT EXISTS (
				SELECT 1
				FROM release_family_readiness_summaries s
				WHERE s.provider_id = c.provider_id
				  AND s.newsgroup_id = c.newsgroup_id
				  AND s.key_kind = c.key_kind
				  AND s.family_key = c.family_key
				  AND COALESCE(s.readiness_bucket, '') = $1
			)
		)`,
		releaseReadinessActionable,
	); err != nil {
		return fmt.Errorf("purge stale release ready candidates: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_ready_candidate_acks a
		WHERE NOT EXISTS (
			SELECT 1
			FROM release_ready_candidates c
			WHERE c.provider_id = a.provider_id
			  AND c.newsgroup_id = a.newsgroup_id
			  AND c.key_kind = a.key_kind
			  AND c.family_key = a.family_key
		)`); err != nil {
		return fmt.Errorf("purge orphaned release ready candidate acks: %w", err)
	}

	if res, err := tx.ExecContext(ctx, `
		DELETE FROM releases r
		WHERE NOT EXISTS (
			SELECT 1
			FROM release_catalog_files cf
			WHERE cf.release_id = r.release_id
		)
		  AND NOT EXISTS (
			SELECT 1
			FROM release_archive_state ras
			WHERE ras.release_id = r.release_id
			  AND ras.archive_status IN ('purge_pending', 'purged')
		)`); err != nil {
		return fmt.Errorf("purge orphan releases: %w", err)
	} else if result.PurgedOrphanReleases, err = res.RowsAffected(); err != nil {
		return fmt.Errorf("purge orphan releases rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit indexer maintenance derived tx: %w", err)
	}
	return nil
}

func (s *Store) purgeArticleHeaderPayloadsInBatches(ctx context.Context, batchSize int64) (int64, error) {
	if batchSize <= 0 {
		batchSize = articleHeaderPayloadPurgeWindowSize
	}

	var maxID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(article_header_id), 0)
		FROM article_header_ingest_payloads`).Scan(&maxID); err != nil {
		return 0, fmt.Errorf("query max article header payload id: %w", err)
	}

	var total int64
	for low := int64(0); low < maxID; low += batchSize {
		high := low + batchSize
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM article_header_ingest_payloads p
			USING article_headers ah
			WHERE p.article_header_id > $1
			  AND p.article_header_id <= $2
			  AND ah.id = p.article_header_id
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
			  )`,
			low,
			high,
		)
		if err != nil {
			return total, fmt.Errorf("purge old article header payloads: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("purge old article header payloads rows affected: %w", err)
		}
		total += affected
	}
	return total, nil
}
