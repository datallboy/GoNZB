package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const inspectDiscoveryReadyQueueSeedLimit = 10000
const inspectReadyQueueSeedLimit = 10000

func isQueuedInspectionStage(stageName string) bool {
	switch strings.TrimSpace(stageName) {
	case "inspect_discovery", "inspect_par2", "inspect_archive", "inspect_media":
		return true
	default:
		return false
	}
}

type BinaryInspectionReadyQueueRefreshResult struct {
	ReadyUpserted int64
	Retired       int64
	Requeued      int64
}

func (s *Store) shouldBackoffInspectDiscoverySeed(now time.Time) bool {
	s.inspectDiscoverySeedMu.Lock()
	defer s.inspectDiscoverySeedMu.Unlock()
	return !s.inspectDiscoverySeedBackoffUntil.IsZero() && now.Before(s.inspectDiscoverySeedBackoffUntil)
}

func (s *Store) clearInspectDiscoverySeedBackoff() {
	s.inspectDiscoverySeedMu.Lock()
	defer s.inspectDiscoverySeedMu.Unlock()
	s.inspectDiscoverySeedConsecutiveEmpty = 0
	s.inspectDiscoverySeedBackoffUntil = time.Time{}
}

func (s *Store) recordInspectDiscoverySeedResult(now time.Time, upserted int64) {
	s.inspectDiscoverySeedMu.Lock()
	defer s.inspectDiscoverySeedMu.Unlock()
	if upserted > 0 {
		s.inspectDiscoverySeedConsecutiveEmpty = 0
		s.inspectDiscoverySeedBackoffUntil = time.Time{}
		return
	}

	s.inspectDiscoverySeedConsecutiveEmpty++
	var backoff time.Duration
	switch s.inspectDiscoverySeedConsecutiveEmpty {
	case 1:
		backoff = time.Minute
	case 2:
		backoff = 5 * time.Minute
	default:
		backoff = 15 * time.Minute
	}
	s.inspectDiscoverySeedBackoffUntil = now.Add(backoff)
}

func (s *Store) RefreshInspectDiscoveryReadyQueue(ctx context.Context, limit int) (*BinaryInspectionReadyQueueRefreshResult, error) {
	return s.refreshInspectionReadyQueueFromReconcile(ctx, "inspect_discovery", limit)
}

func (s *Store) RefreshInspectionReadyQueue(ctx context.Context, stageName string, limit int) (*BinaryInspectionReadyQueueRefreshResult, error) {
	stageName = strings.TrimSpace(stageName)
	if !isQueuedInspectionStage(stageName) {
		return nil, fmt.Errorf("inspection stage %q does not use ready queue", stageName)
	}
	return s.refreshInspectionReadyQueueFromReconcile(ctx, stageName, limit)
}

type inspectionReleaseCursor struct {
	UpdatedAt time.Time
	ReleaseID string
}

type inspectionReleaseBatch struct {
	ReleaseIDs []string
	BinaryIDs  []int64
	Last       inspectionReleaseCursor
}

func (s *Store) refreshInspectionReadyQueueFromReconcile(ctx context.Context, stageName string, limit int) (*BinaryInspectionReadyQueueRefreshResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = inspectReadyQueueSeedLimit
	}
	preview, err := s.loadInspectionReleaseReconcileBatch(ctx, stageName, limit)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePartitionBundleForBinaryIDs(ctx, partitionBundleInspect, preview.BinaryIDs); err != nil {
		return nil, err
	}
	out := &BinaryInspectionReadyQueueRefreshResult{}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin %s ready queue refresh tx: %w", stageName, err)
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "gonzb-"+stageName+"-ready-reconcile"); err != nil {
			return fmt.Errorf("lock %s ready queue refresh: %w", stageName, err)
		}

		requeued, err := requeueStaleInspectReadyRows(ctx, tx, stageName)
		if err != nil {
			return err
		}
		out.Requeued = requeued

		if stageName == "inspect_discovery" {
			retired, err := retireIneligibleInspectDiscoveryReadyRows(ctx, tx)
			if err != nil {
				return err
			}
			out.Retired = retired
		}

		if _, err := tx.ExecContext(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
			return fmt.Errorf("disable parallel gather for %s reconcile: %w", stageName, err)
		}
		lockedBatch, err := loadInspectionReleaseReconcileBatchWithRunner(ctx, tx, stageName, limit)
		if err != nil {
			return err
		}
		if !sameInspectionReleaseBatch(preview, lockedBatch) {
			return fmt.Errorf("%s inspection reconcile cursor advanced concurrently", stageName)
		}
		if len(lockedBatch.ReleaseIDs) > 0 {
			upserted, err := enqueueInspectionReadyForReleases(ctx, tx, stageName, lockedBatch.ReleaseIDs)
			if err != nil {
				return err
			}
			out.ReadyUpserted = upserted
			if err := advanceInspectionReleaseReconcileCursor(ctx, tx, stageName, lockedBatch); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s ready queue refresh tx: %w", stageName, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) loadInspectionReleaseReconcileBatch(ctx context.Context, stageName string, limit int) (inspectionReleaseBatch, error) {
	return loadInspectionReleaseReconcileBatchWithRunner(ctx, s.db, stageName, limit)
}

