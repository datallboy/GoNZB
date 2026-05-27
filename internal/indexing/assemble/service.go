package assemble

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// local interface only, scoped to assembly service.
type repository interface {
	ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]pgindex.AssemblyCandidate, error)
	ClaimUnassembledArticleHeaders(ctx context.Context, req pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error)
	EnsurePoster(ctx context.Context, posterName string) (int64, error)
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaries(ctx context.Context, records []pgindex.BinaryRecord) ([]int64, error)
	UpsertBinaryParts(ctx context.Context, records []pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error
	RefreshBinaryStatsBatch(ctx context.Context, binaryIDs []int64) error
	RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error
}

// narrow matcher dependency.
type subjectMatcher interface {
	Match(candidate match.Candidate) match.Result
}

type articleFetcher interface {
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
}

type Options struct {
	BatchSize               int
	ClaimOwner              string
	ClaimLease              time.Duration
	Concurrency             int
	MaxYEncRecoveryAttempts int
	BinaryUpsertDBChunkSize int
	Lane                    string
}

type recoveryCounters struct {
	attempts         int
	successes        int
	noops            int
	fetchFailures    int
	skippedByCap     int
	skippedByBackoff int
}

type pendingBinaryPart struct {
	binaryCacheKey  string
	articleHeaderID int64
	messageID       string
	partNumber      int
	totalParts      int
	segmentBytes    int64
	fileName        string
}

type Service struct {
	repo    repository
	matcher subjectMatcher
	fetcher articleFetcher
	log     logger
	opts    Options
}

func NewService(repo repository, matcher subjectMatcher, fetcher articleFetcher, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}
	if opts.ClaimOwner == "" {
		opts.ClaimOwner = fmt.Sprintf("assemble-%d", time.Now().UnixNano())
	}
	if opts.ClaimLease <= 0 {
		opts.ClaimLease = 5 * time.Minute
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.MaxYEncRecoveryAttempts <= 0 {
		opts.MaxYEncRecoveryAttempts = 128
	}
	if opts.BinaryUpsertDBChunkSize <= 0 {
		opts.BinaryUpsertDBChunkSize = 250
	}
	switch strings.TrimSpace(strings.ToLower(opts.Lane)) {
	case pgindex.AssemblyClaimLaneCombined:
		opts.Lane = pgindex.AssemblyClaimLaneCombined
	case pgindex.AssemblyClaimLaneA:
		opts.Lane = pgindex.AssemblyClaimLaneA
	case pgindex.AssemblyClaimLaneB:
		opts.Lane = pgindex.AssemblyClaimLaneB
	default:
		opts.Lane = pgindex.AssemblyClaimLaneCombined
	}

	return &Service{
		repo:    repo,
		matcher: matcher,
		fetcher: fetcher,
		log:     log,
		opts:    opts,
	}
}

