package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	yencRecoverySeedScanLowYieldThreshold = 25
	yencRecoveryReadyWindowMultiplier     = 8
	yencRecoveryReadyWindowMax            = 8000
	yencHeaderRecoveryApplyBatchSize      = 500
	yencRecoveryFairnessStageName         = "recover_yenc"
	yencRecoveryFairnessBucketSeconds     = 5 * 60
	// Historical yEnc queues can span years; keep the newest exclusion but let
	// the fairness cursor backfill across the whole retained article window.
	yencRecoveryFairnessHorizon          = 10 * 365 * 24 * time.Hour
	yencRecoveryFairnessNewestExclusion  = 30 * time.Minute
	yencRecoveryFairnessBaseQuota        = 25
	yencRecoveryFairnessMaxQuota         = 60
	yencRecoveryFairnessQuotaStep        = 10
	yencRecoveryFairnessMaxBucketsPerRun = 256
)

type yencRecoveryFairnessState struct {
	CursorBefore *time.Time
	BucketStart  *time.Time
	BucketEnd    *time.Time
	QuotaPercent int
	RepeatFull   int
	WrappedCount int64
}

type yencRecoveryFairnessBucket struct {
	Start   time.Time
	End     time.Time
	Quota   int
	Wrapped bool
}

type YEncRecoverySelectionOptions struct {
	TargetWindowStart   *time.Time
	TargetWindowEnd     *time.Time
	TargetWindowPercent int
	NewestPercent       int
}

type YEncRecoverySelectionStats struct {
	BatchRequested    int
	BatchSelected     int
	ReadyCount        int
	Priority0Ready    int
	PrioritySeedLimit int
	PrioritySeeded    int64
	PriorityRetired   int64
	GenericSeedLimit  int
	GenericSeeded     int64
	GenericRetired    int64
	SeedDuration      time.Duration
	WindowedRequested int
	NewestRequested   int
	SelectedWindowed  int
	SelectedNewest    int
	BucketsScanned    int
	EmptyBuckets      int
}

type YEncRecoveryApplyStats struct {
	Records                     int
	Results                     int
	Merged                      int
	InternalBatches             int
	NormalizeDuration           time.Duration
	TotalDuration               time.Duration
	BeginDuration               time.Duration
	StageDuration               time.Duration
	OrderDuration               time.Duration
	IdentityLockDuration        time.Duration
	MutationDuration            time.Duration
	SeedLoadDuration            time.Duration
	TargetFindDuration          time.Duration
	PairLockDuration            time.Duration
	TargetUpdateDuration        time.Duration
	TargetUpdateSkipped         int
	PartsMergeDuration          time.Duration
	ReleaseFilesMergeDuration   time.Duration
	SourceDeleteDuration        time.Duration
	SourceSupersedeDuration     time.Duration
	IngestPayloadUpdateDuration time.Duration
	WorkItemDoneUpdateDuration  time.Duration
	StatsRefreshDuration        time.Duration
	SummaryDirtyDuration        time.Duration
	CommitDuration              time.Duration
}

func (o YEncRecoverySelectionOptions) HasTargetWindow() bool {
	return o.TargetWindowStart != nil && o.TargetWindowEnd != nil && o.TargetWindowStart.Before(*o.TargetWindowEnd)
}

func (o YEncRecoverySelectionOptions) HasValidSplit() bool {
	return o.TargetWindowPercent >= 0 &&
		o.TargetWindowPercent <= 100 &&
		o.NewestPercent >= 0 &&
		o.NewestPercent <= 100 &&
		o.TargetWindowPercent+o.NewestPercent == 100
}

func yencRecoveryReadyWindowLimit(limit int) int {
	if limit <= 0 {
		return yencRecoverySeedScanLowYieldThreshold
	}
	window := limit * yencRecoveryReadyWindowMultiplier
	if window < limit {
		window = limit
	}
	if window > yencRecoveryReadyWindowMax {
		window = yencRecoveryReadyWindowMax
	}
	return window
}

func (s *Store) ListYEncRecoveryCandidates(ctx context.Context, limit int) ([]YEncRecoveryCandidate, error) {
	return s.ListYEncRecoveryCandidatesWithOptions(ctx, limit, YEncRecoverySelectionOptions{})
}

func (s *Store) ListYEncRecoveryCandidatesWithOptions(ctx context.Context, limit int, opts YEncRecoverySelectionOptions) ([]YEncRecoveryCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	stats := YEncRecoverySelectionStats{BatchRequested: limit}

	if _, err := s.retireStaleReadyYEncRecoveryWorkItems(ctx); err != nil {
		return nil, err
	}

	readyCount, err := s.countReadyYEncRecoveryCandidates(ctx, limit)
	if err != nil {
		return nil, err
	}
	stats.ReadyCount = readyCount
	if readyCount > yencRecoverySeedScanLowYieldThreshold {
		s.clearYEncRecoverySeedScanBackoff()
	}
	priority0Ready, err := s.countReadyYEncRecoveryPriority0Candidates(ctx, limit)
	if err != nil {
		return nil, err
	}
	stats.Priority0Ready = priority0Ready
	priority0Target := limit * s.yEncPriority0ReservoirBatches(ctx)
	if priority0Target < limit {
		priority0Target = limit
	}
	if priority0Ready < priority0Target && !s.shouldBackoffYEncRecoverySeedScan(time.Now()) {
		seedLimit := priority0Target - priority0Ready
		if seedLimit < limit/2 {
			seedLimit = limit / 2
		}
		if seedLimit <= 0 {
			seedLimit = limit
		}
		if seedLimit > yencRecoveryWorkItemSeedLimit {
			seedLimit = yencRecoveryWorkItemSeedLimit
		}
		stats.PrioritySeedLimit = seedLimit
		seedStarted := time.Now()
		upserted, retired, seedErr := s.BackfillPriorityYEncRecoveryWorkItems(ctx, seedLimit)
		stats.SeedDuration += time.Since(seedStarted)
		stats.PrioritySeeded = upserted
		stats.PriorityRetired = retired
		if seedErr != nil {
			return nil, seedErr
		}
		s.recordYEncRecoverySeedScanResult(time.Now(), priority0Ready, upserted)
		if upserted > 0 {
			s.clearYEncRecoverySeedScanBackoff()
		}
	}
	if readyCount == 0 {
		seedStarted := time.Now()
		upserted, retired, seedErr := s.maybeBackfillYEncRecoveryWorkItems(ctx, limit)
		stats.SeedDuration += time.Since(seedStarted)
		stats.GenericSeedLimit = limit
		stats.GenericSeeded = upserted
		stats.GenericRetired = retired
		if seedErr != nil {
			return nil, seedErr
		}
	}

	return s.listReadyYEncRecoveryCandidates(ctx, limit, opts, stats)
}

func (s *Store) LastYEncRecoverySelectionStats() YEncRecoverySelectionStats {
	if s == nil {
		return YEncRecoverySelectionStats{}
	}
	s.yencSelectionMu.Lock()
	defer s.yencSelectionMu.Unlock()
	return s.yencLastSelectionStats
}

func (s *Store) setYEncRecoverySelectionStats(stats YEncRecoverySelectionStats) {
	if s == nil {
		return
	}
	s.yencSelectionMu.Lock()
	s.yencLastSelectionStats = stats
	s.yencSelectionMu.Unlock()
}

func (s *Store) LastYEncRecoveryApplyStats() YEncRecoveryApplyStats {
	if s == nil {
		return YEncRecoveryApplyStats{}
	}
	s.yencApplyMu.Lock()
	defer s.yencApplyMu.Unlock()
	return s.yencLastApplyStats
}

func (s *Store) setYEncRecoveryApplyStats(stats YEncRecoveryApplyStats) {
	if s == nil {
		return
	}
	s.yencApplyMu.Lock()
	s.yencLastApplyStats = stats
	s.yencApplyMu.Unlock()
}

func (s *Store) maybeBackfillYEncRecoveryWorkItems(ctx context.Context, limit int) (int64, int64, error) {
	if limit <= 0 {
		limit = yencRecoveryWorkItemSeedLimit
	}
	readyCount, err := s.countReadyYEncRecoveryCandidates(ctx, limit)
	if err != nil {
		return 0, 0, err
	}
	if readyCount > 0 {
		if readyCount > yencRecoverySeedScanLowYieldThreshold {
			s.clearYEncRecoverySeedScanBackoff()
		}
		return 0, 0, nil
	}
	if s.shouldBackoffYEncRecoverySeedScan(time.Now()) {
		return 0, 0, nil
	}
	seedLimit := limit
	if seedLimit > yencRecoveryWorkItemSeedLimit {
		seedLimit = yencRecoveryWorkItemSeedLimit
	}
	upserted, retired, err := s.BackfillYEncRecoveryWorkItems(ctx, seedLimit)
	if err != nil {
		return 0, 0, err
	}
	s.recordYEncRecoverySeedScanResult(time.Now(), readyCount, upserted)
	return upserted, retired, nil
}

