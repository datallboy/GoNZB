package app

import (
	"context"
	"io"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type DownloaderCommands interface {
	EnqueueByReleaseID(ctx context.Context, releaseID, title string) (*domain.QueueItem, error)
	EnqueueNZB(ctx context.Context, filename string, file io.Reader) (*domain.QueueItem, error)
	EnqueueNZBWithCategory(ctx context.Context, filename, category string, file io.Reader) (*domain.QueueItem, error)
	Cancel(id string) bool
	CancelMany(ids []string) int
	DeleteMany(ctx context.Context, ids []string) (int64, error)
	ClearHistory(ctx context.Context) (int64, error)
	Pause() bool
	Resume() bool
}

type DownloaderQueries interface {
	ListActive() []*domain.QueueItem
	ListHistory(ctx context.Context, status string, limit, offset int) ([]*domain.QueueItem, int, error)
	GetActiveItem() *domain.QueueItem
	GetItem(ctx context.Context, id string) (*domain.QueueItem, error)
	GetItemFiles(ctx context.Context, id string) ([]*domain.DownloadFile, error)
	GetItemEvents(ctx context.Context, id string) ([]*domain.QueueItemEvent, error)
	IsPaused() bool
}

type DownloaderModule interface {
	Commands() DownloaderCommands
	Queries() DownloaderQueries
}

type AggregatorDownloadResult struct {
	Release     *domain.Release
	Reader      io.ReadCloser
	RedirectURL string
}

type AggregatorModule interface {
	Search(ctx context.Context, req SearchRequest) ([]*domain.Release, error)
	PrepareDownload(ctx context.Context, id string) (*AggregatorDownloadResult, error)
}

type SettingsAdmin interface {
	Get(ctx context.Context) (*RuntimeSettings, error)
	Update(ctx context.Context, patch *RuntimeSettingsPatch) (*RuntimeSettings, error)
}

type RuntimeCheck struct {
	Name   string
	OK     bool
	Detail string
}

type RuntimeModule interface {
	Name() string
	Enabled() bool
	Build(ctx context.Context) error
	Start(ctx context.Context) error
	Reload(ctx context.Context) error
	Close() error
	ReadinessChecks(ctx context.Context) []RuntimeCheck
}

type NNTPManager interface {
	// This allows the engine to call the manager without importing the nntp package
	Fetch(ctx context.Context, seg *domain.Segment, groups []string) (io.Reader, error)
	TotalCapacity() int
	Close() error // allows idle runtime swaps on settings reload
}

// Manager defines the contract for our NZB search and download engine.
type IndexerAggregator interface {
	SearchAll(ctx context.Context, query string) ([]*domain.Release, error)
	SearchAllWithRequest(ctx context.Context, req SearchRequest) ([]*domain.Release, error)
	GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error)
	GetResultByID(ctx context.Context, id string) (*domain.Release, error)
}

// minimal PG catalog boundary for Milestone 7 resolver routing.
type UsenetIndexCatalog interface {
	GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error)
}

// current PG-backed store surface used by resolver, indexing runtime,
// health checks, and smoke tests. This keeps Context from depending on the
// concrete *pgindex.Store type directly.
type UsenetIndexStore interface {
	UsenetIndexCatalog

	Ping(ctx context.Context) error
	ValidateSchema(ctx context.Context) error

	ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]pgindex.CatalogReleaseFile, error)
	ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]pgindex.CatalogArticleRef, error)
	ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error)
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error

	EnsureProvider(ctx context.Context, providerKey, displayName string) (int64, error)
	EnsureNewsgroup(ctx context.Context, groupName string) (int64, error)
	StartScrapeRun(ctx context.Context, providerID int64) (int64, error)
	FinishScrapeRun(ctx context.Context, runID int64, status, errorText string) error
	GetLatestCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertLatestCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error
	GetBackfillCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertBackfillCheckpoint(ctx context.Context, providerID, newsgroupID, backfillArticleNumber int64) error
	InsertArticleHeaders(ctx context.Context, providerID, newsgroupID int64, headers []pgindex.ArticleHeader) (int64, error)

	ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]pgindex.AssemblyCandidate, error)
	EnsurePoster(ctx context.Context, posterName string) (int64, error)
	LinkArticlePoster(ctx context.Context, articleHeaderID, posterID int64) error
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaryPart(ctx context.Context, in pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error

	ListReleaseCandidates(ctx context.Context, limit int) ([]pgindex.ReleaseCandidate, error)
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)
	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
}

