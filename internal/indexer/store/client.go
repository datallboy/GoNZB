package store

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type storeClient interface {
	SearchReleases(ctx context.Context, query string) ([]*domain.Release, error)
	GetNZBReader(id string) (io.ReadCloser, error)
}

type StoreIndexer struct {
	store storeClient
}

func New(s storeClient) *StoreIndexer {
	return &StoreIndexer{
		store: s,
	}
}

func (i *StoreIndexer) Name() string {
	return "Local Store"
}

func (i *StoreIndexer) Search(ctx context.Context, query string) ([]*domain.Release, error) {
	return i.store.SearchReleases(ctx, query)
}

func (i *StoreIndexer) DownloadNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error) {
	return i.store.GetNZBReader(res.ID)
}
