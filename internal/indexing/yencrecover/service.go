package yencrecover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	ListYEncRecoveryCandidates(ctx context.Context, limit int) ([]pgindex.YEncRecoveryCandidate, error)
	ApplyYEncHeaderRecovery(ctx context.Context, in pgindex.YEncHeaderRecoveryRecord) (*pgindex.YEncHeaderRecoveryResult, error)
	RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error
	RecordYEncRecoveryNoop(ctx context.Context, articleHeaderID int64) error
	RecordYEncRecoveryTransientFailure(ctx context.Context, articleHeaderID int64) error
}

type repositoryWithSelectionOptions interface {
	ListYEncRecoveryCandidatesWithOptions(ctx context.Context, limit int, opts pgindex.YEncRecoverySelectionOptions) ([]pgindex.YEncRecoveryCandidate, error)
}

type repositoryWithBatchHeaderRecovery interface {
	ApplyYEncHeaderRecoveries(ctx context.Context, in []pgindex.YEncHeaderRecoveryRecord) ([]pgindex.YEncHeaderRecoveryResult, error)
}

type repositoryWithPrioritySiblingAdmission interface {
	BackfillPriorityYEncRecoveryWorkItemsForBinaries(ctx context.Context, binaryIDs []int64) (int64, int64, error)
}

type repositoryWithBatchRecoveryBackoff interface {
	RecordYEncRecoveryNotFoundBatch(ctx context.Context, articleHeaderIDs []int64) error
	RecordYEncRecoveryNoopBatch(ctx context.Context, articleHeaderIDs []int64) error
	RecordYEncRecoveryTransientFailureBatch(ctx context.Context, articleHeaderIDs []int64) error
}

type repositoryWithSelectionStats interface {
	LastYEncRecoverySelectionStats() pgindex.YEncRecoverySelectionStats
}

type repositoryWithApplyStats interface {
	LastYEncRecoveryApplyStats() pgindex.YEncRecoveryApplyStats
}

type bodyPrefixFetcher interface {
	FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error)
}

type matcher interface {
	Match(candidate match.Candidate) match.Result
}

type Options struct {
	BatchSize           int
	MaxHeaderBytes      int64
	FetchTimeout        time.Duration
	Concurrency         int
	TargetWindowEnabled bool
	TargetWindowStart   string
	TargetWindowEnd     string
	TargetWindowPercent int
	NewestPercent       int
}

const yencRecoveryStreamFlushSize = 250

type Service struct {
	repo    repository
	matcher matcher
	fetcher bodyPrefixFetcher
	log     logger
	opts    Options
}