func (s *Store) countReadyYEncRecoveryPriority0Candidates(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	var count int
	windowLimit := yencRecoveryReadyWindowLimit(limit)
	if err := s.withParallelGatherDisabledTx(ctx, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			WITH ready_window AS (
				SELECT wi.binary_id
				FROM yenc_recovery_work_items wi
				WHERE wi.status = 'ready'
				  AND wi.ready_at <= NOW()
				  AND wi.priority_rank = 0
				  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
				ORDER BY wi.date_utc DESC NULLS LAST, wi.updated_at DESC, wi.binary_id
				LIMIT $1
			)
			SELECT COUNT(*) FROM (SELECT 1 FROM ready_window LIMIT $2) ready`,
			windowLimit,
			limit,
		).Scan(&count)
	}); err != nil {
		return 0, fmt.Errorf("count ready priority0 yenc recovery candidates: %w", err)
	}
	return count, nil
}

func (s *Store) shouldBackoffYEncRecoverySeedScan(now time.Time) bool {
	s.yencSeedScanMu.Lock()
	defer s.yencSeedScanMu.Unlock()
	return !s.yencSeedScanBackoffUntil.IsZero() && now.Before(s.yencSeedScanBackoffUntil)
}

func (s *Store) clearYEncRecoverySeedScanBackoff() {
	s.yencSeedScanMu.Lock()
	defer s.yencSeedScanMu.Unlock()
	s.yencSeedScanConsecutiveEmpty = 0
	s.yencSeedScanBackoffUntil = time.Time{}
}

func (s *Store) recordYEncRecoverySeedScanResult(now time.Time, priorReadyCount int, upserted int64) {
	s.yencSeedScanMu.Lock()
	defer s.yencSeedScanMu.Unlock()

	if priorReadyCount > yencRecoverySeedScanLowYieldThreshold || upserted > yencRecoverySeedScanLowYieldThreshold {
		s.yencSeedScanConsecutiveEmpty = 0
		s.yencSeedScanBackoffUntil = time.Time{}
		return
	}

	s.yencSeedScanConsecutiveEmpty++
	var backoff time.Duration
	switch s.yencSeedScanConsecutiveEmpty {
	case 1:
		backoff = 1 * time.Minute
	case 2:
		backoff = 5 * time.Minute
	default:
		backoff = 15 * time.Minute
	}
	s.yencSeedScanBackoffUntil = now.Add(backoff)
}

func (s *Store) retireStaleReadyYEncRecoveryWorkItems(ctx context.Context) (int64, error) {
	var retired int64
	if err := s.db.QueryRowContext(ctx, `
		WITH stale AS (
			SELECT wi.binary_id, wi.source_posted_at
			FROM yenc_recovery_work_items wi
			WHERE wi.status IN ('ready', 'running')
			  AND BTRIM(COALESCE(wi.message_id, '')) = ''
			ORDER BY wi.updated_at
			LIMIT 5000
		),
		retired AS (
			UPDATE yenc_recovery_work_items wi
			SET status = 'stale',
			    updated_at = NOW()
		FROM stale s
		WHERE wi.source_posted_at = s.source_posted_at
		  AND wi.binary_id = s.binary_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM retired`,
	).Scan(&retired); err != nil {
		return 0, fmt.Errorf("retire stale ready yenc recovery work items: %w", err)
	}
	return retired, nil
}

func (s *Store) countReadyYEncRecoveryCandidates(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	var count int
	windowLimit := yencRecoveryReadyWindowLimit(limit)
	if err := s.withParallelGatherDisabledTx(ctx, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			WITH ready_window AS (
				SELECT wi.binary_id
				FROM yenc_recovery_work_items wi
				WHERE wi.status = 'ready'
				  AND wi.ready_at <= NOW()
				  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
				ORDER BY wi.priority_rank, wi.date_utc DESC NULLS LAST, wi.updated_at DESC, wi.binary_id
				LIMIT $1
			)
			SELECT COUNT(*) FROM (SELECT 1 FROM ready_window LIMIT $2) ready`,
			windowLimit,
			limit,
		).Scan(&count)
	}); err != nil {
		return 0, fmt.Errorf("count ready yenc recovery candidates: %w", err)
	}
	return count, nil
}