func loadInspectionReleaseReconcileBatchWithRunner(ctx context.Context, runner sqlExecQueryRower, stageName string, limit int) (inspectionReleaseBatch, error) {
	if limit <= 0 {
		limit = inspectReadyQueueSeedLimit
	}
	var cursor inspectionReleaseCursor
	err := runner.QueryRowContext(ctx, `
		INSERT INTO indexer_inspection_reconcile_state (stage_name)
		VALUES ($1)
		ON CONFLICT (stage_name) DO UPDATE SET stage_name = EXCLUDED.stage_name
		RETURNING cursor_updated_at, cursor_release_id`, stageName).Scan(&cursor.UpdatedAt, &cursor.ReleaseID)
	if err != nil {
		return inspectionReleaseBatch{}, fmt.Errorf("load %s inspection reconcile cursor: %w", stageName, err)
	}

	rows, err := runner.QueryContext(ctx, `
		SELECT r.release_id, r.updated_at
		FROM releases r
		WHERE (r.updated_at, r.release_id) > ($1, $2)
		  AND r.source_kind = 'usenet_index'
		ORDER BY r.updated_at, r.release_id
		LIMIT $3`, cursor.UpdatedAt, cursor.ReleaseID, limit)
	if err != nil {
		return inspectionReleaseBatch{}, fmt.Errorf("list %s inspection reconcile releases: %w", stageName, err)
	}
	defer rows.Close()

	batch := inspectionReleaseBatch{}
	for rows.Next() {
		var releaseID string
		var updatedAt time.Time
		if err := rows.Scan(&releaseID, &updatedAt); err != nil {
			return inspectionReleaseBatch{}, fmt.Errorf("scan %s inspection reconcile release: %w", stageName, err)
		}
		batch.ReleaseIDs = append(batch.ReleaseIDs, releaseID)
		batch.Last = inspectionReleaseCursor{UpdatedAt: updatedAt.UTC(), ReleaseID: releaseID}
	}
	if err := rows.Err(); err != nil {
		return inspectionReleaseBatch{}, fmt.Errorf("iterate %s inspection reconcile releases: %w", stageName, err)
	}
	if len(batch.ReleaseIDs) == 0 {
		return batch, nil
	}

	placeholders, args := inspectionReleaseIDArgs(batch.ReleaseIDs, 0)
	binaryRows, err := runner.QueryContext(ctx, `
		SELECT DISTINCT rf.binary_id
		FROM release_files rf
		WHERE rf.release_id IN (`+placeholders+`)
		  AND rf.binary_id IS NOT NULL
		  AND rf.binary_id > 0
		ORDER BY rf.binary_id`, args...)
	if err != nil {
		return inspectionReleaseBatch{}, fmt.Errorf("list %s inspection reconcile binaries: %w", stageName, err)
	}
	defer binaryRows.Close()
	for binaryRows.Next() {
		var binaryID int64
		if err := binaryRows.Scan(&binaryID); err != nil {
			return inspectionReleaseBatch{}, fmt.Errorf("scan %s inspection reconcile binary: %w", stageName, err)
		}
		batch.BinaryIDs = append(batch.BinaryIDs, binaryID)
	}
	if err := binaryRows.Err(); err != nil {
		return inspectionReleaseBatch{}, fmt.Errorf("iterate %s inspection reconcile binaries: %w", stageName, err)
	}
	return batch, nil
}

func sameInspectionReleaseBatch(a, b inspectionReleaseBatch) bool {
	if len(a.ReleaseIDs) != len(b.ReleaseIDs) {
		return false
	}
	for i := range a.ReleaseIDs {
		if a.ReleaseIDs[i] != b.ReleaseIDs[i] {
			return false
		}
	}
	return true
}

func advanceInspectionReleaseReconcileCursor(ctx context.Context, tx *sql.Tx, stageName string, batch inspectionReleaseBatch) error {
	if len(batch.ReleaseIDs) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE indexer_inspection_reconcile_state
		SET cursor_updated_at = $2,
		    cursor_release_id = $3,
		    reconciled_count = reconciled_count + $4,
		    updated_at = NOW()
		WHERE stage_name = $1`,
		stageName, batch.Last.UpdatedAt, batch.Last.ReleaseID, len(batch.ReleaseIDs))
	if err != nil {
		return fmt.Errorf("advance %s inspection reconcile cursor: %w", stageName, err)
	}
	return nil
}

func (s *Store) CountInspectionReadyQueue(ctx context.Context, stageName string) (int64, error) {
	stageName = strings.TrimSpace(stageName)
	if !isQueuedInspectionStage(stageName) {
		return 0, fmt.Errorf("inspection stage %q does not use ready queue", stageName)
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_inspection_ready_queue q
		WHERE q.stage_name = $1
		  AND q.status = 'ready'
		  AND q.ready_at <= NOW()
		  AND (
			$1 <> 'inspect_discovery' OR EXISTS (
				SELECT 1
				FROM binary_parts bp
				WHERE bp.source_posted_at = q.source_posted_at
				  AND bp.binary_id = q.binary_id
				  AND bp.part_number = 1
			)
		  )`, stageName).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s ready queue: %w", stageName, err)
	}
	return count, nil
}

