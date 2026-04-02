package release

import (
	"context"
	"fmt"

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
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)

	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, releaseKey string, keepGroupNames []string) error
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
}

type Options struct {
	BatchSize            int
	ReleaseMinConfidence float64
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

	return &Service{
		repo: repo,
		log:  log,
		opts: opts,
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("release repo is required")
	}

	candidates, err := s.repo.ListReleaseCandidates(ctx, s.opts.BatchSize)
	if err != nil {
		return fmt.Errorf("list release candidates: %w", err)
	}
	if len(candidates) == 0 {
		s.log.Debug("release: no release candidates found")
		return nil
	}

	formed := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := s.formCandidate(ctx, candidate)
		if err != nil {
			return fmt.Errorf("form release candidate %s: %w", candidate.ReleaseKey, err)
		}
		formed += count
	}

	s.log.Info("release: formed=%d batch_size=%d min_confidence=%.2f", formed, s.opts.BatchSize, s.opts.ReleaseMinConfidence)
	return nil
}

func (s *Service) formCandidate(ctx context.Context, candidate pgindex.ReleaseCandidate) (int, error) {
	binaries, err := s.repo.ListBinariesForReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.ReleaseKey)
	if err != nil {
		return 0, fmt.Errorf("list binaries for release candidate: %w", err)
	}
	if len(binaries) == 0 {
		if err := s.repo.DeleteStaleReleasesForSourceKey(ctx, candidate.ProviderID, candidate.ReleaseKey, nil); err != nil {
			return 0, fmt.Errorf("delete empty stale releases: %w", err)
		}
		return 0, nil
	}

	clusters := clusterBinaries(candidate, binaries)
	keepGroupNames := make([]string, 0, len(clusters))
	formed := 0

	for _, cluster := range clusters {
		if err := ctx.Err(); err != nil {
			return formed, err
		}

		record := buildReleaseRecord(candidate, cluster)
		if record.MatchConfidence < s.opts.ReleaseMinConfidence {
			s.log.Debug(
				"release: skipped group=%s source_release_key=%s confidence=%.2f threshold=%.2f binaries=%d",
				record.GroupName,
				candidate.ReleaseKey,
				record.MatchConfidence,
				s.opts.ReleaseMinConfidence,
				len(cluster.Binaries),
			)
			continue
		}

		files, err := s.buildReleaseFiles(ctx, cluster)
		if err != nil {
			return formed, fmt.Errorf("build release files for %s: %w", record.GroupName, err)
		}

		releaseID, err := s.repo.UpsertRelease(ctx, record)
		if err != nil {
			return formed, fmt.Errorf("upsert release %s: %w", record.GroupName, err)
		}
		if err := s.repo.ReplaceReleaseFiles(ctx, releaseID, files); err != nil {
			return formed, fmt.Errorf("replace release files for %s: %w", releaseID, err)
		}
		if err := s.repo.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{candidate.NewsgroupID}); err != nil {
			return formed, fmt.Errorf("replace release newsgroups for %s: %w", releaseID, err)
		}
		if err := s.repo.UpsertNZBCache(ctx, releaseID, "pending", "", ""); err != nil {
			return formed, fmt.Errorf("upsert nzb cache for %s: %w", releaseID, err)
		}

		keepGroupNames = append(keepGroupNames, record.GroupName)
		formed++

		s.log.Info(
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
		return formed, fmt.Errorf("delete stale release groups for %s: %w", candidate.ReleaseKey, err)
	}

	return formed, nil
}

func (s *Service) buildReleaseFiles(ctx context.Context, cluster releaseCluster) ([]pgindex.ReleaseFileRecord, error) {
	files := make([]pgindex.ReleaseFileRecord, 0, len(cluster.Binaries))

	for idx, binary := range cluster.Binaries {
		articles, err := s.repo.ListBinaryPartArticles(ctx, binary.BinaryID)
		if err != nil {
			return nil, fmt.Errorf("list binary part articles %d: %w", binary.BinaryID, err)
		}

		fileName := pickFileName(binary)
		files = append(files, pgindex.ReleaseFileRecord{
			BinaryID:  binary.BinaryID,
			FileName:  fileName,
			SizeBytes: binary.TotalBytes,
			FileIndex: idx,
			IsPars:    isParFile(fileName),
			Subject:   binary.BinaryName,
			Poster:    binary.Poster,
			PostedAt:  binary.PostedAt,
			Articles:  articles,
		})
	}

	return files, nil
}