func (s *Store) listReadyYEncRecoveryCandidates(ctx context.Context, limit int, opts YEncRecoverySelectionOptions, stats YEncRecoverySelectionStats) ([]YEncRecoveryCandidate, error) {
	var out []YEncRecoveryCandidate
	if stats.BatchRequested <= 0 {
		stats.BatchRequested = limit
	}
	defer func() {
		stats.BatchSelected = len(out)
		s.setYEncRecoverySelectionStats(stats)
	}()
	err := s.withParallelGatherDisabledTx(ctx, false, func(tx *sql.Tx) error {
		if err := expireYEncRecoveryRunningLeases(ctx, tx); err != nil {
			return err
		}

		out = make([]YEncRecoveryCandidate, 0, limit)
		priority, err := claimPriority0YEncRecoveryCandidates(ctx, tx, limit)
		if err != nil {
			return err
		}
		out = append(out, priority...)
		if len(out) >= limit {
			return nil
		}

		if opts.HasTargetWindow() {
			remainingLimit := limit - len(out)
			targetLimit := yencRecoveryPercentLimit(remainingLimit, opts.TargetWindowPercent)
			stats.WindowedRequested = targetLimit
			if targetLimit > 0 {
				targeted, scanned, empty, err := claimYEncRecoveryPostedWindowsBackward(ctx, tx, targetLimit, *opts.TargetWindowStart, *opts.TargetWindowEnd, "target_window")
				if err != nil {
					return err
				}
				stats.BucketsScanned += scanned
				stats.EmptyBuckets += empty
				stats.SelectedWindowed += len(targeted)
				out = append(out, targeted...)
			}
			newestLimit := yencRecoveryPercentLimit(remainingLimit, opts.NewestPercent)
			if newestLimit > limit-len(out) {
				newestLimit = limit - len(out)
			}
			if newestLimit <= 0 && len(out) == 0 && targetLimit > 0 {
				newestLimit = limit
			}
			stats.NewestRequested = newestLimit
			if newestLimit > 0 {
				newest, err := claimNewestYEncRecoveryCandidates(ctx, tx, newestLimit)
				if err != nil {
					return err
				}
				stats.SelectedNewest += len(newest)
				out = append(out, newest...)
			}
			return nil
		}

		fixedSplit := opts.HasValidSplit()
		remainingLimit := limit - len(out)
		fairnessLimit := yencRecoveryFairnessLimit(ctx, tx, remainingLimit)
		if fixedSplit {
			fairnessLimit = yencRecoveryPercentLimit(remainingLimit, opts.TargetWindowPercent)
		}
		stats.WindowedRequested = fairnessLimit
		if fairnessLimit > 0 {
			fairness, scanned, empty, err := s.claimYEncRecoveryFairnessCandidatesBackward(ctx, tx, fairnessLimit)
			if err != nil {
				return err
			}
			stats.BucketsScanned += scanned
			stats.EmptyBuckets += empty
			stats.SelectedWindowed += len(fairness)
			out = append(out, fairness...)
		}

		newestLimit := limit - len(out)
		if fixedSplit {
			newestLimit = yencRecoveryPercentLimit(remainingLimit, opts.NewestPercent)
			if newestLimit > limit-len(out) {
				newestLimit = limit - len(out)
			}
		}
		stats.NewestRequested = newestLimit
		if newestLimit > 0 {
			newest, err := claimNewestYEncRecoveryCandidates(ctx, tx, newestLimit)
			if err != nil {
				return err
			}
			stats.SelectedNewest += len(newest)
			out = append(out, newest...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list yenc recovery candidates: %w", err)
	}
	return out, nil
}

func claimPriority0YEncRecoveryCandidates(ctx context.Context, tx *sql.Tx, limit int) ([]YEncRecoveryCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, claimYEncRecoveryCandidatesSQL(`
		WHERE wi.status = 'ready'
		  AND wi.ready_at <= NOW()
		  AND wi.priority_rank = 0
		  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
		ORDER BY wi.date_utc DESC NULLS LAST, wi.updated_at DESC, wi.binary_id
		LIMIT $2`), limit, yencRecoveryReadyWindowLimit(limit))
	if err != nil {
		return nil, err
	}
	return scanClaimedYEncRecoveryCandidates(rows, "priority0", nil, nil)
}

func claimYEncRecoveryPostedWindowsBackward(ctx context.Context, tx *sql.Tx, limit int, start, end time.Time, lane string) ([]YEncRecoveryCandidate, int, int, error) {
	if limit <= 0 || !start.Before(end) {
		return nil, 0, 0, nil
	}
	window := end.Sub(start)
	if window <= 0 {
		return nil, 0, 0, nil
	}
	out := make([]YEncRecoveryCandidate, 0, limit)
	bucketsScanned := 0
	emptyBuckets := 0
	windowStart := start
	windowEnd := end
	for len(out) < limit && bucketsScanned < yencRecoveryFairnessMaxBucketsPerRun {
		bucketsScanned++
		remaining := limit - len(out)
		candidates, err := claimYEncRecoveryCandidatesInPostedRange(ctx, tx, remaining, windowStart, windowEnd, lane)
		if err != nil {
			return nil, bucketsScanned, emptyBuckets, err
		}
		if len(candidates) == 0 {
			emptyBuckets++
		}
		for i := range candidates {
			candidates[i].FairnessBucketStart = ptrTimeUTC(windowStart)
			candidates[i].FairnessBucketEnd = ptrTimeUTC(windowEnd)
		}
		out = append(out, candidates...)
		windowEnd = windowStart
		windowStart = windowStart.Add(-window)
	}
	return out, bucketsScanned, emptyBuckets, nil
}

func (s *Store) claimYEncRecoveryFairnessCandidatesBackward(ctx context.Context, tx *sql.Tx, limit int) ([]YEncRecoveryCandidate, int, int, error) {
	if limit <= 0 {
		return nil, 0, 0, nil
	}
	out := make([]YEncRecoveryCandidate, 0, limit)
	bucketsScanned := 0
	emptyBuckets := 0
	for len(out) < limit && bucketsScanned < yencRecoveryFairnessMaxBucketsPerRun {
		bucket, err := s.nextYEncRecoveryFairnessBucket(ctx, tx)
		if err != nil {
			return nil, bucketsScanned, emptyBuckets, err
		}
		if bucket == nil {
			break
		}
		bucketsScanned++
		remaining := limit - len(out)
		fairness, err := claimYEncRecoveryCandidatesInPostedRange(ctx, tx, remaining, bucket.Start, bucket.End, "time_cohort_fairness")
		if err != nil {
			return nil, bucketsScanned, emptyBuckets, err
		}
		if len(fairness) == 0 {
			emptyBuckets++
		}
		for i := range fairness {
			fairness[i].FairnessBucketStart = ptrTimeUTC(bucket.Start)
			fairness[i].FairnessBucketEnd = ptrTimeUTC(bucket.End)
		}
		out = append(out, fairness...)
		if err := s.updateYEncRecoveryFairnessStateAfterClaim(ctx, tx, bucket, len(fairness), remaining); err != nil {
			return nil, bucketsScanned, emptyBuckets, err
		}
	}
	return out, bucketsScanned, emptyBuckets, nil
}

func yencRecoveryPercentLimit(limit, percent int) int {
	if limit <= 1 || percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return limit
	}
	n := (limit * percent) / 100
	if n <= 0 {
		n = 1
	}
	if n >= limit {
		n = limit - 1
	}
	return n
}

func expireYEncRecoveryRunningLeases(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE yenc_recovery_work_items wi
		SET status = 'ready',
		    lease_owner = '',
		    lease_expires_at = NULL,
		    updated_at = NOW()
		WHERE wi.status = 'running'
		  AND wi.lease_expires_at <= NOW()`); err != nil {
		return fmt.Errorf("expire yenc recovery leases: %w", err)
	}
	return nil
}

func yencRecoveryFairnessLimit(ctx context.Context, tx *sql.Tx, limit int) int {
	if limit <= 1 {
		return 0
	}
	state, err := loadYEncRecoveryFairnessState(ctx, tx)
	if err != nil {
		return limit / 4
	}
	quota := state.QuotaPercent
	if quota <= 0 {
		quota = yencRecoveryFairnessBaseQuota
	}
	if quota > yencRecoveryFairnessMaxQuota {
		quota = yencRecoveryFairnessMaxQuota
	}
	n := (limit * quota) / 100
	if n <= 0 {
		n = 1
	}
	if n >= limit {
		n = limit - 1
	}
	return n
}

func loadYEncRecoveryFairnessState(ctx context.Context, tx *sql.Tx) (yencRecoveryFairnessState, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO yenc_recovery_fairness_state (stage_name, quota_percent)
		VALUES ($1, $2)
		ON CONFLICT (stage_name) DO NOTHING`,
		yencRecoveryFairnessStageName,
		yencRecoveryFairnessBaseQuota,
	); err != nil {
		return yencRecoveryFairnessState{}, fmt.Errorf("ensure yenc recovery fairness state: %w", err)
	}

	var state yencRecoveryFairnessState
	var cursor, bucketStart, bucketEnd sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT cursor_before, bucket_start, bucket_end, quota_percent, repeat_full_count, wrapped_count
		FROM yenc_recovery_fairness_state
		WHERE stage_name = $1
		FOR UPDATE`,
		yencRecoveryFairnessStageName,
	).Scan(&cursor, &bucketStart, &bucketEnd, &state.QuotaPercent, &state.RepeatFull, &state.WrappedCount); err != nil {
		return state, fmt.Errorf("load yenc recovery fairness state: %w", err)
	}
	if cursor.Valid {
		t := cursor.Time.UTC()
		state.CursorBefore = &t
	}
	if bucketStart.Valid {
		t := bucketStart.Time.UTC()
		state.BucketStart = &t
	}
	if bucketEnd.Valid {
		t := bucketEnd.Time.UTC()
		state.BucketEnd = &t
	}
	return state, nil
}

func (s *Store) nextYEncRecoveryFairnessBucket(ctx context.Context, tx *sql.Tx) (*yencRecoveryFairnessBucket, error) {
	state, err := loadYEncRecoveryFairnessState(ctx, tx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	cutoff := now.Add(-yencRecoveryFairnessNewestExclusion)
	horizon := now.Add(-yencRecoveryFairnessHorizon)
	cursorBefore := cutoff
	if state.CursorBefore != nil && state.CursorBefore.Before(cutoff) {
		cursorBefore = *state.CursorBefore
	}

	if state.BucketStart != nil && state.BucketEnd != nil &&
		state.BucketEnd.After(horizon) &&
		!state.BucketStart.After(cutoff) {
		count, err := countReadyYEncRecoveryInPostedRange(ctx, tx, *state.BucketStart, *state.BucketEnd)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return &yencRecoveryFairnessBucket{Start: *state.BucketStart, End: *state.BucketEnd, Quota: normalizedYEncRecoveryQuota(state.QuotaPercent)}, nil
		}
		cursorBefore = *state.BucketStart
	}

	bucket, err := findYEncRecoveryFairnessBucketBefore(ctx, tx, cursorBefore, horizon)
	if err != nil {
		return nil, err
	}
	if bucket != nil {
		bucket.Quota = normalizedYEncRecoveryQuota(state.QuotaPercent)
		return bucket, nil
	}

	bucket, err = findYEncRecoveryFairnessBucketBefore(ctx, tx, cutoff, horizon)
	if err != nil {
		return nil, err
	}
	if bucket == nil {
		return nil, nil
	}
	bucket.Quota = yencRecoveryFairnessBaseQuota
	bucket.Wrapped = true
	return bucket, nil
}

func countReadyYEncRecoveryInPostedRange(ctx context.Context, tx *sql.Tx, start, end time.Time) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM yenc_recovery_work_items wi
		WHERE wi.status = 'ready'
		  AND wi.ready_at <= NOW()
		  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
		  AND wi.date_utc >= $1
		  AND wi.date_utc < $2`,
		start,
		end,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count ready yenc recovery fairness bucket: %w", err)
	}
	return count, nil
}

func findYEncRecoveryFairnessBucketBefore(ctx context.Context, tx *sql.Tx, before, horizon time.Time) (*yencRecoveryFairnessBucket, error) {
	var posted sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT wi.date_utc
		FROM yenc_recovery_work_items wi
		WHERE wi.status = 'ready'
		  AND wi.ready_at <= NOW()
		  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
		  AND wi.date_utc IS NOT NULL
		  AND wi.date_utc < $1
		  AND wi.date_utc >= $2
		ORDER BY wi.date_utc DESC NULLS LAST, wi.priority_rank, wi.binary_id
		LIMIT 1`,
		before,
		horizon,
	).Scan(&posted); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find yenc recovery fairness bucket: %w", err)
	}
	if !posted.Valid {
		return nil, nil
	}
	bucketStart := yencRecoveryBucketStart(posted.Time.UTC())
	return &yencRecoveryFairnessBucket{
		Start: bucketStart,
		End:   bucketStart.Add(time.Duration(yencRecoveryFairnessBucketSeconds) * time.Second),
	}, nil
}

func yencRecoveryBucketStart(t time.Time) time.Time {
	seconds := t.Unix()
	bucket := int64(yencRecoveryFairnessBucketSeconds)
	return time.Unix((seconds/bucket)*bucket, 0).UTC()
}

func (s *Store) updateYEncRecoveryFairnessStateAfterClaim(ctx context.Context, tx *sql.Tx, bucket *yencRecoveryFairnessBucket, claimed, limit int) error {
	if bucket == nil {
		return nil
	}
	remaining, err := countReadyYEncRecoveryInPostedRange(ctx, tx, bucket.Start, bucket.End)
	if err != nil {
		return err
	}
	state, err := loadYEncRecoveryFairnessState(ctx, tx)
	if err != nil {
		return err
	}
	repeat := 0
	quota := yencRecoveryFairnessBaseQuota
	cursorBefore := bucket.Start
	bucketStart := sql.NullTime{}
	bucketEnd := sql.NullTime{}
	if claimed >= limit && remaining > 0 {
		repeat = state.RepeatFull + 1
		quota = yencRecoveryFairnessBaseQuota + repeat*yencRecoveryFairnessQuotaStep
		if quota > yencRecoveryFairnessMaxQuota {
			quota = yencRecoveryFairnessMaxQuota
		}
		cursorBefore = bucket.End
		bucketStart = sql.NullTime{Time: bucket.Start, Valid: true}
		bucketEnd = sql.NullTime{Time: bucket.End, Valid: true}
	}
	wrappedCount := state.WrappedCount
	if bucket.Wrapped {
		wrappedCount++
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE yenc_recovery_fairness_state
		SET cursor_before = $2,
		    bucket_start = $3,
		    bucket_end = $4,
		    quota_percent = $5,
		    repeat_full_count = $6,
		    wrapped_count = $7,
		    updated_at = NOW()
		WHERE stage_name = $1`,
		yencRecoveryFairnessStageName,
		cursorBefore,
		bucketStart,
		bucketEnd,
		quota,
		repeat,
		wrappedCount,
	); err != nil {
		return fmt.Errorf("update yenc recovery fairness state: %w", err)
	}
	return nil
}