func (s *Store) CountInspectDiscoveryReadyQueue(ctx context.Context) (int64, error) {
	return s.CountInspectionReadyQueue(ctx, "inspect_discovery")
}

func requeueStaleInspectReadyRows(ctx context.Context, tx *sql.Tx, stageName string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE binary_inspection_ready_queue
		SET status = 'ready',
		    ready_at = NOW(),
		    claimed_by = '',
		    claimed_until = NULL,
		    updated_at = NOW()
		WHERE stage_name = $1
		  AND status = 'running'
		  AND (claimed_until IS NULL OR claimed_until < NOW())`, stageName)
	if err != nil {
		return 0, fmt.Errorf("requeue stale %s ready rows: %w", stageName, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("requeue stale %s ready rows affected: %w", stageName, err)
	}
	return rows, nil
}

func retireIneligibleInspectDiscoveryReadyRows(ctx context.Context, tx *sql.Tx) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		WITH retire AS (
			SELECT q.binary_id, q.source_posted_at
			FROM binary_inspection_ready_queue q
			LEFT JOIN binary_core bc ON bc.binary_id = q.binary_id
			LEFT JOIN binary_identity_current bic
			  ON bic.source_posted_at = q.source_posted_at
			 AND bic.binary_id = q.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = q.source_posted_at
			 AND brc.binary_id = q.binary_id
			LEFT JOIN binary_lifecycle bl
			  ON bl.source_posted_at = q.source_posted_at
			 AND bl.binary_id = q.binary_id
			LEFT JOIN binary_inspections cfi
				ON cfi.stage_name = 'inspect_discovery'
				AND cfi.binary_id = q.binary_id
				AND cfi.status = 'completed'
				AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
			WHERE q.stage_name = 'inspect_discovery'
			  AND q.status IN ('ready', 'running')
			  AND (
				bc.binary_id IS NULL OR
				bic.binary_id IS NULL OR
				COALESCE(brc.recovered_extension, '') <> '' OR
				COALESCE(bl.lifecycle_status, 'active') = 'superseded' OR
				EXISTS (
					SELECT 1
					FROM yenc_recovery_work_items wi
					WHERE wi.source_posted_at = q.source_posted_at
					  AND wi.binary_id = q.binary_id
					  AND wi.status IN ('ready', 'running')
				) OR
				cfi.id IS NOT NULL OR
				NOT (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE) OR
				NOT (
					LOWER(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.bin' OR
					COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
				)
			  )
		)
		UPDATE binary_inspection_ready_queue q
		SET status = 'completed',
		    claimed_by = '',
		    claimed_until = NULL,
		    updated_at = NOW()
		FROM retire r
		WHERE q.stage_name = 'inspect_discovery'
		  AND q.source_posted_at = r.source_posted_at
		  AND q.binary_id = r.binary_id`)
	if err != nil {
		return 0, fmt.Errorf("retire ineligible inspect_discovery ready rows: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("retire ineligible inspect_discovery rows affected: %w", err)
	}
	return rows, nil
}

func upsertInspectionReadyQueueCandidates(ctx context.Context, tx *sql.Tx, stageName string, candidates []BinaryInspectionCandidate) (int64, error) {
	stageName = strings.TrimSpace(stageName)
	if len(candidates) == 0 {
		return 0, nil
	}

	args := make([]any, 0, len(candidates)*4+1)
	args = append(args, stageName)
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		base := len(args)
		var sourceUpdated any
		if candidate.SourceUpdatedAt != nil {
			sourceUpdated = candidate.SourceUpdatedAt.UTC()
		}
		args = append(args, candidate.BinaryID, strings.TrimSpace(candidate.ReleaseID), sourceUpdated)
		values = append(values, fmt.Sprintf("($1,$%d::bigint,$%d::text,'ready'::text,NOW(),$%d::timestamptz,''::text,NULL::timestamptz,''::text,NOW())", base+1, base+2, base+3))
	}

	res, err := tx.ExecContext(ctx, fmt.Sprintf(`
		WITH staged(
			stage_name,
			binary_id,
			release_id,
			status,
			ready_at,
			source_updated_at,
			claimed_by,
			claimed_until,
			last_error,
			updated_at
		) AS (
			VALUES %s
		)
		INSERT INTO binary_inspection_ready_queue (
			stage_name,
			binary_id,
			release_id,
			status,
			ready_at,
			source_updated_at,
			source_posted_at,
			claimed_by,
			claimed_until,
			last_error,
			updated_at
		)
		SELECT
			s.stage_name,
			s.binary_id,
			s.release_id,
			s.status,
			s.ready_at,
			s.source_updated_at,
			COALESCE(bc.source_posted_at, bos.source_posted_at, bos.posted_at),
			s.claimed_by,
			s.claimed_until,
			s.last_error,
			s.updated_at
		FROM staged s
		LEFT JOIN binary_core bc ON bc.binary_id = s.binary_id
			LEFT JOIN binary_observation_stats bos
			  ON bos.binary_id = s.binary_id
			 AND bos.source_posted_at = COALESCE(bc.source_posted_at, bos.source_posted_at)
		WHERE to_regclass('public.binary_inspection_ready_queue_' || to_char(COALESCE(bc.source_posted_at, bos.source_posted_at, bos.posted_at) AT TIME ZONE 'UTC', 'YYYYMMDD')) IS NOT NULL
			ON CONFLICT (source_posted_at, stage_name, binary_id) DO UPDATE
			SET release_id = EXCLUDED.release_id,
			    source_updated_at = EXCLUDED.source_updated_at,
			    status = CASE
		    	WHEN binary_inspection_ready_queue.status = 'running'
		    	 AND binary_inspection_ready_queue.claimed_until IS NOT NULL
		    	 AND binary_inspection_ready_queue.claimed_until >= NOW()
		    	THEN binary_inspection_ready_queue.status
		    	ELSE 'ready'
		    END,
		    ready_at = CASE
		    	WHEN binary_inspection_ready_queue.status = 'running'
		    	 AND binary_inspection_ready_queue.claimed_until IS NOT NULL
		    	 AND binary_inspection_ready_queue.claimed_until >= NOW()
		    	THEN binary_inspection_ready_queue.ready_at
		    	ELSE NOW()
		    END,
		    claimed_by = CASE
		    	WHEN binary_inspection_ready_queue.status = 'running'
		    	 AND binary_inspection_ready_queue.claimed_until IS NOT NULL
		    	 AND binary_inspection_ready_queue.claimed_until >= NOW()
		    	THEN binary_inspection_ready_queue.claimed_by
		    	ELSE ''
		    END,
		    claimed_until = CASE
		    	WHEN binary_inspection_ready_queue.status = 'running'
		    	 AND binary_inspection_ready_queue.claimed_until IS NOT NULL
		    	 AND binary_inspection_ready_queue.claimed_until >= NOW()
		    	THEN binary_inspection_ready_queue.claimed_until
		    	ELSE NULL
		    END,
		    updated_at = NOW()`, strings.Join(values, ",")), args...)
	if err != nil {
		return 0, fmt.Errorf("upsert %s ready queue candidates: %w", stageName, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("upsert %s ready queue candidates rows affected: %w", stageName, err)
	}
	return rows, nil
}

