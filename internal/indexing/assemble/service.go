package assemble

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

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
	ClaimAssemblyQueueBatch(ctx context.Context, req pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error)
	CleanupStaleAssemblyQueueRows(ctx context.Context, limit int) (int, error)
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaries(ctx context.Context, records []pgindex.BinaryRecord) ([]int64, error)
	UpsertBinaryParts(ctx context.Context, records []pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error
	RefreshBinaryStatsBatch(ctx context.Context, binaryIDs []int64) error
	RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error
}

type assemblyClaimStatsRepository interface {
	ClaimAssemblyQueueBatchWithStats(ctx context.Context, req pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, pgindex.AssemblyClaimStats, error)
}

type subjectMultipartRegroupRepository interface {
	RegroupSubjectMultipartBinaries(ctx context.Context, limit int) (*pgindex.SubjectMultipartRegroupResult, error)
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
	LaneATargetPct          int
	LaneBMinPct             int
	LaneATimeWindowMinutes  int
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
	sourcePostedAt  time.Time
	messageID       string
	partNumber      int
	totalParts      int
	segmentBytes    int64
	fileName        string
}

type assembleWork struct {
	orderedBinaryKeys   []string
	binaryRecordsByKey  map[string]pgindex.BinaryRecord
	partSeeds           []pendingBinaryPart
	laneASelected       int
	laneBSelected       int
	recovery            recoveryCounters
	headerMatchDuration time.Duration
}

type Service struct {
	repo                        repository
	matcher                     subjectMatcher
	fetcher                     articleFetcher
	log                         logger
	opts                        Options
	subjectMultipartRegroupMu   sync.Mutex
	lastSubjectMultipartRegroup time.Time
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
		opts.BinaryUpsertDBChunkSize = 1000
	}
	if opts.LaneATargetPct <= 0 {
		opts.LaneATargetPct = 70
	}
	if opts.LaneBMinPct <= 0 {
		opts.LaneBMinPct = 30
	}
	if opts.LaneATimeWindowMinutes <= 0 {
		opts.LaneATimeWindowMinutes = 15
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
	cleanupStarted := time.Now()
	staleDeleted, err := s.repo.CleanupStaleAssemblyQueueRows(ctx, s.opts.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("cleanup stale assembly queue rows: %w", err)
	}
	cleanupDuration := time.Since(cleanupStarted)
	headers, claimStats, err := s.claimAssemblyQueueBatch(ctx, pgindex.AssemblyClaimRequest{
		Limit:                  s.opts.BatchSize,
		Owner:                  s.opts.ClaimOwner,
		LeaseDuration:          s.opts.ClaimLease,
		Lane:                   s.opts.Lane,
		LaneATargetPct:         s.opts.LaneATargetPct,
		LaneBMinPct:            s.opts.LaneBMinPct,
		LaneATimeWindowMinutes: s.opts.LaneATimeWindowMinutes,
	})
	if err != nil {
		return nil, fmt.Errorf("claim unassembled article headers: %w", err)
	}
	selectionDuration := cleanupDuration + claimStats.ClaimDuration + claimStats.HydrationDuration
	if len(headers) == 0 {
		metrics := map[string]any{
			"selected_headers":                0,
			"processed_headers":               0,
			"binaries_refreshed":              0,
			"batch_size":                      s.opts.BatchSize,
			"worker_count":                    s.opts.Concurrency,
			"lane_mode":                       laneMetricName(s.opts.Lane),
			"stale_queue_rows_deleted":        staleDeleted,
			"candidate_selection_duration_ms": durationMillis(selectionDuration),
			"queue_cleanup_duration_ms":       durationMillis(cleanupDuration),
			"assembly_claim_duration_ms":      durationMillis(claimStats.ClaimDuration),
			"assembly_hydration_duration_ms":  durationMillis(claimStats.HydrationDuration),
			"assembly_claimed_headers":        claimStats.Claimed,
			"assembly_hydrated_headers":       claimStats.Hydrated,
			"total_duration_ms":               durationMillis(time.Since(started)),
			"headers_per_second":              0.0,
			"refreshed_binaries_per_second":   0.0,
		}
		s.regroupSubjectMultipartBinaries(ctx, metrics, s.opts.BatchSize)
		return metrics, nil
	}

	workerCount := s.opts.Concurrency
	if workerCount > len(headers) {
		workerCount = len(headers)
	}
	baseBatch := len(headers) / workerCount
	remainder := len(headers) % workerCount

	works := make([]assembleWork, workerCount)
	var wg sync.WaitGroup
	errCh := make(chan error, workerCount)

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
		workerIndex := i
		workerHeaders := headers[offset : offset+workerBatch]
		offset += workerBatch
		wg.Add(1)
		go func() {
			defer wg.Done()
			work, err := s.buildAssembleWork(ctx, workerHeaders)
			if err != nil {
				errCh <- fmt.Errorf("%s-worker-%d: %w", s.opts.ClaimOwner, workerID, err)
				return
			}
			works[workerIndex] = work
		}()
	}
	wg.Wait()
	close(errCh)

	combined := map[string]any{
		"batch_size":                      s.opts.BatchSize,
		"worker_count":                    workerCount,
		"selected_headers":                len(headers),
		"lane_mode":                       laneMetricName(s.opts.Lane),
		"stale_queue_rows_deleted":        staleDeleted,
		"candidate_selection_duration_ms": durationMillis(selectionDuration),
		"queue_cleanup_duration_ms":       durationMillis(cleanupDuration),
		"assembly_claim_duration_ms":      durationMillis(claimStats.ClaimDuration),
		"assembly_hydration_duration_ms":  durationMillis(claimStats.HydrationDuration),
		"assembly_claimed_headers":        claimStats.Claimed,
		"assembly_hydrated_headers":       claimStats.Hydrated,
	}
	for err := range errCh {
		if err != nil {
			combined["processed_headers"] = 0
			combined["binaries_refreshed"] = 0
			addAssembleTimingMetrics(combined, started, 0, 0, 0, 0, 0, 0)
			return combined, err
		}
	}

	work := mergeAssembleWorks(works)
	metrics, err := s.persistAssembleWork(ctx, started, combined, work, s.opts.BatchSize)
	combined["selected_headers"] = len(headers)
	combined["candidate_selection_duration_ms"] = durationMillis(selectionDuration)
	if err == nil {
		s.regroupSubjectMultipartBinaries(ctx, metrics, s.opts.BatchSize)
	}
	return metrics, err
}