func normalizedYEncRecoveryQuota(quota int) int {
	if quota <= 0 {
		return yencRecoveryFairnessBaseQuota
	}
	if quota > yencRecoveryFairnessMaxQuota {
		return yencRecoveryFairnessMaxQuota
	}
	return quota
}

func ptrTimeUTC(t time.Time) *time.Time {
	v := t.UTC()
	return &v
}

func claimNewestYEncRecoveryCandidates(ctx context.Context, tx *sql.Tx, limit int) ([]YEncRecoveryCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, claimYEncRecoveryCandidatesSQL(`
		WHERE wi.status = 'ready'
		  AND wi.ready_at <= NOW()
		  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
		ORDER BY wi.priority_rank, wi.date_utc DESC NULLS LAST, wi.updated_at DESC, wi.binary_id
		LIMIT $2`), limit, yencRecoveryReadyWindowLimit(limit))
	if err != nil {
		return nil, err
	}
	return scanClaimedYEncRecoveryCandidates(rows, "newest", nil, nil)
}

func claimYEncRecoveryCandidatesInPostedRange(ctx context.Context, tx *sql.Tx, limit int, start, end time.Time, lane string) ([]YEncRecoveryCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, claimYEncRecoveryCandidatesSQL(`
		WHERE wi.status = 'ready'
		  AND wi.ready_at <= NOW()
		  AND BTRIM(COALESCE(wi.message_id, '')) <> ''
		  AND wi.date_utc >= $2
		  AND wi.date_utc < $3
		ORDER BY wi.priority_rank, wi.date_utc DESC NULLS LAST, wi.updated_at DESC, wi.binary_id
		LIMIT $4`), limit, start, end, yencRecoveryReadyWindowLimit(limit))
	if err != nil {
		return nil, err
	}
	return scanClaimedYEncRecoveryCandidates(rows, lane, &start, &end)
}

func claimYEncRecoveryCandidatesSQL(readyWindowClause string) string {
	return `
		WITH locked_window AS (
			SELECT
				wi.binary_id,
				wi.source_posted_at,
				wi.article_header_id,
				wi.provider_id,
				wi.newsgroup_id,
				wi.newsgroup_name,
				wi.article_number,
				wi.message_id,
				wi.subject,
				wi.poster,
				wi.date_utc,
				wi.article_bytes,
				wi.article_lines,
				wi.xref,
				wi.subject_file_name,
				wi.subject_file_index,
				wi.subject_file_total,
				wi.yenc_part_number,
				wi.yenc_total_parts,
				wi.yenc_file_size,
				wi.missing_count,
				wi.ready_at,
				wi.current_binary_key,
				wi.current_release_family_key,
				wi.current_base_stem,
				wi.current_readiness_bucket,
				wi.structured_identity_binary_matched,
				wi.priority_rank,
				wi.admission_reason,
				wi.group_tier,
				wi.updated_at
			FROM yenc_recovery_work_items wi
			` + readyWindowClause + `
			FOR UPDATE SKIP LOCKED
		),
		ready_window AS (
			SELECT
				lw.*,
				date_trunc('minute', COALESCE(lw.date_utc, lw.updated_at)) AS posted_minute,
				CASE
					WHEN POSITION('@' IN COALESCE(lw.poster, '')) > 0
						THEN LOWER(regexp_replace(split_part(lw.poster, '@', 2), '[^a-z0-9._-]', '', 'g'))
					ELSE ''
				END AS poster_hint,
				CASE
					WHEN POSITION('@' IN BTRIM(COALESCE(lw.message_id, ''), '<>')) > 0
						THEN LOWER(regexp_replace(split_part(BTRIM(lw.message_id, '<>'), '@', 2), '[^a-z0-9._-]', '', 'g'))
					ELSE ''
				END AS message_hint,
				ROW_NUMBER() OVER (
					PARTITION BY
						lw.provider_id,
						lw.newsgroup_id,
						lw.priority_rank,
						date_trunc('minute', COALESCE(lw.date_utc, lw.updated_at)),
						CASE
							WHEN POSITION('@' IN COALESCE(lw.poster, '')) > 0
								THEN LOWER(regexp_replace(split_part(lw.poster, '@', 2), '[^a-z0-9._-]', '', 'g'))
							ELSE ''
						END,
						CASE
							WHEN POSITION('@' IN BTRIM(COALESCE(lw.message_id, ''), '<>')) > 0
								THEN LOWER(regexp_replace(split_part(BTRIM(lw.message_id, '<>'), '@', 2), '[^a-z0-9._-]', '', 'g'))
							ELSE ''
						END
					ORDER BY lw.date_utc DESC NULLS LAST, lw.article_number, lw.binary_id
				) AS group_rank
			FROM locked_window lw
		),
		selected AS (
			SELECT rw.*
			FROM ready_window rw
			ORDER BY rw.priority_rank, rw.posted_minute DESC, rw.poster_hint, rw.message_hint, rw.group_rank, rw.date_utc DESC NULLS LAST, rw.article_number, rw.binary_id
			LIMIT $1
		),
		claimed AS (
			UPDATE yenc_recovery_work_items wi
			SET status = 'running',
			    lease_owner = 'recover_yenc',
			    lease_expires_at = NOW() + INTERVAL '30 minutes',
			    updated_at = NOW()
			FROM selected s
			WHERE wi.source_posted_at = s.source_posted_at
			  AND wi.binary_id = s.binary_id
			RETURNING
				s.binary_id,
				s.article_header_id,
				s.provider_id,
				s.newsgroup_id,
				s.newsgroup_name,
				s.article_number,
				s.message_id,
				s.subject,
				s.poster,
				s.date_utc,
				s.article_bytes,
				s.article_lines,
				s.xref,
				s.subject_file_name,
				s.subject_file_index,
				s.subject_file_total,
				s.yenc_part_number,
				s.yenc_total_parts,
				s.yenc_file_size,
				s.missing_count,
				s.ready_at,
				s.current_binary_key,
				s.current_release_family_key,
				s.current_base_stem,
				s.current_readiness_bucket,
				s.structured_identity_binary_matched,
				s.group_rank,
				s.priority_rank,
				s.admission_reason,
				s.group_tier,
				s.updated_at
		)
		SELECT
			binary_id,
			article_header_id,
			provider_id,
			newsgroup_id,
			newsgroup_name,
			article_number,
			message_id,
			subject,
			poster,
			date_utc,
			article_bytes,
			article_lines,
			xref,
			subject_file_name,
			subject_file_index,
			subject_file_total,
			yenc_part_number,
			yenc_total_parts,
			yenc_file_size,
			missing_count,
			ready_at,
			current_binary_key,
			current_release_family_key,
			current_base_stem,
			current_readiness_bucket,
			structured_identity_binary_matched,
			group_rank,
			priority_rank,
			admission_reason,
			group_tier
		FROM claimed
		ORDER BY
			priority_rank,
			date_trunc('minute', COALESCE(date_utc, updated_at)) DESC,
			group_rank,
			date_utc DESC NULLS LAST,
			article_number,
			binary_id
		LIMIT $1`
}

func scanClaimedYEncRecoveryCandidates(rows *sql.Rows, lane string, bucketStart, bucketEnd *time.Time) ([]YEncRecoveryCandidate, error) {
	defer rows.Close()
	out := make([]YEncRecoveryCandidate, 0)
	for rows.Next() {
		item, err := scanYEncRecoveryCandidateWithRank(rows)
		if err != nil {
			return nil, err
		}
		item.RecoveryLane = lane
		if bucketStart != nil {
			item.FairnessBucketStart = ptrTimeUTC(*bucketStart)
		}
		if bucketEnd != nil {
			item.FairnessBucketEnd = ptrTimeUTC(*bucketEnd)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate yenc recovery candidates: %w", err)
	}
	return out, nil
}

func (s *Store) withParallelGatherDisabledTx(ctx context.Context, readOnly bool, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_parallel_hash = off`); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordYEncRecoveryTransientFailure(ctx context.Context, articleHeaderID int64) error {
	if articleHeaderID <= 0 {
		return fmt.Errorf("article header id is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE yenc_recovery_work_items
		SET status = 'ready',
		    ready_at = NOW() + INTERVAL '15 minutes',
		    lease_owner = '',
		    lease_expires_at = NULL,
		    updated_at = NOW()
		WHERE article_header_id = $1`, articleHeaderID); err != nil {
		return fmt.Errorf("record yenc recovery transient failure for article header %d: %w", articleHeaderID, err)
	}
	return nil
}