func inspectionReleaseIDArgs(releaseIDs []string, offset int) (string, []any) {
	args := make([]any, 0, len(releaseIDs))
	placeholders := make([]string, 0, len(releaseIDs))
	for _, releaseID := range releaseIDs {
		releaseID = strings.TrimSpace(releaseID)
		if releaseID == "" {
			continue
		}
		args = append(args, releaseID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", offset+len(args)))
	}
	if len(placeholders) == 0 {
		return "NULL", nil
	}
	return strings.Join(placeholders, ","), args
}

// enqueueInspectionReadyForReleases is the normal producer path for queued
// inspections. Release persistence calls it immediately; the durable
// reconciliation cursor calls the same function to repair missed events.
func enqueueInspectionReadyForReleases(ctx context.Context, execer inspectionExecer, stageName string, releaseIDs []string) (int64, error) {
	if !isQueuedInspectionStage(stageName) {
		return 0, fmt.Errorf("inspection stage %q does not use ready queue", stageName)
	}
	placeholders, releaseArgs := inspectionReleaseIDArgs(releaseIDs, 1)
	if len(releaseArgs) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(releaseArgs)+1)
	args = append(args, stageName)
	args = append(args, releaseArgs...)

	stagePredicate := ""
	stageSelection := "SELECT * FROM eligible"
	stageRerunPredicate := "FALSE"
	switch stageName {
	case "inspect_discovery":
		stagePredicate = `
			AND sr.release_completion_pct >= 100
			AND (sr.expected_file_count <= 0 OR sr.release_file_count >= sr.expected_file_count)
			AND sr.recovered_extension = ''
			AND (sr.is_main_payload = TRUE OR sr.is_auxiliary = FALSE)
			AND (
				LOWER(sr.effective_file_name) LIKE '%.bin' OR
				sr.effective_file_name !~ '\.[A-Za-z0-9]{1,8}$'
			)
			AND EXISTS (
				SELECT 1 FROM binary_parts bp
				WHERE bp.source_posted_at = sr.source_posted_at
				  AND bp.binary_id = sr.binary_id
				  AND bp.part_number = 1
			)
			AND NOT EXISTS (
				SELECT 1 FROM yenc_recovery_work_items wi
				WHERE wi.source_posted_at = sr.source_posted_at
				  AND wi.binary_id = sr.binary_id
				  AND wi.status IN ('ready', 'running')
			)`
		stageSelection = `
			SELECT DISTINCT ON (release_id) *
			FROM eligible
			ORDER BY release_id, total_bytes DESC, file_index, binary_id`
	case "inspect_par2":
		stageRerunPredicate = `
			(
				bi.status = 'completed' AND
				NOT EXISTS (
					SELECT 1
					FROM binary_par2_targets bpt
					WHERE bpt.source_posted_at = bc.source_posted_at
					  AND bpt.binary_id = bc.binary_id
				) AND
				COALESCE(bi.summary_json->>'probe_skip_reason', '') <> 'article_not_found' AND
				NOT (
					COALESCE(bi.summary_json->>'target_count', '') ~ '^[0-9]+$' AND
					(bi.summary_json->>'target_count')::integer = 0
				)
			)`
		stagePredicate = `
			AND (
				sr.is_pars = TRUE OR
				LOWER(sr.effective_file_name) LIKE '%.par2' OR
				sr.recovered_kind = 'par2' OR
				sr.recovered_extension = '.par2'
			)`
		stageSelection = `
			SELECT DISTINCT ON (
				release_id,
				REGEXP_REPLACE(LOWER(effective_file_name), '\.vol[0-9]+(?:\+| )[0-9]+\.par2$', '.par2')
			) *
			FROM eligible
			ORDER BY
				release_id,
				REGEXP_REPLACE(LOWER(effective_file_name), '\.vol[0-9]+(?:\+| )[0-9]+\.par2$', '.par2'),
				CASE WHEN LOWER(effective_file_name) ~ '\.vol[0-9]+(?:\+| )[0-9]+\.par2$' THEN 1 ELSE 0 END,
				file_index,
				binary_id`
	case "inspect_archive":
		stageRerunPredicate = `
			COALESCE(bi.summary_json->>'probe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'extract_error', '') <> '' OR
			COALESCE(bi.summary_json->>'archive_extract_error', '') <> '' OR
			COALESCE(bi.summary_json->>'probe_error_detail', '') ILIKE '%has no articles%' OR
			(
				bi.status = 'completed' AND
				COALESCE(bi.summary_json->>'probe_strategy', '') = 'metadata_only' AND
				CASE
					WHEN jsonb_typeof(bi.summary_json->'archive_entries') = 'array'
					THEN jsonb_array_length(bi.summary_json->'archive_entries')
					ELSE 0
				END = 0
			)`
		stagePredicate = `
			AND sr.total_parts > 0
			AND sr.observed_parts >= sr.total_parts
			AND (sr.is_main_payload = TRUE OR sr.is_auxiliary = FALSE)
			AND (
				LOWER(sr.effective_file_name) ~ '\.7z\.001$' OR
				LOWER(sr.effective_file_name) ~ '\.zip\.001$' OR
				LOWER(sr.effective_file_name) ~ '\.part0*1\.rar$' OR
				LOWER(sr.effective_file_name) ~ '\.r00$' OR
				LOWER(sr.effective_file_name) LIKE '%.7z' OR
				LOWER(sr.effective_file_name) LIKE '%.zip' OR
				(
					LOWER(sr.effective_file_name) LIKE '%.rar' AND
					LOWER(sr.effective_file_name) !~ '\.part\d+\.rar$' AND
					LOWER(sr.effective_file_name) !~ '\.r\d{2,3}$'
				)
			)`
		stageSelection = `
			SELECT DISTINCT ON (
				release_id,
				CASE
					WHEN LOWER(effective_file_name) ~ '\.7z\.\d{3}$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.7z\.\d{3}$', '.7z')
					WHEN LOWER(effective_file_name) ~ '\.zip\.\d{3}$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.zip\.\d{3}$', '.zip')
					WHEN LOWER(effective_file_name) ~ '\.part\d+\.rar$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.part\d+\.rar$', '.rar')
					WHEN LOWER(effective_file_name) ~ '\.r\d{2,3}$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.r\d{2,3}$', '.rar')
					ELSE LOWER(effective_file_name)
				END
			) *
			FROM eligible
			ORDER BY
				release_id,
				CASE
					WHEN LOWER(effective_file_name) ~ '\.7z\.\d{3}$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.7z\.\d{3}$', '.7z')
					WHEN LOWER(effective_file_name) ~ '\.zip\.\d{3}$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.zip\.\d{3}$', '.zip')
					WHEN LOWER(effective_file_name) ~ '\.part\d+\.rar$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.part\d+\.rar$', '.rar')
					WHEN LOWER(effective_file_name) ~ '\.r\d{2,3}$' THEN REGEXP_REPLACE(LOWER(effective_file_name), '\.r\d{2,3}$', '.rar')
					ELSE LOWER(effective_file_name)
				END,
				CASE
					WHEN LOWER(effective_file_name) ~ '\.(?:7z|zip)\.001$' THEN 0
					WHEN LOWER(effective_file_name) ~ '\.part0*1\.rar$' THEN 0
					WHEN LOWER(effective_file_name) ~ '\.r00$' THEN 0
					ELSE 1
				END,
				file_index,
				binary_id`
	case "inspect_media":
		stageRerunPredicate = `
			COALESCE(bi.summary_json->>'probe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'ffprobe_error', '') <> '' OR
			COALESCE(bi.summary_json->>'extract_error', '') <> '' OR
			COALESCE(bi.summary_json->>'archive_extract_error', '') <> '' OR
			(
				bi.status = 'completed' AND
				COALESCE(bi.summary_json->>'probe_skip_reason', '') = 'ffprobe_failed'
			) OR
			(
				bi.status = 'completed' AND
				COALESCE(bi.summary_json->>'media_title_extractor_version', '') <> 'v2'
			) OR
			EXISTS (
				SELECT 1
				FROM binary_inspections archive_inspection
				WHERE archive_inspection.source_posted_at = bc.source_posted_at
				  AND archive_inspection.stage_name = 'inspect_archive'
				  AND archive_inspection.binary_id = bc.binary_id
				  AND (bi.id IS NULL OR archive_inspection.updated_at > bi.updated_at)
			)`
		stagePredicate = `
			AND sr.total_parts > 0
			AND sr.observed_parts >= sr.total_parts
			AND (sr.is_main_payload = TRUE OR sr.is_auxiliary = FALSE)
			AND (
				LOWER(sr.effective_file_name) LIKE '%.mkv' OR
				LOWER(sr.effective_file_name) LIKE '%.mp4' OR
				LOWER(sr.effective_file_name) LIKE '%.avi' OR
				LOWER(sr.effective_file_name) LIKE '%.ts' OR
				LOWER(sr.effective_file_name) LIKE '%.flac' OR
				LOWER(sr.effective_file_name) LIKE '%.mp3' OR
				LOWER(sr.effective_file_name) LIKE '%.m4a' OR
				(
					EXISTS (
						SELECT 1
						FROM binary_inspections archive_inspection
						WHERE archive_inspection.source_posted_at = sr.source_posted_at
						  AND archive_inspection.stage_name = 'inspect_archive'
						  AND archive_inspection.binary_id = sr.binary_id
						  AND archive_inspection.status = 'completed'
						  AND CASE
							WHEN jsonb_typeof(archive_inspection.summary_json->'archive_entries') = 'array'
							THEN jsonb_array_length(archive_inspection.summary_json->'archive_entries')
							ELSE 0
						  END > 0
					)
					AND (
						LOWER(sr.effective_file_name) LIKE '%.7z' OR
						LOWER(sr.effective_file_name) ~ '\.7z\.001$' OR
						LOWER(sr.effective_file_name) LIKE '%.zip' OR
						LOWER(sr.effective_file_name) ~ '\.zip\.001$' OR
						LOWER(sr.effective_file_name) LIKE '%.rar' OR
						LOWER(sr.effective_file_name) ~ '\.r00$'
					)
				)
			)`
	}

	query := `
		WITH source_rows AS (
			SELECT
				r.release_id,
				r.updated_at AS release_updated_at,
				r.completion_pct AS release_completion_pct,
				r.expected_file_count,
				r.file_count AS release_file_count,
				rf.binary_id,
				rf.is_pars,
				COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') AS effective_file_name,
				COALESCE(NULLIF(rf.file_index, 0), NULLIF(bic.file_index, 0), 2147483647) AS file_index,
				bc.source_posted_at,
				bos.total_bytes,
				bos.total_parts,
				bos.observed_parts,
				bic.is_main_payload,
				bic.is_auxiliary,
				COALESCE(brc.recovered_kind, '') AS recovered_kind,
				COALESCE(brc.recovered_extension, '') AS recovered_extension,
				GREATEST(
					r.updated_at,
					rf.updated_at,
					bc.updated_at,
					bic.updated_at,
					bos.updated_at,
					COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
				) AS source_updated_at
			FROM releases r
			JOIN release_files rf ON rf.release_id = r.release_id AND rf.binary_id > 0
			JOIN binary_core bc ON bc.binary_id = rf.binary_id
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			JOIN binary_observation_stats bos
			  ON bos.source_posted_at = bc.source_posted_at
			 AND bos.binary_id = bc.binary_id
			LEFT JOIN binary_recovery_current brc
			  ON brc.source_posted_at = bc.source_posted_at
			 AND brc.binary_id = bc.binary_id
			LEFT JOIN binary_lifecycle bl
			  ON bl.source_posted_at = bc.source_posted_at
			 AND bl.binary_id = bc.binary_id
			LEFT JOIN binary_inspections bi
			  ON bi.source_posted_at = bc.source_posted_at
			 AND bi.stage_name = $1
			 AND bi.binary_id = bc.binary_id
			WHERE r.release_id IN (` + placeholders + `)
			  AND COALESCE(bl.lifecycle_status, 'active') = 'active'
			  AND (
				bi.id IS NULL OR
				(bi.status = 'failed' AND bi.updated_at <= NOW() - INTERVAL '5 minutes') OR
				(bi.status = 'running' AND (bi.inspection_claimed_until IS NULL OR bi.inspection_claimed_until < NOW())) OR
				GREATEST(
					r.updated_at,
					rf.updated_at,
					bc.updated_at,
					bic.updated_at,
					bos.updated_at,
					COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
				) > bi.updated_at OR
				bi.release_id IS DISTINCT FROM r.release_id OR
				(` + stageRerunPredicate + `)
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_inspections cfi
				WHERE cfi.source_posted_at = bc.source_posted_at
				  AND cfi.stage_name = 'inspect_discovery'
				  AND cfi.binary_id = bc.binary_id
				  AND cfi.status = 'completed'
				  AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
			  )
		),
		eligible AS (
			SELECT *
			FROM source_rows sr
			WHERE TRUE
			  ` + stagePredicate + `
		),
		selected AS (
			` + stageSelection + `
		)
		INSERT INTO binary_inspection_ready_queue (
			stage_name,
			binary_id,
			release_id,
			status,
			ready_at,
			source_updated_at,
			source_posted_at,
			claimed_by,
			claimed_until,
			last_error,
			updated_at
		)
		SELECT
			$1,
			s.binary_id,
			s.release_id,
			'ready',
			NOW(),
			s.source_updated_at,
			s.source_posted_at,
			'',
			NULL,
			'',
			NOW()
		FROM selected s
		WHERE to_regclass(
			'public.binary_inspection_ready_queue_' ||
			to_char(s.source_posted_at AT TIME ZONE 'UTC', 'YYYYMMDD')
		) IS NOT NULL
		ON CONFLICT (source_posted_at, stage_name, binary_id) DO UPDATE
		SET release_id = EXCLUDED.release_id,
		    source_updated_at = EXCLUDED.source_updated_at,
		    status = CASE
			WHEN binary_inspection_ready_queue.status = 'running'
			 AND binary_inspection_ready_queue.claimed_until >= NOW()
			THEN 'running'
			ELSE 'ready'
		    END,
		    ready_at = CASE
			WHEN binary_inspection_ready_queue.status = 'running'
			 AND binary_inspection_ready_queue.claimed_until >= NOW()
			THEN binary_inspection_ready_queue.ready_at
			ELSE NOW()
		    END,
		    claimed_by = CASE
			WHEN binary_inspection_ready_queue.status = 'running'
			 AND binary_inspection_ready_queue.claimed_until >= NOW()
			THEN binary_inspection_ready_queue.claimed_by
			ELSE ''
		    END,
		    claimed_until = CASE
			WHEN binary_inspection_ready_queue.status = 'running'
			 AND binary_inspection_ready_queue.claimed_until >= NOW()
			THEN binary_inspection_ready_queue.claimed_until
			ELSE NULL
		    END,
		    last_error = '',
		    updated_at = NOW()`
	res, err := execer.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("enqueue %s ready work for releases: %w", stageName, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count %s release ready work: %w", stageName, err)
	}
	return rows, nil
}

