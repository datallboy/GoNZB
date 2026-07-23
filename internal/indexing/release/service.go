package release

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	CountQueuedReleaseFamilySummaries(ctx context.Context) (int, error)
	RefreshQueuedReleaseFamilySummaries(ctx context.Context, limit int) (int, error)
	ListReleaseCandidates(ctx context.Context, limit int, opts pgindex.ReleaseCandidateSelectionOptions) ([]pgindex.ReleaseCandidate, error)
	ListExistingReleaseCandidates(ctx context.Context, limit, offset int) ([]pgindex.ReleaseCandidate, error)
	ListAutoReformReleaseCandidates(ctx context.Context, limit int, minReformAge time.Duration) ([]pgindex.ReleaseCandidate, error)
	ListExistingReleaseCandidatesForReleaseIDs(ctx context.Context, releaseIDs []string) ([]pgindex.ReleaseCandidate, error)
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, releaseFamilyKey string) ([]pgindex.BinarySummary, error)
	ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]pgindex.ReleaseTitleCandidate, error)

	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	PersistReleaseSnapshot(ctx context.Context, in pgindex.ReleaseRecord, files []pgindex.ReleaseFileRecord, newsgroupIDs []int64) (pgindex.ReleaseSnapshotResult, error)
	DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, keyKind, releaseFamilyKey string, keepGroupNames []string) error
	DeleteAuxiliaryOnlySiblingReleases(ctx context.Context, providerID, newsgroupID int64, baseStem string, keepReleaseIDs []string) error
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
	ReopenArchivedReleaseForRegeneration(ctx context.Context, releaseID string) error
	AckReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, familyKey string) error
	AckReleaseCandidates(ctx context.Context, candidates []pgindex.ReleaseCandidateAck) error
	PromoteBaseStemCandidatesForReleaseFamily(ctx context.Context, providerID, newsgroupID int64, releaseFamilyKey string) error
}

const autoReformMinAge = 15 * time.Minute

type summaryRefreshMetricsProvider interface {
	RefreshQueuedReleaseFamilySummariesWithMetrics(ctx context.Context, limit int) (pgindex.ReleaseSummaryRefreshMetrics, error)
}

type Options struct {
	BatchSize                                          int
	AutoReformBatchSize                                int
	SummaryRefreshBatchSize                            int
	SummaryRefreshMaxBatches                           int
	SummaryRefreshMaxDuration                          time.Duration
	SummaryRefreshCandidateBacklogLimit                int
	ReleaseMinConfidence                               float64
	ReleaseMinCompletion                               float64
	ReleaseMinExpectedFileCoveragePct                  float64
	RequireExpectedFileCountForContextualObfuscated    bool
	RequireExpectedFileCountForContextualObfuscatedSet bool
	ReopenArchivedNZBOnReleaseChange                   bool
}

type Service struct {
	repo repository
	log  logger
	opts Options
}

func NewService(repo repository, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1000
	}
	if opts.SummaryRefreshBatchSize <= 0 {
		opts.SummaryRefreshBatchSize = 10000
	}
	if opts.SummaryRefreshMaxBatches <= 0 {
		opts.SummaryRefreshMaxBatches = 10
	}
	if opts.SummaryRefreshMaxDuration <= 0 {
		opts.SummaryRefreshMaxDuration = 3 * time.Second
	}
	if opts.SummaryRefreshCandidateBacklogLimit <= 0 {
		opts.SummaryRefreshCandidateBacklogLimit = opts.SummaryRefreshBatchSize * 5
	}
	if opts.ReleaseMinConfidence <= 0 {
		opts.ReleaseMinConfidence = 0.55
	}
	if opts.ReleaseMinCompletion < 0 {
		opts.ReleaseMinCompletion = 0
	}
	if opts.ReleaseMinExpectedFileCoveragePct <= 0 {
		opts.ReleaseMinExpectedFileCoveragePct = 90
	}
	if opts.ReleaseMinExpectedFileCoveragePct > 100 {
		opts.ReleaseMinExpectedFileCoveragePct = 100
	}
	if !opts.RequireExpectedFileCountForContextualObfuscatedSet {
		opts.RequireExpectedFileCountForContextualObfuscated = true
	}

	return &Service{
		repo: repo,
		log:  log,
		opts: opts,
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.runOnceWithMetrics(ctx, false)
	return err
}

func (s *Service) RunReformOnce(ctx context.Context) error {
	_, err := s.runOnceWithMetrics(ctx, true)
	return err
}

func (s *Service) RunReformReleasesOnce(ctx context.Context, releaseIDs []string) error {
	if s.repo == nil {
		return fmt.Errorf("release repo is required")
	}
	candidates, err := s.repo.ListExistingReleaseCandidatesForReleaseIDs(ctx, releaseIDs)
	if err != nil {
		return fmt.Errorf("list existing release candidates for release ids: %w", err)
	}
	var timings releaseTimings
	deferredAcks := make([]pgindex.ReleaseCandidateAck, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			if flushErr := s.flushDeferredAcks(ctx, deferredAcks, &timings); flushErr != nil {
				return flushErr
			}
			return err
		}
		outcome, err := s.formCandidate(ctx, candidate, &timings)
		if err != nil {
			if flushErr := s.flushDeferredAcks(ctx, deferredAcks, &timings); flushErr != nil {
				return fmt.Errorf("%v (also failed to flush deferred release acks: %w)", err, flushErr)
			}
			return fmt.Errorf("form release candidate %s: %w", candidateFamilyKey(candidate), err)
		}
		if outcome.deferredAck != nil {
			deferredAcks = append(deferredAcks, *outcome.deferredAck)
		}
	}
	return s.flushDeferredAcks(ctx, deferredAcks, &timings)
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	return s.runOnceWithMetrics(ctx, false)
}

func (s *Service) RunReformOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	return s.runOnceWithMetrics(ctx, true)
}