func (s *Store) RecordYEncRecoveryTransientFailureBatch(ctx context.Context, articleHeaderIDs []int64) error {
	articleHeaderIDs = dedupeYEncRecoveryInt64s(articleHeaderIDs)
	if len(articleHeaderIDs) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		WITH requested(article_header_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		)
		UPDATE yenc_recovery_work_items wi
		SET status = 'ready',
		    ready_at = NOW() + INTERVAL '15 minutes',
		    lease_owner = '',
		    lease_expires_at = NULL,
		    updated_at = NOW()
		FROM requested r
		WHERE wi.article_header_id = r.article_header_id`,
		articleHeaderIDs,
	); err != nil {
		return fmt.Errorf("record yenc recovery transient failure batch count=%d: %w", len(articleHeaderIDs), err)
	}
	return nil
}

func scanYEncRecoveryCandidate(scanner interface{ Scan(dest ...any) error }) (YEncRecoveryCandidate, error) {
	return scanYEncRecoveryCandidateDest(scanner, nil)
}

func scanYEncRecoveryCandidateWithRank(scanner interface{ Scan(dest ...any) error }) (YEncRecoveryCandidate, error) {
	var groupRank int
	return scanYEncRecoveryCandidateDest(scanner, &groupRank)
}

func scanYEncRecoveryCandidateDest(scanner interface{ Scan(dest ...any) error }, groupRank *int) (YEncRecoveryCandidate, error) {
	var (
		item       YEncRecoveryCandidate
		date       sql.NullTime
		retryAfter sql.NullTime
	)
	dest := []any{
		&item.BinaryID,
		&item.ArticleHeaderID,
		&item.ProviderID,
		&item.NewsgroupID,
		&item.NewsgroupName,
		&item.ArticleNumber,
		&item.MessageID,
		&item.Subject,
		&item.Poster,
		&date,
		&item.Bytes,
		&item.Lines,
		&item.Xref,
		&item.FileName,
		&item.FileIndex,
		&item.FileTotal,
		&item.YEncPart,
		&item.YEncTotal,
		&item.YEncFileSize,
		&item.YEncRecoveryMissingCount,
		&retryAfter,
		&item.CurrentBinaryKey,
		&item.CurrentReleaseFamilyKey,
		&item.CurrentBaseStem,
		&item.CurrentReadinessBucket,
		&item.StructuredIdentityBinaryMatched,
	}
	if groupRank != nil {
		dest = append(dest, groupRank)
	}
	dest = append(dest, &item.PriorityRank, &item.AdmissionReason, &item.GroupTier)
	if err := scanner.Scan(dest...); err != nil {
		return YEncRecoveryCandidate{}, fmt.Errorf("scan yenc recovery candidate: %w", err)
	}
	if date.Valid {
		t := date.Time.UTC()
		item.DateUTC = &t
	}
	if retryAfter.Valid {
		t := retryAfter.Time.UTC()
		item.YEncRecoveryRetryAfter = &t
	}
	item.RawOverview = map[string]any{}
	return item, nil
}

func (s *Store) ApplyYEncHeaderRecovery(ctx context.Context, in YEncHeaderRecoveryRecord) (*YEncHeaderRecoveryResult, error) {
	if in.BinaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}
	if strings.TrimSpace(in.BinaryKey) == "" || strings.TrimSpace(in.FileName) == "" {
		return nil, fmt.Errorf("recovered binary key and file name are required")
	}
	normalizeYEncHeaderRecoveryRecord(&in)

	var result *YEncHeaderRecoveryResult
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin yenc recovery tx: %w", err)
		}
		defer rollbackTx(tx)

		var targetID int64
		var keys []releaseFamilySummaryKey
		supersededSources := []yencRecoverySupersededSource{}
		result, targetID, keys, err = applyYEncHeaderRecoveryMutationInTx(ctx, tx, in, true, nil, &supersededSources, nil)
		if err != nil {
			return err
		}
		if err := recordArticleCohortYEncRecoveredInTx(ctx, tx, []int64{in.ArticleHeaderID}); err != nil {
			return err
		}
		if err := markYEncRecoverySourcesSupersededBatch(ctx, tx, supersededSources); err != nil {
			return err
		}
		statKeys, err := refreshBinaryStatsInTx(ctx, tx, targetID)
		if err != nil {
			return err
		}
		keys = append(keys, statKeys...)
		if err := markReleaseFamiliesDirtyBatch(ctx, tx, dedupeYEncRecoverySummaryKeys(keys)); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit yenc recovery tx: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ApplyYEncHeaderRecoveries(ctx context.Context, records []YEncHeaderRecoveryRecord) ([]YEncHeaderRecoveryResult, error) {
	stats := YEncRecoveryApplyStats{Records: len(records)}
	startedTotal := time.Now()
	defer func() {
		stats.TotalDuration = time.Since(startedTotal)
		s.setYEncRecoveryApplyStats(stats)
	}()
	if len(records) == 0 {
		return nil, nil
	}

	normalizeStarted := time.Now()
	normalized := make([]YEncHeaderRecoveryRecord, 0, len(records))
	for _, record := range records {
		if record.BinaryID <= 0 {
			return nil, fmt.Errorf("binary id is required")
		}
		if strings.TrimSpace(record.BinaryKey) == "" || strings.TrimSpace(record.FileName) == "" {
			return nil, fmt.Errorf("recovered binary key and file name are required")
		}
		normalizeYEncHeaderRecoveryRecord(&record)
		normalized = append(normalized, record)
	}
	stats.NormalizeDuration += time.Since(normalizeStarted)

	out := make([]YEncHeaderRecoveryResult, 0, len(normalized))
	for start := 0; start < len(normalized); start += yencHeaderRecoveryApplyBatchSize {
		end := start + yencHeaderRecoveryApplyBatchSize
		if end > len(normalized) {
			end = len(normalized)
		}
		results, err := s.applyYEncHeaderRecoveryBatch(ctx, normalized[start:end], &stats)
		if err != nil {
			return nil, err
		}
		out = append(out, results...)
		stats.Results += len(results)
		for _, result := range results {
			if result.Merged {
				stats.Merged++
			}
		}
	}
	return out, nil
}

func (s *Store) applyYEncHeaderRecoveryBatch(ctx context.Context, records []YEncHeaderRecoveryRecord, stats *YEncRecoveryApplyStats) ([]YEncHeaderRecoveryResult, error) {
	results := make([]YEncHeaderRecoveryResult, 0, len(records))
	if len(records) == 0 {
		return results, nil
	}
	if err := retryRetryablePostgresTx(ctx, defaultRetryableTxAttempts, func() error {
		if stats != nil {
			stats.InternalBatches++
		}
		started := time.Now()
		tx, err := s.db.BeginTx(ctx, nil)
		if stats != nil {
			stats.BeginDuration += time.Since(started)
		}
		if err != nil {
			return fmt.Errorf("begin yenc recovery batch tx: %w", err)
		}
		defer rollbackTx(tx)

		started = time.Now()
		if err := stageYEncHeaderRecoveryBatch(ctx, tx, records); err != nil {
			return err
		}
		if stats != nil {
			stats.StageDuration += time.Since(started)
		}
		started = time.Now()
		orderedRowIDs, locks, err := loadYEncHeaderRecoveryBatchOrder(ctx, tx)
		if stats != nil {
			stats.OrderDuration += time.Since(started)
		}
		if err != nil {
			return err
		}
		started = time.Now()
		if err := lockBinaryIdentityKeys(ctx, tx, locks); err != nil {
			return err
		}
		if stats != nil {
			stats.IdentityLockDuration += time.Since(started)
		}

		targetIDs := make([]int64, 0, len(orderedRowIDs))
		summaryKeys := make([]releaseFamilySummaryKey, 0, len(orderedRowIDs)*4)
		chunkResults := make([]YEncHeaderRecoveryResult, 0, len(orderedRowIDs))
		recoveredArticleIDs := make([]int64, 0, len(orderedRowIDs))
		targetUpdates := make(map[int64]struct{}, len(orderedRowIDs))
		supersededSources := make([]yencRecoverySupersededSource, 0, len(orderedRowIDs))
		for _, rowID := range orderedRowIDs {
			if rowID < 0 || rowID >= len(records) {
				continue
			}
			started = time.Now()
			result, targetID, keys, err := applyYEncHeaderRecoveryMutationInTx(ctx, tx, records[rowID], false, targetUpdates, &supersededSources, stats)
			if stats != nil {
				stats.MutationDuration += time.Since(started)
			}
			if err != nil {
				if IsBinaryNotFound(err) {
					continue
				}
				return err
			}
			chunkResults = append(chunkResults, *result)
			recoveredArticleIDs = append(recoveredArticleIDs, records[rowID].ArticleHeaderID)
			targetIDs = append(targetIDs, targetID)
			summaryKeys = append(summaryKeys, keys...)
		}
		started = time.Now()
		if err := recordArticleCohortYEncRecoveredInTx(ctx, tx, recoveredArticleIDs); err != nil {
			return err
		}
		if stats != nil {
			stats.WorkItemDoneUpdateDuration += time.Since(started)
		}
		started = time.Now()
		if err := markYEncRecoverySourcesSupersededBatch(ctx, tx, supersededSources); err != nil {
			return err
		}
		if stats != nil {
			stats.SourceSupersedeDuration += time.Since(started)
		}
		started = time.Now()
		statKeys, err := refreshBinaryStatsIDsInTx(ctx, tx, dedupeYEncRecoveryInt64s(targetIDs))
		if stats != nil {
			stats.StatsRefreshDuration += time.Since(started)
		}
		if err != nil {
			return err
		}
		summaryKeys = append(summaryKeys, statKeys...)
		started = time.Now()
		if err := markReleaseFamiliesDirtyBatch(ctx, tx, dedupeYEncRecoverySummaryKeys(summaryKeys)); err != nil {
			return err
		}
		if stats != nil {
			stats.SummaryDirtyDuration += time.Since(started)
		}

		started = time.Now()
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit yenc recovery batch tx: %w", err)
		}
		if stats != nil {
			stats.CommitDuration += time.Since(started)
		}
		results = chunkResults
		return nil
	}); err != nil {
		return nil, err
	}
	return results, nil
}

func applyYEncHeaderRecoveryMutationInTx(ctx context.Context, tx *sql.Tx, in YEncHeaderRecoveryRecord, lockIdentity bool, targetUpdates map[int64]struct{}, supersededSources *[]yencRecoverySupersededSource, stats *YEncRecoveryApplyStats) (*YEncHeaderRecoveryResult, int64, []releaseFamilySummaryKey, error) {
	started := time.Now()
	seed, err := loadYEncRecoveryBinarySeed(ctx, tx, in.BinaryID)
	if stats != nil {
		stats.SeedLoadDuration += time.Since(started)
	}
	if err != nil {
		return nil, 0, nil, err
	}
	if lockIdentity {
		started = time.Now()
		if err := lockBinaryIdentityKey(ctx, tx, seed.ProviderID, seed.NewsgroupID, in.BinaryKey); err != nil {
			return nil, 0, nil, err
		}
		if stats != nil {
			stats.IdentityLockDuration += time.Since(started)
		}
	}

	started = time.Now()
	targetID, err := findYEncRecoveryTargetBinary(ctx, tx, seed.ProviderID, seed.NewsgroupID, in.BinaryKey)
	if stats != nil {
		stats.TargetFindDuration += time.Since(started)
	}
	if err != nil {
		return nil, 0, nil, err
	}

	if targetID > 0 && targetID != in.BinaryID {
		started = time.Now()
		if err := lockYEncRecoveryBinaryPair(ctx, tx, in.BinaryID, targetID); err != nil {
			return nil, 0, nil, err
		}
		if stats != nil {
			stats.PairLockDuration += time.Since(started)
		}
	}

	started = time.Now()
	targetID, err = findYEncRecoveryTargetBinary(ctx, tx, seed.ProviderID, seed.NewsgroupID, in.BinaryKey)
	if stats != nil {
		stats.TargetFindDuration += time.Since(started)
	}
	if err != nil {
		return nil, 0, nil, err
	}
	if targetID == 0 || targetID == in.BinaryID {
		started = time.Now()
		if err := updateBinaryFromYEncRecovery(ctx, tx, in.BinaryID, in); err != nil {
			return nil, 0, nil, err
		}
		if stats != nil {
			stats.TargetUpdateDuration += time.Since(started)
		}
		targetID = in.BinaryID
	} else {
		updateTarget := true
		if targetUpdates != nil {
			if _, ok := targetUpdates[targetID]; ok {
				updateTarget = false
			}
		}
		if updateTarget {
			started = time.Now()
			if err := updateBinaryFromYEncRecovery(ctx, tx, targetID, in); err != nil {
				return nil, 0, nil, err
			}
			if targetUpdates != nil {
				targetUpdates[targetID] = struct{}{}
			}
			if stats != nil {
				stats.TargetUpdateDuration += time.Since(started)
			}
		} else if stats != nil {
			stats.TargetUpdateSkipped++
		}
		started = time.Now()
		if err := mergeRecoveredBinaryParts(ctx, tx, in.BinaryID, targetID, in.FileName, in.PartNumber, in.TotalParts); err != nil {
			return nil, 0, nil, err
		}
		if stats != nil {
			stats.PartsMergeDuration += time.Since(started)
		}
		started = time.Now()
		if err := mergeRecoveredReleaseFiles(ctx, tx, in.BinaryID, targetID, in.FileName); err != nil {
			return nil, 0, nil, err
		}
		if stats != nil {
			stats.ReleaseFilesMergeDuration += time.Since(started)
		}
		record := yencRecoverySupersededSource{
			SourceBinaryID:   in.BinaryID,
			SourcePostedAt:   seed.SourcePostedAt,
			TargetBinaryID:   targetID,
			ProviderID:       seed.ProviderID,
			NewsgroupID:      seed.NewsgroupID,
			ReleaseFamilyKey: seed.ReleaseFamilyKey,
			SourceBinaryKey:  seed.BinaryKey,
		}
		if supersededSources != nil {
			*supersededSources = append(*supersededSources, record)
		} else {
			started = time.Now()
			if err := markYEncRecoverySourcesSupersededBatch(ctx, tx, []yencRecoverySupersededSource{record}); err != nil {
				return nil, 0, nil, err
			}
			if stats != nil {
				stats.SourceSupersedeDuration += time.Since(started)
			}
		}
	}

	started = time.Now()
	if _, err := tx.ExecContext(ctx, `
		WITH target_sources AS (
			SELECT binary_id, source_posted_at
			FROM binary_core
			WHERE binary_id IN ($1, $2)
			  AND source_posted_at IS NOT NULL
		)
		UPDATE yenc_recovery_work_items
		SET status = 'done',
		    ready_at = NOW(),
		    lease_owner = '',
		    lease_expires_at = NULL,
		    missing_count = 0,
		    yenc_part_number = CASE WHEN article_header_id = $3 AND $4 > 0 THEN $4 ELSE yenc_part_number END,
		    yenc_total_parts = GREATEST(yenc_total_parts, $5),
		    yenc_file_size = GREATEST(yenc_file_size, $6),
		    updated_at = NOW()
		FROM target_sources ts
		WHERE yenc_recovery_work_items.source_posted_at = ts.source_posted_at
		  AND yenc_recovery_work_items.binary_id = ts.binary_id`,
		in.BinaryID,
		targetID,
		in.ArticleHeaderID,
		in.PartNumber,
		in.TotalParts,
		in.FileSize,
	); err != nil {
		return nil, 0, nil, fmt.Errorf("mark yenc recovery work items done binary=%d target=%d: %w", in.BinaryID, targetID, err)
	}
	if stats != nil {
		stats.WorkItemDoneUpdateDuration += time.Since(started)
	}

	keys := []releaseFamilySummaryKey{
		{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "release_family", FamilyKey: seed.ReleaseFamilyKey},
		{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "base_stem", FamilyKey: seed.BaseStem},
		{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "release_family", FamilyKey: in.ReleaseFamilyKey},
		{ProviderID: seed.ProviderID, NewsgroupID: seed.NewsgroupID, KeyKind: "base_stem", FamilyKey: in.BaseStem},
	}
	result := &YEncHeaderRecoveryResult{BinaryID: in.BinaryID, TargetBinaryID: targetID, Merged: targetID != in.BinaryID}
	return result, targetID, keys, nil
}

func stageYEncHeaderRecoveryBatch(ctx context.Context, tx *sql.Tx, records []YEncHeaderRecoveryRecord) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS yenc_header_recovery_batch (
			row_id integer PRIMARY KEY,
			binary_id bigint NOT NULL,
			article_header_id bigint NOT NULL,
			binary_key text NOT NULL,
			file_name text NOT NULL
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create yenc recovery batch temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `TRUNCATE yenc_header_recovery_batch`); err != nil {
		return fmt.Errorf("truncate yenc recovery batch temp table: %w", err)
	}
	const chunkSize = 500
	for start := 0; start < len(records); start += chunkSize {
		end := start + chunkSize
		if end > len(records) {
			end = len(records)
		}
		values := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*5)
		for i := start; i < end; i++ {
			base := len(args) + 1
			values = append(values, fmt.Sprintf("($%d::integer,$%d::bigint,$%d::bigint,$%d::text,$%d::text)", base, base+1, base+2, base+3, base+4))
			args = append(args, i, records[i].BinaryID, records[i].ArticleHeaderID, records[i].BinaryKey, records[i].FileName)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO yenc_header_recovery_batch (
				row_id,
				binary_id,
				article_header_id,
				binary_key,
				file_name
			)
			VALUES %s`, strings.Join(values, ",")), args...); err != nil {
			return fmt.Errorf("stage yenc recovery batch rows %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func loadYEncHeaderRecoveryBatchOrder(ctx context.Context, tx *sql.Tx) ([]int, []binaryIdentityLock, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			b.row_id,
			bc.provider_id,
			bc.newsgroup_id,
			b.binary_key
		FROM yenc_header_recovery_batch b
		JOIN binary_core bc ON bc.binary_id = b.binary_id
		ORDER BY bc.provider_id, bc.newsgroup_id, b.binary_key, b.binary_id, b.row_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("load yenc recovery batch order: %w", err)
	}
	defer rows.Close()

	rowIDs := []int{}
	locks := []binaryIdentityLock{}
	for rows.Next() {
		var (
			rowID       int
			providerID  int64
			newsgroupID int64
			binaryKey   string
		)
		if err := rows.Scan(&rowID, &providerID, &newsgroupID, &binaryKey); err != nil {
			return nil, nil, fmt.Errorf("scan yenc recovery batch order: %w", err)
		}
		rowIDs = append(rowIDs, rowID)
		locks = append(locks, binaryIdentityLock{ProviderID: providerID, NewsgroupID: newsgroupID, BinaryKey: binaryKey})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate yenc recovery batch order: %w", err)
	}
	return rowIDs, locks, nil
}