func (s *Service) claimAssemblyQueueBatch(ctx context.Context, req pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, pgindex.AssemblyClaimStats, error) {
	if repo, ok := s.repo.(assemblyClaimStatsRepository); ok {
		return repo.ClaimAssemblyQueueBatchWithStats(ctx, req)
	}
	started := time.Now()
	headers, err := s.repo.ClaimAssemblyQueueBatch(ctx, req)
	stats := pgindex.AssemblyClaimStats{
		ClaimDuration: time.Since(started),
		Claimed:       len(headers),
		Hydrated:      len(headers),
	}
	return headers, stats, err
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

func (r claimedBatchRepository) ClaimAssemblyQueueBatch(context.Context, pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error) {
	return r.headers, nil
}

func (r claimedBatchRepository) CleanupStaleAssemblyQueueRows(context.Context, int) (int, error) {
	return 0, nil
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
	cleanupStarted := time.Now()
	staleDeleted, err := s.repo.CleanupStaleAssemblyQueueRows(ctx, batchSize)
	if err != nil {
		return nil, fmt.Errorf("cleanup stale assembly queue rows: %w", err)
	}
	cleanupDuration := time.Since(cleanupStarted)
	headers, claimStats, err := s.claimAssemblyQueueBatch(ctx, pgindex.AssemblyClaimRequest{
		Limit:                  batchSize,
		Owner:                  claimOwner,
		LeaseDuration:          s.opts.ClaimLease,
		Lane:                   s.opts.Lane,
		LaneATargetPct:         s.opts.LaneATargetPct,
		LaneBMinPct:            s.opts.LaneBMinPct,
		LaneATimeWindowMinutes: s.opts.LaneATimeWindowMinutes,
	})
	if err != nil {
		return nil, fmt.Errorf("claim unassembled article headers: %w", err)
	}
	selectionDuration := cleanupDuration + claimStats.ClaimDuration + claimStats.HydrationDuration
	metrics := map[string]any{
		"selected_headers":                len(headers),
		"batch_size":                      batchSize,
		"lane_mode":                       laneMetricName(s.opts.Lane),
		"stale_queue_rows_deleted":        staleDeleted,
		"candidate_selection_duration_ms": durationMillis(selectionDuration),
		"queue_cleanup_duration_ms":       durationMillis(cleanupDuration),
		"assembly_claim_duration_ms":      durationMillis(claimStats.ClaimDuration),
		"assembly_hydration_duration_ms":  durationMillis(claimStats.HydrationDuration),
		"assembly_claimed_headers":        claimStats.Claimed,
		"assembly_hydrated_headers":       claimStats.Hydrated,
	}
	if len(headers) == 0 {
		s.log.Debug("assemble: no unassembled article headers found")
		metrics["processed_headers"] = 0
		metrics["binaries_refreshed"] = 0
		s.regroupSubjectMultipartBinaries(ctx, metrics, batchSize)
		metrics["total_duration_ms"] = durationMillis(time.Since(started))
		metrics["headers_per_second"] = 0.0
		metrics["refreshed_binaries_per_second"] = 0.0
		return metrics, nil
	}

	work, err := s.buildAssembleWork(ctx, headers)
	if err != nil {
		metrics["processed_headers"] = 0
		metrics["binaries_refreshed"] = 0
		addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, 0, 0, 0, 0, 0)
		return metrics, err
	}

	metrics, err = s.persistAssembleWork(ctx, started, metrics, work, batchSize)
	if err == nil {
		s.regroupSubjectMultipartBinaries(ctx, metrics, batchSize)
	}
	return metrics, err
}

