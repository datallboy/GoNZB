package adapters

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type blob interface {
	GetNZBReader(key string) (io.ReadCloser, error)
	SaveNZBAtomically(key string, data []byte) error
	Exists(key string) bool
}

type aggregatorCache interface {
	UpsertAggregatorReleaseCache(ctx context.Context, releases []*domain.Release) error
	SearchAggregatorReleaseCache(ctx context.Context, query string, limit int) ([]*domain.Release, error)
	GetAggregatorReleaseCacheByID(ctx context.Context, id string) (*domain.Release, error)
}

type AggregatorStore struct {
	blob  blob
	cache aggregatorCache
}

func NewAggregatorStore(blob blob, cache aggregatorCache) *AggregatorStore {
	return &AggregatorStore{
		blob:  blob,
		cache: cache,
	}
}

func (s *AggregatorStore) GetNZBReader(id string) (io.ReadCloser, error) {
	return s.blob.GetNZBReader(id)
}

func (s *AggregatorStore) SaveNZBAtomically(id string, data []byte) error {
	return s.blob.SaveNZBAtomically(id, data)
}

func (s *AggregatorStore) Exists(id string) bool {
	return s.blob.Exists(id)
}

func (s *AggregatorStore) UpsertAggregatorReleaseCache(ctx context.Context, releases []*domain.Release) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.UpsertAggregatorReleaseCache(ctx, releases)
}

func (s *AggregatorStore) SearchAggregatorReleaseCache(ctx context.Context, query string, limit int) ([]*domain.Release, error) {
	if s.cache == nil {
		return []*domain.Release{}, nil
	}
	return s.cache.SearchAggregatorReleaseCache(ctx, query, limit)
}

func (s *AggregatorStore) GetAggregatorReleaseCacheByID(ctx context.Context, id string) (*domain.Release, error) {
	if s.cache == nil {
		return nil, nil
	}
	return s.cache.GetAggregatorReleaseCacheByID(ctx, id)
}