func (s *Service) RunSummaryRefreshOnce(ctx context.Context) error {
	_, err := s.RunSummaryRefreshOnceWithMetrics(ctx)
	return err
}

func (s *Service) effectiveSummaryRefreshBatchSize() int {
	limit := s.opts.SummaryRefreshBatchSize
	if limit <= 0 {
		limit = 10000
	}
	return limit
}

func (s *Service) RunSummaryRefreshOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("release repo is required")
	}

	initialSummaryBacklog, err := s.repo.CountQueuedReleaseFamilySummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("count queued release family summaries: %w", err)
	}

	remainingSummaryBacklog := initialSummaryBacklog
	refreshedSummaries := 0
	dequeuedSummaries := 0
	refreshDuration := time.Duration(0)
	summaryRefreshBatches := 0
	dequeueDuration := time.Duration(0)
	summaryAggregationDuration := time.Duration(0)
	summaryAggregateDuration := time.Duration(0)
	summaryDominantDuration := time.Duration(0)
	readySyncDuration := time.Duration(0)
	recoveredFileSetDuration := time.Duration(0)
	phaseADuration := time.Duration(0)
	phaseBDuration := time.Duration(0)
	hotBatches := 0
	coldBatches := 0
	hotDequeued := 0
	coldDequeued := 0
	timeLimited := false
	runStarted := time.Now()

	for batch := 0; batch < s.opts.SummaryRefreshMaxBatches && remainingSummaryBacklog > 0; batch++ {
		if batch > 0 && s.opts.SummaryRefreshMaxDuration > 0 && time.Since(runStarted) >= s.opts.SummaryRefreshMaxDuration {
			timeLimited = true
			break
		}
		refreshLimit := s.effectiveSummaryRefreshBatchSize()
		refreshStart := time.Now()
		refreshedBatch := 0
		if metricsRepo, ok := s.repo.(summaryRefreshMetricsProvider); ok {
			refreshMetrics, batchErr := metricsRepo.RefreshQueuedReleaseFamilySummariesWithMetrics(ctx, refreshLimit)
			if batchErr != nil {
				return nil, fmt.Errorf("refresh queued release family summaries: %w", batchErr)
			}
			refreshedBatch = refreshMetrics.Refreshed
			dequeuedSummaries += refreshMetrics.Dequeued
			dequeueDuration += refreshMetrics.DequeueDuration
			summaryAggregationDuration += refreshMetrics.SummaryRefreshDuration
			summaryAggregateDuration += refreshMetrics.SummaryAggregateDuration
			summaryDominantDuration += refreshMetrics.SummaryDominantDuration
			readySyncDuration += refreshMetrics.ReadyCandidateSyncDuration
			recoveredFileSetDuration += refreshMetrics.RecoveredFileSetSyncDuration
			phaseADuration += refreshMetrics.PhaseADuration
			phaseBDuration += refreshMetrics.PhaseBDuration
			hotBatches += refreshMetrics.HotAttempts
			coldBatches += refreshMetrics.ColdAttempts
			hotDequeued += refreshMetrics.HotDequeued
			coldDequeued += refreshMetrics.ColdDequeued
		} else {
			batchCount, batchErr := s.repo.RefreshQueuedReleaseFamilySummaries(ctx, refreshLimit)
			if batchErr != nil {
				return nil, fmt.Errorf("refresh queued release family summaries: %w", batchErr)
			}
			refreshedBatch = batchCount
		}
		batchDuration := time.Since(refreshStart)
		refreshDuration += batchDuration
		summaryRefreshBatches++
		refreshedSummaries += refreshedBatch
		if refreshedBatch <= 0 {
			break
		}
		if refreshedBatch >= remainingSummaryBacklog {
			remainingSummaryBacklog = 0
			break
		}
		remainingSummaryBacklog -= refreshedBatch
		if s.opts.SummaryRefreshMaxDuration > 0 && time.Since(runStarted) >= s.opts.SummaryRefreshMaxDuration {
			timeLimited = true
			break
		}
	}

	metrics := map[string]any{
		"summary_refresh_batch_size":              s.opts.SummaryRefreshBatchSize,
		"summary_refresh_effective_batch_size":    s.effectiveSummaryRefreshBatchSize(),
		"summary_refresh_max_batches":             s.opts.SummaryRefreshMaxBatches,
		"summary_refresh_max_duration_ms":         durationMillis(s.opts.SummaryRefreshMaxDuration),
		"summary_refresh_time_limited":            timeLimited,
		"summary_refresh_candidate_backlog_limit": s.opts.SummaryRefreshCandidateBacklogLimit,
		"summary_refresh_initial_count":           initialSummaryBacklog,
		"summary_refresh_remaining_count":         remainingSummaryBacklog,
		"summary_refresh_batches":                 summaryRefreshBatches,
		"summary_refresh_count":                   refreshedSummaries,
		"summary_refresh_dequeued_count":          dequeuedSummaries,
		"summary_refresh_duration_ms":             durationMillis(refreshDuration),
		"summary_refresh_dequeue_duration_ms":     durationMillis(dequeueDuration),
		"summary_refresh_summary_duration_ms":     durationMillis(summaryAggregationDuration),
		"summary_refresh_ready_sync_duration_ms":  durationMillis(readySyncDuration),
		"summary_refresh_file_set_duration_ms":    durationMillis(recoveredFileSetDuration),
		"summary_refresh_aggregate_duration_ms":   durationMillis(summaryAggregateDuration),
		"summary_refresh_dominant_duration_ms":    durationMillis(summaryDominantDuration),
		"summary_refresh_phase_a_duration_ms":     durationMillis(phaseADuration),
		"summary_refresh_phase_b_duration_ms":     durationMillis(phaseBDuration),
		"summary_refresh_hot_batches":             hotBatches,
		"summary_refresh_cold_batches":            coldBatches,
		"summary_refresh_hot_attempts":            hotBatches,
		"summary_refresh_cold_attempts":           coldBatches,
		"summary_refresh_hot_dequeued_count":      hotDequeued,
		"summary_refresh_cold_dequeued_count":     coldDequeued,
	}
	s.log.Info(
		"release summary refresh: initial_summary_backlog=%d remaining_summary_backlog=%d refreshed=%d dequeued=%d batches=%d refresh_duration_ms=%.2f dequeue_duration_ms=%.2f summary_duration_ms=%.2f aggregate_duration_ms=%.2f dominant_duration_ms=%.2f ready_sync_duration_ms=%.2f recovered_file_set_duration_ms=%.2f hot_attempts=%d cold_attempts=%d hot_dequeued=%d cold_dequeued=%d",
		initialSummaryBacklog,
		remainingSummaryBacklog,
		refreshedSummaries,
		dequeuedSummaries,
		summaryRefreshBatches,
		durationMillis(refreshDuration),
		durationMillis(dequeueDuration),
		durationMillis(summaryAggregationDuration),
		durationMillis(summaryAggregateDuration),
		durationMillis(summaryDominantDuration),
		durationMillis(readySyncDuration),
		durationMillis(recoveredFileSetDuration),
		hotBatches,
		coldBatches,
		hotDequeued,
		coldDequeued,
	)
	return metrics, nil
}

