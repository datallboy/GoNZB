package releasegenerate

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type repository interface {
	ListReleaseNZBGenerateCandidates(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) ([]pgindex.ReleaseNZBGenerateCandidate, error)
}

type nzbResolver interface {
	GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error)
}

type Options struct {
	BatchSize int
	Policy    pgindex.ReleaseReadyPolicy
}

type Service struct {
	repo     repository
	resolver nzbResolver
	opts     Options
}

func NewService(repo repository, resolver nzbResolver, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	opts.Policy = pgindex.NormalizeReleaseReadyPolicy(opts.Policy)
	return &Service{repo: repo, resolver: resolver, opts: opts}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil || s.resolver == nil {
		return nil, fmt.Errorf("generate service dependencies are required")
	}

	candidates, err := s.repo.ListReleaseNZBGenerateCandidates(ctx, s.opts.BatchSize, s.opts.Policy)
	if err != nil {
		return nil, err
	}

	metrics := map[string]any{
		"generate_candidates":   len(candidates),
		"generate_attempted":    0,
		"generated_ready_count": 0,
		"generate_failures":     0,
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return metrics, err
		}
		metrics["generate_attempted"] = metrics["generate_attempted"].(int) + 1
		reader, err := s.resolver.GetNZB(ctx, &domain.Release{
			ID:          candidate.ReleaseID,
			Title:       candidate.Title,
			GUID:        candidate.ReleaseID,
			Source:      "usenet_index",
			PublishDate: time.Now().UTC(),
		})
		if err != nil {
			metrics["generate_failures"] = metrics["generate_failures"].(int) + 1
			continue
		}
		_ = reader.Close()
		metrics["generated_ready_count"] = metrics["generated_ready_count"].(int) + 1
	}

	return metrics, nil
}
