package release

import (
	"context"
	"fmt"
	"strings"

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
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseFamilyKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)
	ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]pgindex.ReleaseTitleCandidate, error)

	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, releaseFamilyKey string, keepGroupNames []string) error
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
	AckReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, familyKey string) error
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
	return s.runOnce(ctx, false)
}

func (s *Service) RunReformOnce(ctx context.Context) error {
	return s.runOnce(ctx, true)
}

func (s *Service) runOnce(ctx context.Context, reform bool) error {
	if s.repo == nil {
		return fmt.Errorf("release repo is required")
	}

	var (
		candidates []pgindex.ReleaseCandidate
		err        error
	)
	if reform {
		offset := 0
		for {
			page, pageErr := s.repo.ListExistingReleaseCandidates(ctx, s.opts.BatchSize, offset)
			if pageErr != nil {
				return fmt.Errorf("list existing release candidates: %w", pageErr)
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
		candidates, err = s.repo.ListReleaseCandidates(ctx, s.opts.BatchSize)
		if err != nil {
			return fmt.Errorf("list release candidates: %w", err)
		}
	}
	if len(candidates) == 0 {
		if reform {
			s.log.Debug("release: no existing release candidates found for reform")
		} else {
			s.log.Debug("release: no release candidates found")
		}
		return nil
	}

	formed := 0
	candidateFamiliesInspected := 0
	cooledDownFragmentOnly := 0
	staleCleanupOnly := 0
	skippedFragments := 0
	skippedConfidence := 0
	skippedCompletion := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidateFamiliesInspected++
		outcome, err := s.formCandidate(ctx, candidate)
		if err != nil {
			return fmt.Errorf("form release candidate %s: %w", candidateFamilyKey(candidate), err)
		}
		formed += outcome.formed
		cooledDownFragmentOnly += outcome.cooledDownFragmentOnly
		staleCleanupOnly += outcome.staleCleanupOnly
		skippedFragments += outcome.skippedFragments
		skippedConfidence += outcome.skippedConfidence
		skippedCompletion += outcome.skippedCompletion
	}

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
	return nil
}

type candidateOutcome struct {
	formed                 int
	cooledDownFragmentOnly int
	staleCleanupOnly       int
	skippedFragments       int
	skippedConfidence      int
	skippedCompletion      int
}

func (s *Service) formCandidate(ctx context.Context, candidate pgindex.ReleaseCandidate) (candidateOutcome, error) {
	familyKey := candidateFamilyKey(candidate)
	if familyKey == "" {
		return candidateOutcome{}, fmt.Errorf("release family key is required")
	}

	binaries, err := s.repo.ListBinariesForReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, familyKey)
	if err != nil {
		return candidateOutcome{}, fmt.Errorf("list binaries for release candidate: %w", err)
	}
	if len(binaries) == 0 {
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete empty stale releases: %w", err)
		}
		if candidate.KeyKind != "" {
			if err := s.repo.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey); err != nil {
				return candidateOutcome{}, fmt.Errorf("ack empty release candidate: %w", err)
			}
		}
		return candidateOutcome{staleCleanupOnly: 1}, nil
	}

	if countCompleteBinaries(binaries) == 0 {
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, nil); err != nil {
			return candidateOutcome{}, fmt.Errorf("delete fragment-only stale releases: %w", err)
		}
		if candidate.KeyKind != "" {
			if err := s.repo.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey); err != nil {
				return candidateOutcome{}, fmt.Errorf("ack cooled-down fragment-only candidate: %w", err)
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

		titleCandidates, err := s.repo.ListReleaseTitleCandidates(ctx, binaryIDsForCluster(cluster.Binaries))
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

		files, err := s.buildReleaseFiles(ctx, cluster)
		if err != nil {
			return outcome, fmt.Errorf("build release files for %s: %w", record.GroupName, err)
		}

		releaseID, err := s.repo.UpsertRelease(ctx, record)
		if err != nil {
			return outcome, fmt.Errorf("upsert release %s: %w", record.GroupName, err)
		}
		if err := s.repo.ReplaceReleaseFiles(ctx, releaseID, files); err != nil {
			return outcome, fmt.Errorf("replace release files for %s: %w", releaseID, err)
		}
		if err := s.repo.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{candidate.NewsgroupID}); err != nil {
			return outcome, fmt.Errorf("replace release newsgroups for %s: %w", releaseID, err)
		}
		if err := s.repo.UpsertNZBCache(ctx, releaseID, "pending", "", ""); err != nil {
			return outcome, fmt.Errorf("upsert nzb cache for %s: %w", releaseID, err)
		}

		keepGroupNames = append(keepGroupNames, record.GroupName)
		outcome.formed++
	}

	if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, familyKey, keepGroupNames); err != nil {
		return outcome, fmt.Errorf("delete stale release groups for %s: %w", familyKey, err)
	}
	if candidate.KeyKind != "" {
		if err := s.repo.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, familyKey); err != nil {
			return outcome, fmt.Errorf("ack release candidate %s: %w", familyKey, err)
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

func (s *Service) buildReleaseFiles(ctx context.Context, cluster releaseCluster) ([]pgindex.ReleaseFileRecord, error) {
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

	files := make([]pgindex.ReleaseFileRecord, 0, len(selected))
	for idx, binary := range selected {
		articles, err := s.repo.ListBinaryPartArticles(ctx, binary.BinaryID)
		if err != nil {
			return nil, fmt.Errorf("list binary part articles %d: %w", binary.BinaryID, err)
		}

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
