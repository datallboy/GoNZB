package localblob

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/aggregator"
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

func (s *Source) Search(ctx context.Context, req aggregator.SearchRequest) ([]*domain.Release, error) {
	query := req.Query
	if query == "" {
		return []*domain.Release{}, nil
	}

	items, err := s.store.SearchAggregatorReleaseCache(ctx, query, 100)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Source) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	return s.store.GetNZBReader(rel.ID)
}
