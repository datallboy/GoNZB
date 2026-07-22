package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type SourceBucketOutcomePolicy struct {
	SourceSettleHours    int
	NoYieldGraceDays     int
	YEncTerminalAttempts int
}

type SourceBucketOutcomeSummary struct {
	ProviderID           int64      `json:"provider_id"`
	ProviderKey          string     `json:"provider_key"`
	NewsgroupID          int64      `json:"newsgroup_id"`
	GroupName            string     `json:"group_name"`
	SourceDay            string     `json:"source_day"`
	State                string     `json:"state"`
	HeadersIngested      int64      `json:"headers_ingested"`
	OpenWorkCount        int64      `json:"open_work_count"`
	ExhaustedWorkCount   int64      `json:"exhausted_work_count"`
	TerminalReleaseCount int64      `json:"terminal_release_count"`
	TerminalReason       string     `json:"terminal_reason"`
	LastIngestedAt       time.Time  `json:"last_ingested_at"`
	LastProgressAt       time.Time  `json:"last_progress_at"`
	SettledAt            *time.Time `json:"settled_at,omitempty"`
	PurgeEligibleAt      *time.Time `json:"purge_eligible_at,omitempty"`
	PurgedAt             *time.Time `json:"purged_at,omitempty"`
	LastReconciledAt     *time.Time `json:"last_reconciled_at,omitempty"`
}

type SourceBucketOutcomeReport struct {
	TotalBuckets         int64                        `json:"total_buckets"`
	ActiveBuckets        int64                        `json:"active_buckets"`
	SuccessBuckets       int64                        `json:"success_buckets"`
	NoYieldBuckets       int64                        `json:"no_yield_buckets"`
	PurgeEligibleBuckets int64                        `json:"purge_eligible_buckets"`
	PurgedBuckets        int64                        `json:"purged_buckets"`
	HeadersIngested      int64                        `json:"headers_ingested"`
	OpenWorkCount        int64                        `json:"open_work_count"`
	TerminalReleaseCount int64                        `json:"terminal_release_count"`
	Items                []SourceBucketOutcomeSummary `json:"items"`
}

type sourceBucketOutcomeCandidate struct {
	ProviderID     int64
	NewsgroupID    int64
	SourceDay      time.Time
	LastIngestedAt time.Time
	LastProgressAt time.Time
}

type sourceBucketOutcomeFacts struct {
	LastProgressAt       time.Time
	OpenWorkCount        int64
	ExhaustedWorkCount   int64
	TerminalReleaseCount int64
}

func normalizeSourceBucketOutcomePolicy(policy SourceBucketOutcomePolicy) SourceBucketOutcomePolicy {
	if policy.SourceSettleHours <= 0 {
		policy.SourceSettleHours = 24
	}
	if policy.NoYieldGraceDays <= 0 {
		policy.NoYieldGraceDays = 7
	}
	if policy.YEncTerminalAttempts <= 0 {
		policy.YEncTerminalAttempts = 4
	}
	return policy
}

