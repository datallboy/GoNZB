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
	EnqueueByReleaseID(ctx context.Context, sourceKind, releaseID, title string) (*domain.QueueItem, error)
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
	Capabilities(ctx context.Context) (*ControlPlaneCapabilities, error)
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

type NNTPRuntimeStats struct {
	Scope           string                     `json:"scope"`
	Policy          string                     `json:"policy"`
	Capacity        int                        `json:"capacity"`
	Active          int                        `json:"active"`
	Idle            int                        `json:"idle"`
	Waiting         int64                      `json:"waiting"`
	BusyReturns     int64                      `json:"busy_returns"`
	WaitCount       int64                      `json:"wait_count"`
	WaitDurationMS  int64                      `json:"wait_duration_ms"`
	WaitMaxMS       int64                      `json:"wait_max_ms"`
	Fetches         int64                      `json:"fetches"`
	FetchBodyPrefix int64                      `json:"fetch_body_prefix"`
	GroupStats      int64                      `json:"group_stats"`
	XOver           int64                      `json:"xover"`
	ArticleNotFound int64                      `json:"article_not_found"`
	OperationErrors int64                      `json:"operation_errors"`
	Modules         NNTPModuleRuntimeStats     `json:"modules"`
	Providers       []NNTPProviderRuntimeStats `json:"providers"`
	Scopes          []NNTPScopeRuntimeStats    `json:"scopes"`
}

type NNTPModuleRuntimeStats struct {
	ReservationsEnabled      bool  `json:"reservations_enabled"`
	IdleBorrowEnabled        bool  `json:"idle_borrow_enabled"`
	IndexerMaxPercent        int   `json:"indexer_max_percent"`
	DownloaderReservePercent int   `json:"downloader_reserve_percent"`
	DownloaderDemandWindowMS int64 `json:"downloader_demand_window_ms"`
	IndexerActive            int64 `json:"indexer_active"`
	DownloaderActive         int64 `json:"downloader_active"`
	IndexerLimit             int   `json:"indexer_limit"`
	DownloaderLimit          int   `json:"downloader_limit"`
	DownloaderDemandActive   bool  `json:"downloader_demand_active"`
}

type NNTPScopeRuntimeStats struct {
	Scope           string `json:"scope"`
	Active          int64  `json:"active"`
	Waiting         int64  `json:"waiting"`
	WaitCount       int64  `json:"wait_count"`
	WaitDurationMS  int64  `json:"wait_duration_ms"`
	WaitMaxMS       int64  `json:"wait_max_ms"`
	Fetches         int64  `json:"fetches"`
	FetchBodyPrefix int64  `json:"fetch_body_prefix"`
	GroupStats      int64  `json:"group_stats"`
	XOver           int64  `json:"xover"`
	ArticleNotFound int64  `json:"article_not_found"`
	OperationErrors int64  `json:"operation_errors"`
}

