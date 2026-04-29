package assemble

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/match"
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
	CountUnassembledArticleHeaders(ctx context.Context) (int64, error)
	ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]pgindex.AssemblyCandidate, error)
	EnsurePoster(ctx context.Context, posterName string) (int64, error)
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaryParts(ctx context.Context, records []pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error
}

// narrow matcher dependency.
type subjectMatcher interface {
	Match(candidate match.Candidate) match.Result
}

type articleFetcher interface {
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
}

type Options struct {
	BatchSize int
}

type recoveryCounters struct {
	attempts      int
	successes     int
	noops         int
	fetchFailures int
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
	if s.repo == nil {
		return nil, fmt.Errorf("assembly repo is required")
	}
	if s.matcher == nil {
		return nil, fmt.Errorf("assembly matcher is required")
	}

	started := time.Now()
	countStarted := time.Now()
	pendingCount, err := s.repo.CountUnassembledArticleHeaders(ctx)
	if err != nil {
		return nil, fmt.Errorf("count unassembled article headers: %w", err)
	}
	countDuration := time.Since(countStarted)

	selectionStarted := time.Now()
	headers, err := s.repo.ListUnassembledArticleHeaders(ctx, s.opts.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("list unassembled article headers: %w", err)
	}
	selectionDuration := time.Since(selectionStarted)
	metrics := map[string]any{
		"pending_headers":                 pendingCount,
		"selected_headers":                len(headers),
		"batch_size":                      s.opts.BatchSize,
		"pending_count_duration_ms":       durationMillis(countDuration),
		"candidate_selection_duration_ms": durationMillis(selectionDuration),
	}
	if len(headers) == 0 {
		s.log.Debug("assemble: no unassembled article headers found pending_headers=%d", pendingCount)
		metrics["processed_headers"] = 0
		metrics["binaries_refreshed"] = 0
		metrics["total_duration_ms"] = durationMillis(time.Since(started))
		metrics["headers_per_second"] = 0.0
		metrics["refreshed_binaries_per_second"] = 0.0
		return metrics, nil
	}

	refreshed := make(map[int64]struct{}, len(headers))
	assembledCount := 0
	binaryIDsByKey := make(map[string]int64, len(headers))
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
		if s.shouldAttemptYEncRecovery(header, matched) {
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
		headerMatchDuration += time.Since(matchStarted)

		posterID := header.PosterID
		if posterID <= 0 {
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
		}

		binaryRecord := pgindex.BinaryRecord{
			ProviderID:        header.ProviderID,
			NewsgroupID:       header.NewsgroupID,
			PosterID:          posterID,
			SourceReleaseKey:  matched.SourceReleaseKey,
			ReleaseFamilyKey:  matched.ReleaseFamilyKey,
			FileFamilyKey:     matched.FileFamilyKey,
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
		binaryID, ok := binaryIDsByKey[binaryCacheKey]
		if !ok {
			binaryUpsertStarted := time.Now()
			var err error
			binaryID, err = s.repo.UpsertBinary(ctx, binaryRecord)
			binaryUpsertDuration += time.Since(binaryUpsertStarted)
			if err != nil {
				metrics["processed_headers"] = assembledCount
				metrics["binaries_refreshed"] = len(refreshed)
				addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
				return metrics, fmt.Errorf("upsert binary for article %d: %w", header.ID, err)
			}
			binaryIDsByKey[binaryCacheKey] = binaryID
		}

		partRecords = append(partRecords, pgindex.BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: header.ID,
			MessageID:       header.MessageID,
			PartNumber:      matched.PartNumber,
			TotalParts:      matched.TotalParts,
			SegmentBytes:    header.Bytes,
			FileName:        matched.FileName,
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

	for binaryID := range refreshed {
		refreshStarted := time.Now()
		if err := s.repo.RefreshBinaryStats(ctx, binaryID); err != nil {
			metrics["processed_headers"] = assembledCount
			metrics["binaries_refreshed"] = len(refreshed)
			binaryRefreshDuration += time.Since(refreshStarted)
			addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))
			return metrics, fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
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
	addAssembleTimingMetrics(metrics, started, headerMatchDuration, posterDuration, binaryUpsertDuration, binaryPartUpsertDuration, binaryRefreshDuration, assembledCount, len(refreshed))

	s.log.Info(
		"assemble: pending_headers=%d lane_a_selected=%d lane_b_selected=%d processed_headers=%d binaries_refreshed=%d batch_size=%d headers_per_second=%.2f refreshed_binaries_per_second=%.2f candidate_selection_ms=%.2f header_match_ms=%.2f binary_upsert_ms=%.2f binary_part_upsert_ms=%.2f binary_refresh_ms=%.2f assemble_recovery_attempts=%d assemble_recovery_successes=%d assemble_recovery_noops=%d assemble_recovery_fetch_failures=%d",
		pendingCount,
		laneASelected,
		laneBSelected,
		assembledCount,
		len(refreshed),
		s.opts.BatchSize,
		metrics["headers_per_second"],
		metrics["refreshed_binaries_per_second"],
		metrics["candidate_selection_duration_ms"],
		metrics["header_match_duration_ms"],
		metrics["binary_upsert_duration_ms"],
		metrics["binary_part_upsert_duration_ms"],
		metrics["binary_refresh_duration_ms"],
		recovery.attempts,
		recovery.successes,
		recovery.noops,
		recovery.fetchFailures,
	)

	return metrics, nil
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