func (s *Service) regroupSubjectMultipartBinaries(ctx context.Context, metrics map[string]any, limit int) {
	metrics["subject_multipart_regroup_groups"] = int64(0)
	metrics["subject_multipart_regroup_sources"] = int64(0)
	metrics["subject_multipart_regroup_parts_moved"] = int64(0)
	metrics["subject_multipart_regroup_duplicate_parts_deleted"] = int64(0)
	repo, ok := s.repo.(subjectMultipartRegroupRepository)
	if !ok {
		return
	}
	const regroupInterval = 30 * time.Minute
	s.subjectMultipartRegroupMu.Lock()
	if !s.lastSubjectMultipartRegroup.IsZero() && time.Since(s.lastSubjectMultipartRegroup) < regroupInterval {
		s.subjectMultipartRegroupMu.Unlock()
		metrics["subject_multipart_regroup_skipped"] = true
		return
	}
	s.lastSubjectMultipartRegroup = time.Now()
	s.subjectMultipartRegroupMu.Unlock()

	if limit <= 0 {
		limit = s.opts.BatchSize
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	started := time.Now()
	result, err := repo.RegroupSubjectMultipartBinaries(ctx, limit)
	metrics["subject_multipart_regroup_duration_ms"] = durationMillis(time.Since(started))
	if err != nil {
		metrics["subject_multipart_regroup_error"] = err.Error()
		if s.log != nil {
			s.log.Warn("assemble: subject multipart regroup skipped err=%v", err)
		}
		return
	}
	if result == nil {
		return
	}
	metrics["subject_multipart_regroup_groups"] = result.Groups
	metrics["subject_multipart_regroup_sources"] = result.SourceBinaries
	metrics["subject_multipart_regroup_parts_moved"] = result.PartsMoved
	metrics["subject_multipart_regroup_duplicate_parts_deleted"] = result.DuplicatePartsDeleted
	if result.Groups > 0 && s.log != nil {
		s.log.Info(
			"assemble: subject multipart regroup groups=%d sources=%d parts_moved=%d duplicate_parts_deleted=%d duration_ms=%.2f",
			result.Groups,
			result.SourceBinaries,
			result.PartsMoved,
			result.DuplicatePartsDeleted,
			metrics["subject_multipart_regroup_duration_ms"],
		)
	}
}

func (s *Service) buildAssembleWork(ctx context.Context, headers []pgindex.AssemblyCandidate) (assembleWork, error) {
	work := assembleWork{
		binaryRecordsByKey: make(map[string]pgindex.BinaryRecord, len(headers)),
		orderedBinaryKeys:  make([]string, 0, len(headers)),
		partSeeds:          make([]pendingBinaryPart, 0, len(headers)),
	}

	for _, header := range headers {
		if header.StructuredIdentityBinaryMatched {
			work.laneASelected++
		}
	}
	work.laneBSelected = len(headers) - work.laneASelected

	for _, header := range headers {
		if err := ctx.Err(); err != nil {
			return work, err
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
			work.recovery.skippedByBackoff++
		} else if s.shouldAttemptYEncRecovery(header, matched) {
			if work.recovery.attempts >= s.opts.MaxYEncRecoveryAttempts {
				work.recovery.skippedByCap++
			} else {
				work.recovery.attempts++
				rematched, result, err := s.rematchFromYEncHeader(ctx, header, candidate)
				if err != nil {
					return work, fmt.Errorf("recover yenc metadata for article %d: %w", header.ID, err)
				}
				if result.fetchFailed {
					work.recovery.fetchFailures++
				}
				if result.recovered {
					work.recovery.successes++
					matched = rematched
				} else {
					work.recovery.noops++
				}
			}
		}
		work.headerMatchDuration += time.Since(matchStarted)

		binaryRecord := pgindex.BinaryRecord{
			ProviderID:        header.ProviderID,
			NewsgroupID:       header.NewsgroupID,
			PosterID:          header.PosterID,
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
		if _, ok := work.binaryRecordsByKey[binaryCacheKey]; !ok {
			work.orderedBinaryKeys = append(work.orderedBinaryKeys, binaryCacheKey)
			work.binaryRecordsByKey[binaryCacheKey] = binaryRecord
		}

		work.partSeeds = append(work.partSeeds, pendingBinaryPart{
			binaryCacheKey:  binaryCacheKey,
			articleHeaderID: header.ID,
			sourcePostedAt:  header.SourcePostedAt,
			messageID:       header.MessageID,
			partNumber:      matched.PartNumber,
			totalParts:      matched.TotalParts,
			segmentBytes:    header.Bytes,
			fileName:        matched.FileName,
		})
	}

	return work, nil
}

func (s *Service) persistAssembleWork(ctx context.Context, started time.Time, metrics map[string]any, work assembleWork, batchSize int) (map[string]any, error) {
	refreshed := make(map[int64]struct{}, len(work.partSeeds))
	assembledCount := 0
	binaryIDsByKey := make(map[string]int64, len(work.binaryRecordsByKey))
	partRecords := make([]pgindex.BinaryPartRecord, 0, len(work.partSeeds))
	var (
		binaryUpsertDuration     time.Duration
		binaryPartUpsertDuration time.Duration
		binaryRefreshDuration    time.Duration
	)

	if len(work.orderedBinaryKeys) > 0 {
		sort.Strings(work.orderedBinaryKeys)
		binaryUpsertStarted := time.Now()
		batchRecords := make([]pgindex.BinaryRecord, 0, len(work.orderedBinaryKeys))
		for _, key := range work.orderedBinaryKeys {
			batchRecords = append(batchRecords, work.binaryRecordsByKey[key])
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
			addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("upsert binaries batch: %w", err)
		}
		if len(binaryIDs) != len(work.orderedBinaryKeys) {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("upsert binaries batch returned %d ids for %d records", len(binaryIDs), len(work.orderedBinaryKeys))
		}
		for i, key := range work.orderedBinaryKeys {
			binaryIDsByKey[key] = binaryIDs[i]
		}
	}

	for _, seed := range work.partSeeds {
		binaryID, ok := binaryIDsByKey[seed.binaryCacheKey]
		if !ok {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("missing binary id for cache key %q", seed.binaryCacheKey)
		}
		partRecords = append(partRecords, pgindex.BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: seed.articleHeaderID,
			SourcePostedAt:  seed.sourcePostedAt,
			MessageID:       seed.messageID,
			PartNumber:      seed.partNumber,
			TotalParts:      seed.totalParts,
			SegmentBytes:    seed.segmentBytes,
			FileName:        seed.fileName,
		})
		refreshed[binaryID] = struct{}{}
	}
	sort.Slice(partRecords, func(i, j int) bool {
		if partRecords[i].BinaryID != partRecords[j].BinaryID {
			return partRecords[i].BinaryID < partRecords[j].BinaryID
		}
		if partRecords[i].PartNumber != partRecords[j].PartNumber {
			return partRecords[i].PartNumber < partRecords[j].PartNumber
		}
		return partRecords[i].ArticleHeaderID < partRecords[j].ArticleHeaderID
	})

	binaryPartUpsertStarted := time.Now()
	if err := s.repo.UpsertBinaryParts(ctx, partRecords); err != nil {
		metrics["processed_headers"] = assembledCount
		metrics["binaries_refreshed"] = len(refreshed)
		binaryPartUpsertDuration += time.Since(binaryPartUpsertStarted)
		addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
		return metrics, fmt.Errorf("upsert binary parts batch: %w", err)
	}
	binaryPartUpsertDuration += time.Since(binaryPartUpsertStarted)
	assembledCount = len(partRecords)

	refreshIDs := make([]int64, 0, len(refreshed))
	for binaryID := range refreshed {
		refreshIDs = append(refreshIDs, binaryID)
	}
	sort.Slice(refreshIDs, func(i, j int) bool { return refreshIDs[i] < refreshIDs[j] })
	if len(refreshIDs) > 0 {
		refreshStarted := time.Now()
		refreshCtx := pgindex.WithDeferredReleaseFamilySummaryRefresh(ctx)
		refreshCtx = pgindex.WithSkipYEncRecoveryWorkItemSync(refreshCtx)
		refreshTelemetry := &pgindex.BinaryStatsRefreshTelemetry{}
		refreshCtx = pgindex.WithBinaryStatsRefreshTelemetry(refreshCtx, refreshTelemetry)
		if err := s.repo.RefreshBinaryStatsBatch(refreshCtx, refreshIDs); err != nil {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			binaryRefreshDuration += time.Since(refreshStarted)
			addBinaryStatsRefreshTelemetryMetrics(metrics, refreshTelemetry.Snapshot())
			addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("refresh binary stats batch: %w", err)
		}
		binaryRefreshDuration += time.Since(refreshStarted)
		addBinaryStatsRefreshTelemetryMetrics(metrics, refreshTelemetry.Snapshot())
	}
	metrics["lane_a_selected"] = work.laneASelected
	metrics["lane_b_selected"] = work.laneBSelected
	metrics["processed_headers"] = assembledCount
	metrics["binaries_refreshed"] = len(refreshed)
	metrics["unique_binary_upserts"] = len(binaryIDsByKey)
	metrics["binary_upsert_cache_hits"] = len(partRecords) - len(binaryIDsByKey)
	metrics["binary_part_batch_size"] = len(partRecords)
	metrics["recovery_attempts"] = work.recovery.attempts
	metrics["recovery_successes"] = work.recovery.successes
	metrics["recovery_noops"] = work.recovery.noops
	metrics["recovery_fetch_failures"] = work.recovery.fetchFailures
	metrics["recovery_skipped_by_cap"] = work.recovery.skippedByCap
	metrics["recovery_skipped_by_backoff"] = work.recovery.skippedByBackoff
	addAssembleTimingMetrics(metrics, started, work.headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))

	s.log.Info(
		"assemble: lane_mode=%s lane_a_selected=%d lane_b_selected=%d processed_headers=%d binaries_refreshed=%d batch_size=%d headers_per_second=%.2f refreshed_binaries_per_second=%.2f candidate_selection_ms=%.2f queue_cleanup_ms=%.2f assembly_claim_ms=%.2f assembly_hydration_ms=%.2f assembly_claimed_headers=%d assembly_hydrated_headers=%d header_match_ms=%.2f binary_upsert_ms=%.2f binary_part_upsert_ms=%.2f binary_refresh_ms=%.2f binary_upsert_chunk_count=%d binary_upsert_chunk_rows=%d binary_upsert_chunk_retries=%d binary_upsert_chunk_retry_deadlocks=%d binary_upsert_chunk_retry_serialization=%d binary_upsert_chunk_ms=%.2f binary_upsert_chunk_max_ms=%.2f binary_upsert_lock_ms=%.2f binary_upsert_lock_max_ms=%.2f binary_upsert_stage_ms=%.2f binary_upsert_stage_max_ms=%.2f binary_upsert_existing_snapshot_ms=%.2f binary_upsert_existing_snapshot_max_ms=%.2f binary_upsert_update_ms=%.2f binary_upsert_update_max_ms=%.2f binary_upsert_insert_ms=%.2f binary_upsert_insert_max_ms=%.2f binary_upsert_readback_ms=%.2f binary_upsert_readback_max_ms=%.2f binary_upsert_query_ms=%.2f binary_upsert_query_max_ms=%.2f binary_upsert_evidence_ms=%.2f binary_upsert_evidence_max_ms=%.2f binary_upsert_deferred_summary_chunks=%d binary_upsert_deferred_summary_keys=%d binary_refresh_tx_count=%d binary_refresh_batch_count=%d binary_refresh_binary_count=%d binary_refresh_summary_key_count=%d binary_refresh_deferred_summary_batches=%d binary_refresh_deferred_summary_keys=%d binary_refresh_stats_update_ms=%.2f binary_refresh_stats_update_max_ms=%.2f binary_refresh_summary_mark_ms=%.2f binary_refresh_summary_mark_max_ms=%.2f binary_refresh_yenc_sync_ms=%.2f binary_refresh_yenc_sync_max_ms=%.2f unique_binary_upserts=%d binary_upsert_cache_hits=%d assemble_recovery_attempts=%d assemble_recovery_successes=%d assemble_recovery_noops=%d assemble_recovery_fetch_failures=%d assemble_recovery_skipped_by_cap=%d assemble_recovery_skipped_by_backoff=%d",
		laneMetricName(s.opts.Lane),
		work.laneASelected,
		work.laneBSelected,
		assembledCount,
		len(refreshed),
		batchSize,
		metrics["headers_per_second"],
		metrics["refreshed_binaries_per_second"],
		metrics["candidate_selection_duration_ms"],
		metrics["queue_cleanup_duration_ms"],
		metrics["assembly_claim_duration_ms"],
		metrics["assembly_hydration_duration_ms"],
		metrics["assembly_claimed_headers"],
		metrics["assembly_hydrated_headers"],
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
		metrics["binary_upsert_lock_ms"],
		metrics["binary_upsert_lock_max_ms"],
		metrics["binary_upsert_stage_ms"],
		metrics["binary_upsert_stage_max_ms"],
		metrics["binary_upsert_existing_snapshot_ms"],
		metrics["binary_upsert_existing_snapshot_max_ms"],
		metrics["binary_upsert_update_ms"],
		metrics["binary_upsert_update_max_ms"],
		metrics["binary_upsert_insert_ms"],
		metrics["binary_upsert_insert_max_ms"],
		metrics["binary_upsert_readback_ms"],
		metrics["binary_upsert_readback_max_ms"],
		metrics["binary_upsert_query_ms"],
		metrics["binary_upsert_query_max_ms"],
		metrics["binary_upsert_evidence_ms"],
		metrics["binary_upsert_evidence_max_ms"],
		metrics["binary_upsert_deferred_summary_chunks"],
		metrics["binary_upsert_deferred_summary_keys"],
		metrics["binary_refresh_tx_count"],
		metrics["binary_refresh_batch_count"],
		metrics["binary_refresh_binary_count"],
		metrics["binary_refresh_summary_key_count"],
		metrics["binary_refresh_deferred_summary_batches"],
		metrics["binary_refresh_deferred_summary_keys"],
		metrics["binary_refresh_stats_update_ms"],
		metrics["binary_refresh_stats_update_max_ms"],
		metrics["binary_refresh_summary_mark_ms"],
		metrics["binary_refresh_summary_mark_max_ms"],
		metrics["binary_refresh_yenc_sync_ms"],
		metrics["binary_refresh_yenc_sync_max_ms"],
		metrics["unique_binary_upserts"],
		metrics["binary_upsert_cache_hits"],
		work.recovery.attempts,
		work.recovery.successes,
		work.recovery.noops,
		work.recovery.fetchFailures,
		work.recovery.skippedByCap,
		work.recovery.skippedByBackoff,
	)

	return metrics, nil
}