func lockYEncRecoveryBinaryPair(ctx context.Context, tx *sql.Tx, sourceID, targetID int64) error {
	if sourceID <= 0 || targetID <= 0 || sourceID == targetID {
		return nil
	}
	first, second := sourceID, targetID
	if second < first {
		first, second = second, first
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT binary_id
		FROM binary_core
		WHERE binary_id = ANY($1::bigint[])
		ORDER BY binary_id
		FOR UPDATE`,
		[]int64{first, second},
	)
	if err != nil {
		return fmt.Errorf("lock yenc recovery binary pair source=%d target=%d: %w", sourceID, targetID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan yenc recovery binary pair lock: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate yenc recovery binary pair lock: %w", err)
	}
	return nil
}

func dedupeYEncRecoveryInt64s(in []int64) []int64 {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(in))
	out := make([]int64, 0, len(in))
	for _, value := range in {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type yencRecoveryBinarySeed struct {
	ID               int64
	SourcePostedAt   time.Time
	ProviderID       int64
	NewsgroupID      int64
	ReleaseFamilyKey string
	BaseStem         string
	BinaryKey        string
}

type yencRecoverySupersededSource struct {
	SourceBinaryID   int64
	SourcePostedAt   time.Time
	TargetBinaryID   int64
	ProviderID       int64
	NewsgroupID      int64
	ReleaseFamilyKey string
	SourceBinaryKey  string
}

func loadYEncRecoveryBinarySeed(ctx context.Context, tx *sql.Tx, binaryID int64) (yencRecoveryBinarySeed, error) {
	var seed yencRecoveryBinarySeed
	err := tx.QueryRowContext(ctx, `
		SELECT
				bc.binary_id,
				COALESCE(bc.source_posted_at, bic.source_posted_at, NOW()),
				bc.provider_id,
				bc.newsgroup_id,
			bic.release_family_key,
			bic.base_stem,
			bc.binary_key
			FROM binary_core bc
			JOIN binary_identity_current bic
			  ON bic.source_posted_at = bc.source_posted_at
			 AND bic.binary_id = bc.binary_id
			WHERE bc.binary_id = $1
			  AND bc.source_posted_at IS NOT NULL
			FOR UPDATE OF bc, bic`,
		binaryID,
	).Scan(&seed.ID, &seed.SourcePostedAt, &seed.ProviderID, &seed.NewsgroupID, &seed.ReleaseFamilyKey, &seed.BaseStem, &seed.BinaryKey)
	if err == sql.ErrNoRows {
		return seed, fmt.Errorf("%w: %d for yenc recovery", ErrBinaryNotFound, binaryID)
	}
	if err != nil {
		return seed, fmt.Errorf("load yenc recovery binary %d: %w", binaryID, err)
	}
	return seed, nil
}

func markYEncRecoverySourcesSupersededBatch(ctx context.Context, tx *sql.Tx, records []yencRecoverySupersededSource) error {
	if len(records) == 0 {
		return nil
	}
	bySource := make(map[int64]yencRecoverySupersededSource, len(records))
	targetIDs := make([]int64, 0, len(records))
	seenTargets := make(map[int64]struct{}, len(records))
	for _, record := range records {
		if record.SourceBinaryID <= 0 || record.TargetBinaryID <= 0 || record.SourceBinaryID == record.TargetBinaryID {
			continue
		}
		bySource[record.SourceBinaryID] = record
		if _, ok := seenTargets[record.TargetBinaryID]; !ok {
			seenTargets[record.TargetBinaryID] = struct{}{}
			targetIDs = append(targetIDs, record.TargetBinaryID)
		}
	}
	if len(bySource) == 0 {
		return nil
	}
	sort.Slice(targetIDs, func(i, j int) bool { return targetIDs[i] < targetIDs[j] })
	targetPlaceholders := make([]string, 0, len(targetIDs))
	targetArgs := make([]any, 0, len(targetIDs))
	for _, targetID := range targetIDs {
		targetArgs = append(targetArgs, targetID)
		targetPlaceholders = append(targetPlaceholders, fmt.Sprintf("$%d", len(targetArgs)))
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT binary_id, binary_key
		FROM binary_core
		WHERE binary_id IN (`+strings.Join(targetPlaceholders, ",")+`)`,
		targetArgs...,
	)
	if err != nil {
		return fmt.Errorf("load yenc superseded target binaries: %w", err)
	}
	targetKeys := make(map[int64]string, len(targetIDs))
	for rows.Next() {
		var targetID int64
		var targetBinaryKey string
		if err := rows.Scan(&targetID, &targetBinaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan yenc superseded target binary: %w", err)
		}
		targetKeys[targetID] = targetBinaryKey
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate yenc superseded target binaries: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close yenc superseded target binaries: %w", err)
	}

	sourceIDs := make([]int64, 0, len(bySource))
	for sourceID := range bySource {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Slice(sourceIDs, func(i, j int) bool { return sourceIDs[i] < sourceIDs[j] })

	values := make([]string, 0, len(sourceIDs))
	args := make([]any, 0, len(sourceIDs)*8)
	for _, sourceID := range sourceIDs {
		record := bySource[sourceID]
		targetBinaryKey, ok := targetKeys[record.TargetBinaryID]
		if !ok {
			return fmt.Errorf("load yenc superseded target binary %d: %w", record.TargetBinaryID, sql.ErrNoRows)
		}
		base := len(args) + 1
		values = append(values, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,'yenc_recovery_merge',NOW())", base, base+1, base+2, base+3, base+4, base+5, base+6, base+7))
		args = append(args,
			record.SourceBinaryID,
			record.SourcePostedAt,
			record.TargetBinaryID,
			record.ProviderID,
			record.NewsgroupID,
			record.ReleaseFamilyKey,
			record.SourceBinaryKey,
			targetBinaryKey,
		)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binary_superseded_sources (
			source_binary_id,
			source_posted_at,
			target_binary_id,
			provider_id,
			newsgroup_id,
			release_family_key,
			source_binary_key,
			target_binary_key,
			superseded_reason,
			superseded_at
		)
		VALUES `+strings.Join(values, ",")+`
		ON CONFLICT (source_posted_at, source_binary_id) DO UPDATE
		SET target_binary_id = EXCLUDED.target_binary_id,
		    provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    release_family_key = EXCLUDED.release_family_key,
		    source_binary_key = EXCLUDED.source_binary_key,
		    target_binary_key = EXCLUDED.target_binary_key,
		    superseded_reason = EXCLUDED.superseded_reason,
		    superseded_at = EXCLUDED.superseded_at,
		    purged_at = NULL`,
		args...,
	); err != nil {
		return fmt.Errorf("record %d yenc superseded source binaries: %w", len(sourceIDs), err)
	}

	values = values[:0]
	args = args[:0]
	for _, sourceID := range sourceIDs {
		record := bySource[sourceID]
		base := len(args) + 1
		values = append(values, fmt.Sprintf("($%d,$%d,$%d,$%d,'superseded',NOW())", base, base+1, base+2, base+3))
		args = append(args, record.SourceBinaryID, record.SourcePostedAt, record.ProviderID, record.NewsgroupID)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binary_lifecycle (
			binary_id,
			source_posted_at,
			provider_id,
			newsgroup_id,
			lifecycle_status,
			updated_at
		)
		VALUES `+strings.Join(values, ",")+`
		ON CONFLICT (source_posted_at, binary_id) DO UPDATE
		SET provider_id = EXCLUDED.provider_id,
		    newsgroup_id = EXCLUDED.newsgroup_id,
		    lifecycle_status = 'superseded',
		    updated_at = NOW()`,
		args...,
	); err != nil {
		return fmt.Errorf("mark %d yenc source binaries superseded: %w", len(sourceIDs), err)
	}
	return nil
}

func findYEncRecoveryTargetBinary(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, binaryKey string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT binary_id
		FROM binary_core
		WHERE provider_id = $1
		  AND newsgroup_id = $2
		  AND binary_key = $3
		FOR UPDATE`,
		providerID,
		newsgroupID,
		strings.TrimSpace(binaryKey),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find yenc recovery target binary: %w", err)
	}
	return id, nil
}

func updateBinaryFromYEncRecovery(ctx context.Context, tx *sql.Tx, binaryID int64, in YEncHeaderRecoveryRecord) error {
	var sourcePostedAt time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT source_posted_at
		FROM binary_core
		WHERE binary_id = $1
		  AND source_posted_at IS NOT NULL`,
		binaryID,
	).Scan(&sourcePostedAt); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: %d for yenc recovery update", ErrBinaryNotFound, binaryID)
		}
		return fmt.Errorf("load yenc recovered binary source_posted_at %d: %w", binaryID, err)
	}

	groupingSummaryKind, groupingSummaryStatus, groupingSummaryFallbackUsed := groupingSummaryScalars(sanitizeStringMap(in.GroupingEvidence))
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_core
		SET binary_key = $2,
		    updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
		in.BinaryKey,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary core %d: %w", binaryID, err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE binary_identity_current
		SET source_release_key = $2,
		    release_family_key = $3,
		    file_set_key = $4,
		    file_family_key = $5,
		    identity_strength = $6,
		    identity_reason = $7,
		    subject_set_token = $8,
		    subject_set_kind = $9,
		    family_kind = $10,
		    base_stem = $11,
		    is_auxiliary = $12,
		    is_main_payload = $13,
		    release_key = $14,
		    release_name = $15,
		    binary_name = $16,
		    file_name = $17,
		    file_index = CASE WHEN $18 > 0 THEN $18 ELSE file_index END,
		    expected_file_count = GREATEST(expected_file_count, $19),
		    match_confidence = GREATEST(match_confidence, $20),
		    match_status = $21,
		    grouping_summary_kind = $22,
		    grouping_summary_status = $23,
		    grouping_summary_fallback_used = $24,
		    updated_at = NOW()
		WHERE binary_id = $1
		  AND source_posted_at = $25`,
		binaryID,
		in.SourceReleaseKey,
		in.ReleaseFamilyKey,
		in.FileSetKey,
		in.FileFamilyKey,
		in.IdentityStrength,
		in.IdentityReason,
		in.SubjectSetToken,
		in.SubjectSetKind,
		in.FamilyKind,
		in.BaseStem,
		in.IsAuxiliary,
		in.IsMainPayload,
		in.ReleaseKey,
		in.ReleaseName,
		in.BinaryName,
		in.FileName,
		in.FileIndex,
		in.ExpectedFileCount,
		in.MatchConfidence,
		in.MatchStatus,
		groupingSummaryKind,
		groupingSummaryStatus,
		groupingSummaryFallbackUsed,
		sourcePostedAt,
	)
	if err != nil {
		return fmt.Errorf("update yenc recovered binary identity %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_observation_stats
		SET total_parts = GREATEST(total_parts, $2),
		    updated_at = NOW()
		WHERE binary_id = $1
		  AND source_posted_at = $3`,
		binaryID,
		in.TotalParts,
		sourcePostedAt,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary stats %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_recovery_current
		SET recovered_source = 'yenc_header',
		    recovered_confidence = GREATEST(recovered_confidence, $2),
		    recovered_file_name = $3,
		    recovered_at = NOW(),
		    updated_at = NOW()
		WHERE binary_id = $1
		  AND source_posted_at = $4`,
		binaryID,
		in.MatchConfidence,
		in.FileName,
		sourcePostedAt,
	); err != nil {
		return fmt.Errorf("update yenc recovered binary recovery %d: %w", binaryID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE release_files
		SET file_name = $2
		WHERE binary_id = $1`,
		binaryID,
		in.FileName,
	); err != nil {
		return fmt.Errorf("update yenc recovered release files %d: %w", binaryID, err)
	}
	if err := syncBinaryCompletionKeysForBinaryIDsInTx(ctx, tx, []int64{binaryID}); err != nil {
		return err
	}
	return nil
}

