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

func (s *Store) RefreshInspectDiscoveryReadyQueue(ctx context.Context, limit int) (*BinaryInspectionReadyQueueRefreshResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = inspectDiscoveryReadyQueueSeedLimit
	}

	out := &BinaryInspectionReadyQueueRefreshResult{}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin inspect discovery ready queue refresh tx: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "gonzb-inspect-discovery-ready-refresh"); err != nil {
			return fmt.Errorf("lock inspect discovery ready queue refresh: %w", err)
		}

		requeued, err := requeueStaleInspectReadyRows(ctx, tx, "inspect_discovery")
		if err != nil {
			return err
		}
		out.Requeued = requeued

		retired, err := retireIneligibleInspectDiscoveryReadyRows(ctx, tx)
		if err != nil {
			return err
		}
		out.Retired = retired

		upserted, err := upsertInspectDiscoveryReadyRows(ctx, tx, limit)
		if err != nil {
			return err
		}
		out.ReadyUpserted = upserted

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit inspect discovery ready queue refresh tx: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return out, nil
}

func (s *Store) RefreshInspectionReadyQueue(ctx context.Context, stageName string, limit int) (*BinaryInspectionReadyQueueRefreshResult, error) {
	stageName = strings.TrimSpace(stageName)
	if stageName == "inspect_discovery" {
		return s.RefreshInspectDiscoveryReadyQueue(ctx, limit)
	}
	if !isQueuedInspectionStage(stageName) {
		return nil, fmt.Errorf("inspection stage %q does not use ready queue", stageName)
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = inspectReadyQueueSeedLimit
	}

	out := &BinaryInspectionReadyQueueRefreshResult{}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin %s ready queue refresh tx: %w", stageName, err)
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "gonzb-"+stageName+"-ready-refresh"); err != nil {
			return fmt.Errorf("lock %s ready queue refresh: %w", stageName, err)
		}

		requeued, err := requeueStaleInspectReadyRows(ctx, tx, stageName)
		if err != nil {
			return err
		}
		out.Requeued = requeued

		candidates, err := s.listBinaryInspectionCandidatesRaw(ctx, tx, stageName, limit, BinaryInspectionCandidateOptions{})
		if err != nil {
			return err
		}
		upserted, err := upsertInspectionReadyQueueCandidates(ctx, tx, stageName, candidates)
		if err != nil {
			return err
		}
		out.ReadyUpserted = upserted

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s ready queue refresh tx: %w", stageName, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CountInspectionReadyQueue(ctx context.Context, stageName string) (int64, error) {
	stageName = strings.TrimSpace(stageName)
	if !isQueuedInspectionStage(stageName) {
		return 0, fmt.Errorf("inspection stage %q does not use ready queue", stageName)
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_inspection_ready_queue
		WHERE stage_name = $1
		  AND status = 'ready'
		  AND ready_at <= NOW()`, stageName).Scan(&count); err != nil {
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
			SELECT q.binary_id
			FROM binary_inspection_ready_queue q
			LEFT JOIN binary_core bc ON bc.binary_id = q.binary_id
			LEFT JOIN binary_identity_current bic ON bic.binary_id = q.binary_id
			LEFT JOIN binary_recovery_current brc ON brc.binary_id = q.binary_id
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

func upsertInspectDiscoveryReadyRows(ctx context.Context, tx *sql.Tx, limit int) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		WITH eligible AS (
			SELECT
				bic.binary_id,
				''::text AS release_id,
					GREATEST(
						bc.updated_at,
						bic.updated_at,
						bos.updated_at,
						COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
					) AS source_updated_at,
					COALESCE(bc.source_posted_at, bos.source_posted_at, bos.posted_at) AS source_posted_at
			FROM binary_identity_current bic
			JOIN binary_core bc ON bc.binary_id = bic.binary_id
			JOIN binary_observation_stats bos ON bos.binary_id = bic.binary_id
			LEFT JOIN binary_recovery_current brc ON brc.binary_id = bic.binary_id
			LEFT JOIN binary_inspections bi
				ON bi.stage_name = 'inspect_discovery'
				AND bi.binary_id = bic.binary_id
			WHERE COALESCE(brc.recovered_extension, '') = ''
			  AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
			  AND (
				LOWER(COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '')) LIKE '%.bin' OR
				COALESCE(NULLIF(bic.file_name, ''), NULLIF(bic.binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
			  )
			  AND (
				bi.id IS NULL OR
				bi.status = 'failed' OR
				(
					bi.status = 'running' AND
					(
						bi.inspection_claimed_until IS NULL OR
						bi.inspection_claimed_until < NOW()
					)
				) OR
				GREATEST(
					bc.updated_at,
					bic.updated_at,
					bos.updated_at,
					COALESCE(brc.updated_at, TIMESTAMPTZ 'epoch')
				) > bi.updated_at OR
				COALESCE(bi.summary_json->>'probe_error', '') <> '' OR
				COALESCE(bi.summary_json->>'ffprobe_error', '') <> '' OR
				COALESCE(bi.summary_json->>'extract_error', '') <> '' OR
				COALESCE(bi.summary_json->>'archive_extract_error', '') <> ''
			  )
			  AND (
				bi.inspection_claimed_until IS NULL OR
				bi.inspection_claimed_until < NOW()
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_inspections cfi
				WHERE cfi.stage_name = 'inspect_discovery'
				  AND cfi.binary_id = bic.binary_id
				  AND cfi.status = 'completed'
				  AND COALESCE(cfi.summary_json->>'content_filtered', '') = 'true'
			  )
			ORDER BY bic.updated_at DESC, bic.binary_id DESC
			LIMIT $1
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
			'inspect_discovery',
			e.binary_id,
			e.release_id,
				'ready',
				NOW(),
				e.source_updated_at,
				e.source_posted_at,
				'',
			NULL,
			'',
			NOW()
		FROM eligible e
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
		    updated_at = NOW()`, limit)
	if err != nil {
		return 0, fmt.Errorf("upsert inspect_discovery ready rows: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("upsert inspect_discovery ready rows affected: %w", err)
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
			SELECT q.binary_id, q.release_id
			FROM binary_inspection_ready_queue q
			WHERE q.stage_name = $1
			  AND q.status = 'ready'
			  AND q.ready_at <= NOW()
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
		JOIN binary_identity_current bic ON bic.binary_id = s.binary_id
		JOIN binary_core bc ON bc.binary_id = bic.binary_id
		JOIN binary_observation_stats bos ON bos.binary_id = bic.binary_id
		LEFT JOIN releases r ON r.release_id = s.release_id
		LEFT JOIN release_files rf ON rf.release_id = s.release_id AND rf.binary_id = s.binary_id
		LEFT JOIN binary_recovery_current brc ON brc.binary_id = bic.binary_id
		LEFT JOIN posters p ON p.id = bc.poster_id
		LEFT JOIN binary_inspections bi
			ON bi.stage_name = $1
			AND bi.binary_id = bic.binary_id
		LEFT JOIN binary_inspections abi
			ON abi.stage_name = 'inspect_archive'
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
		UPDATE binary_inspection_ready_queue
		SET status = 'running',
		    claimed_by = $3,
		    claimed_until = NOW() + ($4::DOUBLE PRECISION * INTERVAL '1 second'),
		    source_updated_at = COALESCE($5, source_updated_at),
		    updated_at = NOW()
		WHERE stage_name = $1
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
		UPDATE binary_inspection_ready_queue
		SET status = 'running',
		    claimed_by = $2,
		    claimed_until = NOW() + ($3::DOUBLE PRECISION * INTERVAL '1 second'),
		    updated_at = NOW()
		WHERE stage_name = $1
		  AND binary_id IN (%s)
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
	readyAt := "NOW()"
	if strings.TrimSpace(status) == "failed" {
		queueStatus = "ready"
	}
	_, err := execer.ExecContext(ctx, fmt.Sprintf(`
		UPDATE binary_inspection_ready_queue
		SET status = $3,
		    ready_at = %s,
		    claimed_by = '',
		    claimed_until = NULL,
		    last_error = $4,
		    updated_at = NOW()
		WHERE stage_name = $1
		  AND binary_id = $2`, readyAt),
		stageName,
		binaryID,
		queueStatus,
		strings.TrimSpace(lastError),
	)
	if err != nil {
		return fmt.Errorf("finish inspect ready queue row %s/%d: %w", stageName, binaryID, err)
	}
	return nil
}