// RunOnce assembles one batch of article headers into binaries + binary_parts.
func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.opts.Concurrency <= 1 || s.opts.BatchSize <= 1 {
		return s.runOnceWithMetricsSingle(ctx, s.opts.BatchSize, s.opts.ClaimOwner)
	}
	if s.repo == nil {
		return nil, fmt.Errorf("assembly repo is required")
	}

	started := time.Now()
	selectionStarted := time.Now()
	headers, err := s.repo.ClaimUnassembledArticleHeaders(ctx, pgindex.AssemblyClaimRequest{
		Limit:         s.opts.BatchSize,
		Owner:         s.opts.ClaimOwner,
		LeaseDuration: s.opts.ClaimLease,
		Lane:          s.opts.Lane,
	})
	if err != nil {
		return nil, fmt.Errorf("claim unassembled article headers: %w", err)
	}
	selectionDuration := time.Since(selectionStarted)
	if len(headers) == 0 {
		return map[string]any{
			"selected_headers":                0,
			"processed_headers":               0,
			"binaries_refreshed":              0,
			"batch_size":                      s.opts.BatchSize,
			"worker_count":                    s.opts.Concurrency,
			"lane_mode":                       laneMetricName(s.opts.Lane),
			"candidate_selection_duration_ms": durationMillis(selectionDuration),
			"total_duration_ms":               durationMillis(time.Since(started)),
			"headers_per_second":              0.0,
			"refreshed_binaries_per_second":   0.0,
		}, nil
	}

	workerCount := s.opts.Concurrency
	if workerCount > len(headers) {
		workerCount = len(headers)
	}
	baseBatch := len(headers) / workerCount
	remainder := len(headers) % workerCount

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		combined = map[string]any{"batch_size": s.opts.BatchSize, "worker_count": workerCount}
	)

	offset := 0
	for i := 0; i < workerCount; i++ {
		workerBatch := baseBatch
		if i < remainder {
			workerBatch++
		}
		if workerBatch <= 0 {
			continue
		}

		workerID := i + 1
		workerHeaders := headers[offset : offset+workerBatch]
		offset += workerBatch
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerSvc := *s
			workerSvc.repo = claimedBatchRepository{
				delegate: s.repo,
				headers:  workerHeaders,
			}
			metrics, err := workerSvc.runOnceWithMetricsSingle(ctx, workerBatch, fmt.Sprintf("%s-worker-%d", s.opts.ClaimOwner, workerID))
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			mergeAssembleMetrics(combined, metrics)
		}()
	}
	wg.Wait()

	totalDuration := time.Since(started)
	processed, _ := numericMetricInt64(combined, "processed_headers")
	refreshed, _ := numericMetricInt64(combined, "binaries_refreshed")
	combined["selected_headers"] = len(headers)
	combined["candidate_selection_duration_ms"] = durationMillis(selectionDuration)
	combined["total_duration_ms"] = durationMillis(totalDuration)
	combined["headers_per_second"] = throughputPerSecond(int(processed), totalDuration)
	combined["refreshed_binaries_per_second"] = throughputPerSecond(int(refreshed), totalDuration)

	return combined, firstErr
}

type claimedBatchRepository struct {
	delegate repository
	headers  []pgindex.AssemblyCandidate
}

func (r claimedBatchRepository) ListUnassembledArticleHeaders(context.Context, int) ([]pgindex.AssemblyCandidate, error) {
	return r.headers, nil
}

func (r claimedBatchRepository) ClaimUnassembledArticleHeaders(context.Context, pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error) {
	return r.headers, nil
}

func (r claimedBatchRepository) EnsurePoster(ctx context.Context, posterName string) (int64, error) {
	return r.delegate.EnsurePoster(ctx, posterName)
}

func (r claimedBatchRepository) UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error) {
	return r.delegate.UpsertBinary(ctx, in)
}

func (r claimedBatchRepository) UpsertBinaries(ctx context.Context, records []pgindex.BinaryRecord) ([]int64, error) {
	return r.delegate.UpsertBinaries(ctx, records)
}

func (r claimedBatchRepository) UpsertBinaryParts(ctx context.Context, records []pgindex.BinaryPartRecord) error {
	return r.delegate.UpsertBinaryParts(ctx, records)
}

func (r claimedBatchRepository) RefreshBinaryStats(ctx context.Context, binaryID int64) error {
	return r.delegate.RefreshBinaryStats(ctx, binaryID)
}

func (r claimedBatchRepository) RefreshBinaryStatsBatch(ctx context.Context, binaryIDs []int64) error {
	return r.delegate.RefreshBinaryStatsBatch(ctx, binaryIDs)
}

func (r claimedBatchRepository) RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error {
	return r.delegate.RecordYEncRecoveryNotFound(ctx, articleHeaderID)
}