func (s *Store) listInspectionReadyQueueCandidates(ctx context.Context, q binaryInspectionQueryer, stageName string, limit int) ([]BinaryInspectionCandidate, error) {
	stageName = strings.TrimSpace(stageName)
	if !isQueuedInspectionStage(stageName) {
		return nil, fmt.Errorf("inspection stage %q does not use ready queue", stageName)
	}
	if limit <= 0 {
		limit = 100
	}
	query := `
		WITH selected AS (
			SELECT q.source_posted_at, q.binary_id, q.release_id
			FROM binary_inspection_ready_queue q
			WHERE q.stage_name = $1
			  AND q.status = 'ready'
			  AND q.ready_at <= NOW()
			  AND (
				$1 <> 'inspect_discovery' OR (
					EXISTS (
						SELECT 1
						FROM binary_parts bp
						WHERE bp.source_posted_at = q.source_posted_at
						  AND bp.binary_id = q.binary_id
						  AND bp.part_number = 1
					)
					AND
					NOT EXISTS (
						SELECT 1
						FROM binary_lifecycle bl
						WHERE bl.source_posted_at = q.source_posted_at
						  AND bl.binary_id = q.binary_id
						  AND bl.lifecycle_status = 'superseded'
					)
					AND NOT EXISTS (
						SELECT 1
						FROM yenc_recovery_work_items wi
						WHERE wi.source_posted_at = q.source_posted_at
						  AND wi.binary_id = q.binary_id
						  AND wi.status IN ('ready', 'running')
					)
				)
			  )
			ORDER BY q.source_updated_at DESC NULLS LAST, q.binary_id DESC
			LIMIT $2
		)
		SELECT
			$1 AS stage_name,
			bic.binary_id AS binary_id,
			COALESCE(r.release_id, '') AS release_id,
			bc.provider_id,
			COALESCE(r.title, '') AS title,
			COALESCE(r.source_title, '') AS source_title,
			COALESCE(r.deobfuscated_title, '') AS deobfuscated_title,
			COALESCE(NULLIF(r.group_name, ''), NULLIF(bic.release_family_key, ''), NULLIF(bic.base_stem, ''), '') AS group_name,
			COALESCE(NULLIF(rf.file_name, ''), NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') AS file_name,
			bic.binary_name,
			bic.release_name,
			COALESCE(NULLIF(r.poster, ''), p.poster_name, '') AS poster,
			bos.posted_at,
			bos.total_bytes,
			bos.total_parts,
			bic.match_confidence,
			GREATEST(
				bc.updated_at,
				bic.updated_at,
				bos.updated_at,
				COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
			) AS source_updated_at,
			COALESCE(bi.status, '') AS current_status,
			bi.updated_at AS current_updated_at,
			COALESCE(bi.summary_json, '{}'::jsonb) AS current_summary_json,
			'{}'::jsonb AS archive_summary_json
		FROM selected s
		JOIN binary_core bc ON bc.binary_id = s.binary_id
		JOIN binary_identity_current bic
		  ON bic.source_posted_at = s.source_posted_at
		 AND bic.binary_id = s.binary_id
		JOIN binary_observation_stats bos
		  ON bos.source_posted_at = s.source_posted_at
		 AND bos.binary_id = bic.binary_id
		LEFT JOIN releases r ON r.release_id = s.release_id
		LEFT JOIN release_files rf ON rf.release_id = s.release_id AND rf.binary_id = s.binary_id
		LEFT JOIN binary_recovery_current brc
		  ON brc.source_posted_at = bic.source_posted_at
		 AND brc.binary_id = bic.binary_id
		LEFT JOIN posters p ON p.id = bc.poster_id
		LEFT JOIN binary_inspections bi
			ON bi.source_posted_at = bic.source_posted_at
			AND bi.stage_name = $1
			AND bi.binary_id = bic.binary_id
		LEFT JOIN binary_inspections abi
			ON abi.source_posted_at = bic.source_posted_at
			AND abi.stage_name = 'inspect_archive'
			AND abi.binary_id = bic.binary_id
		ORDER BY source_updated_at DESC, bic.binary_id DESC`

	return scanBinaryInspectionCandidates(ctx, q, query, stageName, limit)
}

