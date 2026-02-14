package app

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/indexer"
	"github.com/datallboy/gonzb/internal/indexer/newsnab"
	storeIndexer "github.com/datallboy/gonzb/internal/indexer/store"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store"
)

type NNTPManager interface {
	// This allows the engine to call the manager without importing the nntp package
	Fetch(ctx context.Context, seg *domain.Segment, groups []string) (io.Reader, error)
	TotalCapacity() int
}

// Manager defines the contract for our NZB search and download engine.
type IndexerManager interface {
	SearchAll(ctx context.Context, query string) ([]*domain.Release, error)
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
	GetResultByID(ctx context.Context, id string) (*domain.Release, error)
}

type Processor interface {
	// This allows the engine to trigger repair/extract without importing processor
	Prepare(ctx context.Context, nzbModel *nzb.Model, nzbFilename string) (*domain.PreparationResult, error)
	Finalize(ctx context.Context, tasks []*domain.DownloadFile) error
	PostProcess(ctx context.Context, tasks []*domain.DownloadFile) error
}

type Downloader interface {
	// The engine's ability to process a specific item
	Download(ctx context.Context, item *domain.QueueItem) error
	RenderCLIProgress(item *domain.QueueItem, speedMbps float64, final bool)
	SetProgressHandler(fn func(*domain.QueueItem))
}

type QueueManager interface {
	Start(ctx context.Context)
	Add(ctx context.Context, releaseID string, title string) (*domain.QueueItem, error)
	GetActiveItem() *domain.QueueItem
	GetItem(ctx context.Context, id string) (*domain.QueueItem, bool)
	GetAllItems() []*domain.QueueItem
	Cancel(id string) bool
	HydrateItem(ctx context.Context, item *domain.QueueItem) error
	UpdateStatus(ctx context.Context, item *domain.QueueItem, status domain.JobStatus)
}

type NZBParser interface {
	ParseFile(nzbPath string) (*nzb.Model, error)
	Parse(r io.Reader) (*nzb.Model, error)
}

// Store defines the contract for NZB storage.
// Allows to use a simple directory FileCache, or Redis / DB / S3 for NZB storage in the future.
// Should be StoreManager similar to others, but we'll just use FileCache and keep it simple for now.
type Store interface {
	// Metadata: SQLLite
	UpsertReleases(ctx context.Context, results []*domain.Release) error
	GetRelease(ctx context.Context, id string) (*domain.Release, error)
	SearchReleases(ctx context.Context, query string) ([]*domain.Release, error)
	UpdateReleaseHash(ctx context.Context, id string, hash string) error
	GetReleaseByHash(ctx context.Context, hash string) (*domain.Release, error)

	// Downloader Queue: SQLite
	SaveQueueItem(ctx context.Context, item *domain.QueueItem) error
	GetQueueItem(ctx context.Context, id string) (*domain.QueueItem, error)
	GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error)
	GetActiveQueueItems(ctx context.Context) ([]*domain.QueueItem, error)
	ResetStuckQueueItems(ctx context.Context, newStatus domain.JobStatus, oldStatuses ...domain.JobStatus) error

	// release_files: SQLite
	SaveReleaseFiles(ctx context.Context, releaseID string, files []*domain.DownloadFile) error
	GetReleaseFiles(ctx context.Context, releaseID string) ([]*domain.DownloadFile, error)

	// Blobs: File System
	GetNZBReader(key string) (io.ReadCloser, error)
	CreateNZBWriter(key string) (io.WriteCloser, error)
	Exists(key string) bool

	Close() error
}

// Context hold the core environment and shared resources for GoNZB.
// It acts as the "Single Source of Truth" for the application state.
type Context struct {
	Config *config.Config
	Logger *logger.Logger

	// High-level interfaces for services to use
	NNTP       NNTPManager
	Indexer    IndexerManager
	Processor  Processor
	Downloader Downloader
	Queue      QueueManager
	NZBParser  NZBParser
	Store      Store

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
	idxManager := indexer.NewManager(store, log)

	// Always add the local store indexer
	idxManager.AddIndexer(storeIndexer.New(store))

	for _, idxCfg := range cfg.Indexers {
		client := newsnab.New(idxCfg.ID, idxCfg.BaseUrl, idxCfg.ApiKey, idxCfg.Redirect)
		idxManager.AddIndexer(client)
	}

	return &Context{
		Config:            cfg,
		Logger:            log,
		ExtractionEnabled: true,
		Indexer:           idxManager,
		Store:             store,
	}, nil
}

func (ctx *Context) Close() {
	ctx.Logger.Info("Shutting down store...")
	if err := ctx.Store.Close(); err != nil {
		ctx.Logger.Error("Error closing store: %v", err)
	}
}
