package aggregator

import (
	"context"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
)

type SearchType string

const (
	SearchTypeGeneric SearchType = "search"
	SearchTypeMovie   SearchType = "movie"
	SearchTypeTV      SearchType = "tvsearch"
)

type SearchRequest struct {
	Type SearchType

	Query string

	IMDbID   string
	TVDBID   string
	TVMazeID string
	RageID   string
	Season   string
	Episode  string
	Genre    string
}

type catalogSource interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) ([]*domain.Release, error)
	GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error)
}

type store interface {
	GetNZBReader(id string) (io.ReadCloser, error)
	SaveNZBAtomically(id string, data []byte) error
	Exists(id string) bool

	UpsertAggregatorReleaseCache(ctx context.Context, releases []*domain.Release) error
	SearchAggregatorReleaseCache(ctx context.Context, query string, limit int) ([]*domain.Release, error)
	GetAggregatorReleaseCacheByID(ctx context.Context, id string) (*domain.Release, error)
}

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}