func (s *Service) runOnceWithMetrics(ctx context.Context, reform bool) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("release repo is required")
	}

	var (
		candidates              []pgindex.ReleaseCandidate
		err                     error
		timings                 releaseTimings
		refreshedSummaries      int
		refreshDuration         time.Duration
		initialSummaryBacklog   int
		remainingSummaryBacklog int
		summaryRefreshBatches   int
	)
	if reform {
		offset := 0
		for {
			start := time.Now()
			page, pageErr := s.repo.ListExistingReleaseCandidates(ctx, s.opts.BatchSize, offset)
			timings.candidateList += time.Since(start)
			if pageErr != nil {
				return nil, fmt.Errorf("list existing release candidates: %w", pageErr)
			}
			if len(page) == 0 {
				break
			}
			candidates = append(candidates, page...)
			if len(page) < s.opts.BatchSize {
				break
			}
			offset += len(page)
		}
	} else {
		var countErr error
		initialSummaryBacklog, countErr = s.repo.CountQueuedReleaseFamilySummaries(ctx)
		if countErr != nil {
			return nil, fmt.Errorf("count queued release family summaries: %w", countErr)
		}
		remainingSummaryBacklog = initialSummaryBacklog
		start := time.Now()
		candidates, err = s.repo.ListReleaseCandidates(ctx, s.opts.BatchSize, pgindex.ReleaseCandidateSelectionOptions{
			MinExpectedFileCoveragePct: s.opts.ReleaseMinExpectedFileCoveragePct,
		})
		timings.candidateList += time.Since(start)
		if err != nil {
			return nil, fmt.Errorf("list release candidates: %w", err)
		}
	}
	newCandidateFamilies := len(candidates)
	autoReformStartIndex := len(candidates)
	metrics := map[string]any{
		"reform":                                  reform,
		"batch_size":                              s.opts.BatchSize,
		"auto_reform_batch_size":                  s.opts.AutoReformBatchSize,
		"summary_refresh_batch_size":              s.opts.SummaryRefreshBatchSize,
		"summary_refresh_max_batches":             s.opts.SummaryRefreshMaxBatches,
		"summary_refresh_candidate_backlog_limit": s.opts.SummaryRefreshCandidateBacklogLimit,
		"min_confidence":                          s.opts.ReleaseMinConfidence,
		"min_completion_pct":                      s.opts.ReleaseMinCompletion,
		"min_expected_file_coverage_pct":          s.opts.ReleaseMinExpectedFileCoveragePct,
		"candidate_families":                      len(candidates),
		"new_candidate_families":                  newCandidateFamilies,
		"auto_reform_candidates":                  0,
		"total_candidate_families":                len(candidates),
		"formed":                                  0,
		"new_candidate_snapshots_persisted":       0,
		"auto_reform_snapshots_persisted":         0,
		"release_rows_inserted":                   0,
		"release_rows_updated":                    0,
		"release_snapshots_skipped_downgrade":     0,
		"skipped_fragments":                       0,
		"skipped_fragments_no_main_payload":       0,
		"skipped_fragments_single_main":           0,
		"skipped_fragments_multi_file":            0,
		"skipped_fragments_contextual_weak":       0,
		"skipped_confidence":                      0,
		"skipped_completion":                      0,
		"cooled_down_low_coverage_families":       0,
		"cooled_down_weak_single_families":        0,
		"cooled_down_weak_obfuscated_families":    0,
		"cooled_down_overgrouped_families":        0,
		"cooled_down_prefer_base_stem_families":   0,
		"stale_cleanup_families":                  0,
		"fragment_only_families":                  0,
	}
	if !reform {
		autoReformCandidates := 0
		if s.opts.AutoReformBatchSize > 0 && len(candidates) < s.opts.BatchSize {
			start := time.Now()
			existing, existingErr := s.repo.ListAutoReformReleaseCandidates(ctx, s.opts.AutoReformBatchSize, autoReformMinAge)
			timings.candidateList += time.Since(start)
			if existingErr != nil {
				return nil, fmt.Errorf("list existing release candidates for auto reform: %w", existingErr)
			}
			autoReformStartIndex = len(candidates)
			candidates, autoReformCandidates = mergeAutoReformCandidates(candidates, existing)
		}
		metrics["candidate_families"] = len(candidates)
		metrics["total_candidate_families"] = len(candidates)
		metrics["new_candidate_families"] = newCandidateFamilies
		metrics["summary_refresh_initial_count"] = initialSummaryBacklog
		metrics["summary_refresh_remaining_count"] = remainingSummaryBacklog
		metrics["summary_refresh_batches"] = summaryRefreshBatches
		metrics["summary_refresh_count"] = refreshedSummaries
		metrics["summary_refresh_duration_ms"] = durationMillis(refreshDuration)
		metrics["refresh_only_due_to_summary_backlog"] = false
		metrics["auto_reform_candidates"] = autoReformCandidates
	}
	if len(candidates) == 0 {
		if reform {
			s.log.Debug("release: no existing release candidates found for reform")
		} else {
			s.log.Debug("release: no release candidates found")
		}
		return metrics, nil
	}

	formed := 0
	candidateFamiliesInspected := 0
	cooledDownFragmentOnly := 0
	cooledDownLowCoverage := 0
	cooledDownWeakSingle := 0
	cooledDownWeakObfuscated := 0
	cooledDownOvergrouped := 0
	cooledDownPreferBaseStem := 0
	staleCleanupOnly := 0
	skippedFragments := 0
	skippedNoMainPayload := 0
	skippedSingleMain := 0
	skippedMultiFileSingle := 0
	skippedContextualWeak := 0
	skippedConfidence := 0
	skippedCompletion := 0
	newCandidateSnapshotsPersisted := 0
	autoReformSnapshotsPersisted := 0
	releaseRowsInserted := 0
	releaseRowsUpdated := 0
	releaseSnapshotsSkippedDowngrade := 0
	deferredAcks := make([]pgindex.ReleaseCandidateAck, 0, 128)
	for idx, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			if flushErr := s.flushDeferredAcks(ctx, deferredAcks, &timings); flushErr != nil {
				return metrics, flushErr
			}
			metrics["candidate_families_inspected"] = candidateFamiliesInspected
			return metrics, err
		}
		candidateFamiliesInspected++
		outcome, err := s.formCandidate(ctx, candidate, &timings)
		if err != nil {
			if flushErr := s.flushDeferredAcks(ctx, deferredAcks, &timings); flushErr != nil {
				return metrics, fmt.Errorf("%v (also failed to flush deferred release acks: %w)", err, flushErr)
			}
			metrics["candidate_families_inspected"] = candidateFamiliesInspected
			metrics["formed"] = formed
			metrics["fragment_only_families"] = cooledDownFragmentOnly
			metrics["cooled_down_low_coverage_families"] = cooledDownLowCoverage
			metrics["cooled_down_weak_single_families"] = cooledDownWeakSingle
			metrics["cooled_down_weak_obfuscated_families"] = cooledDownWeakObfuscated
			metrics["cooled_down_overgrouped_families"] = cooledDownOvergrouped
			metrics["cooled_down_prefer_base_stem_families"] = cooledDownPreferBaseStem
			metrics["stale_cleanup_families"] = staleCleanupOnly
			metrics["skipped_fragments"] = skippedFragments
			metrics["skipped_fragments_no_main_payload"] = skippedNoMainPayload
			metrics["skipped_fragments_single_main"] = skippedSingleMain
			metrics["skipped_fragments_multi_file"] = skippedMultiFileSingle
			metrics["skipped_fragments_contextual_weak"] = skippedContextualWeak
			metrics["skipped_confidence"] = skippedConfidence
			metrics["skipped_completion"] = skippedCompletion
			return metrics, fmt.Errorf("form release candidate %s: %w", candidateFamilyKey(candidate), err)
		}
		formed += outcome.formed
		if !reform && idx >= autoReformStartIndex {
			autoReformSnapshotsPersisted += outcome.formed
		} else {
			newCandidateSnapshotsPersisted += outcome.formed
		}
		releaseRowsInserted += outcome.releaseRowsInserted
		releaseRowsUpdated += outcome.releaseRowsUpdated
		releaseSnapshotsSkippedDowngrade += outcome.releaseSnapshotsSkippedDowngrade
		cooledDownFragmentOnly += outcome.cooledDownFragmentOnly
		cooledDownLowCoverage += outcome.cooledDownLowCoverage
		cooledDownWeakSingle += outcome.cooledDownWeakSingle
		cooledDownWeakObfuscated += outcome.cooledDownWeakObfuscated
		cooledDownOvergrouped += outcome.cooledDownOvergrouped
		cooledDownPreferBaseStem += outcome.cooledDownPreferBaseStem
		staleCleanupOnly += outcome.staleCleanupOnly
		skippedFragments += outcome.skippedFragments
		skippedNoMainPayload += outcome.skippedNoMainPayload
		skippedSingleMain += outcome.skippedSingleMain
		skippedMultiFileSingle += outcome.skippedMultiFileSingle
		skippedContextualWeak += outcome.skippedContextualWeak
		skippedConfidence += outcome.skippedConfidence
		skippedCompletion += outcome.skippedCompletion
		if outcome.deferredAck != nil {
			deferredAcks = append(deferredAcks, *outcome.deferredAck)
			if len(deferredAcks) >= 250 {
				if err := s.flushDeferredAcks(ctx, deferredAcks, &timings); err != nil {
					return metrics, err
				}
				deferredAcks = deferredAcks[:0]
			}
		}
	}
	if err := s.flushDeferredAcks(ctx, deferredAcks, &timings); err != nil {
		return metrics, err
	}
	metrics["candidate_families_inspected"] = candidateFamiliesInspected
	metrics["formed"] = formed
	metrics["new_candidate_snapshots_persisted"] = newCandidateSnapshotsPersisted
	metrics["auto_reform_snapshots_persisted"] = autoReformSnapshotsPersisted
	metrics["release_rows_inserted"] = releaseRowsInserted
	metrics["release_rows_updated"] = releaseRowsUpdated
	metrics["release_snapshots_skipped_downgrade"] = releaseSnapshotsSkippedDowngrade
	metrics["fragment_only_families"] = cooledDownFragmentOnly
	metrics["cooled_down_low_coverage_families"] = cooledDownLowCoverage
	metrics["cooled_down_weak_single_families"] = cooledDownWeakSingle
	metrics["cooled_down_weak_obfuscated_families"] = cooledDownWeakObfuscated
	metrics["cooled_down_overgrouped_families"] = cooledDownOvergrouped
	metrics["cooled_down_prefer_base_stem_families"] = cooledDownPreferBaseStem
	metrics["stale_cleanup_families"] = staleCleanupOnly
	metrics["skipped_fragments"] = skippedFragments
	metrics["skipped_fragments_no_main_payload"] = skippedNoMainPayload
	metrics["skipped_fragments_single_main"] = skippedSingleMain
	metrics["skipped_fragments_multi_file"] = skippedMultiFileSingle
	metrics["skipped_fragments_contextual_weak"] = skippedContextualWeak
	metrics["skipped_confidence"] = skippedConfidence
	metrics["skipped_completion"] = skippedCompletion
	timings.addMetrics(metrics)

	s.log.Info(
		"release: candidate_families=%d inspected=%d new_candidate_families=%d auto_reform_candidates=%d formed=%d new_candidate_snapshots=%d auto_reform_snapshots=%d inserted=%d updated=%d skipped_downgrade=%d cooled_down_fragment_only_families=%d cooled_down_low_coverage_families=%d cooled_down_weak_single_families=%d cooled_down_weak_obfuscated_families=%d cooled_down_overgrouped_families=%d cooled_down_prefer_base_stem_families=%d stale_cleanup_only_families=%d skipped_fragments=%d skipped_fragments_no_main_payload=%d skipped_fragments_single_main=%d skipped_fragments_multi_file=%d skipped_fragments_contextual_weak=%d skipped_confidence=%d skipped_completion=%d batch_size=%d min_confidence=%.2f min_completion_pct=%.2f min_expected_file_coverage_pct=%.2f reform=%t",
		len(candidates),
		candidateFamiliesInspected,
		newCandidateFamilies,
		metrics["auto_reform_candidates"],
		formed,
		newCandidateSnapshotsPersisted,
		autoReformSnapshotsPersisted,
		releaseRowsInserted,
		releaseRowsUpdated,
		releaseSnapshotsSkippedDowngrade,
		cooledDownFragmentOnly,
		cooledDownLowCoverage,
		cooledDownWeakSingle,
		cooledDownWeakObfuscated,
		cooledDownOvergrouped,
		cooledDownPreferBaseStem,
		staleCleanupOnly,
		skippedFragments,
		skippedNoMainPayload,
		skippedSingleMain,
		skippedMultiFileSingle,
		skippedContextualWeak,
		skippedConfidence,
		skippedCompletion,
		s.opts.BatchSize,
		s.opts.ReleaseMinConfidence,
		s.opts.ReleaseMinCompletion,
		s.opts.ReleaseMinExpectedFileCoveragePct,
		reform,
	)
	return metrics, nil
}

