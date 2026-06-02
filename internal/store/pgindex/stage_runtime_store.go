package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type IndexerStageClaimRequest struct {
	StageName     string
	TriggerKind   string
	Owner         string
	Enabled       bool
	Interval      time.Duration
	BatchSize     int
	Concurrency   int
	Backoff       time.Duration
	LeaseDuration time.Duration
}

type IndexerStageClaimResult struct {
	Claimed bool
	Reason  string
	Run     *IndexerStageRun
}

type IndexerStageState struct {
	StageName       string     `json:"stage_name"`
	Enabled         bool       `json:"enabled"`
	Paused          bool       `json:"paused"`
	IntervalSeconds int        `json:"interval_seconds"`
	BatchSize       int        `json:"batch_size"`
	Concurrency     int        `json:"concurrency"`
	BackoffSeconds  int        `json:"backoff_seconds"`
	LeaseOwner      string     `json:"lease_owner"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	LastRunID       int64      `json:"last_run_id"`
	LastSuccessAt   *time.Time `json:"last_success_at,omitempty"`
	LastError       string     `json:"last_error"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type IndexerStageRun struct {
	ID          int64           `json:"id"`
	StageName   string          `json:"stage_name"`
	TriggerKind string          `json:"trigger_kind"`
	Status      string          `json:"status"`
	ClaimedBy   string          `json:"claimed_by"`
	StartedAt   time.Time       `json:"started_at"`
	HeartbeatAt *time.Time      `json:"heartbeat_at,omitempty"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
	ErrorText   string          `json:"error_text"`
	MetricsJSON json.RawMessage `json:"metrics_json"`
}

type IndexerStageRunListParams struct {
	StageName   string
	Status      string
	TriggerKind string
	Limit       int
}

type IndexerStageFinishRequest struct {
	RunID       int64
	Owner       string
	ErrorText   string
	MetricsJSON json.RawMessage
}

type IndexerStageRepairResult struct {
	AbandonedRuns      int64 `json:"abandoned_runs"`
	ClearedStaleLeases int64 `json:"cleared_stale_leases"`
}

func (s *Store) ClaimIndexerStage(ctx context.Context, req IndexerStageClaimRequest) (*IndexerStageClaimResult, error) {
	stageName := strings.TrimSpace(req.StageName)
	if stageName == "" {
		return nil, fmt.Errorf("stage name is required")
	}

	owner := strings.TrimSpace(req.Owner)
	if owner == "" {
		return nil, fmt.Errorf("stage owner is required")
	}

	triggerKind := strings.TrimSpace(strings.ToLower(req.TriggerKind))
	if triggerKind == "" {
		triggerKind = "scheduled"
	}

	if req.Interval <= 0 {
		req.Interval = 10 * time.Minute
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 1
	}
	if req.LeaseDuration <= 0 {
		req.LeaseDuration = 30 * time.Second
	}

	intervalSeconds := int(req.Interval / time.Second)
	if intervalSeconds <= 0 {
		intervalSeconds = 1
	}
	backoffSeconds := int(req.Backoff / time.Second)
	if backoffSeconds < 0 {
		backoffSeconds = 0
	}
	leaseExpiresAt := time.Now().UTC().Add(req.LeaseDuration)

	var result *IndexerStageClaimResult
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin claim stage tx: %w", err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO indexer_stage_state (
				stage_name,
				enabled,
				interval_seconds,
				batch_size,
				concurrency,
				backoff_seconds,
				updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (stage_name) DO UPDATE
			SET enabled = EXCLUDED.enabled,
			    interval_seconds = EXCLUDED.interval_seconds,
			    batch_size = EXCLUDED.batch_size,
			    concurrency = EXCLUDED.concurrency,
			    backoff_seconds = EXCLUDED.backoff_seconds,
			    updated_at = NOW()`,
			stageName,
			req.Enabled,
			intervalSeconds,
			req.BatchSize,
			req.Concurrency,
			backoffSeconds,
		); err != nil {
			return fmt.Errorf("upsert indexer stage state %s: %w", stageName, err)
		}

		var (
			enabled      bool
			paused       bool
			leaseOwner   string
			leaseExpires sql.NullTime
			lastRunID    sql.NullInt64
		)
		if err := tx.QueryRowContext(ctx, `
			SELECT enabled, paused, lease_owner, lease_expires_at, last_run_id
			FROM indexer_stage_state
			WHERE stage_name = $1
			FOR UPDATE`,
			stageName,
		).Scan(&enabled, &paused, &leaseOwner, &leaseExpires, &lastRunID); err != nil {
			return fmt.Errorf("lock indexer stage state %s: %w", stageName, err)
		}

		if !enabled {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit disabled stage claim %s: %w", stageName, err)
			}
			result = &IndexerStageClaimResult{Claimed: false, Reason: "disabled"}
			return nil
		}

		if paused {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit paused stage claim %s: %w", stageName, err)
			}
			result = &IndexerStageClaimResult{Claimed: false, Reason: "paused"}
			return nil
		}

		if leaseOwner != "" && leaseExpires.Valid && leaseExpires.Time.After(time.Now().UTC()) {
			if leaseOwner != owner {
				if err := tx.Commit(); err != nil {
					return fmt.Errorf("commit leased stage claim %s: %w", stageName, err)
				}
				result = &IndexerStageClaimResult{Claimed: false, Reason: "leased"}
				return nil
			}

			if lastRunID.Valid {
				var status string
				if err := tx.QueryRowContext(ctx, `
					SELECT status
					FROM indexer_stage_runs
					WHERE id = $1`,
					lastRunID.Int64,
				).Scan(&status); err == nil && strings.EqualFold(status, "running") {
					if err := tx.Commit(); err != nil {
						return fmt.Errorf("commit running stage claim %s: %w", stageName, err)
					}
					result = &IndexerStageClaimResult{Claimed: false, Reason: "running"}
					return nil
				}
			}
		}

		if lastRunID.Valid {
			if _, err := tx.ExecContext(ctx, `
				UPDATE indexer_stage_runs
				SET status = 'abandoned',
				    error_text = CASE
				        WHEN error_text = '' THEN 'lease expired before completion'
				        ELSE error_text
				    END,
				    heartbeat_at = COALESCE(heartbeat_at, NOW()),
				    finished_at = COALESCE(finished_at, NOW())
				WHERE id = $1
				  AND status = 'running'`,
				lastRunID.Int64,
			); err != nil {
				return fmt.Errorf("mark stale stage run %d abandoned: %w", lastRunID.Int64, err)
			}
		}

		run := &IndexerStageRun{
			StageName:   stageName,
			TriggerKind: triggerKind,
			Status:      "running",
			ClaimedBy:   owner,
			StartedAt:   time.Now().UTC(),
		}

		if err := tx.QueryRowContext(ctx, `
			INSERT INTO indexer_stage_runs (
				stage_name,
				trigger_kind,
				status,
				claimed_by,
				started_at,
				heartbeat_at,
				error_text,
				metrics_json
			)
			VALUES ($1, $2, 'running', $3, NOW(), NOW(), '', '{}'::jsonb)
			RETURNING id, started_at, heartbeat_at`,
			stageName,
			triggerKind,
			owner,
		).Scan(&run.ID, &run.StartedAt, &leaseExpires); err != nil {
			return fmt.Errorf("insert indexer stage run %s: %w", stageName, err)
		}
		if leaseExpires.Valid {
			t := leaseExpires.Time.UTC()
			run.HeartbeatAt = &t
		}
		run.MetricsJSON = json.RawMessage("{}")

		if _, err := tx.ExecContext(ctx, `
			UPDATE indexer_stage_state
			SET lease_owner = $2,
			    lease_expires_at = $3,
			    last_heartbeat_at = NOW(),
			    last_run_id = $4,
			    last_error = '',
			    updated_at = NOW()
			WHERE stage_name = $1`,
			stageName,
			owner,
			leaseExpiresAt,
			run.ID,
		); err != nil {
			return fmt.Errorf("update claimed stage state %s: %w", stageName, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit stage claim %s: %w", stageName, err)
		}

		result = &IndexerStageClaimResult{
			Claimed: true,
			Run:     run,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Store) HeartbeatIndexerStageRun(ctx context.Context, runID int64, owner string, leaseDuration time.Duration) error {
	if runID <= 0 {
		return fmt.Errorf("run id is required")
	}

	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("stage owner is required")
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	leaseSeconds := int(leaseDuration / time.Second)
	if leaseSeconds <= 0 {
		leaseSeconds = 1
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE indexer_stage_runs
		SET heartbeat_at = NOW()
		WHERE id = $1
		  AND claimed_by = $2
		  AND status = 'running'`,
		runID,
		owner,
	)
	if err != nil {
		return fmt.Errorf("heartbeat stage run %d: %w", runID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat stage run %d rows affected: %w", runID, err)
	}
	if rows == 0 {
		return fmt.Errorf("stage run %d is no longer running for owner %s", runID, owner)
	}

	stateResult, err := s.db.ExecContext(ctx, `
		UPDATE indexer_stage_state
		SET lease_expires_at = NOW() + make_interval(secs => $3),
		    last_heartbeat_at = NOW(),
		    updated_at = NOW()
		WHERE last_run_id = $1
		  AND lease_owner = $2`,
		runID,
		owner,
		leaseSeconds,
	)
	if err != nil {
		return fmt.Errorf("heartbeat stage state for run %d: %w", runID, err)
	}

	stateRows, err := stateResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat stage state rows affected for run %d: %w", runID, err)
	}
	if stateRows == 0 {
		return fmt.Errorf("stage state for run %d is no longer owned by %s", runID, owner)
	}

	return nil
}

func (s *Store) CompleteIndexerStageRun(ctx context.Context, req IndexerStageFinishRequest) error {
	return s.finishIndexerStageRun(ctx, req, "completed")
}

func (s *Store) FailIndexerStageRun(ctx context.Context, req IndexerStageFinishRequest) error {
	return s.finishIndexerStageRun(ctx, req, "failed")
}

func (s *Store) PauseIndexerStage(ctx context.Context, stageName string) error {
	return s.setIndexerStagePaused(ctx, stageName, true)
}

func (s *Store) ResumeIndexerStage(ctx context.Context, stageName string) error {
	return s.setIndexerStagePaused(ctx, stageName, false)
}

func (s *Store) RepairIndexerStageRuntime(ctx context.Context) (*IndexerStageRepairResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	repair := &IndexerStageRepairResult{}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin repair indexer stage runtime tx: %w", err)
		}
		defer rollbackTx(tx)

		abandonedResult, err := tx.ExecContext(ctx, `
			UPDATE indexer_stage_runs r
			SET status = 'abandoned',
			    error_text = CASE
			    	WHEN r.error_text = '' THEN 'lease expired before repair cleanup'
			    	ELSE r.error_text
			    END,
			    heartbeat_at = COALESCE(r.heartbeat_at, NOW()),
			    finished_at = COALESCE(r.finished_at, NOW())
			FROM indexer_stage_state s
			WHERE s.last_run_id = r.id
			  AND r.status = 'running'
			  AND s.lease_expires_at IS NOT NULL
			  AND s.lease_expires_at < NOW()`)
		if err != nil {
			return fmt.Errorf("abandon stale indexer stage runs: %w", err)
		}
		if repair.AbandonedRuns, err = abandonedResult.RowsAffected(); err != nil {
			return fmt.Errorf("abandon stale indexer stage runs rows affected: %w", err)
		}

		clearedResult, err := tx.ExecContext(ctx, `
			UPDATE indexer_stage_state
			SET lease_owner = '',
			    lease_expires_at = NULL,
			    updated_at = NOW()
			WHERE lease_owner <> ''
			  AND lease_expires_at IS NOT NULL
			  AND lease_expires_at < NOW()`)
		if err != nil {
			return fmt.Errorf("clear stale indexer stage leases: %w", err)
		}
		if repair.ClearedStaleLeases, err = clearedResult.RowsAffected(); err != nil {
			return fmt.Errorf("clear stale indexer stage leases rows affected: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit repair indexer stage runtime tx: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return repair, nil
}

func (s *Store) ListIndexerStageStates(ctx context.Context) ([]IndexerStageState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			stage_name,
			enabled,
			paused,
			interval_seconds,
			batch_size,
			concurrency,
			backoff_seconds,
			lease_owner,
			lease_expires_at,
			last_heartbeat_at,
			last_run_id,
			last_success_at,
			last_error,
			updated_at
		FROM indexer_stage_state
		ORDER BY stage_name`)
	if err != nil {
		return nil, fmt.Errorf("list indexer stage states: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerStageState, 0, 16)
	for rows.Next() {
		item, err := scanIndexerStageState(rows)
		if err != nil {
			return nil, fmt.Errorf("scan indexer stage state: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer stage states: %w", err)
	}

	return out, nil
}

func (s *Store) ListIndexerStageRuns(ctx context.Context, stageName string, limit int) ([]IndexerStageRun, error) {
	return s.ListIndexerStageRunsFiltered(ctx, IndexerStageRunListParams{
		StageName: stageName,
		Limit:     limit,
	})
}

func (s *Store) ListIndexerStageRunsFiltered(ctx context.Context, params IndexerStageRunListParams) ([]IndexerStageRun, error) {
	params.StageName = strings.TrimSpace(params.StageName)
	params.Status = strings.ToLower(strings.TrimSpace(params.Status))
	params.TriggerKind = strings.ToLower(strings.TrimSpace(params.TriggerKind))
	if params.Limit <= 0 {
		params.Limit = 50
	}

	query := `
		SELECT
			id,
			stage_name,
			trigger_kind,
			status,
			claimed_by,
			started_at,
			heartbeat_at,
			finished_at,
			error_text,
			metrics_json
		FROM indexer_stage_runs`
	args := []any{}
	clauses := make([]string, 0, 3)
	if params.StageName != "" {
		args = append(args, params.StageName)
		clauses = append(clauses, fmt.Sprintf("stage_name = $%d", len(args)))
	}
	if params.Status != "" {
		args = append(args, params.Status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if params.TriggerKind != "" {
		args = append(args, params.TriggerKind)
		clauses = append(clauses, fmt.Sprintf("trigger_kind = $%d", len(args)))
	}
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, ` AND `)
	}
	args = append(args, params.Limit)
	query += fmt.Sprintf(` ORDER BY started_at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list indexer stage runs: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerStageRun, 0, params.Limit)
	for rows.Next() {
		item, err := scanIndexerStageRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan indexer stage run: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer stage runs: %w", err)
	}

	return out, nil
}

func (s *Store) GetIndexerStageRun(ctx context.Context, runID int64) (*IndexerStageRun, error) {
	if runID <= 0 {
		return nil, fmt.Errorf("run id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			stage_name,
			trigger_kind,
			status,
			claimed_by,
			started_at,
			heartbeat_at,
			finished_at,
			error_text,
			metrics_json
		FROM indexer_stage_runs
		WHERE id = $1`, runID)
	item, err := scanIndexerStageRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get indexer stage run %d: %w", runID, err)
	}
	return &item, nil
}

func (s *Store) finishIndexerStageRun(ctx context.Context, req IndexerStageFinishRequest, status string) error {
	if req.RunID <= 0 {
		return fmt.Errorf("run id is required")
	}

	req.Owner = strings.TrimSpace(req.Owner)
	if req.Owner == "" {
		return fmt.Errorf("stage owner is required")
	}

	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return fmt.Errorf("stage run status is required")
	}

	metrics := req.MetricsJSON
	if len(metrics) == 0 {
		metrics = json.RawMessage("{}")
	}

	return retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin finish stage run tx: %w", err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		var stageName string
		if err := tx.QueryRowContext(ctx, `
			SELECT stage_name
			FROM indexer_stage_runs
			WHERE id = $1
			  AND claimed_by = $2
			FOR UPDATE`,
			req.RunID,
			req.Owner,
		).Scan(&stageName); err != nil {
			return fmt.Errorf("lock stage run %d: %w", req.RunID, err)
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE indexer_stage_runs
			SET status = $2,
			    heartbeat_at = NOW(),
			    finished_at = NOW(),
			    error_text = $3,
			    metrics_json = $4
			WHERE id = $1`,
			req.RunID,
			status,
			req.ErrorText,
			metrics,
		); err != nil {
			return fmt.Errorf("finish stage run %d: %w", req.RunID, err)
		}

		success := status == "completed"
		if _, err := tx.ExecContext(ctx, `
			UPDATE indexer_stage_state
			SET lease_owner = CASE
			        WHEN last_run_id = $1 AND lease_owner = $2 THEN ''
			        ELSE lease_owner
			    END,
			    lease_expires_at = CASE
			        WHEN last_run_id = $1 AND lease_owner = $2 THEN NULL
			        ELSE lease_expires_at
			    END,
			    last_heartbeat_at = CASE
			        WHEN last_run_id = $1 THEN NOW()
			        ELSE last_heartbeat_at
			    END,
			    last_success_at = CASE
			        WHEN last_run_id = $1 AND $3 THEN NOW()
			        ELSE last_success_at
			    END,
			    last_error = CASE
			        WHEN last_run_id = $1 THEN $4
			        ELSE last_error
			    END,
			    updated_at = CASE
			        WHEN last_run_id = $1 THEN NOW()
			        ELSE updated_at
			    END
			WHERE stage_name = $5`,
			req.RunID,
			req.Owner,
			success,
			req.ErrorText,
			stageName,
		); err != nil {
			return fmt.Errorf("update stage state for run %d: %w", req.RunID, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit finish stage run %d: %w", req.RunID, err)
		}

		return nil
	})
}

func (s *Store) setIndexerStagePaused(ctx context.Context, stageName string, paused bool) error {
	stageName = strings.TrimSpace(stageName)
	if stageName == "" {
		return fmt.Errorf("stage name is required")
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_stage_state (stage_name, paused, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (stage_name) DO UPDATE
		SET paused = EXCLUDED.paused,
		    updated_at = NOW()`,
		stageName,
		paused,
	); err != nil {
		return fmt.Errorf("set paused=%t for stage %s: %w", paused, stageName, err)
	}

	return nil
}

func scanIndexerStageState(scanner releaseScanner) (IndexerStageState, error) {
	var (
		item            IndexerStageState
		leaseExpiresAt  sql.NullTime
		lastHeartbeatAt sql.NullTime
		lastRunID       sql.NullInt64
		lastSuccessAt   sql.NullTime
	)

	if err := scanner.Scan(
		&item.StageName,
		&item.Enabled,
		&item.Paused,
		&item.IntervalSeconds,
		&item.BatchSize,
		&item.Concurrency,
		&item.BackoffSeconds,
		&item.LeaseOwner,
		&leaseExpiresAt,
		&lastHeartbeatAt,
		&lastRunID,
		&lastSuccessAt,
		&item.LastError,
		&item.UpdatedAt,
	); err != nil {
		return IndexerStageState{}, err
	}

	if leaseExpiresAt.Valid {
		t := leaseExpiresAt.Time.UTC()
		item.LeaseExpiresAt = &t
	}
	if lastHeartbeatAt.Valid {
		t := lastHeartbeatAt.Time.UTC()
		item.LastHeartbeatAt = &t
	}
	if lastRunID.Valid {
		item.LastRunID = lastRunID.Int64
	}
	if lastSuccessAt.Valid {
		t := lastSuccessAt.Time.UTC()
		item.LastSuccessAt = &t
	}
	item.UpdatedAt = item.UpdatedAt.UTC()

	return item, nil
}

func scanIndexerStageRun(scanner releaseScanner) (IndexerStageRun, error) {
	var (
		item        IndexerStageRun
		heartbeatAt sql.NullTime
		finishedAt  sql.NullTime
		metricsJSON []byte
	)

	if err := scanner.Scan(
		&item.ID,
		&item.StageName,
		&item.TriggerKind,
		&item.Status,
		&item.ClaimedBy,
		&item.StartedAt,
		&heartbeatAt,
		&finishedAt,
		&item.ErrorText,
		&metricsJSON,
	); err != nil {
		return IndexerStageRun{}, err
	}

	item.StartedAt = item.StartedAt.UTC()
	if heartbeatAt.Valid {
		t := heartbeatAt.Time.UTC()
		item.HeartbeatAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time.UTC()
		item.FinishedAt = &t
	}
	if len(metricsJSON) == 0 {
		item.MetricsJSON = json.RawMessage("{}")
	} else {
		item.MetricsJSON = json.RawMessage(append([]byte(nil), metricsJSON...))
	}

	return item, nil
}

func nullIfZero(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}