func (s *Service) runOnceWithMetricsSingle(ctx context.Context, batchSize int, claimOwner string) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("assembly repo is required")
	}
	if s.matcher == nil {
		return nil, fmt.Errorf("assembly matcher is required")
	}

	started := time.Now()
	selectionStarted := time.Now()
	headers, err := s.repo.ClaimUnassembledArticleHeaders(ctx, pgindex.AssemblyClaimRequest{
		Limit:         batchSize,
		Owner:         claimOwner,
		LeaseDuration: s.opts.ClaimLease,
		Lane:          s.opts.Lane,
	})
	if err != nil {
		return nil, fmt.Errorf("claim unassembled article headers: %w", err)
	}
	selectionDuration := time.Since(selectionStarted)
	metrics := map[string]any{
		"selected_headers":                len(headers),
		"batch_size":                      batchSize,
		"lane_mode":                       laneMetricName(s.opts.Lane),
		"candidate_selection_duration_ms": durationMillis(selectionDuration),
	}
	if len(headers) == 0 {
		s.log.Debug("assemble: no unassembled article headers found")
		metrics["processed_headers"] = 0
		metrics["binaries_refreshed"] = 0
		metrics["total_duration_ms"] = durationMillis(time.Since(started))
		metrics["headers_per_second"] = 0.0
		metrics["refreshed_binaries_per_second"] = 0.0
		return metrics, nil
	}

	refreshed := make(map[int64]struct{}, len(headers))
	posterIDsByName := make(map[string]int64, 64)
	assembledCount := 0
	binaryIDsByKey := make(map[string]int64, len(headers))
	orderedBinaryKeys := make([]string, 0, len(headers))
	binaryRecordsByKey := make(map[string]pgindex.BinaryRecord, len(headers))
	partSeeds := make([]pendingBinaryPart, 0, len(headers))
	partRecords := make([]pgindex.BinaryPartRecord, 0, len(headers))
	laneASelected := 0
	recovery := recoveryCounters{}
	var (
		headerMatchDuration      time.Duration
		posterDuration           time.Duration
		binaryUpsertDuration     time.Duration
		binaryPartUpsertDuration time.Duration
		binaryRefreshDuration    time.Duration
	)

	for _, header := range headers {
		if header.StructuredIdentityBinaryMatched {
			laneASelected++
		}
	}
	laneBSelected := len(headers) - laneASelected

	for _, header := range headers {
		if err := ctx.Err(); err != nil {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			return metrics, err
		}

		candidate := match.Candidate{
			ArticleNumber: header.ArticleNumber,
			MessageID:     header.MessageID,
			Subject:       header.Subject,
			Poster:        header.Poster,
			PostedAt:      header.DateUTC,
			Bytes:         header.Bytes,
			Lines:         header.Lines,
			Xref:          header.Xref,
			RawOverview:   header.RawOverview,
		}
		matchStarted := time.Now()
		matched := s.matcher.Match(candidate)
		if header.YEncRecoveryRetryAfter != nil && header.YEncRecoveryRetryAfter.After(time.Now().UTC()) {
			recovery.skippedByBackoff++
		} else if s.shouldAttemptYEncRecovery(header, matched) {
			if recovery.attempts >= s.opts.MaxYEncRecoveryAttempts {
				recovery.skippedByCap++
			} else {
				recovery.attempts++
				rematched, result, err := s.rematchFromYEncHeader(ctx, header, candidate)
				if err != nil {
					metrics["processed_headers"] = assembledCount
					metrics["binaries_refreshed"] = len(refreshed)
					addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
					return metrics, fmt.Errorf("recover yenc metadata for article %d: %w", header.ID, err)
				}
				if result.fetchFailed {
					recovery.fetchFailures++
				}
				if result.recovered {
					recovery.successes++
					matched = rematched
				} else {
					recovery.noops++
				}
			}
		}
		headerMatchDuration += time.Since(matchStarted)

		posterID := header.PosterID
		if posterID <= 0 {
			if cachedPosterID, ok := posterIDsByName[header.Poster]; ok {
				posterID = cachedPosterID
			} else {
				var err error
				posterStarted := time.Now()
				posterID, err = s.repo.EnsurePoster(ctx, header.Poster)
				posterDuration += time.Since(posterStarted)
				if err != nil {
					metrics["processed_headers"] = assembledCount
					metrics["binaries_refreshed"] = len(refreshed)
					addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
					return metrics, fmt.Errorf("ensure poster for article %d: %w", header.ID, err)
				}
				if posterID > 0 {
					posterIDsByName[header.Poster] = posterID
				}
			}
		}

		binaryRecord := pgindex.BinaryRecord{
			ProviderID:        header.ProviderID,
			NewsgroupID:       header.NewsgroupID,
			PosterID:          posterID,
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
			TotalParts:        matched.TotalParts,
			PostedAt:          header.DateUTC,
			MatchConfidence:   matched.MatchConfidence,
			MatchStatus:       matched.MatchStatus,
			GroupingEvidence:  matched.GroupingEvidence,
		}
		binaryCacheKey := assembleBinaryCacheKey(binaryRecord)
		if _, ok := binaryRecordsByKey[binaryCacheKey]; !ok {
			orderedBinaryKeys = append(orderedBinaryKeys, binaryCacheKey)
			binaryRecordsByKey[binaryCacheKey] = binaryRecord
		}

		partSeeds = append(partSeeds, pendingBinaryPart{
			binaryCacheKey:  binaryCacheKey,
			articleHeaderID: header.ID,
			messageID:       header.MessageID,
			partNumber:      matched.PartNumber,
			totalParts:      matched.TotalParts,
			segmentBytes:    header.Bytes,
			fileName:        matched.FileName,
		})
	}

	if len(orderedBinaryKeys) > 0 {
		binaryUpsertStarted := time.Now()
		batchRecords := make([]pgindex.BinaryRecord, 0, len(orderedBinaryKeys))
		for _, key := range orderedBinaryKeys {
			batchRecords = append(batchRecords, binaryRecordsByKey[key])
		}
		upsertCtx := pgindex.WithBinaryUpsertChunkSize(ctx, s.opts.BinaryUpsertDBChunkSize)
		upsertCtx = pgindex.WithDeferredReleaseFamilySummaryRefresh(upsertCtx)
		upsertTelemetry := &pgindex.BinaryUpsertTelemetry{}
		upsertCtx = pgindex.WithBinaryUpsertTelemetry(upsertCtx, upsertTelemetry)
		binaryIDs, err := s.repo.UpsertBinaries(upsertCtx, batchRecords)
		binaryUpsertDuration += time.Since(binaryUpsertStarted)
		addBinaryUpsertTelemetryMetrics(metrics, upsertTelemetry.Snapshot())
		if err != nil {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("upsert binaries batch: %w", err)
		}
		if len(binaryIDs) != len(orderedBinaryKeys) {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("upsert binaries batch returned %d ids for %d records", len(binaryIDs), len(orderedBinaryKeys))
		}
		for i, key := range orderedBinaryKeys {
			binaryIDsByKey[key] = binaryIDs[i]
		}
	}

	for _, seed := range partSeeds {
		binaryID, ok := binaryIDsByKey[seed.binaryCacheKey]
		if !ok {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("missing binary id for cache key %q", seed.binaryCacheKey)
		}
		partRecords = append(partRecords, pgindex.BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: seed.articleHeaderID,
			MessageID:       seed.messageID,
			PartNumber:      seed.partNumber,
			TotalParts:      seed.totalParts,
			SegmentBytes:    seed.segmentBytes,
			FileName:        seed.fileName,
		})
		refreshed[binaryID] = struct{}{}
	}

	binaryPartUpsertStarted := time.Now()
	if err := s.repo.UpsertBinaryParts(ctx, partRecords); err != nil {
		metrics["processed_headers"] = assembledCount
		metrics["binaries_refreshed"] = len(refreshed)
		binaryPartUpsertDuration += time.Since(binaryPartUpsertStarted)
		addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
		return metrics, fmt.Errorf("upsert binary parts batch: %w", err)
	}
	binaryPartUpsertDuration += time.Since(binaryPartUpsertStarted)
	assembledCount = len(partRecords)

	refreshIDs := make([]int64, 0, len(refreshed))
	for binaryID := range refreshed {
		refreshIDs = append(refreshIDs, binaryID)
	}
	if len(refreshIDs) > 0 {
		refreshStarted := time.Now()
		if err := s.repo.RefreshBinaryStatsBatch(ctx, refreshIDs); err != nil {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			binaryRefreshDuration += time.Since(refreshStarted)
			addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("refresh binary stats batch: %w", err)
		}
		binaryRefreshDuration += time.Since(refreshStarted)
	}
	metrics["lane_a_selected"] = laneASelected
	metrics["lane_b_selected"] = laneBSelected
	metrics["processed_headers"] = assembledCount
	metrics["binaries_refreshed"] = len(refreshed)
	metrics["unique_binary_upserts"] = len(binaryIDsByKey)
	metrics["binary_upsert_cache_hits"] = len(partRecords) - len(binaryIDsByKey)
	metrics["binary_part_batch_size"] = len(partRecords)
	metrics["recovery_attempts"] = recovery.attempts
	metrics["recovery_successes"] = recovery.successes
	metrics["recovery_noops"] = recovery.noops
	metrics["recovery_fetch_failures"] = recovery.fetchFailures
	metrics["recovery_skipped_by_cap"] = recovery.skippedByCap
	metrics["recovery_skipped_by_backoff"] = recovery.skippedByBackoff
	addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))

	s.log.Info(
		"assemble: lane_mode=%s lane_a_selected=%d lane_b_selected=%d processed_headers=%d binaries_refreshed=%d batch_size=%d headers_per_second=%.2f refreshed_binaries_per_second=%.2f candidate_selection_ms=%.2f header_match_ms=%.2f binary_upsert_ms=%.2f binary_part_upsert_ms=%.2f binary_refresh_ms=%.2f binary_upsert_chunk_count=%d binary_upsert_chunk_rows=%d binary_upsert_chunk_retries=%d binary_upsert_chunk_retry_deadlocks=%d binary_upsert_chunk_retry_serialization=%d binary_upsert_chunk_ms=%.2f binary_upsert_chunk_max_ms=%.2f binary_upsert_deferred_summary_chunks=%d binary_upsert_deferred_summary_keys=%d unique_binary_upserts=%d binary_upsert_cache_hits=%d assemble_recovery_attempts=%d assemble_recovery_successes=%d assemble_recovery_noops=%d assemble_recovery_fetch_failures=%d assemble_recovery_skipped_by_cap=%d assemble_recovery_skipped_by_backoff=%d",
		laneMetricName(s.opts.Lane),
		laneASelected,
		laneBSelected,
		assembledCount,
		len(refreshed),
		batchSize,
		metrics["headers_per_second"],
		metrics["refreshed_binaries_per_second"],
		metrics["candidate_selection_duration_ms"],
		metrics["header_match_duration_ms"],
		metrics["binary_upsert_duration_ms"],
		metrics["binary_part_upsert_duration_ms"],
		metrics["binary_refresh_duration_ms"],
		metrics["binary_upsert_chunk_count"],
		metrics["binary_upsert_chunk_rows"],
		metrics["binary_upsert_chunk_retries"],
		metrics["binary_upsert_chunk_retry_deadlocks"],
		metrics["binary_upsert_chunk_retry_serialization"],
		metrics["binary_upsert_chunk_duration_ms"],
		metrics["binary_upsert_chunk_max_ms"],
		metrics["binary_upsert_deferred_summary_chunks"],
		metrics["binary_upsert_deferred_summary_keys"],
		metrics["unique_binary_upserts"],
		metrics["binary_upsert_cache_hits"],
		recovery.attempts,
		recovery.successes,
		recovery.noops,
		recovery.fetchFailures,
		recovery.skippedByCap,
		recovery.skippedByBackoff,
	)

	return metrics, nil
}

