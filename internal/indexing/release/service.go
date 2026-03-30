package release

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
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

// private repository boundary for Milestone 6 release formation.
type repository interface {
	ListReleaseCandidates(ctx context.Context, limit int) ([]pgindex.ReleaseCandidate, error)
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)

	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
}

type Options struct {
	BatchSize int
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

	return &Service{
		repo: repo,
		log:  log,
		opts: opts,
	}
}

// RunOnce forms one batch of PG releases from assembled binaries.
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
		if err := s.formRelease(ctx, candidate); err != nil {
			return fmt.Errorf("form release %s: %w", candidate.ReleaseKey, err)
		}
		formed++
	}

	s.log.Info("release: formed=%d batch_size=%d", formed, s.opts.BatchSize)
	return nil
}

func (s *Service) formRelease(ctx context.Context, candidate pgindex.ReleaseCandidate) error {
	binaries, err := s.repo.ListBinariesForReleaseCandidate(
		ctx,
		candidate.ProviderID,
		candidate.NewsgroupID,
		candidate.ReleaseKey,
	)
	if err != nil {
		return fmt.Errorf("list binaries for release candidate: %w", err)
	}
	if len(binaries) == 0 {
		return nil
	}

	title := bestReleaseTitle(candidate, binaries)
	poster := bestPoster(binaries)
	postedAt := earliestPostedAt(candidate.PostedAt, binaries)

	files := make([]pgindex.ReleaseFileRecord, 0, len(binaries))
	totalBytes := int64(0)
	parCount := 0
	totalObservedParts := 0
	totalExpectedParts := 0

	sort.SliceStable(binaries, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(binaries[i].FileName))
		right := strings.ToLower(strings.TrimSpace(binaries[j].FileName))
		if left == right {
			return binaries[i].BinaryID < binaries[j].BinaryID
		}
		return left < right
	})

	for idx, binary := range binaries {
		articles, err := s.repo.ListBinaryPartArticles(ctx, binary.BinaryID)
		if err != nil {
			return fmt.Errorf("list binary part articles %d: %w", binary.BinaryID, err)
		}

		fileName := pickFileName(binary)
		isPars := isParFile(fileName)

		files = append(files, pgindex.ReleaseFileRecord{
			BinaryID:  binary.BinaryID,
			FileName:  fileName,
			SizeBytes: binary.TotalBytes,
			FileIndex: idx,
			IsPars:    isPars,
			Subject:   binary.BinaryName,
			Poster:    binary.Poster,
			PostedAt:  binary.PostedAt,
			Articles:  articles,
		})

		totalBytes += binary.TotalBytes
		totalObservedParts += binary.ObservedParts
		totalExpectedParts += max(binary.TotalParts, binary.ObservedParts)
		if isPars {
			parCount++
		}
	}

	completionPct := 100.0
	if totalExpectedParts > 0 {
		completionPct = (float64(totalObservedParts) / float64(totalExpectedParts)) * 100.0
		if completionPct > 100.0 {
			completionPct = 100.0
		}
	}

	releaseID, err := s.repo.UpsertRelease(ctx, pgindex.ReleaseRecord{
		ProviderID:    candidate.ProviderID,
		ReleaseKey:    candidate.ReleaseKey,
		Title:         title,
		SearchTitle:   normalizeSearchTitle(title),
		Category:      "usenet",
		Poster:        poster,
		SizeBytes:     totalBytes,
		PostedAt:      postedAt,
		FileCount:     len(files),
		ParFileCount:  parCount,
		CompletionPct: completionPct,
	})
	if err != nil {
		return fmt.Errorf("upsert release: %w", err)
	}

	if err := s.repo.ReplaceReleaseFiles(ctx, releaseID, files); err != nil {
		return fmt.Errorf("replace release files: %w", err)
	}

	if err := s.repo.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{candidate.NewsgroupID}); err != nil {
		return fmt.Errorf("replace release newsgroups: %w", err)
	}

	// Milestone 6 only seeds metadata; actual NZB generation comes later.
	if err := s.repo.UpsertNZBCache(ctx, releaseID, "pending", "", ""); err != nil {
		return fmt.Errorf("upsert nzb cache: %w", err)
	}

	s.log.Info(
		"release: release_id=%s title=%q files=%d completion_pct=%.2f",
		releaseID,
		title,
		len(files),
		completionPct,
	)

	return nil
}

func bestReleaseTitle(candidate pgindex.ReleaseCandidate, binaries []pgindex.BinarySummary) string {
	title := strings.TrimSpace(candidate.ReleaseName)
	if title != "" {
		return title
	}

	for _, binary := range binaries {
		if v := strings.TrimSpace(binary.ReleaseName); v != "" {
			return v
		}
	}

	title = strings.TrimSpace(candidate.ReleaseKey)
	if title != "" {
		return title
	}

	return "unknown-release"
}

func bestPoster(binaries []pgindex.BinarySummary) string {
	counts := make(map[string]int)
	best := ""
	bestCount := 0

	for _, binary := range binaries {
		poster := strings.TrimSpace(binary.Poster)
		if poster == "" {
			continue
		}
		counts[poster]++
		if counts[poster] > bestCount {
			best = poster
			bestCount = counts[poster]
		}
	}

	return best
}

func earliestPostedAt(candidate *time.Time, binaries []pgindex.BinarySummary) *time.Time {
	var best *time.Time

	if candidate != nil {
		t := candidate.UTC()
		best = &t
	}

	for _, binary := range binaries {
		if binary.PostedAt == nil {
			continue
		}
		t := binary.PostedAt.UTC()
		if best == nil || t.Before(*best) {
			best = &t
		}
	}

	return best
}

func pickFileName(binary pgindex.BinarySummary) string {
	name := strings.TrimSpace(binary.FileName)
	if name != "" {
		return name
	}

	name = strings.TrimSpace(binary.BinaryName)
	if name != "" {
		return name
	}

	name = strings.TrimSpace(binary.ReleaseName)
	if name == "" {
		name = strings.TrimSpace(binary.ReleaseKey)
	}
	if name == "" {
		name = fmt.Sprintf("binary-%d.bin", binary.BinaryID)
	}

	if filepath.Ext(name) == "" {
		name += ".bin"
	}
	return name
}

func isParFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".par2") || strings.Contains(lower, ".vol")
}

func normalizeSearchTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = strings.ReplaceAll(v, "-", " ")
	v = strings.Join(strings.Fields(v), " ")
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
