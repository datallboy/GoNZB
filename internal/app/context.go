package app

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/indexer"
	"github.com/datallboy/gonzb/internal/indexer/newsnab"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store"
)

type NNTPManager interface {
	// This allows the engine to call the manager without importing the nntp package
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
	TotalCapacity() int
}

// Manager defines the contract for our NZB search and download engine.
type IndexerManager interface {
	SearchAll(ctx context.Context, query string) ([]indexer.SearchResult, error)
	FetchNZB(ctx context.Context, id string) ([]byte, error)
	GetResultByID(ctx context.Context, id string) (indexer.SearchResult, error)
}

type Processor interface {
	// This allows the engine to trigger repair/extract without importing processor
	Prepare(nzbModel *nzb.Model, nzbFilename string) ([]*nzb.DownloadFile, error)
	Finalize(ctx context.Context, tasks []*nzb.DownloadFile) error
	PostProcess(ctx context.Context, tasks []*nzb.DownloadFile) error
}

// Store defines the contract for NZB storage.
// Allows to use a simple directory FileCache, or Redis / DB / S3 for NZB storage in the future.
// Should be StoreManager similar to others, but we'll just use FileCache and keep it simple for now.
type Store interface {
	// Metadata: SQLLite
	SaveReleases(ctx context.Context, results []indexer.SearchResult) error
	GetRelease(ctx context.Context, id string) (indexer.SearchResult, error)

	// Blobs: File System
	GetNZB(key string) ([]byte, error)
	PutNZB(key string, data []byte) error
	Exists(key string) bool
}

// Context hold the core environment and shared resources for GoNZB.
// It acts as the "Single Source of Truth" for the application state.
type Context struct {
	Config *config.Config
	Logger *logger.Logger

	// High-level interfaces for services to use
	NNTP      NNTPManager
	Indexer   IndexerManager
	Processor Processor
	NZBStore  Store

	ExtractionEnabled bool
}

// NewContext initializes the base environment.
func NewContext(cfg *config.Config, log *logger.Logger) (*Context, error) {
	// Initialize file cache for NZBs
	store, err := store.NewPersistentStore(cfg.Store.SQLitePath, cfg.Store.BlobDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	// Initialize Indexer Manager
	idxManager := indexer.NewManager(store)

	for _, idxCfg := range cfg.Indexers {
		client := newsnab.New(idxCfg.ID, idxCfg.BaseUrl, idxCfg.ApiKey)
		idxManager.AddIndexer(client)
	}

	return &Context{
		Config:            cfg,
		Logger:            log,
		ExtractionEnabled: true,
		Indexer:           idxManager,
		NZBStore:          store,
	}, nil
}