func mergeAssembleWorks(works []assembleWork) assembleWork {
	merged := assembleWork{
		binaryRecordsByKey: make(map[string]pgindex.BinaryRecord),
	}
	for _, work := range works {
		merged.laneASelected += work.laneASelected
		merged.laneBSelected += work.laneBSelected
		merged.headerMatchDuration += work.headerMatchDuration
		merged.recovery.attempts += work.recovery.attempts
		merged.recovery.successes += work.recovery.successes
		merged.recovery.noops += work.recovery.noops
		merged.recovery.fetchFailures += work.recovery.fetchFailures
		merged.recovery.skippedByCap += work.recovery.skippedByCap
		merged.recovery.skippedByBackoff += work.recovery.skippedByBackoff
		for _, key := range work.orderedBinaryKeys {
			if _, ok := merged.binaryRecordsByKey[key]; ok {
				continue
			}
			merged.orderedBinaryKeys = append(merged.orderedBinaryKeys, key)
			merged.binaryRecordsByKey[key] = work.binaryRecordsByKey[key]
		}
		merged.partSeeds = append(merged.partSeeds, work.partSeeds...)
	}
	return merged
}

func mergeAssembleMetrics(dst map[string]any, src map[string]any) {
	for key, value := range src {
		switch key {
		case "batch_size", "total_duration_ms", "headers_per_second", "refreshed_binaries_per_second":
			continue
		case "binary_upsert_chunk_max_ms",
			"binary_upsert_lock_max_ms",
			"binary_upsert_stage_max_ms",
			"binary_upsert_existing_snapshot_max_ms",
			"binary_upsert_update_max_ms",
			"binary_upsert_insert_max_ms",
			"binary_upsert_readback_max_ms",
			"binary_upsert_query_max_ms",
			"binary_upsert_evidence_max_ms",
			"binary_refresh_stats_update_max_ms",
			"binary_refresh_summary_mark_max_ms",
			"binary_refresh_yenc_sync_max_ms":
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

func addAssembleTimingMetrics(metrics map[string]any, started time.Time, headerMatchDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration time.Duration, processedHeaders, refreshedBinaries int) {
	totalDuration := time.Since(started)
	metrics["header_match_duration_ms"] = durationMillis(headerMatchDuration)
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
	metrics["binary_upsert_lock_ms"] = telemetry.LockDurationMs
	metrics["binary_upsert_lock_max_ms"] = telemetry.LockDurationMaxMs
	metrics["binary_upsert_stage_ms"] = telemetry.StageDurationMs
	metrics["binary_upsert_stage_max_ms"] = telemetry.StageDurationMaxMs
	metrics["binary_upsert_existing_snapshot_ms"] = telemetry.ExistingSnapshotDurationMs
	metrics["binary_upsert_existing_snapshot_max_ms"] = telemetry.ExistingSnapshotDurationMaxMs
	metrics["binary_upsert_update_ms"] = telemetry.UpdateDurationMs
	metrics["binary_upsert_update_max_ms"] = telemetry.UpdateDurationMaxMs
	metrics["binary_upsert_insert_ms"] = telemetry.InsertDurationMs
	metrics["binary_upsert_insert_max_ms"] = telemetry.InsertDurationMaxMs
	metrics["binary_upsert_readback_ms"] = telemetry.ReadbackDurationMs
	metrics["binary_upsert_readback_max_ms"] = telemetry.ReadbackDurationMaxMs
	metrics["binary_upsert_query_ms"] = telemetry.UpsertQueryDurationMs
	metrics["binary_upsert_query_max_ms"] = telemetry.UpsertQueryDurationMaxMs
	metrics["binary_upsert_evidence_ms"] = telemetry.EvidenceDurationMs
	metrics["binary_upsert_evidence_max_ms"] = telemetry.EvidenceDurationMaxMs
	metrics["binary_upsert_deferred_summary_chunks"] = telemetry.DeferredSummaryRefreshChunks
	metrics["binary_upsert_deferred_summary_keys"] = telemetry.DeferredSummaryKeyCount
}

func addBinaryStatsRefreshTelemetryMetrics(metrics map[string]any, telemetry pgindex.BinaryStatsRefreshTelemetry) {
	metrics["binary_refresh_tx_count"] = telemetry.TxCount
	metrics["binary_refresh_batch_count"] = telemetry.BatchCount
	metrics["binary_refresh_binary_count"] = telemetry.BinaryCount
	metrics["binary_refresh_summary_key_count"] = telemetry.SummaryKeyCount
	metrics["binary_refresh_deferred_summary_batches"] = telemetry.DeferredSummaryRefreshBatches
	metrics["binary_refresh_deferred_summary_keys"] = telemetry.DeferredSummaryKeyCount
	metrics["binary_refresh_stats_update_ms"] = telemetry.StatsUpdateDurationMs
	metrics["binary_refresh_stats_update_max_ms"] = telemetry.StatsUpdateDurationMaxMs
	metrics["binary_refresh_summary_mark_ms"] = telemetry.SummaryMarkDurationMs
	metrics["binary_refresh_summary_mark_max_ms"] = telemetry.SummaryMarkDurationMaxMs
	metrics["binary_refresh_yenc_sync_ms"] = telemetry.YEncSyncDurationMs
	metrics["binary_refresh_yenc_sync_max_ms"] = telemetry.YEncSyncDurationMaxMs
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
	rematched = canonicalizeRecoveredYEncMatch(rematched)
	return rematched, recoveryAttemptResult{recovered: true}, nil
}

func canonicalizeRecoveredYEncMatch(in match.Result) match.Result {
	fileKey := normalizeRecoveredYEncKey(firstNonEmptyString(in.FileName, in.BinaryName))
	familyKey := normalizeRecoveredYEncKey(firstNonEmptyString(in.FileSetKey, in.ReleaseFamilyKey, in.ReleaseKey, in.SourceReleaseKey))
	if fileKey == "" || familyKey == "" {
		return in
	}
	in.BinaryKey = familyKey + "::" + fileKey
	if in.FileSetKey != "" {
		in.SourceReleaseKey = in.FileSetKey
		in.ReleaseFamilyKey = firstNonEmptyString(in.ReleaseFamilyKey, in.FileSetKey)
		in.ReleaseKey = firstNonEmptyString(in.ReleaseFamilyKey, in.FileSetKey)
	}
	if strings.TrimSpace(in.FileFamilyKey) == "" {
		in.FileFamilyKey = normalizeRecoveredYEncKey(familyKey + " " + fileKey)
	}
	return in
}

func normalizeRecoveredYEncKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	lastSpace := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