func mergeAssembleMetrics(dst map[string]any, src map[string]any) {
	for key, value := range src {
		switch key {
		case "batch_size", "total_duration_ms", "headers_per_second", "refreshed_binaries_per_second":
			continue
		case "binary_upsert_chunk_max_ms":
			if current := numericMetricFloat64(dst, key); numericMetricFloat64(src, key) > current {
				dst[key] = numericMetricFloat64(src, key)
			}
			continue
		}
		switch tv := value.(type) {
		case int:
			dst[key] = numericMetricFloat64(dst, key) + float64(tv)
		case int64:
			dst[key] = numericMetricFloat64(dst, key) + float64(tv)
		case float64:
			dst[key] = numericMetricFloat64(dst, key) + tv
		}
	}
}

func numericMetricFloat64(metrics map[string]any, key string) float64 {
	switch value := metrics[key].(type) {
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case float64:
		return value
	default:
		return 0
	}
}

func numericMetricInt64(metrics map[string]any, key string) (int64, bool) {
	switch value := metrics[key].(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		return int64(value), true
	default:
		return 0, false
	}
}

func addAssembleTimingMetrics(metrics map[string]any, started time.Time, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration time.Duration, processedHeaders, refreshedBinaries int) {
	totalDuration := time.Since(started)
	metrics["header_match_duration_ms"] = durationMillis(headerMatchDuration)
	metrics["poster_lookup_duration_ms"] = durationMillis(posterDuration)
	metrics["binary_upsert_duration_ms"] = durationMillis(binaryUpsertDuration)
	metrics["binary_part_upsert_duration_ms"] = durationMillis(binaryPartUpsertDuration)
	metrics["binary_refresh_duration_ms"] = durationMillis(binaryRefreshDuration)
	metrics["total_duration_ms"] = durationMillis(totalDuration)
	metrics["headers_per_second"] = throughputPerSecond(processedHeaders, totalDuration)
	metrics["refreshed_binaries_per_second"] = throughputPerSecond(refreshedBinaries, totalDuration)
}