func (s *Store) markInspectReadyQueueRunning(ctx context.Context, stageName string, binaryID int64, owner string, lease time.Duration, sourceUpdatedAt *time.Time) error {
	if !isQueuedInspectionStage(stageName) || binaryID <= 0 {
		return nil
	}
	var sourceUpdated any
	if sourceUpdatedAt != nil {
		sourceUpdated = sourceUpdatedAt.UTC()
	}
	if owner == "" {
		owner = "inspection.start"
	}
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	_, err := s.db.ExecContext(ctx, `
		WITH target AS (
			SELECT source_posted_at
			FROM binary_core
			WHERE binary_id = $2
			  AND source_posted_at IS NOT NULL
		)
		UPDATE binary_inspection_ready_queue
		SET status = 'running',
		    claimed_by = $3,
		    claimed_until = NOW() + ($4::DOUBLE PRECISION * INTERVAL '1 second'),
		    source_updated_at = COALESCE($5, source_updated_at),
		    updated_at = NOW()
		FROM target t
		WHERE stage_name = $1
		  AND binary_inspection_ready_queue.source_posted_at = t.source_posted_at
		  AND binary_id = $2
		  AND status IN ('ready', 'running')`,
		stageName,
		binaryID,
		owner,
		lease.Seconds(),
		sourceUpdated,
	)
	if err != nil {
		return fmt.Errorf("mark inspect ready queue running %s/%d: %w", stageName, binaryID, err)
	}
	return nil
}

