package assemble

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/indexing/match"
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
	EnsurePoster(ctx context.Context, posterName string) (int64, error)
	LinkArticlePoster(ctx context.Context, articleHeaderID, posterID int64) error
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaryPart(ctx context.Context, in pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error
}

// narrow matcher dependency.
type subjectMatcher interface {
	MatchSubject(subject, messageID string) match.Result
}

type Options struct {
	BatchSize int
}

type Service struct {
	repo    repository
	matcher subjectMatcher
	log     logger
	opts    Options
}

func NewService(repo repository, matcher subjectMatcher, log logger, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}

	return &Service{
		repo:    repo,
		matcher: matcher,
		log:     log,
		opts:    opts,
	}
}

// RunOnce assembles one batch of article headers into binaries + binary_parts.
func (s *Service) RunOnce(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("assembly repo is required")
	}
	if s.matcher == nil {
		return fmt.Errorf("assembly matcher is required")
	}

	headers, err := s.repo.ListUnassembledArticleHeaders(ctx, s.opts.BatchSize)
	if err != nil {
		return fmt.Errorf("list unassembled article headers: %w", err)
	}
	if len(headers) == 0 {
		s.log.Debug("assemble: no unassembled article headers found")
		return nil
	}

	refreshed := make(map[int64]struct{}, len(headers))
	assembledCount := 0

	for _, header := range headers {
		if err := ctx.Err(); err != nil {
			return err
		}

		matched := s.matcher.MatchSubject(header.Subject, header.MessageID)

		posterID, err := s.repo.EnsurePoster(ctx, header.Poster)
		if err != nil {
			return fmt.Errorf("ensure poster for article %d: %w", header.ID, err)
		}

		if err := s.repo.LinkArticlePoster(ctx, header.ID, posterID); err != nil {
			return fmt.Errorf("link poster for article %d: %w", header.ID, err)
		}

		binaryID, err := s.repo.UpsertBinary(ctx, pgindex.BinaryRecord{
			ProviderID:  header.ProviderID,
			NewsgroupID: header.NewsgroupID,
			PosterID:    posterID,
			ReleaseKey:  matched.ReleaseKey,
			ReleaseName: matched.ReleaseName,
			BinaryKey:   matched.BinaryKey,
			BinaryName:  matched.BinaryName,
			FileName:    matched.FileName,
			TotalParts:  matched.TotalParts,
			PostedAt:    header.DateUTC,
		})
		if err != nil {
			return fmt.Errorf("upsert binary for article %d: %w", header.ID, err)
		}

		if err := s.repo.UpsertBinaryPart(ctx, pgindex.BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: header.ID,
			MessageID:       header.MessageID,
			PartNumber:      matched.PartNumber,
			TotalParts:      matched.TotalParts,
			SegmentBytes:    header.Bytes,
			FileName:        matched.FileName,
		}); err != nil {
			return fmt.Errorf("upsert binary part for article %d: %w", header.ID, err)
		}

		refreshed[binaryID] = struct{}{}
		assembledCount++
	}

	for binaryID := range refreshed {
		if err := s.repo.RefreshBinaryStats(ctx, binaryID); err != nil {
			return fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
		}
	}

	s.log.Info(
		"assemble: processed_headers=%d binaries_refreshed=%d batch_size=%d",
		assembledCount,
		len(refreshed),
		s.opts.BatchSize,
	)

	return nil
}
