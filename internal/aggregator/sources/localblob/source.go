package localblob

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type store interface {
	SearchAggregatorReleaseCache(ctx context.Context, query string, limit int) ([]*domain.Release, error)
	GetNZBReader(id string) (io.ReadCloser, error)
}

type Source struct {
	store store
}

func New(store store) *Source {
	return &Source{store: store}
}

func (s *Source) Name() string {
	return "Local Store"
}

func (s *Source) Search(ctx context.Context, query string) ([]*domain.Release, error) {
	return s.store.SearchAggregatorReleaseCache(ctx, query, 100)
}

func (s *Source) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	return s.store.GetNZBReader(rel.ID)
}
