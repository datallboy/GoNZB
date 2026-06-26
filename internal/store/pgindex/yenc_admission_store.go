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
	defaultYEncAdmissionPriority0OverflowCap   = 25000
	defaultYEncAdmissionNearTimeBucketMinutes  = 5
)

type YEncRecoveryAdmissionConfig struct {
	SoftQueueHours              int
	HardQueueMultiplier         int
	AbsoluteHardQueueCap        int64
	BootstrapProbesPerHour      float64
	EWMAWindowMinutes           int
	Priority0OverflowCap        int64
	NearTimeCohortBucketMinutes int
}

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

func (s *Store) ConfigureYEncRecoveryAdmission(ctx context.Context, cfg YEncRecoveryAdmissionConfig) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is required")
	}
	cfg = normalizeYEncAdmissionConfig(cfg)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_recovery_capacity_state (
			id,
			soft_queue_hours,
			hard_queue_multiplier,
			absolute_hard_queue_cap,
			bootstrap_probes_per_hour,
			ewma_window_minutes,
			priority0_overflow_cap,
			near_time_cohort_bucket_minutes,
			config_updated_at
		)
		VALUES (true, $1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (id) DO UPDATE
		SET soft_queue_hours = EXCLUDED.soft_queue_hours,
		    hard_queue_multiplier = EXCLUDED.hard_queue_multiplier,
		    absolute_hard_queue_cap = EXCLUDED.absolute_hard_queue_cap,
		    bootstrap_probes_per_hour = EXCLUDED.bootstrap_probes_per_hour,
		    ewma_window_minutes = EXCLUDED.ewma_window_minutes,
		    priority0_overflow_cap = EXCLUDED.priority0_overflow_cap,
		    near_time_cohort_bucket_minutes = EXCLUDED.near_time_cohort_bucket_minutes,
		    config_updated_at = NOW()`,
		cfg.SoftQueueHours,
		cfg.HardQueueMultiplier,
		cfg.AbsoluteHardQueueCap,
		cfg.BootstrapProbesPerHour,
		cfg.EWMAWindowMinutes,
		cfg.Priority0OverflowCap,
		cfg.NearTimeCohortBucketMinutes,
	); err != nil {
		return fmt.Errorf("configure yenc recovery admission: %w", err)
	}
	return nil
}

func normalizeYEncAdmissionConfig(cfg YEncRecoveryAdmissionConfig) YEncRecoveryAdmissionConfig {
	if cfg.SoftQueueHours <= 0 {
		cfg.SoftQueueHours = defaultYEncAdmissionSoftQueueHours
	}
	if cfg.HardQueueMultiplier <= 0 {
		cfg.HardQueueMultiplier = defaultYEncAdmissionHardQueueMultiplier
	}
	if cfg.AbsoluteHardQueueCap <= 0 {
		cfg.AbsoluteHardQueueCap = defaultYEncAdmissionAbsoluteHardQueueCap
	}
	if cfg.BootstrapProbesPerHour <= 0 {
		cfg.BootstrapProbesPerHour = defaultYEncAdmissionBootstrapProbesPerHour
	}
	if cfg.EWMAWindowMinutes <= 0 {
		cfg.EWMAWindowMinutes = int(defaultYEncAdmissionEWMAWindow / time.Minute)
	}
	if cfg.Priority0OverflowCap <= 0 {
		cfg.Priority0OverflowCap = defaultYEncAdmissionPriority0OverflowCap
	}
	if cfg.NearTimeCohortBucketMinutes <= 0 {
		cfg.NearTimeCohortBucketMinutes = defaultYEncAdmissionNearTimeBucketMinutes
	}
	return cfg
}

func (s *Store) yEncPriority0OverflowCap(ctx context.Context, tx *sql.Tx) (int64, error) {
	if tx == nil {
		return defaultYEncAdmissionPriority0OverflowCap, nil
	}
	var cap int64
	if err := tx.QueryRowContext(ctx, `
		SELECT GREATEST(0, COALESCE(priority0_overflow_cap, $1::bigint))
		FROM indexer_recovery_capacity_state
		WHERE id = true`,
		defaultYEncAdmissionPriority0OverflowCap,
	).Scan(&cap); err != nil {
		if err == sql.ErrNoRows {
			return defaultYEncAdmissionPriority0OverflowCap, nil
		}
		return 0, fmt.Errorf("load yenc priority0 overflow cap: %w", err)
	}
	return cap, nil
}

func (s *Store) GetYEncRecoveryAdmissionSnapshot(ctx context.Context) (*YEncRecoveryAdmissionSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is required")
	}
	var (
		snapshot    YEncRecoveryAdmissionSnapshot
		oldestReady sql.NullTime
		newestReady sql.NullTime
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			probes_per_hour_ewma,
			soft_cap,
			hard_cap,
			open_ready,
			open_running,
			oldest_ready_at,
			newest_ready_at,
			calculated_at
		FROM indexer_recovery_capacity_state
		WHERE id = true`,
	).Scan(
		&snapshot.ProbesPerHourEWMA,
		&snapshot.SoftCap,
		&snapshot.HardCap,
		&snapshot.OpenReady,
		&snapshot.OpenRunning,
		&oldestReady,
		&newestReady,
		&snapshot.CalculatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return &YEncRecoveryAdmissionSnapshot{
				ProbesPerHourEWMA: defaultYEncAdmissionBootstrapProbesPerHour,
				SoftCap:           defaultYEncAdmissionBootstrapProbesPerHour * defaultYEncAdmissionSoftQueueHours,
				HardCap:           defaultYEncAdmissionAbsoluteHardQueueCap,
				RemainingToHard:   defaultYEncAdmissionAbsoluteHardQueueCap,
				CalculatedAt:      time.Now().UTC(),
			}, nil
		}
		return nil, fmt.Errorf("get yenc recovery admission snapshot: %w", err)
	}
	snapshot.OpenTotal = snapshot.OpenReady + snapshot.OpenRunning
	snapshot.RemainingToHard = snapshot.HardCap - snapshot.OpenTotal
	if snapshot.RemainingToHard < 0 {
		snapshot.RemainingToHard = 0
	}
	snapshot.CalculatedAt = snapshot.CalculatedAt.UTC()
	if oldestReady.Valid {
		t := oldestReady.Time.UTC()
		snapshot.OldestReadyAt = &t
	}
	if newestReady.Valid {
		t := newestReady.Time.UTC()
		snapshot.NewestReadyAt = &t
	}
	return &snapshot, nil
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
		WITH cfg AS (
			SELECT
				GREATEST(1, COALESCE(soft_queue_hours, $2::integer)) AS soft_queue_hours,
				GREATEST(1, COALESCE(hard_queue_multiplier, $3::integer)) AS hard_queue_multiplier,
				GREATEST(1, COALESCE(absolute_hard_queue_cap, $4::bigint)) AS absolute_hard_queue_cap,
				GREATEST(1, COALESCE(bootstrap_probes_per_hour, $1::double precision)) AS bootstrap_probes_per_hour,
				GREATEST(1, COALESCE(ewma_window_minutes, $5::integer)) AS ewma_window_minutes
			FROM indexer_recovery_capacity_state
			WHERE id = true
			UNION ALL
			SELECT $2::integer, $3::integer, $4::bigint, $1::double precision, $5::integer
			WHERE NOT EXISTS (SELECT 1 FROM indexer_recovery_capacity_state WHERE id = true)
			LIMIT 1
		),
		recent_runs AS (
			SELECT
				COALESCE(SUM(NULLIF(metrics_json->>'attempted', '')::bigint), 0) AS attempted,
				EXTRACT(EPOCH FROM GREATEST(MAX(finished_at) - MIN(started_at), INTERVAL '1 second')) AS active_seconds
			FROM indexer_stage_runs
			WHERE stage_name = 'recover_yenc'
			  AND status = 'completed'
			  AND finished_at >= NOW() - make_interval(mins => (SELECT ewma_window_minutes FROM cfg))
		),
		throughput AS (
			SELECT
				CASE
					WHEN attempted > 0 AND active_seconds > 0 THEN GREATEST(1, attempted::double precision / active_seconds * 3600)
					ELSE (SELECT bootstrap_probes_per_hour FROM cfg)
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
				GREATEST(1, CEIL(t.probes_per_hour * (SELECT soft_queue_hours FROM cfg)::double precision))::bigint AS soft_cap,
				LEAST(
					GREATEST(1, CEIL(t.probes_per_hour * (SELECT soft_queue_hours FROM cfg)::double precision * (SELECT hard_queue_multiplier FROM cfg)::double precision))::bigint,
					(SELECT absolute_hard_queue_cap FROM cfg)
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
		int(defaultYEncAdmissionEWMAWindow/time.Minute),
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