func addBinaryUpsertTelemetryMetrics(metrics map[string]any, telemetry pgindex.BinaryUpsertTelemetry) {
	metrics["binary_upsert_chunk_count"] = telemetry.ChunkCount
	metrics["binary_upsert_chunk_rows"] = telemetry.ChunkRows
	metrics["binary_upsert_chunk_retries"] = telemetry.ChunkRetries
	metrics["binary_upsert_chunk_retry_deadlocks"] = telemetry.ChunkRetryDeadlocks
	metrics["binary_upsert_chunk_retry_serialization"] = telemetry.ChunkRetrySerialization
	metrics["binary_upsert_chunk_duration_ms"] = telemetry.ChunkDurationMs
	metrics["binary_upsert_chunk_max_ms"] = telemetry.ChunkDurationMaxMs
	metrics["binary_upsert_deferred_summary_chunks"] = telemetry.DeferredSummaryRefreshChunks
	metrics["binary_upsert_deferred_summary_keys"] = telemetry.DeferredSummaryKeyCount
}

func laneMetricName(lane string) string {
	switch strings.TrimSpace(strings.ToLower(lane)) {
	case pgindex.AssemblyClaimLaneA:
		return pgindex.AssemblyClaimLaneA
	case pgindex.AssemblyClaimLaneB:
		return pgindex.AssemblyClaimLaneB
	default:
		return "combined"
	}
}

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func throughputPerSecond(count int, elapsed time.Duration) float64 {
	if count <= 0 || elapsed <= 0 {
		return 0
	}
	return float64(count) / elapsed.Seconds()
}

