package pgindex

import (
	"context"
	"database/sql"
	"fmt"
)

type MaintenanceTaskResult struct {
	TaskKey              string           `json:"task_key"`
	DryRun               bool             `json:"dry_run"`
	EstimatedRowsByTable map[string]int64 `json:"estimated_rows_by_table,omitempty"`
	DeletedRowsByTable   map[string]int64 `json:"deleted_rows_by_table,omitempty"`
	EstimatedBytes       int64            `json:"estimated_bytes,omitempty"`
	Blockers             []string         `json:"blockers,omitempty"`
	Warnings             []string         `json:"warnings,omitempty"`
}

func (s *Store) DryRunReleaseSourcePurge(ctx context.Context, limit int, policy ReleaseReadyPolicy) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	candidates, err := s.ClaimReleasePurgeCandidates(ctx, limit, policy)
	if err != nil {
		return nil, err
	}
	result := &MaintenanceTaskResult{
		TaskKey:              "release_source_purge",
		DryRun:               true,
		EstimatedRowsByTable: map[string]int64{"release_archive_state": int64(len(candidates))},
		Warnings:             []string{"article headers are only purgeable when no remaining binary parts reference them"},
	}
	for _, candidate := range candidates {
		estimate, err := s.estimateArchivedReleaseSourcePurge(ctx, candidate.ReleaseID)
		if err != nil {
			result.Blockers = append(result.Blockers, err.Error())
			continue
		}
		for table, count := range estimate.DeletedRowsByTable {
			result.EstimatedRowsByTable[table] += count
		}
	}
	return result, nil
}

func (s *Store) RunReleaseSourcePurge(ctx context.Context, limit int, policy ReleaseReadyPolicy) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	candidates, err := s.ClaimReleasePurgeCandidates(ctx, limit, policy)
	if err != nil {
		return nil, err
	}
	result := &MaintenanceTaskResult{
		TaskKey:            "release_source_purge",
		DryRun:             false,
		DeletedRowsByTable: map[string]int64{"release_archive_state": 0},
		Warnings:           []string{"article headers are only purged when no remaining binary parts reference them"},
	}
	for _, candidate := range candidates {
		purged, err := s.PurgeArchivedReleaseSources(ctx, candidate.ReleaseID)
		if err != nil {
			result.Blockers = append(result.Blockers, err.Error())
			continue
		}
		if purged == nil {
			continue
		}
		result.DeletedRowsByTable["release_archive_state"]++
		for table, count := range purged.DeletedRowsByTable {
			result.DeletedRowsByTable[table] += count
		}
	}
	return result, nil
}