type releaseTimings struct {
	candidateList     time.Duration
	listBinaries      time.Duration
	titleCandidates   time.Duration
	buildFiles        time.Duration
	upsertRelease     time.Duration
	replaceFiles      time.Duration
	replaceNewsgroups time.Duration
	upsertNZBCache    time.Duration
	deleteStale       time.Duration
	ackCandidate      time.Duration
	binariesListed    int
	filesBuilt        int
}

func (t *releaseTimings) addMetrics(metrics map[string]any) {
	if t == nil {
		return
	}
	metrics["candidate_list_duration_ms"] = durationMillis(t.candidateList)
	metrics["list_binaries_duration_ms"] = durationMillis(t.listBinaries)
	metrics["title_candidates_duration_ms"] = durationMillis(t.titleCandidates)
	metrics["build_files_duration_ms"] = durationMillis(t.buildFiles)
	metrics["upsert_release_duration_ms"] = durationMillis(t.upsertRelease)
	metrics["replace_files_duration_ms"] = durationMillis(t.replaceFiles)
	metrics["replace_newsgroups_duration_ms"] = durationMillis(t.replaceNewsgroups)
	metrics["upsert_nzb_cache_duration_ms"] = durationMillis(t.upsertNZBCache)
	metrics["delete_stale_duration_ms"] = durationMillis(t.deleteStale)
	metrics["ack_candidate_duration_ms"] = durationMillis(t.ackCandidate)
	metrics["binaries_listed"] = t.binariesListed
	metrics["files_built"] = t.filesBuilt
}

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