func assembleBinaryCacheKey(in pgindex.BinaryRecord) string {
	return fmt.Sprintf("%d:%d:%s", in.ProviderID, in.NewsgroupID, strings.TrimSpace(in.BinaryKey))
}

func (s *Service) shouldAttemptYEncRecovery(header pgindex.AssemblyCandidate, matched match.Result) bool {
	if s == nil || s.fetcher == nil {
		return false
	}
	if header.MessageID == "" {
		return false
	}
	if header.YEncRecoveryRetryAfter != nil && header.YEncRecoveryRetryAfter.After(time.Now().UTC()) {
		return false
	}
	// If scrape/XOVER already exposed a structured file name from the subject,
	// inline body-header recovery is rarely worth it on the hot path.
	if strings.TrimSpace(header.FileName) != "" {
		return false
	}
	// Lane B is our broad backlog-drain path. Keep it cheap and deterministic:
	// defer opaque body-header recovery there instead of letting a pathological
	// slice of backlog monopolize assemble throughput.
	if !header.StructuredIdentityBinaryMatched {
		return false
	}
	if hasSufficientStructuredIdentity(header) && header.StructuredIdentityBinaryMatched && hasStableMultipartMatch(matched) {
		return false
	}
	if matched.MatchConfidence >= 0.85 && matched.TotalParts > 1 && matched.FileName != "" && !strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		return false
	}
	if matched.TotalParts > 1 && matched.FileName != "" && matched.FileName == matched.BinaryName && !strings.HasSuffix(strings.ToLower(matched.FileName), ".bin") {
		return false
	}
	opaqueSubject := isOpaqueAssemblySubject(header.Subject)
	opaqueFile := matched.FileName == "" || strings.HasSuffix(strings.ToLower(strings.TrimSpace(matched.FileName)), ".bin")
	return opaqueSubject || opaqueFile || matched.TotalParts <= 1
}

