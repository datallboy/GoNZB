package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	defaultYEncAdmissionSoftQueueHours         = 4
	defaultYEncAdmissionHardQueueMultiplier    = 2
	defaultYEncAdmissionAbsoluteHardQueueCap   = 250000
	defaultYEncAdmissionBootstrapProbesPerHour = 25000
	defaultYEncAdmissionEWMAWindow             = 30 * time.Minute
)

type YEncRecoveryAdmissionSnapshot struct {
	ProbesPerHourEWMA float64    `json:"probes_per_hour_ewma"`
	SoftCap           int64      `json:"soft_cap"`
	HardCap           int64      `json:"hard_cap"`
	OpenReady         int64      `json:"open_ready"`
	OpenRunning       int64      `json:"open_running"`
	OpenTotal         int64      `json:"open_total"`
	RemainingToHard   int64      `json:"remaining_to_hard"`
	OldestReadyAt     *time.Time `json:"oldest_ready_at,omitempty"`
	NewestReadyAt     *time.Time `json:"newest_ready_at,omitempty"`
	CalculatedAt      time.Time  `json:"calculated_at"`
}

func (s *Store) RefreshYEncRecoveryAdmissionSnapshot(ctx context.Context) (*YEncRecoveryAdmissionSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is required")
	}
	var (
		probesPerHour float64
		softCap       int64
		hardCap       int64
		openReady     int64
		openRunning   int64
		oldestReady   sql.NullTime
		newestReady   sql.NullTime
		calculatedAt  time.Time
	)
	if err := s.db.QueryRowContext(ctx, `
		WITH recent_runs AS (
			SELECT
				COALESCE(SUM(NULLIF(metrics_json->>'attempted', '')::bigint), 0) AS attempted,
				EXTRACT(EPOCH FROM GREATEST(MAX(finished_at) - MIN(started_at), INTERVAL '1 second')) AS active_seconds
			FROM indexer_stage_runs
			WHERE stage_name = 'recover_yenc'
			  AND status = 'completed'
			  AND finished_at >= NOW() - INTERVAL '30 minutes'
		),
		throughput AS (
			SELECT
				CASE
					WHEN attempted > 0 AND active_seconds > 0 THEN GREATEST(1, attempted::double precision / active_seconds * 3600)
					ELSE $1::double precision
				END AS probes_per_hour
			FROM recent_runs
		),
		open_queue AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'ready') AS open_ready,
				COUNT(*) FILTER (WHERE status = 'running') AS open_running,
				MIN(ready_at) FILTER (WHERE status = 'ready') AS oldest_ready_at,
				MAX(ready_at) FILTER (WHERE status = 'ready') AS newest_ready_at
			FROM yenc_recovery_work_items
			WHERE status IN ('ready', 'running')
		),
		calc AS (
			SELECT
				t.probes_per_hour,
				GREATEST(1, CEIL(t.probes_per_hour * $2::double precision))::bigint AS soft_cap,
				LEAST(
					GREATEST(1, CEIL(t.probes_per_hour * $2::double precision * $3::double precision))::bigint,
					$4::bigint
				) AS hard_cap,
				q.open_ready,
				q.open_running,
				q.oldest_ready_at,
				q.newest_ready_at
			FROM throughput t
			CROSS JOIN open_queue q
		),
		upserted AS (
			INSERT INTO indexer_recovery_capacity_state (
				id,
				probes_per_hour_ewma,
				soft_cap,
				hard_cap,
				open_ready,
				open_running,
				oldest_ready_at,
				newest_ready_at,
				calculated_at
			)
			SELECT
				true,
				probes_per_hour,
				soft_cap,
				hard_cap,
				open_ready,
				open_running,
				oldest_ready_at,
				newest_ready_at,
				NOW()
			FROM calc
			ON CONFLICT (id) DO UPDATE
			SET probes_per_hour_ewma = EXCLUDED.probes_per_hour_ewma,
			    soft_cap = EXCLUDED.soft_cap,
			    hard_cap = EXCLUDED.hard_cap,
			    open_ready = EXCLUDED.open_ready,
			    open_running = EXCLUDED.open_running,
			    oldest_ready_at = EXCLUDED.oldest_ready_at,
			    newest_ready_at = EXCLUDED.newest_ready_at,
			    calculated_at = NOW()
			RETURNING probes_per_hour_ewma, soft_cap, hard_cap, open_ready, open_running, oldest_ready_at, newest_ready_at, calculated_at
		)
		SELECT probes_per_hour_ewma, soft_cap, hard_cap, open_ready, open_running, oldest_ready_at, newest_ready_at, calculated_at
		FROM upserted`,
		defaultYEncAdmissionBootstrapProbesPerHour,
		defaultYEncAdmissionSoftQueueHours,
		defaultYEncAdmissionHardQueueMultiplier,
		defaultYEncAdmissionAbsoluteHardQueueCap,
	).Scan(&probesPerHour, &softCap, &hardCap, &openReady, &openRunning, &oldestReady, &newestReady, &calculatedAt); err != nil {
		return nil, fmt.Errorf("refresh yenc recovery admission snapshot: %w", err)
	}

	snapshot := &YEncRecoveryAdmissionSnapshot{
		ProbesPerHourEWMA: probesPerHour,
		SoftCap:           softCap,
		HardCap:           hardCap,
		OpenReady:         openReady,
		OpenRunning:       openRunning,
		OpenTotal:         openReady + openRunning,
		CalculatedAt:      calculatedAt.UTC(),
	}
	snapshot.RemainingToHard = snapshot.HardCap - snapshot.OpenTotal
	if snapshot.RemainingToHard < 0 {
		snapshot.RemainingToHard = 0
	}
	if oldestReady.Valid {
		t := oldestReady.Time.UTC()
		snapshot.OldestReadyAt = &t
	}
	if newestReady.Valid {
		t := newestReady.Time.UTC()
		snapshot.NewestReadyAt = &t
	}
	return snapshot, nil
}