func (s *Store) estimateArchivedReleaseSourcePurge(ctx context.Context, releaseID string) (*ReleasePurgeResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin purge dry-run tx: %w", err)
	}
	defer rollbackTx(tx)

	result := &ReleasePurgeResult{
		ReleaseID:          releaseID,
		DeletedRowsByTable: map[string]int64{},
	}
	if err := estimateArchivedReleaseSourcesTx(ctx, tx, releaseID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func estimateArchivedReleaseSourcesTx(ctx context.Context, tx *sql.Tx, releaseID string, result *ReleasePurgeResult) error {
	preflight, err := loadReleasePurgePreflight(ctx, tx, releaseID, false)
	if err != nil {
		return err
	}
	if preflight.ArchiveStatus != "purge_pending" {
		return fmt.Errorf("release %s is not purge_pending", releaseID)
	}
	if preflight.ObjectKey == "" || !preflight.ReleaseExists || !preflight.HasCatalogFiles || !preflight.HasCompletedMediaInspect {
		return fmt.Errorf("release %s does not satisfy purge preflight", releaseID)
	}

	var totalLineageBinaries int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM release_archive_lineage_binaries WHERE release_id = $1`, releaseID).Scan(&totalLineageBinaries); err != nil {
		return fmt.Errorf("count release lineage binaries %s: %w", releaseID, err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT lb.binary_id
		FROM release_archive_lineage_binaries lb
		WHERE lb.release_id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM release_files other_rf
			LEFT JOIN release_archive_state other_ras ON other_ras.release_id = other_rf.release_id
			WHERE other_rf.binary_id = lb.binary_id
			  AND other_rf.release_id <> $1
			  AND COALESCE(other_ras.archive_status, 'active') NOT IN ('archived', 'purge_pending', 'purged')
		  )`, releaseID)
	if err != nil {
		return fmt.Errorf("list purgeable binaries %s: %w", releaseID, err)
	}
	defer rows.Close()
	binaryIDs := make([]int64, 0, 64)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			return fmt.Errorf("scan purgeable binary id: %w", err)
		}
		binaryIDs = append(binaryIDs, binaryID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate purgeable binary ids: %w", err)
	}
	result.SkippedSharedBinaryRows = totalLineageBinaries - int64(len(binaryIDs))
	if len(binaryIDs) > 0 {
		if err := stageReleasePurgeBinaryIDs(ctx, tx, binaryIDs); err != nil {
			return fmt.Errorf("stage purgeable binary ids for %s: %w", releaseID, err)
		}
		for _, table := range []string{"binary_parts", "binary_grouping_evidence", "binary_inspections", "binary_inspection_artifacts", "binary_archive_entries", "binary_text_evidence", "binary_media_streams", "binary_par2_sets", "binary_par2_targets", "yenc_recovery_work_items"} {
			count, err := countRowsByStagedBinaryIDs(ctx, tx, table)
			if err != nil {
				return fmt.Errorf("count %s rows for %s: %w", table, releaseID, err)
			}
			result.DeletedRowsByTable[table] = count
		}
		result.DeletedRowsByTable["binary_core"] = int64(len(binaryIDs))
		result.DeletedBinaryRows = int64(len(binaryIDs))
	}
	countQueries := map[string]string{
		"release_files":      `SELECT COUNT(*) FROM release_files WHERE release_id = $1`,
		"release_newsgroups": `SELECT COUNT(*) FROM release_newsgroups WHERE release_id = $1`,
		"nzb_cache":          `SELECT COUNT(*) FROM nzb_cache WHERE release_id = $1`,
		"article_header_ingest_payloads": `
			SELECT COUNT(*)
			FROM article_header_ingest_payloads p
			WHERE p.article_header_id IN (
				SELECT lah.article_header_id
				FROM release_archive_lineage_article_headers lah
				WHERE lah.release_id = $1
			)
			  AND NOT EXISTS (
				SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = p.article_header_id
			  )`,
		"article_headers": `
			SELECT COUNT(*)
			FROM article_headers ah
			WHERE ah.id IN (
				SELECT lah.article_header_id
				FROM release_archive_lineage_article_headers lah
				WHERE lah.release_id = $1
			)
			  AND NOT EXISTS (
				SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = ah.id
			  )`,
		"release_archive_lineage_binaries":        `SELECT COUNT(*) FROM release_archive_lineage_binaries WHERE release_id = $1`,
		"release_archive_lineage_article_headers": `SELECT COUNT(*) FROM release_archive_lineage_article_headers WHERE release_id = $1`,
	}
	for table, query := range countQueries {
		var count int64
		if err := tx.QueryRowContext(ctx, query, releaseID).Scan(&count); err != nil {
			return fmt.Errorf("count %s rows for %s: %w", table, releaseID, err)
		}
		result.DeletedRowsByTable[table] = count
	}
	result.DeletedArticlePayloadRows = result.DeletedRowsByTable["article_header_ingest_payloads"]
	result.DeletedArticleHeaderRows = result.DeletedRowsByTable["article_headers"]
	return nil
}

func (s *Store) DryRunSimpleMaintenanceTask(ctx context.Context, taskKey string) (*MaintenanceTaskResult, error) {
	return s.runSimpleMaintenanceTask(ctx, taskKey, true)
}

