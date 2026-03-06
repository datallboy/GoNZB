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
	"github.com/datallboy/gonzb/internal/resolver"
	"github.com/datallboy/gonzb/internal/store"
	"github.com/datallboy/gonzb/internal/store/adapters"
	blobstore "github.com/datallboy/gonzb/internal/store/blob"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
	"github.com/datallboy/gonzb/internal/store/sqlitejob"
)

type NNTPManager interface {
	// This allows the engine to call the manager without importing the nntp package
	Fetch(ctx context.Context, seg *domain.Segment, groups []string) (io.Reader, error)
	TotalCapacity() int
}

// Manager defines the contract for our NZB search and download engine.
type IndexerAggregator interface {
	SearchAll(ctx context.Context, query string) ([]*domain.Release, error)
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
	GetResultByID(ctx context.Context, id string) (*domain.Release, error)
}

type ReleaseResolver interface {
	UpsertReleases(ctx context.Context, results []*domain.Release) error
	GetRelease(ctx context.Context, id string) (*domain.Release, error)
	SearchReleases(ctx context.Context, query string) ([]*domain.Release, error)
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
}

type UsenetIndexerService interface{}

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
	Delete(id string) bool
	HydrateItem(ctx context.Context, item *domain.QueueItem) error
	UpdateStatus(ctx context.Context, item *domain.QueueItem, status domain.JobStatus)
}

type NZBParser interface {
	ParseFile(nzbPath string) (*nzb.Model, error)
	Parse(r io.Reader) (*nzb.Model, error)
}

// JobStore defines downloader queue/event/history persistence.
type JobStore interface {
	// Downloader Queue: SQLite
	SaveQueueItem(ctx context.Context, item *domain.QueueItem) error
	GetQueueItem(ctx context.Context, id string) (*domain.QueueItem, error)
	GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error)
	GetActiveQueueItems(ctx context.Context) ([]*domain.QueueItem, error)
	DeleteQueueItems(ctx context.Context, ids []string) (int64, error)
	ClearQueueHistory(ctx context.Context, statuses []domain.JobStatus) (int64, error)
	SaveQueueEvent(ctx context.Context, ev *domain.QueueItemEvent) error
	GetQueueEvents(ctx context.Context, queueID string) ([]*domain.QueueItemEvent, error)
	ResetStuckQueueItems(ctx context.Context, newStatus domain.JobStatus, oldStatuses ...domain.JobStatus) error
}

// downloader-owned queue item file metadata
type QueueFileStore interface {
	SaveQueueItemFiles(ctx context.Context, queueItemID string, files []*domain.DownloadFile) error
	GetQueueItemFiles(ctx context.Context, queueItemID string) ([]*domain.DownloadFile, error)
}

type BlobStore interface {
	// Blobs: File System
	GetNZBReader(key string) (io.ReadCloser, error)
	CreateNZBWriter(key string) (io.WriteCloser, error)
	SaveNZBAtomically(key string, data []byte) error
	Exists(key string) bool
}

type PayloadFetcher interface {
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
}

type PayloadCacheStore interface {
	GetNZBReader(key string) (io.ReadCloser, error)
	CreateNZBWriter(key string) (io.WriteCloser, error)
	SaveNZBAtomically(key string, data []byte) error
	Exists(key string) bool
}

// Runtime settings
type SettingsStore interface {
	LoadEffectiveSettings(ctx context.Context) error
	UpdateSettings(ctx context.Context, patch any) error
	WatchSettingsChanges(ctx context.Context) (<-chan struct{}, error)
}

// Context hold the core environment and shared resources for GoNZB.
// It acts as the "Single Source of Truth" for the application state.
type Context struct {
	Config *config.Config
	Logger *logger.Logger

	// High-level interfaces for services to use
	NNTP              NNTPManager
	Aggregator        IndexerAggregator
	Resolver          ReleaseResolver
	UsenetIndexer     UsenetIndexerService
	Processor         Processor
	Downloader        Downloader
	Queue             QueueManager
	NZBParser         NZBParser
	JobStore          JobStore
	QueueFileStore    QueueFileStore
	BlobStore         BlobStore
	PayloadFetcher    PayloadFetcher
	PayloadCacheStore PayloadCacheStore
	SettingsStore     SettingsStore

	ExtractionEnabled bool
	closers           []io.Closer
}

// NewContext initializes the base environment.
func NewContext(cfg *config.Config, log *logger.Logger) (*Context, error) {
	// Initialize aggregator catalog store
	catalogStore, err := store.NewPersistentStore(cfg.Store.SQLitePath, cfg.Store.BlobDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	jobStore, err := sqlitejob.NewStore(cfg.Store.SQLitePath, cfg.Store.BlobDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize sqlite job store: %w", err)
	}

	var payloadStore PayloadCacheStore
	if cfg.Store.PayloadCacheEnabled {
		fsStore, err := blobstore.NewFSBlobStore(cfg.Store.BlobDir, jobStore)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize fs blob store: %w", err)
		}
		payloadStore = fsStore
	} else {
		payloadStore = blobstore.NewEphemeralBlobStore()
	}

	settingsStore, err := settingsstore.NewStore(cfg.Store.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize settings store: %w", err)
	}

	aggregatorStore := adapters.NewAggregatorStore(catalogStore, payloadStore)

	// Initialize Indexer Manager
	aggregator := indexer.NewManager(aggregatorStore, log)

	// Always add the local store indexer
	aggregator.AddIndexer(storeIndexer.New(aggregatorStore))

	for _, idxCfg := range cfg.Indexers {
		client := newsnab.New(idxCfg.ID, idxCfg.BaseUrl, idxCfg.ApiPath, idxCfg.ApiKey, idxCfg.Redirect)
		aggregator.AddIndexer(client)
	}

	releaseResolver := &resolver.DefaultReleaseResolver{
		Catalog:    catalogStore,
		Aggregator: aggregator,
	}

	return &Context{
		Config:            cfg,
		Logger:            log,
		ExtractionEnabled: true,
		Aggregator:        aggregator,
		Resolver:          releaseResolver,
		JobStore:          jobStore,
		QueueFileStore:    jobStore,
		BlobStore:         payloadStore,
		PayloadFetcher:    releaseResolver,
		PayloadCacheStore: payloadStore,
		SettingsStore:     settingsStore,
		closers:           []io.Closer{catalogStore, jobStore, settingsStore},
	}, nil
}

func (ctx *Context) Close() {
	for _, c := range ctx.closers {
		if c == nil {
			continue
		}

		if err := c.Close(); err != nil {
			ctx.Logger.Error("Error closing resource: %v", err)
		}
	}
}
