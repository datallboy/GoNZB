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
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)
	ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]pgindex.ReleaseTitleCandidate, error)

	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, releaseKey string, keepGroupNames []string) error
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
}

type Options struct {
	BatchSize            int
	ReleaseMinConfidence float64
	ReleaseMinCompletion float64
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
	skippedFragments := 0
	skippedConfidence := 0
	skippedCompletion := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, fragmentSkips, confidenceSkips, completionSkips, err := s.formCandidate(ctx, candidate)
		if err != nil {
			return fmt.Errorf("form release candidate %s: %w", candidate.ReleaseKey, err)
		}
		formed += count
		skippedFragments += fragmentSkips
		skippedConfidence += confidenceSkips
		skippedCompletion += completionSkips
	}

	s.log.Info(
		"release: formed=%d skipped_fragments=%d skipped_confidence=%d skipped_completion=%d batch_size=%d min_confidence=%.2f min_completion_pct=%.2f reform=%t",
		formed,
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

func (s *Service) formCandidate(ctx context.Context, candidate pgindex.ReleaseCandidate) (int, int, int, int, error) {
	binaries, err := s.repo.ListBinariesForReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.ReleaseKey)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("list binaries for release candidate: %w", err)
	}
	if len(binaries) == 0 {
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.ReleaseKey, nil); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("delete empty stale releases: %w", err)
		}
		return 0, 0, 0, 0, nil
	}

	clusters := clusterBinaries(candidate, binaries)
	keepGroupNames := make([]string, 0, len(clusters))
	formed := 0
	skippedFragments := 0
	skippedConfidence := 0
	skippedCompletion := 0

	for _, cluster := range clusters {
		if err := ctx.Err(); err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, err
		}

		titleCandidates, err := s.repo.ListReleaseTitleCandidates(ctx, binaryIDsForCluster(cluster.Binaries))
		if err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("list release title candidates for %s: %w", candidate.ReleaseKey, err)
		}

		record := buildReleaseRecord(candidate, cluster, titleCandidates)
		if !shouldPersistCluster(cluster) {
			skippedFragments++
			continue
		}
		if record.MatchConfidence < s.opts.ReleaseMinConfidence {
			skippedConfidence++
			continue
		}
		if record.CompletionPct < s.opts.ReleaseMinCompletion {
			skippedCompletion++
			continue
		}

		files, err := s.buildReleaseFiles(ctx, cluster)
		if err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("build release files for %s: %w", record.GroupName, err)
		}

		releaseID, err := s.repo.UpsertRelease(ctx, record)
		if err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("upsert release %s: %w", record.GroupName, err)
		}
		if err := s.repo.ReplaceReleaseFiles(ctx, releaseID, files); err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("replace release files for %s: %w", releaseID, err)
		}
		if err := s.repo.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{candidate.NewsgroupID}); err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("replace release newsgroups for %s: %w", releaseID, err)
		}
		if err := s.repo.UpsertNZBCache(ctx, releaseID, "pending", "", ""); err != nil {
			return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("upsert nzb cache for %s: %w", releaseID, err)
		}

		keepGroupNames = append(keepGroupNames, record.GroupName)
		formed++

		s.log.Debug(
			"release: release_id=%s group=%s title=%q files=%d confidence=%.2f completion_pct=%.2f availability_score=%.2f",
			releaseID,
			record.GroupName,
			record.Title,
			len(files),
			record.MatchConfidence,
			record.CompletionPct,
			record.AvailabilityScore,
		)
	}

	if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.ReleaseKey, keepGroupNames); err != nil {
		return formed, skippedFragments, skippedConfidence, skippedCompletion, fmt.Errorf("delete stale release groups for %s: %w", candidate.ReleaseKey, err)
	}

	return formed, skippedFragments, skippedConfidence, skippedCompletion, nil
}

func shouldPersistCluster(cluster releaseCluster) bool {
	mainPayloadCount := countMainPayloadBinaries(cluster.Binaries)
	if mainPayloadCount == 0 {
		return false
	}
	expectedFiles := clusterExpectedFileCount(cluster.Binaries)
	if expectedFiles > 1 && mainPayloadCount < 2 {
		return false
	}
	return true
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