func markInspectReadyQueueRowsRunning(ctx context.Context, execer inspectionExecer, stageName string, candidates []BinaryInspectionCandidate, owner string, lease time.Duration) error {
	stageName = strings.TrimSpace(stageName)
	if !isQueuedInspectionStage(stageName) || len(candidates) == 0 {
		return nil
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "inspect"
	}
	if lease <= 0 {
		lease = 15 * time.Minute
	}

	args := make([]any, 0, len(candidates)+3)
	args = append(args, stageName, owner, lease.Seconds())
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		args = append(args, candidate.BinaryID)
		values = append(values, fmt.Sprintf("$%d::bigint", len(args)))
	}

	if _, err := execer.ExecContext(ctx, fmt.Sprintf(`
		WITH target AS (
			SELECT binary_id, source_posted_at
			FROM binary_core
			WHERE binary_id IN (%s)
			  AND source_posted_at IS NOT NULL
		)
		UPDATE binary_inspection_ready_queue
		SET status = 'running',
		    claimed_by = $2,
		    claimed_until = NOW() + ($3::DOUBLE PRECISION * INTERVAL '1 second'),
		    updated_at = NOW()
		FROM target t
		WHERE stage_name = $1
		  AND binary_inspection_ready_queue.source_posted_at = t.source_posted_at
		  AND binary_inspection_ready_queue.binary_id = t.binary_id
		  AND status IN ('ready', 'running')`, strings.Join(values, ",")), args...); err != nil {
		return fmt.Errorf("mark inspect ready queue rows running %s: %w", stageName, err)
	}
	return nil
}