func mergeRecoveredBinaryParts(ctx context.Context, tx *sql.Tx, sourceID, targetID int64, fileName string, recoveredPartNumber, recoveredTotalParts int) error {
	var sourcePostedAt time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT source_posted_at
		FROM binary_core
		WHERE binary_id = $1
		  AND source_posted_at IS NOT NULL`,
		sourceID,
	).Scan(&sourcePostedAt); err != nil {
		return fmt.Errorf("load yenc source binary partition key %d: %w", sourceID, err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, source_posted_at, part_number, segment_bytes
		FROM binary_parts
		WHERE binary_id = $1
		  AND source_posted_at = $2
		ORDER BY part_number, id`,
		sourceID,
		sourcePostedAt,
	)
	if err != nil {
		return fmt.Errorf("list yenc source binary parts %d: %w", sourceID, err)
	}
	defer rows.Close()

	type part struct {
		ID             int64
		SourcePostedAt time.Time
		PartNumber     int
		SegmentBytes   int64
	}
	parts := []part{}
	for rows.Next() {
		var p part
		if err := rows.Scan(&p.ID, &p.SourcePostedAt, &p.PartNumber, &p.SegmentBytes); err != nil {
			return fmt.Errorf("scan yenc source part: %w", err)
		}
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate yenc source parts: %w", err)
	}

	for _, p := range parts {
		partNumber := p.PartNumber
		if recoveredPartNumber > 0 {
			partNumber = recoveredPartNumber
		}
		var existingID int64
		var existingBytes int64
		err := tx.QueryRowContext(ctx, `
			SELECT id, segment_bytes
			FROM binary_parts
			WHERE binary_id = $1
			  AND part_number = $2
			  AND source_posted_at = $3
			FOR UPDATE`,
			targetID,
			partNumber,
			p.SourcePostedAt,
		).Scan(&existingID, &existingBytes)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("lock yenc target part binary=%d part=%d: %w", targetID, partNumber, err)
		}
		if err == nil && existingBytes >= p.SegmentBytes {
			if _, err := tx.ExecContext(ctx, `DELETE FROM binary_parts WHERE source_posted_at = $1 AND id = $2`, p.SourcePostedAt, p.ID); err != nil {
				return fmt.Errorf("delete duplicate yenc source part %d: %w", p.ID, err)
			}
			continue
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM binary_parts WHERE source_posted_at = $1 AND id = $2`, p.SourcePostedAt, existingID); err != nil {
				return fmt.Errorf("delete weaker yenc target part %d: %w", existingID, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE binary_parts
			SET binary_id = $2,
			    file_name = $3,
			    part_number = $4,
			    total_parts = GREATEST(total_parts, $5),
			    updated_at = NOW()
			WHERE id = $1
			  AND source_posted_at = $6`,
			p.ID,
			targetID,
			fileName,
			partNumber,
			recoveredTotalParts,
			p.SourcePostedAt,
		); err != nil {
			return fmt.Errorf("move yenc source part %d to binary %d: %w", p.ID, targetID, err)
		}
	}
	return nil
}

