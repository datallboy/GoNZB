package indexer

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

// Indexer is the contract any source (Newznab, Local, Scraper) must fulfill
type Indexer interface {
	Name() string
	Search(ctx context.Context, query string) ([]*domain.Release, error)
	DownloadNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
}
