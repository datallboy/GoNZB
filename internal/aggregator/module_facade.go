package aggregator

import (
	"context"
	"errors"
	"fmt"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

var (
	ErrUnavailable    = errors.New("aggregator runtime is unavailable")
	ErrReleaseMissing = errors.New("release not found")
)

type Logger interface {
	Error(format string, args ...any)
}

type DependencyProvider struct {
	Aggregator func() app.IndexerAggregator
	BlobStore  func() app.BlobStore
	Logger     func() Logger
}

type Module struct {
	provider DependencyProvider
}

func NewModule(provider DependencyProvider) *Module {
	return &Module{provider: provider}
}

func (m *Module) Search(ctx context.Context, req app.SearchRequest) ([]*domain.Release, error) {
	aggregator := m.provider.Aggregator()
	if aggregator == nil {
		return nil, ErrUnavailable
	}

	results, err := aggregator.SearchAllWithRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search releases: %w", err)
	}

	return results, nil
}

func (m *Module) PrepareDownload(ctx context.Context, id string) (*app.AggregatorDownloadResult, error) {
	aggregator := m.provider.Aggregator()
	if aggregator == nil {
		return nil, ErrUnavailable
	}

	res, err := aggregator.GetResultByID(ctx, id)
	if err != nil {
		if log := m.provider.Logger(); log != nil {
			log.Error("Failed release lookup for id %s: %v", id, err)
		}
		return nil, fmt.Errorf("lookup release: %w", err)
	}
	if res == nil {
		return nil, ErrReleaseMissing
	}

	blobStore := m.provider.BlobStore()
	if res.RedirectAllowed && (blobStore == nil || !blobStore.Exists(res.ID)) {
		return &app.AggregatorDownloadResult{
			Release:     res,
			RedirectURL: res.DownloadURL,
		}, nil
	}

	reader, err := aggregator.GetNZB(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("fetch nzb: %w", err)
	}

	return &app.AggregatorDownloadResult{
		Release: res,
		Reader:  reader,
	}, nil
}