type candidateOutcome struct {
	formed                           int
	releaseRowsInserted              int
	releaseRowsUpdated               int
	releaseSnapshotsSkippedDowngrade int
	cooledDownFragmentOnly           int
	cooledDownLowCoverage            int
	cooledDownWeakSingle             int
	cooledDownWeakObfuscated         int
	cooledDownOvergrouped            int
	cooledDownPreferBaseStem         int
	staleCleanupOnly                 int
	skippedFragments                 int
	skippedNoMainPayload             int
	skippedSingleMain                int
	skippedMultiFileSingle           int
	skippedContextualWeak            int
	skippedConfidence                int
	skippedCompletion                int
	deferredAck                      *pgindex.ReleaseCandidateAck
}

const (
	fragmentReasonNoMainPayload              = "no_main_payload"
	fragmentReasonSingleMainPayload          = "single_main_payload"
	fragmentReasonMultiFileSingleMainPayload = "multi_file_single_main_payload"
	fragmentReasonContextualWeak             = "contextual_obfuscated_missing_expected"
)

func (s *Service) formCandidate(ctx context.Context, candidate pgindex.ReleaseCandidate, timings *releaseTimings) (candidateOutcome, error) {
	familyKey := candidateFamilyKey(candidate)
	if familyKey == "" {
		return candidateOutcome{}, fmt.Errorf("release family key is required")
	}

	if candidate.ReadinessBucket == "stale_cleanup_only" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete empty stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		return candidateOutcome{
			staleCleanupOnly: 1,
			deferredAck:      buildDeferredReleaseAck(candidate, familyKey),
		}, nil
	}

	if candidate.ReadinessBucket == "fragment_only" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete fragment-only stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		return candidateOutcome{
			cooledDownFragmentOnly: 1,
			deferredAck:            buildDeferredReleaseAck(candidate, familyKey),
		}, nil
	}

	if candidate.ReadinessBucket == "weak_single_binary" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete weak-single stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		return candidateOutcome{
			cooledDownWeakSingle: 1,
			deferredAck:          buildDeferredReleaseAck(candidate, familyKey),
		}, nil
	}

	if candidate.ReadinessBucket == "weak_obfuscated_set" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete weak-obfuscated stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		return candidateOutcome{
			cooledDownWeakObfuscated: 1,
			deferredAck:              buildDeferredReleaseAck(candidate, familyKey),
		}, nil
	}

	if candidate.ReadinessBucket == "overgrouped_contextual" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete overgrouped stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		return candidateOutcome{
			cooledDownOvergrouped: 1,
			deferredAck:           buildDeferredReleaseAck(candidate, familyKey),
		}, nil
	}

	if candidate.ReadinessBucket == "prefer_base_stem" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete prefer-base-stem stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		if err := s.repo.PromoteBaseStemCandidatesForReleaseFamily(ctx, candidate.ProviderID, candidate.NewsgroupID, familyKey); err != nil {
			return candidateOutcome{}, fmt.Errorf("promote base-stem candidates for %s: %w", familyKey, err)
		}
		return candidateOutcome{
			cooledDownPreferBaseStem: 1,
			deferredAck:              buildDeferredReleaseAck(candidate, familyKey),
		}, nil
	}

	start := time.Now()
	binaries, err := s.repo.ListBinariesForReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey)
	if timings != nil {
		timings.listBinaries += time.Since(start)
		timings.binariesListed += len(binaries)
	}
	if err != nil {
		return candidateOutcome{}, fmt.Errorf("list binaries for release candidate: %w", err)
	}
	if len(binaries) == 0 {
		start = time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete empty stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		if canAckReleaseCandidate(candidate) {
			start = time.Now()
			if err := s.repo.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey); err != nil {
				return candidateOutcome{}, fmt.Errorf("ack empty release candidate: %w", err)
			}
			if timings != nil {
				timings.ackCandidate += time.Since(start)
			}
		}
		return candidateOutcome{staleCleanupOnly: 1}, nil
	}

	if countCompleteBinaries(binaries) == 0 {
		start = time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete fragment-only stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		if canAckReleaseCandidate(candidate) {
			start = time.Now()
			if err := s.repo.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey); err != nil {
				return candidateOutcome{}, fmt.Errorf("ack cooled-down fragment-only candidate: %w", err)
			}
			if timings != nil {
				timings.ackCandidate += time.Since(start)
			}
		}
		return candidateOutcome{cooledDownFragmentOnly: 1}, nil
	}

	clusters := clusterBinaries(candidate, binaries)
	keepGroupNames := make([]string, 0, len(clusters))
	outcome := candidateOutcome{}
	preserveExistingOnSkippedLowCoverage := false
	clusterTitleCandidates := make([][]pgindex.ReleaseTitleCandidate, len(clusters))

	start = time.Now()
	allTitleCandidates, err := s.repo.ListReleaseTitleCandidates(ctx, binaryIDsForClusters(clusters))
	if timings != nil {
		timings.titleCandidates += time.Since(start)
	}
	if err != nil {
		return outcome, fmt.Errorf("list release title candidates for %s: %w", familyKey, err)
	}
	titleCandidatesByID := titleCandidatesByBinaryID(allTitleCandidates)
	for idx, cluster := range clusters {
		clusterTitleCandidates[idx] = titleCandidatesForCluster(cluster.Binaries, titleCandidatesByID)
	}

	for idx, cluster := range clusters {
		if err := ctx.Err(); err != nil {
			return outcome, err
		}

		record := buildReleaseRecord(candidate, cluster, clusterTitleCandidates[idx])
		if ok, reason := shouldPersistCluster(candidate, cluster, record, s.opts); !ok {
			outcome.skippedFragments++
			switch reason {
			case fragmentReasonNoMainPayload:
				outcome.skippedNoMainPayload++
			case fragmentReasonSingleMainPayload:
				outcome.skippedSingleMain++
			case fragmentReasonMultiFileSingleMainPayload:
				outcome.skippedMultiFileSingle++
			case fragmentReasonContextualWeak:
				outcome.skippedContextualWeak++
			}
			continue
		}
		if candidate.ExpectedFileCount > 1 &&
			record.ExpectedFileCount > 1 &&
			record.CompletionPct < s.opts.ReleaseMinExpectedFileCoveragePct &&
			!allowsIncompleteReleaseFormation(candidate, cluster, record) {
			outcome.skippedCompletion++
			if !clusterHasSplitArchivePayload(cluster.Binaries) {
				preserveExistingOnSkippedLowCoverage = true
			}
			continue
		}
		if record.MatchConfidence < s.opts.ReleaseMinConfidence {
			outcome.skippedConfidence++
			continue
		}
		if record.CompletionPct < s.opts.ReleaseMinCompletion {
			outcome.skippedCompletion++
			continue
		}

		start = time.Now()
		files, err := s.buildReleaseFiles(ctx, cluster, timings)
		if timings != nil {
			timings.buildFiles += time.Since(start)
		}
		if err != nil {
			return outcome, fmt.Errorf("build release files for %s: %w", record.GroupName, err)
		}

		newsgroupIDs := newsgroupIDsForCluster(cluster.Binaries)
		start = time.Now()
		persisted, err := s.repo.PersistReleaseSnapshot(ctx, record, files, newsgroupIDs)
		if timings != nil {
			elapsed := time.Since(start)
			timings.upsertRelease += elapsed
		}
		if err != nil {
			return outcome, fmt.Errorf("persist release snapshot %s: %w", record.GroupName, err)
		}
		if s.opts.ReopenArchivedNZBOnReleaseChange {
			if err := s.repo.ReopenArchivedReleaseForRegeneration(ctx, persisted.ReleaseID); err != nil {
				return outcome, fmt.Errorf("reopen archived release %s for regeneration: %w", record.GroupName, err)
			}
		}
		if baseStem := clusterPrimaryBaseStem(cluster.Binaries); baseStem != "" && candidate.NewsgroupID > 0 {
			if err := s.repo.DeleteAuxiliaryOnlySiblingReleases(ctx, candidate.ProviderID, candidate.NewsgroupID, baseStem, []string{persisted.ReleaseID}); err != nil {
				return outcome, fmt.Errorf("delete auxiliary-only sibling releases for %s: %w", record.GroupName, err)
			}
		}

		keepGroupNames = append(keepGroupNames, record.GroupName)
		outcome.formed++
		switch persisted.Status {
		case pgindex.ReleaseSnapshotStatusInserted:
			outcome.releaseRowsInserted++
		case pgindex.ReleaseSnapshotStatusSkippedDowngrade:
			outcome.releaseSnapshotsSkippedDowngrade++
		default:
			outcome.releaseRowsUpdated++
		}
	}

	start = time.Now()
	if len(keepGroupNames) > 0 || !preserveExistingOnSkippedLowCoverage {
		for _, cleanupKey := range staleReleaseCleanupKeys(candidate, familyKey) {
			if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.KeyKind, cleanupKey, keepGroupNames); err != nil {
				return outcome, fmt.Errorf("delete stale release groups for %s: %w", cleanupKey, err)
			}
		}
	}
	if timings != nil {
		timings.deleteStale += time.Since(start)
	}
	if canAckReleaseCandidate(candidate) {
		start = time.Now()
		if err := s.repo.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey); err != nil {
			return outcome, fmt.Errorf("ack release candidate %s: %w", familyKey, err)
		}
		if timings != nil {
			timings.ackCandidate += time.Since(start)
		}
	}

	return outcome, nil
}

