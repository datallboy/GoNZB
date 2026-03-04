package resolver

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type releaseCatalog interface {
	UpsertReleases(ctx context.Context, results []*domain.Release) error
	GetRelease(ctx context.Context, id string) (*domain.Release, error)
	SearchReleases(ctx context.Context, query string) ([]*domain.Release, error)
}

type aggregator interface {
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
}

type DefaultReleaseResolver struct {
	Catalog    releaseCatalog
	Aggregator aggregator
}

func (r *DefaultReleaseResolver) UpsertReleases(ctx context.Context, results []*domain.Release) error {
	return r.Catalog.UpsertReleases(ctx, results)
}

func (r *DefaultReleaseResolver) GetRelease(ctx context.Context, id string) (*domain.Release, error) {
	return r.Catalog.GetRelease(ctx, id)
}

func (r *DefaultReleaseResolver) SearchReleases(ctx context.Context, query string) ([]*domain.Release, error) {
	return r.Catalog.SearchReleases(ctx, query)
}

func (r *DefaultReleaseResolver) GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error) {
	if r.Aggregator == nil {
		return nil, fmt.Errorf("indexer aggregator is not configured")
	}
	return r.Aggregator.GetNZB(ctx, res)
}