func finishInspectReadyQueueRow(ctx context.Context, execer inspectionExecer, stageName string, binaryID int64, status string, lastError string) error {
	if !isQueuedInspectionStage(stageName) || binaryID <= 0 {
		return nil
	}
	queueStatus := "completed"
	retryDelaySeconds := float64(0)
	if strings.TrimSpace(status) == "failed" {
		queueStatus = "ready"
		retryDelaySeconds = (5 * time.Minute).Seconds()
	}
	_, err := execer.ExecContext(ctx, `
		WITH target AS (
			SELECT source_posted_at
			FROM binary_core
			WHERE binary_id = $2
			  AND source_posted_at IS NOT NULL
		)
		UPDATE binary_inspection_ready_queue
		SET status = $3,
		    ready_at = NOW() + ($5::double precision * INTERVAL '1 second'),
		    claimed_by = '',
		    claimed_until = NULL,
		    last_error = $4,
		    updated_at = NOW()
		FROM target t
		WHERE stage_name = $1
		  AND binary_inspection_ready_queue.source_posted_at = t.source_posted_at
		  AND binary_id = $2`,
		stageName,
		binaryID,
		queueStatus,
		strings.TrimSpace(lastError),
		retryDelaySeconds,
	)
	if err != nil {
		return fmt.Errorf("finish inspect ready queue row %s/%d: %w", stageName, binaryID, err)
	}
	return nil
}