func candidateFamilyKey(candidate pgindex.ReleaseCandidate) string {
	if key := strings.TrimSpace(candidate.ReleaseFamilyKey); key != "" {
		return key
	}
	if key := strings.TrimSpace(candidate.SourceReleaseKey); key != "" {
		return key
	}
	return strings.TrimSpace(candidate.ReleaseKey)
}

func staleReleaseCleanupKeys(candidate pgindex.ReleaseCandidate, familyKey string) []string {
	keys := make([]string, 0, 2)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range keys {
			if existing == value {
				return
			}
		}
		keys = append(keys, value)
	}
	add(familyKey)
	if candidate.KeyKind != pgindex.ReleaseCandidateKeyKindRecoveredFileSet {
		add(candidate.SourceReleaseKey)
	}
	return keys
}

func buildDeferredReleaseAck(candidate pgindex.ReleaseCandidate, familyKey string) *pgindex.ReleaseCandidateAck {
	if !canAckReleaseCandidate(candidate) || familyKey == "" {
		return nil
	}
	return &pgindex.ReleaseCandidateAck{
		ProviderID:  candidate.ProviderID,
		NewsgroupID: candidate.NewsgroupID,
		KeyKind:     candidate.KeyKind,
		FamilyKey:   familyKey,
	}
}