// resolver routes by source kind instead of assuming aggregator-only resolution.
type ReleaseResolver interface {
	GetRelease(ctx context.Context, sourceKind, sourceReleaseID string) (*domain.Release, error)
	GetNZB(ctx context.Context, sourceKind string, res *domain.Release) (io.ReadCloser, error)
}

type UsenetIndexerService interface {
	ScrapeOnce(ctx context.Context) error
	ScrapeLatestOnce(ctx context.Context) error
	ScrapeBackfillOnce(ctx context.Context) error
	AssembleOnce(ctx context.Context) error
	ReleaseOnce(ctx context.Context) error
	RunPipelineOnce(ctx context.Context) error
	Start(ctx context.Context, interval time.Duration) error
}

type Processor interface {
	// processor now needs the queue item so it can use a per-job work dir
	Prepare(ctx context.Context, item *domain.QueueItem, nzbModel *nzb.Model, nzbFilename string) (*domain.PreparationResult, error)
	Finalize(ctx context.Context, tasks []*domain.DownloadFile) error
	PostProcess(ctx context.Context, item *domain.QueueItem, tasks []*domain.DownloadFile) error
}

type Downloader interface {
	// The engine's ability to process a specific item
	Download(ctx context.Context, item *domain.QueueItem) error
	RenderCLIProgress(item *domain.QueueItem, speedMbps float64, final bool)
	SetProgressHandler(fn func(*domain.QueueItem))
}

// explicit queue enqueue contract so downloader does not infer source provenance.
type QueueAddRequest struct {
	SourceKind      string
	SourceReleaseID string
	Release         *domain.Release
	Title           string
}

type QueueManager interface {
	Start(ctx context.Context)
	Add(ctx context.Context, req QueueAddRequest) (*domain.QueueItem, error)
	GetActiveItem() *domain.QueueItem
	GetItem(ctx context.Context, id string) (*domain.QueueItem, bool)
	GetAllItems() []*domain.QueueItem
	Cancel(id string) bool
	Delete(id string) bool
	Stop()

	Pause() bool
	Resume() bool
	IsPaused() bool

	HydrateItem(ctx context.Context, item *domain.QueueItem) error
	UpdateStatus(ctx context.Context, item *domain.QueueItem, status domain.JobStatus)
	ReloadRuntime(appCtx *Context) // refresh future-job dependencies after settings reload
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

	// store liveness + schema handshake.
	Ping(ctx context.Context) error
	SchemaVersion(ctx context.Context) (int, error)
	ExpectedSchemaVersion() int
	ValidateSchema(ctx context.Context) error
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

// payload fetch now routes by persisted source kind.
type PayloadFetcher interface {
	GetNZB(ctx context.Context, sourceKind string, res *domain.Release) (io.ReadCloser, error)
}

type PayloadCacheStore interface {
	GetNZBReader(key string) (io.ReadCloser, error)
	CreateNZBWriter(key string) (io.WriteCloser, error)
	SaveNZBAtomically(key string, data []byte) error
	Exists(key string) bool
}

// Runtime settings
type SettingsStore interface {
	LoadEffectiveSettings(ctx context.Context, base *config.Config) (*config.Config, error)
	GetRuntimeSettings(ctx context.Context, base ...*config.Config) (*RuntimeSettings, error)
	UpdateSettings(ctx context.Context, next *RuntimeSettings) error
	WatchSettingsChanges(ctx context.Context) (<-chan struct{}, error)

	// store liveness + schema handshake.
	Ping(ctx context.Context) error
	SchemaVersion(ctx context.Context) (int, error)
	ExpectedSchemaVersion() int
	ValidateSchema(ctx context.Context) error
}

type ArrNotifier interface {
	NotifyQueueTerminal(ctx context.Context, item *domain.QueueItem) error
}