func (s *Store) RunSimpleMaintenanceTask(ctx context.Context, taskKey string) (*MaintenanceTaskResult, error) {
	return s.runSimpleMaintenanceTask(ctx, taskKey, false)
}

func (s *Store) runSimpleMaintenanceTask(ctx context.Context, taskKey string, dryRun bool) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	result := &MaintenanceTaskResult{TaskKey: taskKey, DryRun: dryRun}
	if dryRun {
		result.EstimatedRowsByTable = map[string]int64{}
	} else {
		result.DeletedRowsByTable = map[string]int64{}
	}
	add := func(table string, count int64) {
		if dryRun {
			result.EstimatedRowsByTable[table] += count
		} else {
			result.DeletedRowsByTable[table] += count
		}
	}
	switch taskKey {
	case "assembly_queue_stale_cleanup":
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, `FROM article_header_assembly_queue q WHERE EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = q.article_header_id)`)
			if err != nil {
				return nil, err
			}
			add("article_header_assembly_queue", count)
		} else {
			count, err := execDeleteCount(ctx, s.db, `DELETE FROM article_header_assembly_queue q WHERE EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.article_header_id = q.article_header_id)`)
			if err != nil {
				return nil, err
			}
			add("article_header_assembly_queue", count)
		}
	case "runtime_history_cleanup":
		for table, query := range map[string]string{
			"indexer_stage_runs": `FROM indexer_stage_runs WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days') OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')`,
			"scrape_runs":        `FROM scrape_runs WHERE (status IN ('completed', 'abandoned') AND started_at < NOW() - INTERVAL '14 days') OR (status = 'failed' AND started_at < NOW() - INTERVAL '30 days')`,
			"binary_inspections": `FROM binary_inspections WHERE (status = 'completed' AND updated_at < NOW() - INTERVAL '14 days') OR (status = 'failed' AND updated_at < NOW() - INTERVAL '30 days')`,
		} {
			var (
				count int64
				err   error
			)
			if dryRun {
				count, err = countRowsFrom(ctx, s.db, query)
			} else {
				count, err = execDeleteCount(ctx, s.db, "DELETE "+query)
			}
			if err != nil {
				return nil, err
			}
			add(table, count)
		}
	case "grouping_evidence_cleanup":
		from := `FROM binary_grouping_evidence bge JOIN binary_identity_current bic ON bic.binary_id = bge.binary_id WHERE bge.updated_at < NOW() - INTERVAL '24 hours' AND bic.match_confidence >= 0.85 AND LOWER(COALESCE(bic.identity_strength, '')) NOT IN ('weak', 'provisional') AND LOWER(COALESCE(bic.family_kind, '')) NOT IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set') AND COALESCE((bge.payload_json->'summary'->>'fallback_used')::boolean, false) = false AND bge.payload_json ? 'summary'`
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, from)
			if err != nil {
				return nil, err
			}
			add("binary_grouping_evidence", count)
		} else {
			count, err := execDeleteCount(ctx, s.db, `WITH eligible AS (SELECT bge.binary_id `+from+`) DELETE FROM binary_grouping_evidence bge USING eligible e WHERE bge.binary_id = e.binary_id`)
			if err != nil {
				return nil, err
			}
			add("binary_grouping_evidence", count)
		}
	case "header_payload_purge":
		if dryRun {
			count, err := countRowsFrom(ctx, s.db, `FROM article_header_ingest_payloads`)
			if err != nil {
				return nil, err
			}
			add("article_header_ingest_payloads", count)
			result.Warnings = []string{"manual payload purge is destructive and intentionally disabled by default", "dry-run count is an upper-bound of retained payload rows"}
		} else {
			count, err := s.PurgeArticleHeaderPayloads(ctx)
			if err != nil {
				return nil, err
			}
			add("article_header_ingest_payloads", count)
		}
	default:
		return nil, fmt.Errorf("unsupported maintenance task %q", taskKey)
	}
	return result, nil
}

func countRowsFrom(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, from string, args ...any) (int64, error) {
	var count int64
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) `+from, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
