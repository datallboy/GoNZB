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
	ListReleaseCandidates(ctx context.Context, limit int) ([]pgindex.ReleaseCandidate, error)
	ListExistingReleaseCandidates(ctx context.Context, limit, offset int) ([]pgindex.ReleaseCandidate, error)
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, releaseFamilyKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)
	ListBinaryPartArticlesBatch(ctx context.Context, binaryIDs []int64) (map[int64][]pgindex.ReleaseFileArticleRecord, error)
	ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]pgindex.ReleaseTitleCandidate, error)

	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, releaseFamilyKey string, keepGroupNames []string) error
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
	AckReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, familyKey string) error
	AckReleaseCandidates(ctx context.Context, candidates []pgindex.ReleaseCandidateAck) error
}

type Options struct {
	BatchSize                                          int
	ReleaseMinConfidence                               float64
	ReleaseMinCompletion                               float64
	RequireExpectedFileCountForContextualObfuscated    bool
	RequireExpectedFileCountForContextualObfuscatedSet bool
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
	if opts.ReleaseMinConfidence <= 0 {
		opts.ReleaseMinConfidence = 0.55
	}
	if opts.ReleaseMinCompletion < 0 {
		opts.ReleaseMinCompletion = 0
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

func (s *Service) runOnce(ctx context.Context, reform bool) error {
	_, err := s.runOnceWithMetrics(ctx, reform)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	return s.runOnceWithMetrics(ctx, false)
}

func (s *Service) RunReformOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	return s.runOnceWithMetrics(ctx, true)
}

func (s *Service) runOnceWithMetrics(ctx context.Context, reform bool) (map[string]any, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("release repo is required")
	}

	var (
		candidates []pgindex.ReleaseCandidate
		err        error
		timings    releaseTimings
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
		start := time.Now()
		candidates, err = s.repo.ListReleaseCandidates(ctx, s.opts.BatchSize)
		timings.candidateList += time.Since(start)
		if err != nil {
			return nil, fmt.Errorf("list release candidates: %w", err)
		}
	}
	metrics := map[string]any{
		"reform":                 reform,
		"batch_size":             s.opts.BatchSize,
		"min_confidence":         s.opts.ReleaseMinConfidence,
		"min_completion_pct":     s.opts.ReleaseMinCompletion,
		"candidate_families":     len(candidates),
		"formed":                 0,
		"skipped_fragments":      0,
		"skipped_confidence":     0,
		"skipped_completion":     0,
		"stale_cleanup_families": 0,
		"fragment_only_families": 0,
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
	staleCleanupOnly := 0
	skippedFragments := 0
	skippedConfidence := 0
	skippedCompletion := 0
	deferredAcks := make([]pgindex.ReleaseCandidateAck, 0, 128)
	for _, candidate := range candidates {
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
			metrics["stale_cleanup_families"] = staleCleanupOnly
			metrics["skipped_fragments"] = skippedFragments
			metrics["skipped_confidence"] = skippedConfidence
			metrics["skipped_completion"] = skippedCompletion
			return metrics, fmt.Errorf("form release candidate %s: %w", candidateFamilyKey(candidate), err)
		}
		formed += outcome.formed
		cooledDownFragmentOnly += outcome.cooledDownFragmentOnly
		staleCleanupOnly += outcome.staleCleanupOnly
		skippedFragments += outcome.skippedFragments
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
	metrics["fragment_only_families"] = cooledDownFragmentOnly
	metrics["stale_cleanup_families"] = staleCleanupOnly
	metrics["skipped_fragments"] = skippedFragments
	metrics["skipped_confidence"] = skippedConfidence
	metrics["skipped_completion"] = skippedCompletion
	timings.addMetrics(metrics)

	s.log.Info(
		"release: candidate_families=%d formed=%d cooled_down_fragment_only_families=%d stale_cleanup_only_families=%d skipped_fragments=%d skipped_confidence=%d skipped_completion=%d batch_size=%d min_confidence=%.2f min_completion_pct=%.2f reform=%t",
		candidateFamiliesInspected,
		formed,
		cooledDownFragmentOnly,
		staleCleanupOnly,
		skippedFragments,
		skippedConfidence,
		skippedCompletion,
		s.opts.BatchSize,
		s.opts.ReleaseMinConfidence,
		s.opts.ReleaseMinCompletion,
		reform,
	)
	return metrics, nil
}

type releaseTimings struct {
	candidateList      time.Duration
	listBinaries       time.Duration
	titleCandidates    time.Duration
	buildFiles         time.Duration
	articleLookup      time.Duration
	upsertRelease      time.Duration
	replaceFiles       time.Duration
	replaceNewsgroups  time.Duration
	upsertNZBCache     time.Duration
	deleteStale        time.Duration
	ackCandidate       time.Duration
	binariesListed     int
	articleLookupCalls int
	filesBuilt         int
	articlesBuilt      int
}

func (t *releaseTimings) addMetrics(metrics map[string]any) {
	if t == nil {
		return
	}
	metrics["candidate_list_duration_ms"] = durationMillis(t.candidateList)
	metrics["list_binaries_duration_ms"] = durationMillis(t.listBinaries)
	metrics["title_candidates_duration_ms"] = durationMillis(t.titleCandidates)
	metrics["build_files_duration_ms"] = durationMillis(t.buildFiles)
	metrics["article_lookup_duration_ms"] = durationMillis(t.articleLookup)
	metrics["upsert_release_duration_ms"] = durationMillis(t.upsertRelease)
	metrics["replace_files_duration_ms"] = durationMillis(t.replaceFiles)
	metrics["replace_newsgroups_duration_ms"] = durationMillis(t.replaceNewsgroups)
	metrics["upsert_nzb_cache_duration_ms"] = durationMillis(t.upsertNZBCache)
	metrics["delete_stale_duration_ms"] = durationMillis(t.deleteStale)
	metrics["ack_candidate_duration_ms"] = durationMillis(t.ackCandidate)
	metrics["binaries_listed"] = t.binariesListed
	metrics["article_lookup_calls"] = t.articleLookupCalls
	metrics["files_built"] = t.filesBuilt
	metrics["articles_built"] = t.articlesBuilt
}

func durationMillis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

type candidateOutcome struct {
	formed                 int
	cooledDownFragmentOnly int
	staleCleanupOnly       int
	skippedFragments       int
	skippedConfidence      int
	skippedCompletion      int
	deferredAck            *pgindex.ReleaseCandidateAck
}

func (s *Service) formCandidate(ctx context.Context, candidate pgindex.ReleaseCandidate, timings *releaseTimings) (candidateOutcome, error) {
	familyKey := candidateFamilyKey(candidate)
	if familyKey == "" {
		return candidateOutcome{}, fmt.Errorf("release family key is required")
	}

	if candidate.ReadinessBucket == "stale_cleanup_only" {
		start := time.Now()
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, nil); err != nil {
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
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, nil); err != nil {
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
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete empty stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		if candidate.KeyKind != "" {
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
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete fragment-only stale releases: %w", err)
		}
		if timings != nil {
			timings.deleteStale += time.Since(start)
		}
		if candidate.KeyKind != "" {
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

	for _, cluster := range clusters {
		if err := ctx.Err(); err != nil {
			return outcome, err
		}

		start = time.Now()
		titleCandidates, err := s.repo.ListReleaseTitleCandidates(ctx, binaryIDsForCluster(cluster.Binaries))
		if timings != nil {
			timings.titleCandidates += time.Since(start)
		}
		if err != nil {
			return outcome, fmt.Errorf("list release title candidates for %s: %w", familyKey, err)
		}

		record := buildReleaseRecord(candidate, cluster, titleCandidates)
		if !shouldPersistCluster(cluster, record, s.opts) {
			outcome.skippedFragments++
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

		start = time.Now()
		releaseID, err := s.repo.UpsertRelease(ctx, record)
		if timings != nil {
			timings.upsertRelease += time.Since(start)
		}
		if err != nil {
			return outcome, fmt.Errorf("upsert release %s: %w", record.GroupName, err)
		}
		start = time.Now()
		if err := s.repo.ReplaceReleaseFiles(ctx, releaseID, files); err != nil {
			return outcome, fmt.Errorf("replace release files for %s: %w", releaseID, err)
		}
		if timings != nil {
			timings.replaceFiles += time.Since(start)
		}
		start = time.Now()
		if err := s.repo.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{candidate.NewsgroupID}); err != nil {
			return outcome, fmt.Errorf("replace release newsgroups for %s: %w", releaseID, err)
		}
		if timings != nil {
			timings.replaceNewsgroups += time.Since(start)
		}
		start = time.Now()
		if err := s.repo.UpsertNZBCache(ctx, releaseID, "pending", "", ""); err != nil {
			return outcome, fmt.Errorf("upsert nzb cache for %s: %w", releaseID, err)
		}
		if timings != nil {
			timings.upsertNZBCache += time.Since(start)
		}

		keepGroupNames = append(keepGroupNames, record.GroupName)
		outcome.formed++
	}

	start = time.Now()
	if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, keepGroupNames); err != nil {
		return outcome, fmt.Errorf("delete stale release groups for %s: %w", familyKey, err)
	}
	if timings != nil {
		timings.deleteStale += time.Since(start)
	}
	if candidate.KeyKind != "" {
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

func buildDeferredReleaseAck(candidate pgindex.ReleaseCandidate, familyKey string) *pgindex.ReleaseCandidateAck {
	if candidate.KeyKind == "" || familyKey == "" {
		return nil
	}
	return &pgindex.ReleaseCandidateAck{
		ProviderID:  candidate.ProviderID,
		NewsgroupID: candidate.NewsgroupID,
		KeyKind:     candidate.KeyKind,
		FamilyKey:   familyKey,
	}
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

func shouldPersistCluster(cluster releaseCluster, record pgindex.ReleaseRecord, opts Options) bool {
	mainPayloadCount := countMainPayloadBinaries(cluster.Binaries)
	if mainPayloadCount == 0 {
		return false
	}
	expectedFiles := clusterExpectedFileCount(cluster.Binaries)
	if expectedFiles > 1 && mainPayloadCount < 2 {
		return false
	}
	if opts.RequireExpectedFileCountForContextualObfuscated &&
		expectedFiles <= 0 &&
		clusterIsContextualObfuscated(cluster.Binaries) &&
		!allowsStandaloneBinaryRelease(cluster.Binaries, record) {
		return false
	}
	if mainPayloadCount == 1 && !allowsStandaloneBinaryRelease(cluster.Binaries, record) {
		return false
	}
	return true
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

func (s *Service) buildReleaseFiles(ctx context.Context, cluster releaseCluster, timings *releaseTimings) ([]pgindex.ReleaseFileRecord, error) {
	selected := make([]pgindex.BinarySummary, 0, len(cluster.Binaries))
	byName := make(map[string]int, len(cluster.Binaries))
	for _, binary := range cluster.Binaries {
		fileName := pickFileName(binary)
		key := strings.ToLower(strings.TrimSpace(fileName))
		if key == "" {
			key = fmt.Sprintf("binary-%d", binary.BinaryID)
		}
		if existingIdx, ok := byName[key]; ok {
			if prefersBinaryForReleaseFile(binary, selected[existingIdx]) {
				selected[existingIdx] = binary
			}
			continue
		}
		byName[key] = len(selected)
		selected = append(selected, binary)
	}

	binaryIDs := make([]int64, 0, len(selected))
	for _, binary := range selected {
		if binary.BinaryID > 0 {
			binaryIDs = append(binaryIDs, binary.BinaryID)
		}
	}

	start := time.Now()
	articlesByBinaryID, err := s.repo.ListBinaryPartArticlesBatch(ctx, binaryIDs)
	if timings != nil {
		timings.articleLookup += time.Since(start)
		if len(binaryIDs) > 0 {
			timings.articleLookupCalls++
		}
	}
	if err != nil {
		return nil, fmt.Errorf("list binary part articles batch: %w", err)
	}

	files := make([]pgindex.ReleaseFileRecord, 0, len(selected))
	for idx, binary := range selected {
		articles := articlesByBinaryID[binary.BinaryID]

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
			Articles:  articles,
		})
		if timings != nil {
			timings.filesBuilt++
			timings.articlesBuilt += len(articles)
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
