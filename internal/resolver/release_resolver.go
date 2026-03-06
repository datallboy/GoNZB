package resolver

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type aggregator interface {
	GetResultByID(ctx context.Context, id string) (*domain.Release, error)
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
}

type DefaultReleaseResolver struct {
	Aggregator aggregator
}

func (r *DefaultReleaseResolver) GetRelease(ctx context.Context, id string) (*domain.Release, error) {
	if r.Aggregator == nil {
		return nil, fmt.Errorf("indexer aggregator is not configured")
	}
	return r.Aggregator.GetResultByID(ctx, id)
}

func (r *DefaultReleaseResolver) GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error) {
	if r.Aggregator == nil {
		return nil, fmt.Errorf("indexer aggregator is not configured")
	}
	return r.Aggregator.GetNZB(ctx, res)
}
