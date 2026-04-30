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
		  AND ah.assembled_at < NOW() - INTERVAL '7 days'`); err != nil {
		return nil, fmt.Errorf("purge old article header payloads: %w", err)
	} else if result.PurgedHeaderPayloads, err = res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("purge old article header payloads rows affected: %w", err)
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