func canAckReleaseCandidate(candidate pgindex.ReleaseCandidate) bool {
	return strings.TrimSpace(candidate.KeyKind) != "" && candidate.ProviderID > 0 && candidate.NewsgroupID > 0
}

func mergeAutoReformCandidates(current, existing []pgindex.ReleaseCandidate) ([]pgindex.ReleaseCandidate, int) {
	if len(existing) == 0 {
		return current, 0
	}
	type candidateKey struct {
		providerID  int64
		newsgroupID int64
		keyKind     string
		familyKey   string
	}
	seen := make(map[candidateKey]struct{}, len(current)+len(existing))
	for _, candidate := range current {
		seen[candidateKey{
			providerID:  candidate.ProviderID,
			newsgroupID: candidate.NewsgroupID,
			keyKind:     strings.TrimSpace(candidate.KeyKind),
			familyKey:   candidateFamilyKey(candidate),
		}] = struct{}{}
	}
	added := 0
	for _, candidate := range existing {
		key := candidateKey{
			providerID:  candidate.ProviderID,
			newsgroupID: candidate.NewsgroupID,
			keyKind:     strings.TrimSpace(candidate.KeyKind),
			familyKey:   candidateFamilyKey(candidate),
		}
		if key.familyKey == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		current = append(current, candidate)
		added++
	}
	return current, added
}

func clusterPrimaryBaseStem(binaries []pgindex.BinarySummary) string {
	main := dominantMainPayloadBinary(binaries)
	if main == nil {
		return ""
	}
	if value := strings.ToLower(strings.TrimSpace(main.BaseStem)); value != "" {
		return value
	}
	return normalizeStem(pickFileName(*main))
}

func (s *Service) flushDeferredAcks(ctx context.Context, candidates []pgindex.ReleaseCandidateAck, timings *releaseTimings) error {
	if len(candidates) == 0 {
		return nil
	}
	start := time.Now()
	if err := s.repo.AckReleaseCandidates(ctx, candidates); err != nil {
		return fmt.Errorf("ack deferred release candidates: %w", err)
	}
	if timings != nil {
		timings.ackCandidate += time.Since(start)
	}
	return nil
}

