package adapters

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type catalog interface {
	UpsertReleases(ctx context.Context, results []*domain.Release) error
	GetRelease(ctx context.Context, id string) (*domain.Release, error)
	SearchReleases(ctx context.Context, query string) ([]*domain.Release, error)
}

type blob interface {
	GetNZBReader(key string) (io.ReadCloser, error)
	SaveNZBAtomically(key string, data []byte) error
	Exists(key string) bool
}

type AggregatorStore struct {
	catalog catalog
	blob    blob
}

func NewAggregatorStore(catalog catalog, blob blob) *AggregatorStore {
	return &AggregatorStore{
		catalog: catalog,
		blob:    blob,
	}
}

func (s *AggregatorStore) UpsertReleases(ctx context.Context, results []*domain.Release) error {
	return s.catalog.UpsertReleases(ctx, results)
}

func (s *AggregatorStore) GetRelease(ctx context.Context, id string) (*domain.Release, error) {
	return s.catalog.GetRelease(ctx, id)
}

func (s *AggregatorStore) SearchReleases(ctx context.Context, query string) ([]*domain.Release, error) {
	return s.catalog.SearchReleases(ctx, query)
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