type NNTPProviderRuntimeStats struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	Roles             []string `json:"roles,omitempty"`
	Priority          int      `json:"priority"`
	Capacity          int      `json:"capacity"`
	Active            int      `json:"active"`
	Idle              int      `json:"idle"`
	Dials             int64    `json:"dials"`
	DialFailures      int64    `json:"dial_failures"`
	PoolReuses        int64    `json:"pool_reuses"`
	PoolReturns       int64    `json:"pool_returns"`
	PoolDiscardIdle   int64    `json:"pool_discard_idle"`
	PoolDiscardAge    int64    `json:"pool_discard_age"`
	PoolDiscardError  int64    `json:"pool_discard_error"`
	FetchRetries      int64    `json:"fetch_retries"`
	GroupStatsRetries int64    `json:"group_stats_retries"`
	XOverRetries      int64    `json:"xover_retries"`
	RecoverableErrors int64    `json:"recoverable_errors"`
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
	GetReleaseArchiveState(ctx context.Context, releaseID string) (*pgindex.ReleaseArchiveState, error)
	ClaimReleaseArchiveCandidates(ctx context.Context, limit int) ([]pgindex.ReleaseArchiveCandidate, error)
	MarkReleaseArchiveStored(ctx context.Context, in pgindex.ReleaseArchiveStoredRecord) error
	MarkReleaseArchiveFailed(ctx context.Context, releaseID, errText string) error
	ClaimReleasePurgeCandidates(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) ([]pgindex.ReleasePurgeCandidate, error)
	PurgeArchivedReleaseSources(ctx context.Context, releaseID string) (*pgindex.ReleasePurgeResult, error)
	ClaimIndexerStage(ctx context.Context, req pgindex.IndexerStageClaimRequest) (*pgindex.IndexerStageClaimResult, error)
	HeartbeatIndexerStageRun(ctx context.Context, runID int64, owner string, leaseDuration time.Duration) error
	CompleteIndexerStageRun(ctx context.Context, req pgindex.IndexerStageFinishRequest) error
	FailIndexerStageRun(ctx context.Context, req pgindex.IndexerStageFinishRequest) error
	PauseIndexerStage(ctx context.Context, stageName string) error
	ResumeIndexerStage(ctx context.Context, stageName string) error
	RepairIndexerStageRuntime(ctx context.Context) (*pgindex.IndexerStageRepairResult, error)
	ListIndexerStageStates(ctx context.Context) ([]pgindex.IndexerStageState, error)
	ListIndexerStageRuns(ctx context.Context, stageName string, limit int) ([]pgindex.IndexerStageRun, error)
	ListIndexerStageRunsFiltered(ctx context.Context, params pgindex.IndexerStageRunListParams) ([]pgindex.IndexerStageRun, error)
	GetIndexerStageRun(ctx context.Context, runID int64) (*pgindex.IndexerStageRun, error)
	GetIndexerOverview(ctx context.Context) (*pgindex.IndexerOverview, error)
	GetIndexerDashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error)
	RefreshIndexerDashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error)
	GetIndexerBackfillProgress(ctx context.Context) (*pgindex.IndexerBackfillProgress, error)
	GetIndexerCrosspostNewsgroupPopularity(ctx context.Context, limit int) ([]pgindex.IndexerCrosspostPopularityItem, error)
	ReplaceIndexerProviderGroupInventory(ctx context.Context, rows []pgindex.IndexerProviderGroupInventoryItem) error
	GetIndexerProviderGroupInventoryStats(ctx context.Context) (pgindex.IndexerProviderGroupInventoryStats, error)
	ListIndexerProviderGroupInventoryCandidates(ctx context.Context, query string, patternHints []string) ([]pgindex.IndexerProviderGroupInventoryItem, error)
	ListIndexerProviderGroupInventoryPage(ctx context.Context, query string, limit, offset int, sortKey, direction string) (pgindex.IndexerProviderGroupInventoryPage, error)
	GetIndexerStageThroughput(ctx context.Context) (*pgindex.IndexerStageThroughput, error)
	ListIndexerReleases(ctx context.Context, params pgindex.AdminIndexerReleaseListParams) ([]pgindex.IndexerReleaseSummary, int, error)
	GetIndexerReleaseDetail(ctx context.Context, releaseID string) (*pgindex.IndexerReleaseDetail, error)
	ListPublicIndexerReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error)
	GetPublicIndexerReleaseDetailWithPolicy(ctx context.Context, releaseID string, policy pgindex.ReleaseReadyPolicy) (*pgindex.PublicIndexerReleaseDetail, error)
	GetPublicIndexerReleaseDetail(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error)
	UpsertReleaseOverride(ctx context.Context, in pgindex.ReleaseOverrideRecord) error
	GetReleaseOverride(ctx context.Context, releaseID string) (*pgindex.ReleaseOverrideRecord, error)
	ResetReleaseInspectionState(ctx context.Context, releaseID string) error
	ResetReleaseEnrichmentState(ctx context.Context, releaseID string) error
	GetIndexerBinaryDetail(ctx context.Context, binaryID int64) (*pgindex.IndexerBinaryDetail, error)
	GetIndexerFileDetail(ctx context.Context, fileID int64) (*pgindex.IndexerFileDetail, error)

	EnsureProvider(ctx context.Context, providerKey, displayName string) (int64, error)
	EnsureNewsgroup(ctx context.Context, groupName string) (int64, error)
	StartScrapeRun(ctx context.Context, providerID int64) (int64, error)
	FinishScrapeRun(ctx context.Context, runID int64, status, errorText string) error
	GetLatestCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertLatestCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error
	GetBackfillCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error)
	UpsertBackfillCheckpoint(ctx context.Context, providerID, newsgroupID, backfillArticleNumber int64) error
	GetBackfillCheckpointState(ctx context.Context, providerID, newsgroupID int64) (*pgindex.BackfillCheckpointState, error)
	HasBackfillCutoffReachedForGroup(ctx context.Context, newsgroupID int64, untilDate time.Time) (bool, error)
	SetBackfillCheckpointState(ctx context.Context, providerID, newsgroupID int64, untilDate *time.Time, cutoffReached bool, stoppedReason string) error
	InsertArticleHeaders(ctx context.Context, providerID, newsgroupID int64, headers []pgindex.ArticleHeader) (int64, error)
	RefreshYEncRecoveryAdmissionSnapshot(ctx context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error)
	ConfigureYEncRecoveryAdmission(ctx context.Context, cfg pgindex.YEncRecoveryAdmissionConfig) error
	UpsertIndexerGroupProfile(ctx context.Context, providerID, newsgroupID int64, tier, reason string) error
	RefreshIndexerGroupProfiles(ctx context.Context) (int64, error)
	UpsertDeferredArticleRange(ctx context.Context, in pgindex.DeferredArticleRangeRecord) error
	ListIndexerGroupProfiles(ctx context.Context, limit int) ([]pgindex.IndexerGroupProfileSummary, error)
	ListDeferredArticleRanges(ctx context.Context, state string, limit int) ([]pgindex.DeferredArticleRangeSummary, error)
	ListIndexerDailyBucketStats(ctx context.Context, limit int) ([]pgindex.IndexerDailyBucketSummary, error)

	ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]pgindex.AssemblyCandidate, error)
	ClaimUnassembledArticleHeaders(ctx context.Context, req pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error)
	ClaimAssemblyQueueBatch(ctx context.Context, req pgindex.AssemblyClaimRequest) ([]pgindex.AssemblyCandidate, error)
	CleanupStaleAssemblyQueueRows(ctx context.Context, limit int) (int, error)
	RecordYEncRecoveryNotFound(ctx context.Context, articleHeaderID int64) error
	RecordYEncRecoveryNoop(ctx context.Context, articleHeaderID int64) error
	RecordYEncRecoveryTransientFailure(ctx context.Context, articleHeaderID int64) error
	UpsertBinary(ctx context.Context, in pgindex.BinaryRecord) (int64, error)
	UpsertBinaries(ctx context.Context, records []pgindex.BinaryRecord) ([]int64, error)
	UpsertBinaryPart(ctx context.Context, in pgindex.BinaryPartRecord) error
	UpsertBinaryParts(ctx context.Context, records []pgindex.BinaryPartRecord) error
	RefreshBinaryStats(ctx context.Context, binaryID int64) error
	RefreshBinaryStatsBatch(ctx context.Context, binaryIDs []int64) error
	CountQueuedReleaseFamilySummaries(ctx context.Context) (int, error)
	RefreshQueuedReleaseFamilySummaries(ctx context.Context, limit int) (int, error)

	ListReleaseCandidates(ctx context.Context, limit int, opts pgindex.ReleaseCandidateSelectionOptions) ([]pgindex.ReleaseCandidate, error)
	ListReleaseNZBGenerateCandidates(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) ([]pgindex.ReleaseNZBGenerateCandidate, error)
	ListExistingReleaseCandidates(ctx context.Context, limit, offset int) ([]pgindex.ReleaseCandidate, error)
	ListAutoReformReleaseCandidates(ctx context.Context, limit int, minReformAge time.Duration) ([]pgindex.ReleaseCandidate, error)
	ListExistingReleaseCandidatesForReleaseIDs(ctx context.Context, releaseIDs []string) ([]pgindex.ReleaseCandidate, error)
	ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, releaseKey string) ([]pgindex.BinarySummary, error)
	ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error)
	ListBinaryPartArticlesBatch(ctx context.Context, binaryIDs []int64) (map[int64][]pgindex.ReleaseFileArticleRecord, error)
	ListReleaseTitleCandidates(ctx context.Context, binaryIDs []int64) ([]pgindex.ReleaseTitleCandidate, error)
	UpsertRelease(ctx context.Context, in pgindex.ReleaseRecord) (string, error)
	PersistReleaseSnapshot(ctx context.Context, in pgindex.ReleaseRecord, files []pgindex.ReleaseFileRecord, newsgroupIDs []int64) (pgindex.ReleaseSnapshotResult, error)
	DeleteStaleReleasesForSourceKey(ctx context.Context, providerID int64, keyKind, releaseKey string, keepGroupNames []string) error
	DeleteAuxiliaryOnlySiblingReleases(ctx context.Context, providerID, newsgroupID int64, baseStem string, keepReleaseIDs []string) error
	ReplaceReleaseFiles(ctx context.Context, releaseID string, files []pgindex.ReleaseFileRecord) error
	ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error
	AckReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, keyKind, familyKey string) error
	AckReleaseCandidates(ctx context.Context, candidates []pgindex.ReleaseCandidateAck) error
	PromoteBaseStemCandidatesForReleaseFamily(ctx context.Context, providerID, newsgroupID int64, releaseFamilyKey string) error
	ReopenArchivedReleaseForRegeneration(ctx context.Context, releaseID string) error
	RunIndexerMaintenance(ctx context.Context) (*pgindex.IndexerMaintenanceResult, error)
	DryRunReleaseSourcePurge(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) (*pgindex.MaintenanceTaskResult, error)
	RunReleaseSourcePurge(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) (*pgindex.MaintenanceTaskResult, error)
	DryRunSimpleMaintenanceTask(ctx context.Context, taskKey string, batchSize int) (*pgindex.MaintenanceTaskResult, error)
	RunSimpleMaintenanceTask(ctx context.Context, taskKey string, batchSize int) (*pgindex.MaintenanceTaskResult, error)
	DryRunRawStageRetentionTask(ctx context.Context, batchSize int, policy pgindex.RawStageRetentionPolicy) (*pgindex.MaintenanceTaskResult, error)
	RunRawStageRetentionTask(ctx context.Context, batchSize int, policy pgindex.RawStageRetentionPolicy) (*pgindex.MaintenanceTaskResult, error)
	PurgeArticleHeaderPayloads(ctx context.Context) (int64, error)
	BackfillIndexerCrosspostGroups(ctx context.Context, batchSize, maxBatches int) (*pgindex.IndexerCrosspostBackfillResult, error)
	MaterializeArticleHeaderPosters(ctx context.Context, limit int) (*pgindex.IndexerPosterMaterializationResult, error)
	RefreshCrosspostPopularity(ctx context.Context, limit int) (*pgindex.IndexerCrosspostPopularityRefreshResult, error)
	RunIndexerStorageReclaim(ctx context.Context, options pgindex.IndexerStorageReclaimOptions) (*pgindex.IndexerStorageReclaimResult, error)
	CheckCriticalIndexerIntegrity(ctx context.Context, ensureExtension bool) (*pgindex.IndexerIntegrityReport, error)
	ReindexCriticalIndexerIndexes(ctx context.Context) (*pgindex.IndexerIntegrityRepairResult, error)
	DatabaseStorageStatus(ctx context.Context) (*pgindex.DatabaseStorageStatus, error)
	ListBinaryInspectionCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.BinaryInspectionCandidate, error)
	ListBinaryInspectionCandidatesWithOptions(ctx context.Context, stageName string, limit int, opts pgindex.BinaryInspectionCandidateOptions) ([]pgindex.BinaryInspectionCandidate, error)
	ClaimBinaryInspectionCandidates(ctx context.Context, req pgindex.BinaryInspectionClaimRequest) ([]pgindex.BinaryInspectionCandidate, error)
	StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error
	CompleteBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	FailBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryInspectionArtifactRecord) error
	ReplaceBinaryArchiveEntries(ctx context.Context, binaryID int64, rows []pgindex.BinaryArchiveEntryRecord) error
	ReplaceBinaryMediaStreams(ctx context.Context, binaryID int64, rows []pgindex.BinaryMediaStreamRecord) error
	ReplaceBinaryTextEvidence(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryTextEvidenceRecord) error
	ReplaceBinaryPAR2Sets(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2SetRecord) error
	ReplaceBinaryPAR2Targets(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2TargetRecord) error
	ApplyBinaryPAR2TargetCoverage(ctx context.Context, binaryID int64, rows []pgindex.BinaryPAR2TargetRecord) (*pgindex.BinaryPAR2TargetCoverageResult, error)
	ApplyPAR2InspectionBatch(ctx context.Context, rows []pgindex.PAR2InspectionBatchRecord) (*pgindex.PAR2InspectionBatchResult, error)
	ApplyBinaryRecovery(ctx context.Context, in pgindex.BinaryRecoveryRecord) error
	ListYEncRecoveryCandidates(ctx context.Context, limit int) ([]pgindex.YEncRecoveryCandidate, error)
	ApplyYEncHeaderRecovery(ctx context.Context, in pgindex.YEncHeaderRecoveryRecord) (*pgindex.YEncHeaderRecoveryResult, error)
	UpsertReleasePasswordCandidate(ctx context.Context, in pgindex.ReleasePasswordCandidateRecord) (int64, error)
	ListPasswordVerificationCandidates(ctx context.Context, limit int) ([]pgindex.PasswordVerificationCandidate, error)
	UpdateReleasePasswordCandidateStatus(ctx context.Context, candidateID int64, status string, verifiedAt *time.Time, lastError string) error
	ApplyReleaseInspectionUpdate(ctx context.Context, in pgindex.ReleaseInspectionUpdate) error
	SetReleaseArchivePreview(ctx context.Context, releaseID, objectKey, contentType, sourceKind string) error
	ListReleaseEnrichmentCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.ReleaseEnrichmentCandidate, error)
	UpsertPredbEntries(ctx context.Context, rows []pgindex.PredbEntryRecord) error
	GetPredbBackfillWindow(ctx context.Context) (*pgindex.PredbBackfillWindow, error)
	GetPredbEntryWindow(ctx context.Context) (*pgindex.PredbBackfillWindow, error)
	GetPredbBackfillCheckpoint(ctx context.Context, provider string) (*pgindex.PredbBackfillCheckpoint, error)
	UpsertPredbBackfillCheckpoint(ctx context.Context, in pgindex.PredbBackfillCheckpoint) error
	ListPredbEntriesForWindow(ctx context.Context, from, to *time.Time, categoryHint string, limit int) ([]pgindex.PredbEntrySummary, error)
	ReplaceReleasePredbMatches(ctx context.Context, releaseID string, rows []pgindex.ReleasePredbMatchRecord) error
	ReplaceReleaseTMDBMatches(ctx context.Context, releaseID string, rows []pgindex.ReleaseTMDBMatchRecord) error
	ReplaceReleaseTVDBMatches(ctx context.Context, releaseID string, rows []pgindex.ReleaseTVDBMatchRecord) error
	ApplyReleasePredbUpdate(ctx context.Context, in pgindex.ReleasePredbUpdate) error
	ApplyReleaseEnrichmentUpdate(ctx context.Context, in pgindex.ReleaseEnrichmentUpdate) error
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
	RecoverYEncOnce(ctx context.Context) error
	ReleaseSummaryRefreshOnce(ctx context.Context) error
	ReleaseOnce(ctx context.Context) error
	ReleaseGenerateNZBOnce(ctx context.Context) error
	ReleaseArchiveNZBOnce(ctx context.Context) error
	ReleasePurgeArchivedSourcesOnce(ctx context.Context) error
	ReformReleasesOnce(ctx context.Context) error
	ReformSelectedReleasesOnce(ctx context.Context, releaseIDs []string) error
	InspectOnce(ctx context.Context) error
	InspectDiscoveryOnce(ctx context.Context) error
	InspectPAR2Once(ctx context.Context) error
	InspectNFOOnce(ctx context.Context) error
	InspectArchiveOnce(ctx context.Context) error
	InspectPasswordOnce(ctx context.Context) error
	InspectMediaOnce(ctx context.Context) error
	EnrichPredbOnce(ctx context.Context) error
	EnrichPredbSceneNameRecoveryOnce(ctx context.Context) error
	EnrichPredbMetadataFallbackOnce(ctx context.Context) error
	EnrichPredbSyncFeedOnce(ctx context.Context) error
	EnrichPredbSyncBackfillOnce(ctx context.Context) error
	EnrichTMDBOnce(ctx context.Context) error
	RunStageOnce(ctx context.Context, stageName string) error
	RunPipelineOnce(ctx context.Context) error
	Start(ctx context.Context, interval time.Duration) error
	NNTPStats(ctx context.Context) (*NNTPRuntimeStats, error)
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
	GetObjectReader(key string) (io.ReadCloser, error)
	CreateObjectWriter(key string) (io.WriteCloser, error)
	SaveObjectAtomically(key string, data []byte) error
	ExistsObject(key string) bool
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
	GetObjectReader(key string) (io.ReadCloser, error)
	CreateObjectWriter(key string) (io.WriteCloser, error)
	SaveObjectAtomically(key string, data []byte) error
	ExistsObject(key string) bool
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