type recoveryAttemptResult struct {
	recovered   bool
	fetchFailed bool
}

func (s *Service) rematchFromYEncHeader(ctx context.Context, header pgindex.AssemblyCandidate, candidate match.Candidate) (match.Result, recoveryAttemptResult, error) {
	groups := assemblyFetchGroups(header)
	if len(groups) == 0 {
		return match.Result{}, recoveryAttemptResult{}, nil
	}

	reader, err := s.fetcher.Fetch(ctx, header.MessageID, groups)
	if err != nil {
		if errors.Is(err, nntp.ErrArticleNotFound) && s.repo != nil {
			if markErr := s.repo.RecordYEncRecoveryNotFound(ctx, header.ID); markErr != nil && s.log != nil {
				s.log.Warn("assemble: failed to persist yenc not_found backoff article=%d err=%v", header.ID, markErr)
			}
		}
		return match.Result{}, recoveryAttemptResult{fetchFailed: true}, nil
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	yh, err := nzb.ReadYencHeader(reader)
	if err != nil {
		return match.Result{}, recoveryAttemptResult{}, nil
	}
	if strings.TrimSpace(yh.FileName) == "" {
		return match.Result{}, recoveryAttemptResult{}, nil
	}

	enrichedRaw := cloneRawOverview(candidate.RawOverview)
	enrichedRaw["name"] = yh.FileName
	if yh.PartNumber > 0 {
		enrichedRaw["part"] = yh.PartNumber
	}
	if yh.TotalParts > 0 {
		enrichedRaw["total"] = yh.TotalParts
	}
	if yh.FileSize > 0 {
		enrichedRaw["size"] = yh.FileSize
	}

	candidate.RawOverview = enrichedRaw
	rematched := s.matcher.Match(candidate)
	if rematched.FileName == "" || strings.HasSuffix(strings.ToLower(rematched.FileName), ".bin") {
		return match.Result{}, recoveryAttemptResult{}, nil
	}
	return rematched, recoveryAttemptResult{recovered: true}, nil
}

func hasSufficientStructuredIdentity(header pgindex.AssemblyCandidate) bool {
	fileName := strings.TrimSpace(header.FileName)
	if fileName == "" {
		return false
	}
	return header.FileTotal > 1 || header.YEncTotal > 1
}

func hasStableMultipartMatch(matched match.Result) bool {
	fileName := strings.ToLower(strings.TrimSpace(matched.FileName))
	if matched.TotalParts <= 1 || fileName == "" {
		return false
	}
	if strings.HasSuffix(fileName, ".bin") {
		return false
	}
	return matched.BinaryName == matched.FileName
}

func cloneRawOverview(in map[string]any) map[string]any {
	if len(in) == 0 {
		return make(map[string]any, 4)
	}
	out := make(map[string]any, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isOpaqueAssemblySubject(subject string) bool {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return true
	}
	if strings.ContainsAny(subject, " []()/\"'") {
		return false
	}
	if len(subject) < 16 {
		return false
	}
	for _, r := range subject {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func assemblyFetchGroups(header pgindex.AssemblyCandidate) []string {
	groups := make([]string, 0, 3)
	seen := map[string]struct{}{}
	fields := strings.Fields(strings.TrimSpace(header.Xref))
	for idx, field := range fields {
		if idx == 0 && !strings.Contains(field, ":") {
			continue
		}
		group := field
		if idx := strings.IndexByte(group, ':'); idx >= 0 {
			group = group[:idx]
		}
		if idx := strings.IndexByte(group, ' '); idx >= 0 {
			group = group[:idx]
		}
		group = strings.TrimSpace(group)
		if group == "" || strings.EqualFold(group, "xref:") {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	if strings.TrimSpace(header.NewsgroupName) != "" {
		if _, ok := seen[header.NewsgroupName]; !ok {
			groups = append(groups, header.NewsgroupName)
		}
	}
	return groups
}
