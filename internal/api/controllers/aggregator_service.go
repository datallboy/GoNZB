package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	aggregatormodule "github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

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

type aggregatorService interface {
	Search(ctx context.Context, req aggregatorSearchRequest) ([]*domain.Release, error)
	PrepareDownload(ctx context.Context, id string) (*app.AggregatorDownloadResult, error)
}

type runtimeAggregatorService struct {
	module app.AggregatorModule
}

func newAggregatorService(module app.AggregatorModule) aggregatorService {
	if module == nil {
		return &runtimeAggregatorService{}
	}

	return &runtimeAggregatorService{
		module: module,
	}
}

func (s *runtimeAggregatorService) Search(ctx context.Context, req aggregatorSearchRequest) ([]*domain.Release, error) {
	if s == nil || s.module == nil {
		return nil, aggregatormodule.ErrUnavailable
	}

	results, err := s.module.Search(ctx, app.SearchRequest{
		Type:     req.Type,
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

func (s *runtimeAggregatorService) PrepareDownload(ctx context.Context, id string) (*app.AggregatorDownloadResult, error) {
	if s == nil || s.module == nil {
		return nil, aggregatormodule.ErrUnavailable
	}
	return s.module.PrepareDownload(ctx, id)
}

func aggregatorErrorStatus(err error) int {
	switch {
	case errors.Is(err, aggregatormodule.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, aggregatormodule.ErrReleaseMissing):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