func shouldPersistCluster(candidate pgindex.ReleaseCandidate, cluster releaseCluster, record pgindex.ReleaseRecord, opts Options) (bool, string) {
	mainPayloadCount := countMainPayloadBinaries(cluster.Binaries)
	if mainPayloadCount == 0 {
		return false, fragmentReasonNoMainPayload
	}
	if countCompleteMainPayloadBinaries(cluster.Binaries) == 0 {
		return false, fragmentReasonNoMainPayload
	}
	expectedFiles := clusterExpectedFileCount(cluster.Binaries)
	expectedArchiveFiles := clusterExpectedArchiveFileCount(cluster.Binaries)
	recoveredFileSet := candidate.KeyKind == pgindex.ReleaseCandidateKeyKindRecoveredFileSet
	if (expectedFiles > 1 || expectedArchiveFiles > 1) &&
		mainPayloadCount < 2 &&
		!recordHasStrongTitleEvidence(record) &&
		!clusterHasUsableFileIdentity(cluster.Binaries, record) {
		return false, fragmentReasonMultiFileSingleMainPayload
	}
	if !recoveredFileSet &&
		expectedFiles <= 0 &&
		expectedArchiveFiles <= 0 &&
		mainPayloadCount > 1 &&
		clusterHasSplitArchiveParts(cluster.Binaries) &&
		!recordHasStrongTitleEvidence(record) &&
		releaseTitleNeedsMoreEvidence(record) {
		return false, fragmentReasonContextualWeak
	}
	if opts.RequireExpectedFileCountForContextualObfuscated &&
		!recoveredFileSet &&
		expectedFiles <= 0 &&
		expectedArchiveFiles <= 0 &&
		clusterIsContextualObfuscated(cluster.Binaries) &&
		!allowsStandaloneBinaryRelease(cluster.Binaries, record) {
		return false, fragmentReasonContextualWeak
	}
	if !recoveredFileSet &&
		!clusterHasUsableFileIdentity(cluster.Binaries, record) &&
		(looksWeakGeneratedReleaseTitle(record.Title) || looksWeakGeneratedReleaseTitle(record.SourceTitle)) {
		return false, fragmentReasonContextualWeak
	}
	if mainPayloadCount == 1 &&
		!allowsStandaloneBinaryRelease(cluster.Binaries, record) &&
		!recordHasStrongTitleEvidence(record) &&
		!allowsSingleCompletePayloadWithAuxiliaryEvidence(cluster.Binaries) {
		return false, fragmentReasonSingleMainPayload
	}
	return true, ""
}

func allowsIncompleteReleaseFormation(candidate pgindex.ReleaseCandidate, cluster releaseCluster, record pgindex.ReleaseRecord) bool {
	if clusterHasSplitArchivePayload(cluster.Binaries) {
		return false
	}
	return candidate.KeyKind == pgindex.ReleaseCandidateKeyKindRecoveredFileSet ||
		recordHasStrongTitleEvidence(record) ||
		allowsSingleCompletePayloadWithAuxiliaryEvidence(cluster.Binaries)
}

func clusterHasSplitArchiveParts(binaries []pgindex.BinarySummary) bool {
	for _, binary := range binaries {
		if binary.IsAuxiliary && !binary.IsMainPayload {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(pickFileName(binary)))
		if name == "" {
			continue
		}
		if rarPartRE.MatchString(name) || splitSevenZipRE.MatchString(name) || splitZipRE.MatchString(name) {
			return true
		}
	}
	return false
}

func recordHasStrongTitleEvidence(record pgindex.ReleaseRecord) bool {
	return record.TitleSource != "" && record.TitleSource != "source" && record.TitleConfidence >= 0.82
}

func releaseTitleNeedsMoreEvidence(record pgindex.ReleaseRecord) bool {
	title := firstNonBlank(record.DeobfuscatedTitle, record.MatchedMediaTitle, record.Title, record.SourceTitle)
	if title == "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(title), "unknown-release") {
		return true
	}
	return looksWeakGeneratedReleaseTitle(title) || looksObfuscatedReleaseTitle(title) || !looksReadableReleaseTitle(title)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func countCompleteBinaries(binaries []pgindex.BinarySummary) int {
	count := 0
	for _, binary := range binaries {
		if binary.TotalParts > 0 && binary.ObservedParts == binary.TotalParts {
			count++
		}
	}
	return count
}

func (s *Service) buildReleaseFiles(_ context.Context, cluster releaseCluster, timings *releaseTimings) ([]pgindex.ReleaseFileRecord, error) {
	selected := selectReleaseFileBinaries(cluster.Binaries)

	files := make([]pgindex.ReleaseFileRecord, 0, len(selected))
	for idx, binary := range selected {
		fileName := pickFileName(binary)
		fileIndex := binary.FileIndex
		if fileIndex <= 0 {
			fileIndex = idx
		}
		files = append(files, pgindex.ReleaseFileRecord{
			BinaryID:  binary.BinaryID,
			FileName:  fileName,
			SizeBytes: binary.TotalBytes,
			FileIndex: fileIndex,
			IsPars:    isParFile(fileName),
			Subject:   binary.BinaryName,
			Poster:    binary.Poster,
			PostedAt:  binary.PostedAt,
		})
		if timings != nil {
			timings.filesBuilt++
		}
	}

	return files, nil
}

func prefersBinaryForReleaseFile(candidate, current pgindex.BinarySummary) bool {
	if candidate.ObservedParts != current.ObservedParts {
		return candidate.ObservedParts > current.ObservedParts
	}
	if candidate.TotalBytes != current.TotalBytes {
		return candidate.TotalBytes > current.TotalBytes
	}
	if candidate.MatchConfidence != current.MatchConfidence {
		return candidate.MatchConfidence > current.MatchConfidence
	}
	return candidate.BinaryID < current.BinaryID
}
