package resolver

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

type aggregatorSource interface {
	GetResultByID(ctx context.Context, id string) (*domain.Release, error)
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
}

type aggregatorResolver struct {
	aggregator aggregatorSource
}

func NewAggregatorResolver(aggregator aggregatorSource) *aggregatorResolver {
	return &aggregatorResolver{aggregator: aggregator}
}

func (r *aggregatorResolver) GetRelease(ctx context.Context, sourceReleaseID string) (*domain.Release, error) {
	if r.aggregator == nil {
		return nil, fmt.Errorf("aggregator is not configured")
	}

	sourceReleaseID = strings.TrimSpace(sourceReleaseID)
	if sourceReleaseID == "" {
		return nil, fmt.Errorf("source release id is required")
	}

	return r.aggregator.GetResultByID(ctx, sourceReleaseID)
}

func (r *aggregatorResolver) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	if r.aggregator == nil {
		return nil, fmt.Errorf("aggregator is not configured")
	}
	if rel == nil {
		return nil, fmt.Errorf("aggregator release is required")
	}

	return r.aggregator.GetNZB(ctx, rel)
}