func (s *Store) GetSourceBucketOutcomeReport(ctx context.Context, limit int) (*SourceBucketOutcomeReport, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	report := &SourceBucketOutcomeReport{}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)::bigint,
		       COUNT(*) FILTER (WHERE state = 'active')::bigint,
		       COUNT(*) FILTER (WHERE state = 'success')::bigint,
		       COUNT(*) FILTER (WHERE state = 'no_yield')::bigint,
		       COUNT(*) FILTER (WHERE state IN ('success', 'no_yield', 'purge_eligible') AND purge_eligible_at <= NOW())::bigint,
		       COUNT(*) FILTER (WHERE state = 'purged')::bigint,
		       COALESCE(SUM(headers_ingested), 0)::bigint,
		       COALESCE(SUM(open_work_count), 0)::bigint,
		       COALESCE(SUM(terminal_release_count), 0)::bigint
		FROM indexer_source_bucket_state`).Scan(
		&report.TotalBuckets,
		&report.ActiveBuckets,
		&report.SuccessBuckets,
		&report.NoYieldBuckets,
		&report.PurgeEligibleBuckets,
		&report.PurgedBuckets,
		&report.HeadersIngested,
		&report.OpenWorkCount,
		&report.TerminalReleaseCount,
	); err != nil {
		return nil, fmt.Errorf("summarize source bucket outcomes: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT state.provider_id, up.provider_key,
		       state.newsgroup_id, ng.group_name, state.source_day,
		       state.state, state.headers_ingested, state.open_work_count,
		       state.exhausted_work_count, state.terminal_release_count,
		       state.terminal_reason, state.last_ingested_at,
		       state.last_progress_at, state.settled_at,
		       state.purge_eligible_at, state.purged_at,
		       state.last_reconciled_at
		FROM indexer_source_bucket_state state
		JOIN usenet_providers up ON up.id = state.provider_id
		JOIN newsgroups ng ON ng.id = state.newsgroup_id
		ORDER BY CASE state.state
		           WHEN 'active' THEN 0
		           WHEN 'success' THEN 1
		           WHEN 'no_yield' THEN 2
		           WHEN 'purge_eligible' THEN 3
		           ELSE 4
		         END,
		         state.open_work_count DESC,
		         state.last_progress_at DESC,
		         state.source_day DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list source bucket outcomes: %w", err)
	}
	defer rows.Close()
	report.Items = make([]SourceBucketOutcomeSummary, 0, limit)
	for rows.Next() {
		var item SourceBucketOutcomeSummary
		var sourceDay time.Time
		var settledAt, purgeEligibleAt, purgedAt, lastReconciledAt sql.NullTime
		if err := rows.Scan(
			&item.ProviderID, &item.ProviderKey,
			&item.NewsgroupID, &item.GroupName, &sourceDay,
			&item.State, &item.HeadersIngested, &item.OpenWorkCount,
			&item.ExhaustedWorkCount, &item.TerminalReleaseCount,
			&item.TerminalReason, &item.LastIngestedAt,
			&item.LastProgressAt, &settledAt,
			&purgeEligibleAt, &purgedAt, &lastReconciledAt,
		); err != nil {
			return nil, fmt.Errorf("scan source bucket outcome: %w", err)
		}
		item.SourceDay = sourceDay.UTC().Format("2006-01-02")
		if settledAt.Valid {
			item.SettledAt = ptrUTC(settledAt.Time)
		}
		if purgeEligibleAt.Valid {
			item.PurgeEligibleAt = ptrUTC(purgeEligibleAt.Time)
		}
		if purgedAt.Valid {
			item.PurgedAt = ptrUTC(purgedAt.Time)
		}
		if lastReconciledAt.Valid {
			item.LastReconciledAt = ptrUTC(lastReconciledAt.Time)
		}
		report.Items = append(report.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source bucket outcomes: %w", err)
	}
	return report, nil
}

// ReconcileSourceBucketOutcomes refreshes durable per-provider/group/day
// outcome state. It never deletes source data.
func (s *Store) ReconcileSourceBucketOutcomes(ctx context.Context, batchSize int, policy SourceBucketOutcomePolicy) (*MaintenanceTaskResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if batchSize > 1000 {
		batchSize = 1000
	}
	policy = normalizeSourceBucketOutcomePolicy(policy)
	result := &MaintenanceTaskResult{
		TaskKey:              "outcome_reconcile",
		DryRun:               false,
		EstimatedRowsByTable: map[string]int64{},
		Warnings:             []string{"reconciliation updates outcome metadata only; it never deletes source data"},
	}

	bootstrapped, err := s.bootstrapSourceBucketOutcomes(ctx, batchSize)
	if err != nil {
		return nil, err
	}
	result.EstimatedRowsByTable["buckets_bootstrapped"] = bootstrapped

	candidates, err := s.sourceBucketOutcomeCandidates(ctx, batchSize)
	if err != nil {
		return nil, err
	}
	result.EstimatedRowsByTable["buckets_examined"] = int64(len(candidates))
	now := time.Now().UTC()
	for _, candidate := range candidates {
		facts, err := s.loadSourceBucketOutcomeFacts(ctx, candidate, policy.YEncTerminalAttempts)
		if err != nil {
			return nil, err
		}
		lastProgress := candidate.LastProgressAt
		if facts.LastProgressAt.After(lastProgress) {
			lastProgress = facts.LastProgressAt
		}
		settled := now.Sub(candidate.LastIngestedAt) >= time.Duration(policy.SourceSettleHours)*time.Hour
		idleLongEnough := now.Sub(lastProgress) >= time.Duration(policy.NoYieldGraceDays)*24*time.Hour
		state := "active"
		reason := ""
		var purgeEligibleAt *time.Time
		if facts.OpenWorkCount == 0 && facts.TerminalReleaseCount > 0 {
			state = "success"
			reason = "durable_archive_and_catalog"
			purgeEligibleAt = ptrUTC(now)
		} else if facts.OpenWorkCount == 0 && settled && idleLongEnough {
			state = "no_yield"
			reason = "bounded_work_exhausted"
			purgeEligibleAt = ptrUTC(now)
		}
		if err := s.updateSourceBucketOutcome(ctx, candidate, lastProgress, facts, state, reason, settled, purgeEligibleAt); err != nil {
			return nil, err
		}
		result.EstimatedRowsByTable["state_"+state]++
	}
	return result, nil
}

func (s *Store) bootstrapSourceBucketOutcomes(ctx context.Context, limit int) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		WITH missing AS (
			SELECT
				ah.provider_id,
				ah.newsgroup_id,
				(ah.source_posted_at AT TIME ZONE 'UTC')::date AS source_day,
				MIN(ah.scraped_at) AS first_ingested_at,
				MAX(ah.scraped_at) AS last_ingested_at,
				COUNT(*)::bigint AS headers_ingested
			FROM article_headers ah
			WHERE NOT EXISTS (
				SELECT 1
				FROM indexer_source_bucket_state state
				WHERE state.provider_id = ah.provider_id
				  AND state.newsgroup_id = ah.newsgroup_id
				  AND state.source_day = (ah.source_posted_at AT TIME ZONE 'UTC')::date
			)
			GROUP BY ah.provider_id, ah.newsgroup_id, (ah.source_posted_at AT TIME ZONE 'UTC')::date
			ORDER BY MAX(ah.scraped_at) DESC
			LIMIT $1
		)
		INSERT INTO indexer_source_bucket_state (
			provider_id, newsgroup_id, source_day, state,
			first_ingested_at, last_ingested_at, last_progress_at, headers_ingested, updated_at
		)
		SELECT provider_id, newsgroup_id, source_day, 'active',
		       first_ingested_at, last_ingested_at, last_ingested_at, headers_ingested, NOW()
		FROM missing
		ON CONFLICT (provider_id, newsgroup_id, source_day) DO NOTHING`, limit)
	if err != nil {
		return 0, fmt.Errorf("bootstrap source bucket outcomes: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count bootstrapped source bucket outcomes: %w", err)
	}
	return count, nil
}