func NewService(repo repository, matcher matcher, fetcher bodyPrefixFetcher, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.MaxHeaderBytes <= 0 {
		opts.MaxHeaderBytes = 8192
	}
	if opts.FetchTimeout <= 0 {
		opts.FetchTimeout = 10 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.TargetWindowPercent <= 0 && opts.NewestPercent <= 0 {
		opts.TargetWindowPercent = 60
		opts.NewestPercent = 40
	}
	return &Service{repo: repo, matcher: matcher, fetcher: fetcher, log: log, opts: opts}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	metrics := map[string]any{
		"batch_size":                        s.opts.BatchSize,
		"batch_requested":                   s.opts.BatchSize,
		"max_header_bytes":                  s.opts.MaxHeaderBytes,
		"concurrency":                       s.opts.Concurrency,
		"effective_concurrency":             0,
		"batch_full":                        false,
		"batch_selected":                    0,
		"batch_fill_pct":                    float64(0),
		"candidates":                        0,
		"attempted":                         0,
		"candidate_selection_ms":            float64(0),
		"processing_ms":                     float64(0),
		"fetch_ms":                          float64(0),
		"parse_ms":                          float64(0),
		"match_ms":                          float64(0),
		"write_ms":                          float64(0),
		"write_flush_ms":                    float64(0),
		"write_flush_count":                 0,
		"write_flush_rows":                  0,
		"write_flush_max_size":              0,
		"write_flush_max_ms":                float64(0),
		"write_records_queued":              0,
		"write_records_skipped_after_error": 0,
		"writer_wait_ms":                    float64(0),
		"writer_wait_count":                 0,
		"write_failures":                    0,
		"write_apply_records":               0,
		"write_apply_results":               0,
		"write_apply_merged":                0,
		"write_apply_batches":               0,
		"write_apply_total_ms":              float64(0),
		"write_apply_normalize_ms":          float64(0),
		"write_apply_begin_ms":              float64(0),
		"write_apply_stage_ms":              float64(0),
		"write_apply_order_ms":              float64(0),
		"write_apply_identity_lock_ms":      float64(0),
		"write_apply_mutation_ms":           float64(0),
		"write_apply_seed_load_ms":          float64(0),
		"write_apply_target_find_ms":        float64(0),
		"write_apply_pair_lock_ms":          float64(0),
		"write_apply_target_update_ms":      float64(0),
		"write_apply_target_update_skipped": 0,
		"write_apply_parts_merge_ms":        float64(0),
		"write_apply_release_files_ms":      float64(0),
		"write_apply_source_delete_ms":      float64(0),
		"write_apply_source_supersede_ms":   float64(0),
		"write_apply_ingest_payload_ms":     float64(0),
		"write_apply_work_item_done_ms":     float64(0),
		"write_apply_stats_refresh_ms":      float64(0),
		"write_apply_summary_dirty_ms":      float64(0),
		"write_apply_commit_ms":             float64(0),
		"sibling_admission_ms":              float64(0),
		"sibling_admission_attempts":        0,
		"sibling_admission_upserted":        0,
		"sibling_admission_failures":        0,
		"not_found_write_ms":                float64(0),
		"recovered":                         0,
		"merged":                            0,
		"noops":                             0,
		"fetch_failures":                    0,
		"not_found":                         0,
		"parse_failures":                    0,
		"stale_candidates":                  0,
		"selected_newest":                   0,
		"selected_fairness":                 0,
		"selected_target_window":            0,
		"selected_windowed":                 0,
		"selection_buckets":                 0,
		"selection_empty_buckets":           0,
		"selection_ready_count":             0,
		"selection_priority0_ready":         0,
		"selection_priority_seed_limit":     0,
		"selection_priority_seeded":         0,
		"selection_priority_retired":        0,
		"selection_generic_seed_limit":      0,
		"selection_generic_seeded":          0,
		"selection_generic_retired":         0,
		"selection_seed_ms":                 float64(0),
		"selection_windowed_requested":      0,
		"selection_newest_requested":        0,
		"fairness_bucket_start":             "",
		"fairness_bucket_end":               "",
		"target_window_enabled":             s.opts.TargetWindowEnabled,
		"target_window_start":               s.opts.TargetWindowStart,
		"target_window_end":                 s.opts.TargetWindowEnd,
		"target_window_pct":                 s.opts.TargetWindowPercent,
		"newest_pct":                        s.opts.NewestPercent,
	}
	if s == nil || s.repo == nil || s.matcher == nil || s.fetcher == nil {
		return metrics, fmt.Errorf("yenc recovery service is not configured")
	}

	selectionStarted := time.Now()
	selectionOpts, err := s.selectionOptions()
	if err != nil {
		metrics["candidate_selection_ms"] = durationMillis(time.Since(selectionStarted))
		return metrics, err
	}
	var candidates []pgindex.YEncRecoveryCandidate
	if repo, ok := s.repo.(repositoryWithSelectionOptions); ok {
		candidates, err = repo.ListYEncRecoveryCandidatesWithOptions(ctx, s.opts.BatchSize, selectionOpts)
	} else {
		candidates, err = s.repo.ListYEncRecoveryCandidates(ctx, s.opts.BatchSize)
	}
	metrics["candidate_selection_ms"] = durationMillis(time.Since(selectionStarted))
	if err != nil {
		return metrics, fmt.Errorf("list yenc recovery candidates: %w", err)
	}
	metrics["candidates"] = len(candidates)
	metrics["batch_selected"] = len(candidates)
	if s.opts.BatchSize > 0 {
		metrics["batch_fill_pct"] = (float64(len(candidates)) / float64(s.opts.BatchSize)) * 100.0
	}
	if repo, ok := s.repo.(repositoryWithSelectionStats); ok {
		stats := repo.LastYEncRecoverySelectionStats()
		if stats.BatchRequested > 0 {
			metrics["batch_requested"] = stats.BatchRequested
			metrics["batch_selected"] = stats.BatchSelected
			if stats.BatchRequested > 0 {
				metrics["batch_fill_pct"] = (float64(stats.BatchSelected) / float64(stats.BatchRequested)) * 100.0
			}
			metrics["selection_buckets"] = stats.BucketsScanned
			metrics["selection_empty_buckets"] = stats.EmptyBuckets
			metrics["selection_ready_count"] = stats.ReadyCount
			metrics["selection_priority0_ready"] = stats.Priority0Ready
			metrics["selection_priority_seed_limit"] = stats.PrioritySeedLimit
			metrics["selection_priority_seeded"] = stats.PrioritySeeded
			metrics["selection_priority_retired"] = stats.PriorityRetired
			metrics["selection_generic_seed_limit"] = stats.GenericSeedLimit
			metrics["selection_generic_seeded"] = stats.GenericSeeded
			metrics["selection_generic_retired"] = stats.GenericRetired
			metrics["selection_seed_ms"] = durationMillis(stats.SeedDuration)
			metrics["selection_windowed_requested"] = stats.WindowedRequested
			metrics["selection_newest_requested"] = stats.NewestRequested
		}
	}
	for _, candidate := range candidates {
		recordYEncSelectionMetric(metrics, "selected", candidate)
		switch candidate.RecoveryLane {
		case "time_cohort_fairness":
			metrics["selected_fairness"] = metrics["selected_fairness"].(int) + 1
			if candidate.FairnessBucketStart != nil {
				metrics["fairness_bucket_start"] = candidate.FairnessBucketStart.Format(time.RFC3339)
			}
			if candidate.FairnessBucketEnd != nil {
				metrics["fairness_bucket_end"] = candidate.FairnessBucketEnd.Format(time.RFC3339)
			}
		case "target_window":
			metrics["selected_target_window"] = metrics["selected_target_window"].(int) + 1
		default:
			metrics["selected_newest"] = metrics["selected_newest"].(int) + 1
		}
	}
	metrics["selected_windowed"] = metrics["selected_fairness"].(int) + metrics["selected_target_window"].(int)
	if len(candidates) == 0 {
		if s.log != nil {
			s.log.Debug("recover_yenc: no recovery candidates available")
		}
		return metrics, nil
	}

	workerCount := s.opts.Concurrency
	if workerCount > len(candidates) {
		workerCount = len(candidates)
	}
	metrics["max_effective_concurrency"] = 0
	metrics["effective_concurrency"] = workerCount
	metrics["batch_full"] = len(candidates) >= s.opts.BatchSize
	batchRepo, batchWrites := s.repo.(repositoryWithBatchHeaderRecovery)
	metrics["batch_writes"] = batchWrites
	candidateByBinaryID := make(map[int64]pgindex.YEncRecoveryCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.BinaryID > 0 {
			candidateByBinaryID[candidate.BinaryID] = candidate
		}
	}
	jobs := make(chan pgindex.YEncRecoveryCandidate)
	var (
		recordCh   chan pgindex.YEncHeaderRecoveryRecord
		writerDone chan error
	)
	var (
		mu                  sync.Mutex
		wg                  sync.WaitGroup
		firstErr            error
		notFoundArticleIDs  []int64
		noopArticleIDs      []int64
		transientArticleIDs []int64
	)
	setFirstErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	recordResult := func(candidate pgindex.YEncRecoveryCandidate, outcome *yencCandidateOutcome, kind string, timings yencCandidateTimings, err error) {
		mu.Lock()
		defer mu.Unlock()
		addYEncDurationMetric(metrics, "fetch_ms", timings.Fetch)
		addYEncDurationMetric(metrics, "parse_ms", timings.Parse)
		addYEncDurationMetric(metrics, "match_ms", timings.Match)
		addYEncDurationMetric(metrics, "write_ms", timings.Write)
		addYEncDurationMetric(metrics, "not_found_write_ms", timings.NotFoundWrite)
		metrics["attempted"] = metrics["attempted"].(int) + 1
		recordYEncSelectionMetric(metrics, "attempted", candidate)
		switch kind {
		case "not_found":
			metrics["not_found"] = metrics["not_found"].(int) + 1
			recordYEncSelectionMetric(metrics, "not_found", candidate)
			if batchWrites && outcome != nil && outcome.ArticleHeaderID > 0 {
				notFoundArticleIDs = append(notFoundArticleIDs, outcome.ArticleHeaderID)
			}
		case "fetch_failure":
			metrics["fetch_failures"] = metrics["fetch_failures"].(int) + 1
			recordYEncSelectionMetric(metrics, "fetch_failure", candidate)
			if batchWrites && outcome != nil && outcome.ArticleHeaderID > 0 {
				transientArticleIDs = append(transientArticleIDs, outcome.ArticleHeaderID)
			}
		case "parse_failure":
			metrics["parse_failures"] = metrics["parse_failures"].(int) + 1
			recordYEncSelectionMetric(metrics, "parse_failure", candidate)
			if batchWrites && outcome != nil && outcome.ArticleHeaderID > 0 {
				notFoundArticleIDs = append(notFoundArticleIDs, outcome.ArticleHeaderID)
			}
		case "stale":
			metrics["stale_candidates"] = metrics["stale_candidates"].(int) + 1
			recordYEncSelectionMetric(metrics, "stale", candidate)
		case "noop":
			metrics["noops"] = metrics["noops"].(int) + 1
			recordYEncSelectionMetric(metrics, "noop", candidate)
			if batchWrites && outcome != nil && outcome.ArticleHeaderID > 0 {
				noopArticleIDs = append(noopArticleIDs, outcome.ArticleHeaderID)
			}
		case "recovered":
			metrics["recovered"] = metrics["recovered"].(int) + 1
			recordYEncSelectionMetric(metrics, "recovered", candidate)
			if outcome != nil && outcome.Result != nil && outcome.Result.Merged {
				metrics["merged"] = metrics["merged"].(int) + 1
				recordYEncSelectionMetric(metrics, "merged", candidate)
			}
		case "ready":
			recordYEncSelectionMetric(metrics, "body_matched", candidate)
		}
		attempted := metrics["attempted"].(int)
		if s.log != nil && (attempted == len(candidates) || attempted%100 == 0) {
			s.log.Info(
				"recover_yenc: progress attempted=%d/%d recovered=%d merged=%d noops=%d not_found=%d fetch_failures=%d parse_failures=%d stale_candidates=%d concurrency=%d",
				attempted,
				len(candidates),
				metrics["recovered"],
				metrics["merged"],
				metrics["noops"],
				metrics["not_found"],
				metrics["fetch_failures"],
				metrics["parse_failures"],
				metrics["stale_candidates"],
				workerCount,
			)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	queueRecoveryRecord := func(record pgindex.YEncHeaderRecoveryRecord) error {
		if recordCh == nil {
			return nil
		}
		started := time.Now()
		select {
		case recordCh <- record:
			wait := time.Since(started)
			mu.Lock()
			metrics["write_records_queued"] = metrics["write_records_queued"].(int) + 1
			if wait > 0 {
				addYEncDurationMetric(metrics, "writer_wait_ms", wait)
				metrics["writer_wait_count"] = metrics["writer_wait_count"].(int) + 1
			}
			mu.Unlock()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	processingStarted := time.Now()
	if batchWrites {
		recordCh = make(chan pgindex.YEncHeaderRecoveryRecord, maxYEncInt(yencRecoveryStreamFlushSize, workerCount*2))
		writerDone = make(chan error, 1)
		go func() {
			writerDone <- s.runStreamingRecoveryWriter(ctx, batchRepo, recordCh, candidateByBinaryID, metrics, &mu)
		}()
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					recordResult(candidate, nil, "noop", yencCandidateTimings{}, ctx.Err())
					continue
				}
				outcome, kind, timings, err := s.recoverCandidate(ctx, candidate, batchWrites)
				recordResult(candidate, outcome, kind, timings, err)
				if err == nil && outcome != nil && outcome.Record != nil {
					if queueErr := queueRecoveryRecord(*outcome.Record); queueErr != nil {
						setFirstErr(queueErr)
					}
				}
			}
		}()
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			close(jobs)
			wg.Wait()
			if recordCh != nil {
				close(recordCh)
				<-writerDone
			}
			return metrics, err
		}
		jobs <- candidate
	}
	close(jobs)
	wg.Wait()
	if recordCh != nil {
		close(recordCh)
		if err := <-writerDone; err != nil {
			setFirstErr(err)
		}
	}
	if batchWrites {
		started := time.Now()
		mu.Lock()
		notFoundBatch := append([]int64(nil), notFoundArticleIDs...)
		noopBatch := append([]int64(nil), noopArticleIDs...)
		transientBatch := append([]int64(nil), transientArticleIDs...)
		mu.Unlock()
		if err := s.recordBatchRecoveryBackoffs(ctx, notFoundBatch, noopBatch, transientBatch); err != nil {
			setFirstErr(err)
		}
		addYEncDurationMetric(metrics, "not_found_write_ms", time.Since(started))
	}
	metrics["processing_ms"] = durationMillis(time.Since(processingStarted))
	mu.Lock()
	runErr := firstErr
	mu.Unlock()
	if runErr != nil {
		return metrics, runErr
	}

	if s.log != nil {
		s.log.Info(
			"recover_yenc: candidates=%d attempted=%d recovered=%d merged=%d noops=%d not_found=%d fetch_failures=%d parse_failures=%d stale_candidates=%d selected_priority0=%d selected_priority1=%d priority0_ready=%d priority_seeded=%d generic_seeded=%d max_header_bytes=%d concurrency=%d",
			metrics["candidates"],
			metrics["attempted"],
			metrics["recovered"],
			metrics["merged"],
			metrics["noops"],
			metrics["not_found"],
			metrics["fetch_failures"],
			metrics["parse_failures"],
			metrics["stale_candidates"],
			yencIntMetric(metrics, "selected_priority_0"),
			yencIntMetric(metrics, "selected_priority_1"),
			metrics["selection_priority0_ready"],
			metrics["selection_priority_seeded"],
			metrics["selection_generic_seeded"],
			s.opts.MaxHeaderBytes,
			workerCount,
		)
	}
	return metrics, nil
}

func (s *Service) recordBatchRecoveryBackoffs(ctx context.Context, notFoundArticleIDs, noopArticleIDs, transientArticleIDs []int64) error {
	if len(notFoundArticleIDs) == 0 && len(noopArticleIDs) == 0 && len(transientArticleIDs) == 0 {
		return nil
	}
	if repo, ok := s.repo.(repositoryWithBatchRecoveryBackoff); ok {
		if len(notFoundArticleIDs) > 0 {
			if err := repo.RecordYEncRecoveryNotFoundBatch(ctx, notFoundArticleIDs); err != nil {
				return fmt.Errorf("record yenc recovery not-found batch: %w", err)
			}
		}
		if len(noopArticleIDs) > 0 {
			if err := repo.RecordYEncRecoveryNoopBatch(ctx, noopArticleIDs); err != nil {
				return fmt.Errorf("record yenc recovery noop batch: %w", err)
			}
		}
		if len(transientArticleIDs) > 0 {
			if err := repo.RecordYEncRecoveryTransientFailureBatch(ctx, transientArticleIDs); err != nil {
				return fmt.Errorf("record yenc recovery transient batch: %w", err)
			}
		}
		return nil
	}
	for _, articleID := range notFoundArticleIDs {
		if err := s.repo.RecordYEncRecoveryNotFound(ctx, articleID); err != nil {
			return err
		}
	}
	for _, articleID := range noopArticleIDs {
		if err := s.repo.RecordYEncRecoveryNoop(ctx, articleID); err != nil {
			return err
		}
	}
	for _, articleID := range transientArticleIDs {
		if err := s.repo.RecordYEncRecoveryTransientFailure(ctx, articleID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) selectionOptions() (pgindex.YEncRecoverySelectionOptions, error) {
	if s == nil {
		return pgindex.YEncRecoverySelectionOptions{}, nil
	}
	if s.opts.TargetWindowPercent < 0 || s.opts.TargetWindowPercent > 100 || s.opts.NewestPercent < 0 || s.opts.NewestPercent > 100 || s.opts.TargetWindowPercent+s.opts.NewestPercent != 100 {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("recover_yenc target and newest percentages must be 0-100 and total 100")
	}
	if !s.opts.TargetWindowEnabled {
		return pgindex.YEncRecoverySelectionOptions{
			TargetWindowPercent: s.opts.TargetWindowPercent,
			NewestPercent:       s.opts.NewestPercent,
		}, nil
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(s.opts.TargetWindowStart))
	if err != nil {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("parse recover_yenc target window start: %w", err)
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(s.opts.TargetWindowEnd))
	if err != nil {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("parse recover_yenc target window end: %w", err)
	}
	if !start.Before(end) {
		return pgindex.YEncRecoverySelectionOptions{}, fmt.Errorf("recover_yenc target window start must be before end")
	}
	return pgindex.YEncRecoverySelectionOptions{
		TargetWindowStart:   &start,
		TargetWindowEnd:     &end,
		TargetWindowPercent: s.opts.TargetWindowPercent,
		NewestPercent:       s.opts.NewestPercent,
	}, nil
}

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func addYEncDurationMetric(metrics map[string]any, key string, d time.Duration) {
	if d <= 0 {
		return
	}
	current, _ := metrics[key].(float64)
	metrics[key] = current + durationMillis(d)
}

func recordYEncSelectionMetric(metrics map[string]any, prefix string, candidate pgindex.YEncRecoveryCandidate) {
	if metrics == nil {
		return
	}
	incrementYEncIntMetric(metrics, prefix+"_priority_"+yencPriorityMetricSuffix(candidate.PriorityRank), 1)
	incrementYEncIntMetric(metrics, prefix+"_admission_"+yencMetricSuffix(candidate.AdmissionReason), 1)
	incrementYEncIntMetric(metrics, prefix+"_tier_"+yencMetricSuffix(candidate.GroupTier), 1)
	incrementYEncIntMetric(metrics, prefix+"_lane_"+yencMetricSuffix(candidate.RecoveryLane), 1)
}

func yencPriorityMetricSuffix(priority int) string {
	if priority < 0 {
		return "unknown"
	}
	if priority > 2 {
		return "2plus"
	}
	return fmt.Sprintf("%d", priority)
}

func yencMetricSuffix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func incrementYEncIntMetric(metrics map[string]any, key string, delta int) {
	metrics[key] = yencIntMetric(metrics, key) + delta
}

func yencIntMetric(metrics map[string]any, key string) int {
	if metrics == nil {
		return 0
	}
	switch v := metrics[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func (s *Service) runStreamingRecoveryWriter(ctx context.Context, repo repositoryWithBatchHeaderRecovery, records <-chan pgindex.YEncHeaderRecoveryRecord, candidateByBinaryID map[int64]pgindex.YEncRecoveryCandidate, metrics map[string]any, mu *sync.Mutex) error {
	batch := make([]pgindex.YEncHeaderRecoveryRecord, 0, yencRecoveryStreamFlushSize)
	var firstErr error
	flush := func() {
		if len(batch) == 0 {
			return
		}
		rows := len(batch)
		if siblingRepo, ok := repo.(repositoryWithPrioritySiblingAdmission); ok {
			binaryIDs := make([]int64, 0, rows)
			seen := make(map[int64]struct{}, rows)
			for _, record := range batch {
				if record.BinaryID <= 0 {
					continue
				}
				if _, ok := seen[record.BinaryID]; ok {
					continue
				}
				seen[record.BinaryID] = struct{}{}
				binaryIDs = append(binaryIDs, record.BinaryID)
			}
			if len(binaryIDs) > 0 {
				sort.Slice(binaryIDs, func(i, j int) bool { return binaryIDs[i] < binaryIDs[j] })
				started := time.Now()
				upserted, _, err := siblingRepo.BackfillPriorityYEncRecoveryWorkItemsForBinaries(ctx, binaryIDs)
				elapsed := time.Since(started)
				mu.Lock()
				addYEncDurationMetric(metrics, "sibling_admission_ms", elapsed)
				metrics["sibling_admission_attempts"] = metrics["sibling_admission_attempts"].(int) + 1
				metrics["sibling_admission_upserted"] = metrics["sibling_admission_upserted"].(int) + int(upserted)
				if err != nil {
					metrics["sibling_admission_failures"] = metrics["sibling_admission_failures"].(int) + 1
				}
				mu.Unlock()
			}
		}
		started := time.Now()
		results, err := repo.ApplyYEncHeaderRecoveries(ctx, batch)
		elapsed := time.Since(started)
		mu.Lock()
		addYEncDurationMetric(metrics, "write_ms", elapsed)
		addYEncDurationMetric(metrics, "write_flush_ms", elapsed)
		metrics["write_flush_count"] = metrics["write_flush_count"].(int) + 1
		metrics["write_flush_rows"] = metrics["write_flush_rows"].(int) + rows
		if rows > metrics["write_flush_max_size"].(int) {
			metrics["write_flush_max_size"] = rows
		}
		elapsedMS := durationMillis(elapsed)
		if elapsedMS > metrics["write_flush_max_ms"].(float64) {
			metrics["write_flush_max_ms"] = elapsedMS
		}
		if statsRepo, ok := repo.(repositoryWithApplyStats); ok {
			addYEncApplyStatsMetrics(metrics, statsRepo.LastYEncRecoveryApplyStats())
		}
		if err != nil {
			metrics["write_failures"] = metrics["write_failures"].(int) + 1
			if firstErr == nil {
				firstErr = fmt.Errorf("apply yenc recovery stream batch: %w", err)
			}
		} else {
			metrics["recovered"] = metrics["recovered"].(int) + len(results)
			for _, result := range results {
				candidate := candidateByBinaryID[result.BinaryID]
				recordYEncSelectionMetric(metrics, "recovered", candidate)
				if result.Merged {
					metrics["merged"] = metrics["merged"].(int) + 1
					recordYEncSelectionMetric(metrics, "merged", candidate)
				}
			}
		}
		mu.Unlock()
		batch = batch[:0]
	}

	for record := range records {
		if firstErr != nil {
			mu.Lock()
			metrics["write_records_skipped_after_error"] = metrics["write_records_skipped_after_error"].(int) + 1
			mu.Unlock()
			continue
		}
		batch = append(batch, record)
		if len(batch) >= yencRecoveryStreamFlushSize {
			flush()
		}
	}
	if firstErr == nil {
		flush()
	}
	return firstErr
}

func addYEncApplyStatsMetrics(metrics map[string]any, stats pgindex.YEncRecoveryApplyStats) {
	metrics["write_apply_records"] = metrics["write_apply_records"].(int) + stats.Records
	metrics["write_apply_results"] = metrics["write_apply_results"].(int) + stats.Results
	metrics["write_apply_merged"] = metrics["write_apply_merged"].(int) + stats.Merged
	metrics["write_apply_batches"] = metrics["write_apply_batches"].(int) + stats.InternalBatches
	addYEncDurationMetric(metrics, "write_apply_total_ms", stats.TotalDuration)
	addYEncDurationMetric(metrics, "write_apply_normalize_ms", stats.NormalizeDuration)
	addYEncDurationMetric(metrics, "write_apply_begin_ms", stats.BeginDuration)
	addYEncDurationMetric(metrics, "write_apply_stage_ms", stats.StageDuration)
	addYEncDurationMetric(metrics, "write_apply_order_ms", stats.OrderDuration)
	addYEncDurationMetric(metrics, "write_apply_identity_lock_ms", stats.IdentityLockDuration)
	addYEncDurationMetric(metrics, "write_apply_mutation_ms", stats.MutationDuration)
	addYEncDurationMetric(metrics, "write_apply_seed_load_ms", stats.SeedLoadDuration)
	addYEncDurationMetric(metrics, "write_apply_target_find_ms", stats.TargetFindDuration)
	addYEncDurationMetric(metrics, "write_apply_pair_lock_ms", stats.PairLockDuration)
	addYEncDurationMetric(metrics, "write_apply_target_update_ms", stats.TargetUpdateDuration)
	metrics["write_apply_target_update_skipped"] = metrics["write_apply_target_update_skipped"].(int) + stats.TargetUpdateSkipped
	addYEncDurationMetric(metrics, "write_apply_parts_merge_ms", stats.PartsMergeDuration)
	addYEncDurationMetric(metrics, "write_apply_release_files_ms", stats.ReleaseFilesMergeDuration)
	addYEncDurationMetric(metrics, "write_apply_source_delete_ms", stats.SourceDeleteDuration)
	addYEncDurationMetric(metrics, "write_apply_source_supersede_ms", stats.SourceSupersedeDuration)
	addYEncDurationMetric(metrics, "write_apply_ingest_payload_ms", stats.IngestPayloadUpdateDuration)
	addYEncDurationMetric(metrics, "write_apply_work_item_done_ms", stats.WorkItemDoneUpdateDuration)
	addYEncDurationMetric(metrics, "write_apply_stats_refresh_ms", stats.StatsRefreshDuration)
	addYEncDurationMetric(metrics, "write_apply_summary_dirty_ms", stats.SummaryDirtyDuration)
	addYEncDurationMetric(metrics, "write_apply_commit_ms", stats.CommitDuration)
}

func maxYEncInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type yencCandidateTimings struct {
	Fetch         time.Duration
	Parse         time.Duration
	Match         time.Duration
	Write         time.Duration
	NotFoundWrite time.Duration
}

type yencCandidateOutcome struct {
	Result          *pgindex.YEncHeaderRecoveryResult
	Record          *pgindex.YEncHeaderRecoveryRecord
	ArticleHeaderID int64
}

func (s *Service) recoverCandidate(ctx context.Context, candidate pgindex.YEncRecoveryCandidate, batchWrites bool) (*yencCandidateOutcome, string, yencCandidateTimings, error) {
	var timings yencCandidateTimings
	groups := candidate.FetchGroups()
	if candidate.MessageID == "" || len(groups) == 0 {
		return nil, "noop", timings, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.opts.FetchTimeout)
	defer cancel()

	started := time.Now()
	prefix, err := s.fetcher.FetchBodyPrefix(fetchCtx, candidate.MessageID, groups, s.opts.MaxHeaderBytes)
	timings.Fetch = time.Since(started)
	if err != nil {
		if errors.Is(err, nntp.ErrArticleNotFound) {
			if batchWrites {
				return &yencCandidateOutcome{ArticleHeaderID: candidate.ArticleHeaderID}, "not_found", timings, nil
			}
			started = time.Now()
			if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
				s.log.Warn("recover_yenc: failed to persist not_found backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
			}
			timings.NotFoundWrite = time.Since(started)
			return nil, "not_found", timings, nil
		}
		if s.log != nil {
			s.log.Warn("recover_yenc: fetch prefix failed article=%d binary=%d err=%v", candidate.ArticleHeaderID, candidate.BinaryID, err)
		}
		if batchWrites {
			return &yencCandidateOutcome{ArticleHeaderID: candidate.ArticleHeaderID}, "fetch_failure", timings, nil
		}
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryTransientFailure(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist transient backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "fetch_failure", timings, nil
	}

	started = time.Now()
	header, err := nzb.ReadYencHeader(bytes.NewReader(prefix))
	timings.Parse = time.Since(started)
	if err != nil {
		if batchWrites {
			return &yencCandidateOutcome{ArticleHeaderID: candidate.ArticleHeaderID}, "parse_failure", timings, nil
		}
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist parse backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "parse_failure", timings, nil
	}
	if strings.TrimSpace(header.FileName) == "" {
		if batchWrites {
			return &yencCandidateOutcome{ArticleHeaderID: candidate.ArticleHeaderID}, "noop", timings, nil
		}
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryNoop(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist noop backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "noop", timings, nil
	}

	raw := candidate.CloneRawOverview()
	raw["name"] = header.FileName
	if header.PartNumber > 0 {
		raw["part"] = header.PartNumber
	}
	if header.TotalParts > 0 {
		raw["total"] = header.TotalParts
	}
	if header.FileSize > 0 {
		raw["size"] = header.FileSize
	}

	started = time.Now()
	matched := s.matcher.Match(match.Candidate{
		ArticleNumber: candidate.ArticleNumber,
		MessageID:     candidate.MessageID,
		Subject:       candidate.Subject,
		Poster:        candidate.Poster,
		PostedAt:      candidate.DateUTC,
		Bytes:         candidate.Bytes,
		Lines:         candidate.Lines,
		Xref:          candidate.Xref,
		RawOverview:   raw,
	})
	timings.Match = time.Since(started)
	if strings.TrimSpace(matched.FileName) == "" || strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		if batchWrites {
			return &yencCandidateOutcome{ArticleHeaderID: candidate.ArticleHeaderID}, "noop", timings, nil
		}
		started = time.Now()
		if markErr := s.repo.RecordYEncRecoveryNoop(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to persist noop backoff article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		timings.NotFoundWrite = time.Since(started)
		return nil, "noop", timings, nil
	}

	started = time.Now()
	record := pgindex.YEncHeaderRecoveryRecord{
		BinaryID:          candidate.BinaryID,
		ArticleHeaderID:   candidate.ArticleHeaderID,
		SourceReleaseKey:  matched.SourceReleaseKey,
		ReleaseFamilyKey:  matched.ReleaseFamilyKey,
		FileSetKey:        matched.FileSetKey,
		FileFamilyKey:     matched.FileFamilyKey,
		IdentityStrength:  matched.IdentityStrength,
		IdentityReason:    matched.IdentityReason,
		SubjectSetToken:   matched.SubjectSetToken,
		SubjectSetKind:    matched.SubjectSetKind,
		FamilyKind:        matched.FamilyKind,
		BaseStem:          matched.BaseStem,
		IsAuxiliary:       matched.IsAuxiliary,
		IsMainPayload:     matched.IsMainPayload,
		ReleaseKey:        matched.ReleaseKey,
		ReleaseName:       matched.ReleaseName,
		BinaryKey:         matched.BinaryKey,
		BinaryName:        matched.BinaryName,
		FileName:          matched.FileName,
		FileIndex:         matched.FileIndex,
		ExpectedFileCount: matched.ExpectedFileCount,
		PartNumber:        matched.PartNumber,
		TotalParts:        matched.TotalParts,
		FileSize:          header.FileSize,
		MatchConfidence:   matched.MatchConfidence,
		MatchStatus:       matched.MatchStatus,
		GroupingEvidence:  matched.GroupingEvidence,
	}
	if batchWrites {
		return &yencCandidateOutcome{Record: &record}, "ready", timings, nil
	}

	result, err := s.repo.ApplyYEncHeaderRecovery(ctx, record)
	timings.Write = time.Since(started)
	if err != nil {
		if pgindex.IsBinaryNotFound(err) {
			if s.log != nil {
				s.log.Debug("recover_yenc: skipped stale binary article=%d binary=%d err=%v", candidate.ArticleHeaderID, candidate.BinaryID, err)
			}
			if markErr := s.repo.RecordYEncRecoveryTransientFailure(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
				s.log.Warn("recover_yenc: failed to release stale candidate article=%d err=%v", candidate.ArticleHeaderID, markErr)
			}
			return nil, "stale", timings, nil
		}
		if markErr := s.repo.RecordYEncRecoveryTransientFailure(ctx, candidate.ArticleHeaderID); markErr != nil && s.log != nil {
			s.log.Warn("recover_yenc: failed to release failed candidate article=%d err=%v", candidate.ArticleHeaderID, markErr)
		}
		return nil, "", timings, fmt.Errorf("apply yenc recovery binary=%d article=%d: %w", candidate.BinaryID, candidate.ArticleHeaderID, err)
	}
	return &yencCandidateOutcome{Result: result}, "recovered", timings, nil
}

func DefaultStage() Options {
	return Options{BatchSize: 25, MaxHeaderBytes: 8192, FetchTimeout: 10 * time.Second}
}

func DefaultInterval() time.Duration {
	return 10 * time.Minute
}