func mergeRecoveredReleaseFiles(ctx context.Context, tx *sql.Tx, sourceID, targetID int64, fileName string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE release_files
		SET binary_id = $2,
		    file_name = $3
		WHERE binary_id = $1`,
		sourceID,
		targetID,
		fileName,
	); err != nil {
		return fmt.Errorf("move yenc release files from binary %d to %d: %w", sourceID, targetID, err)
	}
	return nil
}

func normalizeYEncHeaderRecoveryRecord(in *YEncHeaderRecoveryRecord) {
	if in == nil {
		return
	}
	in.SourceReleaseKey = normalizeBinaryIdentityKey(in.SourceReleaseKey)
	in.ReleaseFamilyKey = normalizeBinaryIdentityKey(firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey))
	in.FileSetKey = normalizeBinaryIdentityKey(in.FileSetKey)
	in.FileFamilyKey = normalizeBinaryIdentityKey(in.FileFamilyKey)
	in.SubjectSetToken = normalizeBinaryIdentityKey(in.SubjectSetToken)
	in.FamilyKind = strings.TrimSpace(in.FamilyKind)
	in.BaseStem = normalizeBinaryIdentityKey(in.BaseStem)
	in.ReleaseKey = normalizeBinaryIdentityKey(firstNonBlank(in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey))
	in.ReleaseName = strings.TrimSpace(in.ReleaseName)
	in.BinaryKey = strings.TrimSpace(in.BinaryKey)
	in.BinaryName = strings.TrimSpace(in.BinaryName)
	in.FileName = strings.TrimSpace(in.FileName)
	if strings.TrimSpace(in.FileName) != "" {
		if fallbackFamily := recoveredYEncFallbackFamilyKey(in); fallbackFamily != "" && normalizeBinaryIdentityKey(firstNonBlank(in.FileSetKey, in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey)) == "" {
			in.SourceReleaseKey = fallbackFamily
			in.ReleaseFamilyKey = fallbackFamily
			in.FileSetKey = fallbackFamily
			in.ReleaseKey = fallbackFamily
			if in.BaseStem == "" {
				in.BaseStem = fallbackFamily
			}
		}
	}
	if recoveredKey := recoveredYEncBinaryKey(in); recoveredKey != "" {
		in.BinaryKey = recoveredKey
		if in.FileSetKey != "" {
			in.SourceReleaseKey = in.FileSetKey
			in.ReleaseFamilyKey = firstNonBlank(in.ReleaseFamilyKey, in.FileSetKey)
			in.ReleaseKey = firstNonBlank(in.ReleaseFamilyKey, in.FileSetKey)
		}
	} else {
		in.BinaryKey = normalizeBinaryIdentityKey(in.BinaryKey)
	}
	if in.FileFamilyKey == "" {
		in.FileFamilyKey = normalizeBinaryIdentityKey(firstNonBlank(in.FileSetKey, in.ReleaseFamilyKey) + "::" + in.FileName)
	}
	in.MatchStatus = firstNonBlank(in.MatchStatus, "probable")
	if in.GroupingEvidence == nil {
		in.GroupingEvidence = map[string]any{}
	}
}

func recoveredYEncFallbackFamilyKey(in *YEncHeaderRecoveryRecord) string {
	if in == nil {
		return ""
	}
	fileKey := normalizeBinaryIdentityKey(firstNonBlank(in.FileName, in.BinaryName))
	if fileKey == "" {
		return ""
	}
	parts := []string{"yenc", fileKey}
	if in.TotalParts > 0 {
		parts = append(parts, fmt.Sprintf("parts%d", in.TotalParts))
	}
	if in.FileSize > 0 {
		parts = append(parts, fmt.Sprintf("size%d", in.FileSize))
	}
	return normalizeBinaryIdentityKey(strings.Join(parts, " "))
}

func recoveredYEncBinaryKey(in *YEncHeaderRecoveryRecord) string {
	if in == nil {
		return ""
	}
	fileKey := normalizeBinaryIdentityKey(firstNonBlank(in.FileName, in.BinaryName))
	if fileKey == "" {
		return ""
	}
	familyKey := normalizeBinaryIdentityKey(firstNonBlank(in.FileSetKey, in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey))
	if familyKey == "" {
		return ""
	}
	return familyKey + "::" + fileKey
}

func dedupeYEncRecoverySummaryKeys(in []releaseFamilySummaryKey) []releaseFamilySummaryKey {
	seen := make(map[releaseFamilySummaryKey]struct{}, len(in))
	out := make([]releaseFamilySummaryKey, 0, len(in))
	for _, key := range in {
		if key.ProviderID <= 0 || key.NewsgroupID <= 0 || strings.TrimSpace(key.KeyKind) == "" || strings.TrimSpace(key.FamilyKey) == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderID != out[j].ProviderID {
			return out[i].ProviderID < out[j].ProviderID
		}
		if out[i].NewsgroupID != out[j].NewsgroupID {
			return out[i].NewsgroupID < out[j].NewsgroupID
		}
		if out[i].KeyKind != out[j].KeyKind {
			return out[i].KeyKind < out[j].KeyKind
		}
		return out[i].FamilyKey < out[j].FamilyKey
	})
	return out
}
