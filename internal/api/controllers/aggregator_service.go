package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	aggregatorpkg "github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

var (
	errAggregatorUnavailable = errors.New("aggregator runtime is unavailable")
	errReleaseNotFound       = errors.New("release not found")
)

type aggregatorLogger interface {
	Error(format string, args ...any)
}

type aggregatorSearchRequest struct {
	Type     string
	Query    string
	IMDbID   string
	TVDBID   string
	TVMazeID string
	RageID   string
	Season   string
	Episode  string
	Genre    string
}

type aggregatorDownloadResult struct {
	Release     *domain.Release
	Reader      io.ReadCloser
	RedirectURL string
}

type aggregatorService interface {
	Search(ctx context.Context, req aggregatorSearchRequest) ([]*domain.Release, error)
	PrepareDownload(ctx context.Context, id string) (*aggregatorDownloadResult, error)
}

type runtimeAggregatorService struct {
	aggregator app.IndexerAggregator
	blobStore  app.BlobStore
	logger     aggregatorLogger
}

func newAggregatorService(appCtx *app.Context) aggregatorService {
	if appCtx == nil {
		return &runtimeAggregatorService{}
	}

	return &runtimeAggregatorService{
		aggregator: appCtx.Aggregator,
		blobStore:  appCtx.BlobStore,
		logger:     appCtx.Logger,
	}
}

func (s *runtimeAggregatorService) Search(ctx context.Context, req aggregatorSearchRequest) ([]*domain.Release, error) {
	if s == nil || s.aggregator == nil {
		return nil, errAggregatorUnavailable
	}

	searchType := aggregatorpkg.SearchType(req.Type)
	if searchType == "" {
		searchType = aggregatorpkg.SearchTypeGeneric
	}

	results, err := s.aggregator.SearchAllWithRequest(ctx, aggregatorpkg.SearchRequest{
		Type:     searchType,
		Query:    req.Query,
		IMDbID:   req.IMDbID,
		TVDBID:   req.TVDBID,
		TVMazeID: req.TVMazeID,
		RageID:   req.RageID,
		Season:   req.Season,
		Episode:  req.Episode,
		Genre:    req.Genre,
	})
	if err != nil {
		return nil, fmt.Errorf("search releases: %w", err)
	}

	return results, nil
}

func (s *runtimeAggregatorService) PrepareDownload(ctx context.Context, id string) (*aggregatorDownloadResult, error) {
	if s == nil || s.aggregator == nil {
		return nil, errAggregatorUnavailable
	}

	res, err := s.aggregator.GetResultByID(ctx, id)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Failed release lookup for id %s: %v", id, err)
		}
		return nil, fmt.Errorf("lookup release: %w", err)
	}
	if res == nil {
		return nil, errReleaseNotFound
	}

	if res.RedirectAllowed && (s.blobStore == nil || !s.blobStore.Exists(res.ID)) {
		return &aggregatorDownloadResult{
			Release:     res,
			RedirectURL: res.DownloadURL,
		}, nil
	}

	reader, err := s.aggregator.GetNZB(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("fetch nzb: %w", err)
	}

	return &aggregatorDownloadResult{
		Release: res,
		Reader:  reader,
	}, nil
}

func aggregatorErrorStatus(err error) int {
	switch {
	case errors.Is(err, errAggregatorUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, errReleaseNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