func (s *Store) sourceBucketOutcomeCandidates(ctx context.Context, limit int) ([]sourceBucketOutcomeCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_id, newsgroup_id, source_day, last_ingested_at, last_progress_at
		FROM indexer_source_bucket_state
		WHERE state IN ('active', 'success', 'no_yield')
		ORDER BY
			CASE WHEN state = 'active' THEN 0 ELSE 1 END,
			last_reconciled_at NULLS FIRST,
			last_progress_at,
			source_day
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list source bucket outcome candidates: %w", err)
	}
	defer rows.Close()
	out := make([]sourceBucketOutcomeCandidate, 0, limit)
	for rows.Next() {
		var candidate sourceBucketOutcomeCandidate
		if err := rows.Scan(&candidate.ProviderID, &candidate.NewsgroupID, &candidate.SourceDay, &candidate.LastIngestedAt, &candidate.LastProgressAt); err != nil {
			return nil, fmt.Errorf("scan source bucket outcome candidate: %w", err)
		}
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source bucket outcome candidates: %w", err)
	}
	return out, nil
}

func (s *Store) loadSourceBucketOutcomeFacts(ctx context.Context, candidate sourceBucketOutcomeCandidate, terminalAttempts int) (sourceBucketOutcomeFacts, error) {
	start := candidate.SourceDay.UTC()
	end := start.AddDate(0, 0, 1)
	var facts sourceBucketOutcomeFacts
	var progress sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH progress AS (
			SELECT MAX(updated_at) AS at FROM article_header_assembly_queue
			 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4
			UNION ALL
			SELECT MAX(updated_at) FROM yenc_recovery_work_items
			 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4
			UNION ALL
			SELECT MAX(updated_at) FROM binary_lifecycle
			 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4
			UNION ALL
			SELECT MAX(updated_at) FROM release_stage_dirty_families
			 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4
		),
		open_work AS (
			SELECT (
				(SELECT COUNT(*) FROM article_header_assembly_queue q
				 WHERE q.provider_id = $1 AND q.newsgroup_id = $2 AND q.source_posted_at >= $3 AND q.source_posted_at < $4
				   AND NOT EXISTS (SELECT 1 FROM binary_parts bp WHERE bp.source_posted_at = q.source_posted_at AND bp.article_header_id = q.article_header_id)) +
				(SELECT COUNT(*) FROM yenc_recovery_work_items
				 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4 AND status IN ('ready', 'running')) +
				(SELECT COUNT(*) FROM article_cohort_assembly_queue
				 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4 AND status IN ('ready', 'running')) +
				(SELECT COUNT(*) FROM article_cohort_yenc_queue
				 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4 AND status IN ('ready', 'admitted')) +
				(SELECT COUNT(*) FROM binary_inspection_ready_queue rq
				 JOIN binary_core bc ON bc.binary_id = rq.binary_id
				 WHERE bc.provider_id = $1 AND bc.newsgroup_id = $2 AND rq.source_posted_at >= $3 AND rq.source_posted_at < $4 AND rq.status IN ('ready', 'running')) +
				(SELECT COUNT(*) FROM release_stage_dirty_families
				 WHERE provider_id = $1 AND newsgroup_id = $2 AND source_posted_at >= $3 AND source_posted_at < $4)
			)::bigint AS count
		),
		exhausted AS (
			SELECT COUNT(*)::bigint AS count
			FROM yenc_recovery_work_items
			WHERE provider_id = $1 AND newsgroup_id = $2
			  AND source_posted_at >= $3 AND source_posted_at < $4
			  AND (status IN ('done', 'stale') OR missing_count >= $5)
		),
		terminal_releases AS (
			SELECT COUNT(DISTINCT ras.release_id)::bigint AS count
			FROM article_headers ah
			JOIN release_archive_lineage_article_headers lah ON lah.article_header_id = ah.id
			JOIN release_archive_state ras ON ras.release_id = lah.release_id
			JOIN nzb_cache nzb ON nzb.release_id = ras.release_id AND nzb.generation_status = 'ready'
			WHERE ah.provider_id = $1 AND ah.newsgroup_id = $2
			  AND ah.source_posted_at >= $3 AND ah.source_posted_at < $4
			  AND ras.archive_status IN ('purge_pending', 'purged')
			  AND COALESCE(ras.object_key, '') <> ''
			  AND COALESCE(ras.content_hash_sha256, '') <> ''
			  AND EXISTS (SELECT 1 FROM release_catalog_files rcf WHERE rcf.release_id = ras.release_id)
		)
		SELECT (SELECT MAX(at) FROM progress), open_work.count, exhausted.count, terminal_releases.count
		FROM open_work, exhausted, terminal_releases`, candidate.ProviderID, candidate.NewsgroupID, start, end, terminalAttempts).Scan(
		&progress, &facts.OpenWorkCount, &facts.ExhaustedWorkCount, &facts.TerminalReleaseCount,
	)
	if err != nil {
		return sourceBucketOutcomeFacts{}, fmt.Errorf("load outcome facts provider=%d group=%d day=%s: %w", candidate.ProviderID, candidate.NewsgroupID, start.Format("2006-01-02"), err)
	}
	if progress.Valid {
		facts.LastProgressAt = progress.Time.UTC()
	}
	return facts, nil
}

func (s *Store) updateSourceBucketOutcome(ctx context.Context, candidate sourceBucketOutcomeCandidate, lastProgress time.Time, facts sourceBucketOutcomeFacts, state, reason string, settled bool, purgeEligibleAt *time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE indexer_source_bucket_state
		SET state = $4,
		    last_progress_at = GREATEST(last_progress_at, $5),
		    settled_at = CASE WHEN $6 THEN COALESCE(settled_at, NOW()) ELSE NULL END,
		    open_work_count = $7,
		    exhausted_work_count = $8,
		    terminal_release_count = $9,
		    terminal_reason = $10,
		    purge_eligible_at = $11,
		    last_reconciled_at = NOW(),
		    updated_at = NOW()
		WHERE provider_id = $1 AND newsgroup_id = $2 AND source_day = $3`,
		candidate.ProviderID, candidate.NewsgroupID, candidate.SourceDay, state, lastProgress, settled,
		facts.OpenWorkCount, facts.ExhaustedWorkCount, facts.TerminalReleaseCount, reason, purgeEligibleAt,
	)
	if err != nil {
		return fmt.Errorf("update source bucket outcome provider=%d group=%d day=%s: %w", candidate.ProviderID, candidate.NewsgroupID, candidate.SourceDay.Format("2006-01-02"), err)
	}
	return nil
}

func ptrUTC(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
