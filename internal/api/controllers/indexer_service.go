package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/settingsadmin"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

var errIndexerUnavailable = errors.New("indexer runtime is unavailable")

type indexerService interface {
	Overview(ctx context.Context) (*pgindex.IndexerOverview, error)
	DashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error)
	RefreshDashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error)
	StorageStatus(ctx context.Context) (*indexerStorageStatusView, error)
	StorageAudit(ctx context.Context) (*pgindex.IndexerStorageAuditReport, error)
	BackfillProgress(ctx context.Context) (*pgindex.IndexerBackfillProgress, error)
	RecoveryCapacity(ctx context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error)
	GroupProfiles(ctx context.Context, limit int) ([]pgindex.IndexerGroupProfileSummary, error)
	DeferredArticleRanges(ctx context.Context, state string, limit int) ([]pgindex.DeferredArticleRangeSummary, error)
	DailyBucketStats(ctx context.Context, limit int) ([]pgindex.IndexerDailyBucketSummary, error)
	StageThroughput(ctx context.Context) (*pgindex.IndexerStageThroughput, error)
	NNTPStats(ctx context.Context) (*app.NNTPRuntimeStats, error)
	ListStages(ctx context.Context) ([]indexerStageView, error)
	GetStage(ctx context.Context, stageName string) (*indexerStageView, error)
	ListRuns(ctx context.Context, params pgindex.IndexerStageRunListParams) ([]pgindex.IndexerStageRun, error)
	GetRun(ctx context.Context, runID int64) (*pgindex.IndexerStageRun, error)
	RunStage(ctx context.Context, stageName string) error
	PauseStage(ctx context.Context, stageName string) (*indexerStageView, error)
	ResumeStage(ctx context.Context, stageName string) (*indexerStageView, error)
	UpdateStageConfig(ctx context.Context, stageName string, patch indexerStageConfigPatch) (*indexerStageView, error)
	ListMaintenanceTasks(ctx context.Context) ([]indexerMaintenanceTaskView, error)
	DryRunMaintenanceTask(ctx context.Context, taskKey string) (*indexerMaintenanceTaskRunView, error)
	RunMaintenanceTask(ctx context.Context, taskKey string) (*indexerMaintenanceTaskRunView, error)
	UpdateMaintenanceTask(ctx context.Context, taskKey string, patch indexerMaintenanceTaskPatch) (*indexerMaintenanceTaskView, error)
	ListReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error)
	GetRelease(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error)
	ListAdminReleases(ctx context.Context, params pgindex.AdminIndexerReleaseListParams) ([]pgindex.IndexerReleaseSummary, int, error)
	GetAdminRelease(ctx context.Context, releaseID string) (*indexerAdminReleaseView, error)
	UpdateReleaseOverride(ctx context.Context, releaseID string, patch indexerReleaseOverridePatch) (*pgindex.ReleaseOverrideRecord, error)
	ReinspectRelease(ctx context.Context, releaseID string) error
	ReenrichRelease(ctx context.Context, releaseID string) error
	GetBinary(ctx context.Context, binaryID int64) (*pgindex.IndexerBinaryDetail, error)
	GetFile(ctx context.Context, fileID int64) (*pgindex.IndexerFileDetail, error)
}

type indexerStageView struct {
	StageName           string                   `json:"stage_name"`
	Enabled             bool                     `json:"enabled"`
	Paused              bool                     `json:"paused"`
	IntervalSeconds     int                      `json:"interval_seconds"`
	BatchSize           int                      `json:"batch_size"`
	Concurrency         int                      `json:"concurrency,omitempty"`
	SupportsConcurrency bool                     `json:"supports_concurrency"`
	BackoffSeconds      int                      `json:"backoff_seconds"`
	BacklogCount        *int64                   `json:"backlog_count,omitempty"`
	LeaseOwner          string                   `json:"lease_owner"`
	LeaseExpiresAt      *time.Time               `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt     *time.Time               `json:"last_heartbeat_at,omitempty"`
	LastRunID           int64                    `json:"last_run_id"`
	LastSuccessAt       *time.Time               `json:"last_success_at,omitempty"`
	LastError           string                   `json:"last_error"`
	UpdatedAt           *time.Time               `json:"updated_at,omitempty"`
	LatestRun           *pgindex.IndexerStageRun `json:"latest_run,omitempty"`
}

type indexerStageConfigPatch struct {
	Enabled                 *bool    `json:"enabled,omitempty"`
	IntervalMinutes         *float64 `json:"interval_minutes,omitempty"`
	BatchSize               *int     `json:"batch_size,omitempty"`
	Concurrency             *int     `json:"concurrency,omitempty"`
	MaxEffectiveConcurrency *int     `json:"max_effective_concurrency,omitempty"`
	BackoffSeconds          *int     `json:"backoff_seconds,omitempty"`
	BinaryUpsertDBChunkSize *int     `json:"binary_upsert_db_chunk_size,omitempty"`
	LaneATargetPct          *int     `json:"lane_a_target_pct,omitempty"`
	LaneBMinPct             *int     `json:"lane_b_min_pct,omitempty"`
	LaneATimeWindowMinutes  *int     `json:"lane_a_time_window_minutes,omitempty"`
	TargetWindowEnabled     *bool    `json:"target_window_enabled,omitempty"`
	TargetWindowStart       *string  `json:"target_window_start,omitempty"`
	TargetWindowEnd         *string  `json:"target_window_end,omitempty"`
	TargetWindowPct         *int     `json:"target_window_pct,omitempty"`
	NewestPct               *int     `json:"newest_pct,omitempty"`
}

type inspectionReadyQueueCounter interface {
	CountInspectionReadyQueue(ctx context.Context, stageName string) (int64, error)
}

type indexerStorageStatusView struct {
	DatabaseBytes         int64   `json:"database_bytes"`
	DataDirectory         string  `json:"data_directory"`
	FilesystemFreeBytes   int64   `json:"filesystem_free_bytes"`
	FilesystemTotalBytes  int64   `json:"filesystem_total_bytes"`
	FilesystemFreePercent float64 `json:"filesystem_free_percent"`
	FilesystemVisible     bool    `json:"filesystem_visible"`
	VisibilitySource      string  `json:"visibility_source"`
	GuardEnabled          bool    `json:"guard_enabled"`
	MinFreeBytes          int64   `json:"min_free_bytes"`
	MinFreePercent        float64 `json:"min_free_percent"`
	Blocked               bool    `json:"blocked"`
	Reason                string  `json:"reason,omitempty"`
}

type indexerReleaseOverridePatch struct {
	DisplayTitle           *string   `json:"display_title,omitempty"`
	ClassificationOverride *string   `json:"classification_override,omitempty"`
	TMDBIDOverride         *int64    `json:"tmdb_id_override,omitempty"`
	TVDBIDOverride         *int64    `json:"tvdb_id_override,omitempty"`
	IMDBIDOverride         *string   `json:"imdb_id_override,omitempty"`
	Hidden                 *bool     `json:"hidden,omitempty"`
	Notes                  *string   `json:"notes,omitempty"`
	Tags                   *[]string `json:"tags,omitempty"`
}

type indexerMaintenanceTaskView struct {
	TaskKey          string                         `json:"task_key"`
	Label            string                         `json:"label"`
	Purpose          string                         `json:"purpose"`
	Risk             string                         `json:"risk"`
	SpaceEffect      string                         `json:"space_effect"`
	SupervisorEffect string                         `json:"supervisor_effect"`
	DataEffect       string                         `json:"data_effect"`
	ReleaseSafety    string                         `json:"release_safety"`
	Destructive      bool                           `json:"destructive"`
	Enabled          bool                           `json:"enabled"`
	ScheduleEnabled  bool                           `json:"schedule_enabled"`
	IntervalHours    int                            `json:"interval_hours"`
	MinIntervalHours int                            `json:"min_interval_hours"`
	UsesBatchSize    bool                           `json:"uses_batch_size"`
	BatchSize        int                            `json:"batch_size"`
	LastDryRunAt     string                         `json:"last_dry_run_at,omitempty"`
	LastRun          *pgindex.IndexerStageRun       `json:"last_run,omitempty"`
	LastDryRun       *indexerMaintenanceTaskRunView `json:"last_dry_run,omitempty"`
	Warnings         []string                       `json:"warnings,omitempty"`
	Blockers         []string                       `json:"blockers,omitempty"`
}

type indexerMaintenanceTaskRunView struct {
	TaskKey              string                                  `json:"task_key"`
	DryRun               bool                                    `json:"dry_run"`
	EstimatedRowsByTable map[string]int64                        `json:"estimated_rows_by_table,omitempty"`
	DeletedRowsByTable   map[string]int64                        `json:"deleted_rows_by_table,omitempty"`
	VacuumedTables       []string                                `json:"vacuumed_tables,omitempty"`
	EstimatedBytes       int64                                   `json:"estimated_bytes,omitempty"`
	BeforeStorage        *pgindex.MaintenanceTaskStorageSnapshot `json:"before_storage,omitempty"`
	AfterStorage         *pgindex.MaintenanceTaskStorageSnapshot `json:"after_storage,omitempty"`
	Blockers             []string                                `json:"blockers,omitempty"`
	Warnings             []string                                `json:"warnings,omitempty"`
}

type indexerMaintenanceTaskPatch struct {
	Enabled         *bool `json:"enabled,omitempty"`
	ScheduleEnabled *bool `json:"schedule_enabled,omitempty"`
	IntervalHours   *int  `json:"interval_hours,omitempty"`
	BatchSize       *int  `json:"batch_size,omitempty"`
}

type indexerAdminReleaseView struct {
	Release  *pgindex.IndexerReleaseDetail  `json:"release"`
	Override *pgindex.ReleaseOverrideRecord `json:"override,omitempty"`
	Files    []*pgindex.IndexerFileDetail   `json:"files"`
	Binaries []*pgindex.IndexerBinaryDetail `json:"binaries"`
}

type runtimeIndexerService struct {
	appCtx        *app.Context
	store         app.UsenetIndexStore
	nntpSnapshots interface {
		GetLatestNNTPSnapshot(ctx context.Context, moduleName string) (*pgindex.NNTPRuntimeSnapshot, error)
		ListRecentNNTPSnapshots(ctx context.Context, moduleName string, since time.Time) ([]pgindex.NNTPRuntimeSnapshot, error)
	}
	settingsAdmin   app.SettingsAdmin
	archiveStore    app.BlobStore
	downloaderReady bool
	log             interface {
		Error(format string, v ...interface{})
	}
}

func newIndexerService(appCtx *app.Context) indexerService {
	if appCtx == nil {
		return &runtimeIndexerService{}
	}
	var log interface {
		Error(format string, v ...interface{})
	}
	if appCtx.Logger != nil {
		log = appCtx.Logger
	}
	return &runtimeIndexerService{
		appCtx:          appCtx,
		store:           appCtx.PGIndexStore,
		nntpSnapshots:   snapshotReaderFromStore(appCtx.PGIndexStore),
		settingsAdmin:   appCtx.SettingsAdmin,
		archiveStore:    appCtx.IndexerArchiveStore,
		downloaderReady: appCtx.DownloaderModule != nil,
		log:             log,
	}
}

func (s *runtimeIndexerService) Overview(ctx context.Context) (*pgindex.IndexerOverview, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerOverview(ctx)
}

func (s *runtimeIndexerService) DashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerDashboardStats(ctx)
}

func (s *runtimeIndexerService) RefreshDashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.RefreshIndexerDashboardStats(ctx)
}

func (s *runtimeIndexerService) StorageStatus(ctx context.Context) (*indexerStorageStatusView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	status, err := s.store.DatabaseStorageStatus(ctx)
	if err != nil {
		return nil, err
	}
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	cfg := pgindex.DatabaseStorageGuardConfig{Enabled: true, MinFreeBytes: 0, MinFreePercent: 15}
	if runtime != nil && runtime.Indexing != nil {
		storage := runtime.Indexing.StorageGuard
		cfg = pgindex.DatabaseStorageGuardConfig{
			Enabled:        storage.Enabled,
			DataDirectory:  strings.TrimSpace(storage.DataDirectory),
			MinFreeBytes:   storage.MinFreeBytes,
			MinFreePercent: storage.MinFreePercent,
		}
	}

	visibilitySource := "postgres_data_directory"
	if strings.TrimSpace(cfg.DataDirectory) != "" {
		visibilitySource = "configured_host_path"
		if err := pgindex.PopulateDatabaseStorageFilesystemStatus(status, cfg.DataDirectory); err != nil {
			return nil, err
		}
	}
	evaluation := pgindex.EvaluateDatabaseStorageGuard(*status, cfg)
	return &indexerStorageStatusView{
		DatabaseBytes:         status.DatabaseBytes,
		DataDirectory:         status.DataDirectory,
		FilesystemFreeBytes:   status.FilesystemFreeBytes,
		FilesystemTotalBytes:  status.FilesystemTotalBytes,
		FilesystemFreePercent: status.FilesystemFreePercent,
		FilesystemVisible:     status.FilesystemVisible,
		VisibilitySource:      visibilitySource,
		GuardEnabled:          cfg.Enabled,
		MinFreeBytes:          cfg.MinFreeBytes,
		MinFreePercent:        cfg.MinFreePercent,
		Blocked:               evaluation.Blocked,
		Reason:                evaluation.Reason,
	}, nil
}

type indexerStorageAuditor interface {
	GetIndexerStorageAudit(ctx context.Context) (*pgindex.IndexerStorageAuditReport, error)
}

func (s *runtimeIndexerService) StorageAudit(ctx context.Context) (*pgindex.IndexerStorageAuditReport, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	store, ok := s.store.(indexerStorageAuditor)
	if !ok {
		return nil, errIndexerUnavailable
	}
	return store.GetIndexerStorageAudit(ctx)
}

func (s *runtimeIndexerService) BackfillProgress(ctx context.Context) (*pgindex.IndexerBackfillProgress, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	progress, err := s.store.GetIndexerBackfillProgress(ctx)
	if err != nil {
		return nil, err
	}
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil || runtime == nil || runtime.Indexing == nil {
		return progress, nil
	}
	overlayBackfillProgressFromRuntime(progress, runtime.Indexing)
	return progress, nil
}

func (s *runtimeIndexerService) RecoveryCapacity(ctx context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.RefreshYEncRecoveryAdmissionSnapshot(ctx)
}

func (s *runtimeIndexerService) GroupProfiles(ctx context.Context, limit int) ([]pgindex.IndexerGroupProfileSummary, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.ListIndexerGroupProfiles(ctx, limit)
}

func (s *runtimeIndexerService) DeferredArticleRanges(ctx context.Context, state string, limit int) ([]pgindex.DeferredArticleRangeSummary, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.ListDeferredArticleRanges(ctx, state, limit)
}

func (s *runtimeIndexerService) DailyBucketStats(ctx context.Context, limit int) ([]pgindex.IndexerDailyBucketSummary, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.ListIndexerDailyBucketStats(ctx, limit)
}

func (s *runtimeIndexerService) StageThroughput(ctx context.Context) (*pgindex.IndexerStageThroughput, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerStageThroughput(ctx)
}

func (s *runtimeIndexerService) NNTPStats(ctx context.Context) (*app.NNTPRuntimeStats, error) {
	indexer := s.currentIndexer()
	if s != nil && indexer != nil {
		stats, err := indexer.NNTPStats(ctx)
		if err == nil && stats != nil {
			return stats, nil
		}
		if err != nil && s.log != nil {
			s.log.Error("load local nntp runtime stats: %v", err)
		}
	}
	if s != nil && s.nntpSnapshots != nil {
		snapshots, err := s.nntpSnapshots.ListRecentNNTPSnapshots(ctx, "indexer", time.Now().Add(-10*time.Second))
		if err != nil {
			if s.log != nil {
				s.log.Error("load shared nntp runtime snapshots: %v", err)
			}
		} else if len(snapshots) > 0 {
			aggregated, ok := aggregateNNTPSnapshots(snapshots, s.log)
			if ok {
				return aggregated, nil
			}
		} else {
			snapshot, err := s.nntpSnapshots.GetLatestNNTPSnapshot(ctx, "indexer")
			if err != nil {
				if s.log != nil {
					s.log.Error("load latest shared nntp runtime snapshot: %v", err)
				}
			} else if snapshot != nil && len(snapshot.Payload) > 0 {
				var stats app.NNTPRuntimeStats
				if err := json.Unmarshal(snapshot.Payload, &stats); err != nil {
					if s.log != nil {
						s.log.Error("decode shared nntp runtime snapshot: %v", err)
					}
				} else {
					return &stats, nil
				}
			}
		}
	}
	if s == nil || indexer == nil {
		return nil, errIndexerUnavailable
	}
	return indexer.NNTPStats(ctx)
}

func snapshotReaderFromStore(store app.UsenetIndexStore) interface {
	GetLatestNNTPSnapshot(ctx context.Context, moduleName string) (*pgindex.NNTPRuntimeSnapshot, error)
	ListRecentNNTPSnapshots(ctx context.Context, moduleName string, since time.Time) ([]pgindex.NNTPRuntimeSnapshot, error)
} {
	if store == nil {
		return nil
	}
	reader, _ := store.(interface {
		GetLatestNNTPSnapshot(ctx context.Context, moduleName string) (*pgindex.NNTPRuntimeSnapshot, error)
		ListRecentNNTPSnapshots(ctx context.Context, moduleName string, since time.Time) ([]pgindex.NNTPRuntimeSnapshot, error)
	})
	return reader
}

func aggregateNNTPSnapshots(snapshots []pgindex.NNTPRuntimeSnapshot, log interface {
	Error(format string, v ...interface{})
}) (*app.NNTPRuntimeStats, bool) {
	if len(snapshots) == 0 {
		return nil, false
	}
	var (
		aggregate       app.NNTPRuntimeStats
		providerTotals  = make(map[string]app.NNTPProviderRuntimeStats)
		scopeTotals     = make(map[string]app.NNTPScopeRuntimeStats)
		policy          string
		modulesAssigned bool
		decodedAny      bool
	)
	aggregate.Scope = "indexer"
	for _, snapshot := range snapshots {
		if len(snapshot.Payload) == 0 {
			continue
		}
		var stats app.NNTPRuntimeStats
		if err := json.Unmarshal(snapshot.Payload, &stats); err != nil {
			if log != nil {
				log.Error("decode shared nntp runtime snapshot: %v", err)
			}
			continue
		}
		decodedAny = true
		if policy == "" {
			policy = stats.Policy
		} else if policy != stats.Policy {
			policy = "mixed"
		}
		aggregate.Capacity += stats.Capacity
		aggregate.Active += stats.Active
		aggregate.Idle += stats.Idle
		aggregate.Waiting += stats.Waiting
		aggregate.BusyReturns += stats.BusyReturns
		aggregate.WaitCount += stats.WaitCount
		aggregate.WaitDurationMS += stats.WaitDurationMS
		if stats.WaitMaxMS > aggregate.WaitMaxMS {
			aggregate.WaitMaxMS = stats.WaitMaxMS
		}
		aggregate.Fetches += stats.Fetches
		aggregate.FetchBodyPrefix += stats.FetchBodyPrefix
		aggregate.GroupStats += stats.GroupStats
		aggregate.XOver += stats.XOver
		aggregate.ArticleNotFound += stats.ArticleNotFound
		aggregate.OperationErrors += stats.OperationErrors
		if !modulesAssigned {
			aggregate.Modules = stats.Modules
			modulesAssigned = true
		} else {
			aggregate.Modules.IndexerActive += stats.Modules.IndexerActive
			aggregate.Modules.DownloaderActive += stats.Modules.DownloaderActive
			aggregate.Modules.IndexerLimit += stats.Modules.IndexerLimit
			aggregate.Modules.DownloaderLimit += stats.Modules.DownloaderLimit
			aggregate.Modules.DownloaderDemandActive = aggregate.Modules.DownloaderDemandActive || stats.Modules.DownloaderDemandActive
		}
		for _, provider := range stats.Providers {
			total := providerTotals[provider.ID]
			total.ID = provider.ID
			total.Label = provider.Label
			total.Roles = append([]string(nil), provider.Roles...)
			total.Priority = provider.Priority
			total.Capacity += provider.Capacity
			total.Active += provider.Active
			total.Idle += provider.Idle
			total.Dials += provider.Dials
			total.DialFailures += provider.DialFailures
			total.PoolReuses += provider.PoolReuses
			total.PoolReturns += provider.PoolReturns
			total.PoolDiscardIdle += provider.PoolDiscardIdle
			total.PoolDiscardAge += provider.PoolDiscardAge
			total.PoolDiscardError += provider.PoolDiscardError
			total.FetchRetries += provider.FetchRetries
			total.GroupStatsRetries += provider.GroupStatsRetries
			total.XOverRetries += provider.XOverRetries
			total.RecoverableErrors += provider.RecoverableErrors
			providerTotals[provider.ID] = total
		}
		for _, scope := range stats.Scopes {
			total := scopeTotals[scope.Scope]
			total.Scope = scope.Scope
			total.Active += scope.Active
			total.Waiting += scope.Waiting
			total.WaitCount += scope.WaitCount
			total.WaitDurationMS += scope.WaitDurationMS
			if scope.WaitMaxMS > total.WaitMaxMS {
				total.WaitMaxMS = scope.WaitMaxMS
			}
			total.Fetches += scope.Fetches
			total.FetchBodyPrefix += scope.FetchBodyPrefix
			total.GroupStats += scope.GroupStats
			total.XOver += scope.XOver
			total.ArticleNotFound += scope.ArticleNotFound
			total.OperationErrors += scope.OperationErrors
			scopeTotals[scope.Scope] = total
		}
	}
	if !decodedAny {
		return nil, false
	}
	aggregate.Policy = policy
	aggregate.Providers = make([]app.NNTPProviderRuntimeStats, 0, len(providerTotals))
	for _, provider := range providerTotals {
		aggregate.Providers = append(aggregate.Providers, provider)
	}
	aggregate.Scopes = make([]app.NNTPScopeRuntimeStats, 0, len(scopeTotals))
	for _, scope := range scopeTotals {
		aggregate.Scopes = append(aggregate.Scopes, scope)
	}
	return &aggregate, true
}

func overlayBackfillProgressFromRuntime(progress *pgindex.IndexerBackfillProgress, indexing *app.IndexingRuntimeSettings) {
	if progress == nil || indexing == nil {
		return
	}
	effective := app.EffectiveScrapeGroups(indexing)
	cutoffs := make(map[string]*time.Time, len(effective))
	for _, group := range effective {
		name := strings.TrimSpace(group.GroupName)
		if name == "" {
			continue
		}
		var cutoff *time.Time
		if until := strings.TrimSpace(group.BackfillUntilDate); until != "" {
			if parsed, err := time.Parse("2006-01-02", until); err == nil {
				utc := parsed.UTC()
				cutoff = &utc
			}
		}
		cutoffs[strings.ToLower(name)] = cutoff
	}

	seen := make(map[string]struct{}, len(progress.Items))
	for i := range progress.Items {
		item := &progress.Items[i]
		key := strings.ToLower(strings.TrimSpace(item.GroupName))
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
		runtimeCutoff, ok := cutoffs[key]
		if !ok {
			item.ConfiguredCutoffDate = nil
			item.CutoffReached = false
			continue
		}
		if runtimeCutoff == nil {
			item.ConfiguredCutoffDate = nil
			item.CutoffReached = false
			continue
		}
		if item.ConfiguredCutoffDate == nil || !item.ConfiguredCutoffDate.Equal(*runtimeCutoff) {
			item.ConfiguredCutoffDate = runtimeCutoff
			item.CutoffReached = false
			continue
		}
		item.ConfiguredCutoffDate = runtimeCutoff
	}

	for _, group := range effective {
		if !group.Enabled {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(group.GroupName))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		item := pgindex.IndexerBackfillProgressItem{
			GroupName: strings.TrimSpace(group.GroupName),
		}
		if until := strings.TrimSpace(group.BackfillUntilDate); until != "" {
			if parsed, err := time.Parse("2006-01-02", until); err == nil {
				utc := parsed.UTC()
				item.ConfiguredCutoffDate = &utc
			}
		}
		progress.Items = append(progress.Items, item)
	}

	slices.SortFunc(progress.Items, func(a, b pgindex.IndexerBackfillProgressItem) int {
		switch {
		case a.GroupName < b.GroupName:
			return -1
		case a.GroupName > b.GroupName:
			return 1
		default:
			return 0
		}
	})
	progress.Count = len(progress.Items)
}

func (s *runtimeIndexerService) ListStages(ctx context.Context) ([]indexerStageView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}

	states, err := s.store.ListIndexerStageStates(ctx)
	if err != nil {
		return nil, err
	}
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := s.store.ListIndexerStageRuns(ctx, "", len(allIndexerStages)*8)
	if err != nil {
		return nil, err
	}

	stateByName := make(map[string]pgindex.IndexerStageState, len(states))
	for _, state := range states {
		stateByName[state.StageName] = state
	}

	latestRunByStage := make(map[string]pgindex.IndexerStageRun, len(allIndexerStages))
	for _, run := range runs {
		if _, exists := latestRunByStage[run.StageName]; exists {
			continue
		}
		latestRunByStage[run.StageName] = run
	}

	items := make([]indexerStageView, 0, len(allIndexerStages))
	for _, stageName := range allIndexerStages {
		state, ok := stateByName[stageName]
		view := stageViewFromSettings(stageName, runtime)
		overlayStageState(&view, state, ok)
		if backlog, ok, err := s.stageBacklogCount(ctx, stageName); err != nil {
			return nil, err
		} else if ok {
			view.BacklogCount = &backlog
		}
		if run, ok := latestRunByStage[stageName]; ok {
			runCopy := run
			view.LatestRun = &runCopy
		}
		items = append(items, view)
	}

	return items, nil
}

func (s *runtimeIndexerService) stageBacklogCount(ctx context.Context, stageName string) (int64, bool, error) {
	counter, ok := s.store.(inspectionReadyQueueCounter)
	if !ok {
		return 0, false, nil
	}
	inspectStageName := ""
	switch stageName {
	case string(supervisor.StageInspectDiscoveryReadyRefresh):
		inspectStageName = string(supervisor.StageInspectDiscovery)
	case string(supervisor.StageInspectPAR2ReadyRefresh):
		inspectStageName = string(supervisor.StageInspectPAR2)
	case string(supervisor.StageInspectArchiveReadyRefresh):
		inspectStageName = string(supervisor.StageInspectArchive)
	case string(supervisor.StageInspectMediaReadyRefresh):
		inspectStageName = string(supervisor.StageInspectMedia)
	default:
		return 0, false, nil
	}
	count, err := counter.CountInspectionReadyQueue(ctx, inspectStageName)
	if err != nil {
		return 0, false, err
	}
	return count, true, nil
}

func (s *runtimeIndexerService) ListRuns(ctx context.Context, params pgindex.IndexerStageRunListParams) ([]pgindex.IndexerStageRun, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}

	params.StageName = normalizeStageName(params.StageName)
	if params.StageName != "" {
		if _, err := parseIndexerStage(params.StageName); err != nil {
			return nil, err
		}
	}

	return s.store.ListIndexerStageRunsFiltered(ctx, params)
}

func (s *runtimeIndexerService) GetStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	stage, err := parseIndexerStage(stageName)
	if err != nil {
		return nil, err
	}
	return s.getStage(ctx, string(stage))
}

func (s *runtimeIndexerService) GetRun(ctx context.Context, runID int64) (*pgindex.IndexerStageRun, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerStageRun(ctx, runID)
}

func (s *runtimeIndexerService) RunStage(ctx context.Context, stageName string) error {
	indexer := s.currentIndexer()
	if s == nil || indexer == nil {
		return errIndexerUnavailable
	}

	stage, err := parseIndexerStage(stageName)
	if err != nil {
		return err
	}

	go func(stage string) {
		if err := indexer.RunStageOnce(context.Background(), stage); err != nil && s.log != nil {
			s.log.Error("indexer api stage run failed stage=%s err=%v", stage, err)
		}
	}(string(stage))

	return nil
}

func (s *runtimeIndexerService) PauseStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}

	stage, err := parseIndexerStage(stageName)
	if err != nil {
		return nil, err
	}

	if err := s.store.PauseIndexerStage(ctx, string(stage)); err != nil {
		return nil, err
	}

	return s.getStage(ctx, string(stage))
}

func (s *runtimeIndexerService) ResumeStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}

	stage, err := parseIndexerStage(stageName)
	if err != nil {
		return nil, err
	}

	if err := s.store.ResumeIndexerStage(ctx, string(stage)); err != nil {
		return nil, err
	}

	return s.getStage(ctx, string(stage))
}

func (s *runtimeIndexerService) UpdateStageConfig(ctx context.Context, stageName string, patch indexerStageConfigPatch) (*indexerStageView, error) {
	if s == nil || s.settingsAdmin == nil {
		return nil, settingsadmin.ErrUnavailable
	}
	stage, err := parseIndexerStage(stageName)
	if err != nil {
		return nil, err
	}
	current, err := s.settingsAdmin.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := app.CloneRuntimeSettings(current)
	if next.Indexing == nil {
		next.Indexing = &app.IndexingRuntimeSettings{}
	}
	if err := applyIndexerStageConfigPatch(next.Indexing, string(stage), patch); err != nil {
		return nil, err
	}
	if _, err := s.settingsAdmin.Update(ctx, &app.RuntimeSettingsPatch{Indexing: next.Indexing}); err != nil {
		return nil, err
	}
	return s.getStage(ctx, string(stage))
}

func (s *runtimeIndexerService) ListMaintenanceTasks(ctx context.Context) ([]indexerMaintenanceTaskView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	runs, _ := s.store.ListIndexerStageRuns(ctx, "", 100)
	lastRunByStage := map[string]pgindex.IndexerStageRun{}
	for _, run := range runs {
		if _, ok := lastRunByStage[run.StageName]; ok {
			continue
		}
		lastRunByStage[run.StageName] = run
	}
	items := make([]indexerMaintenanceTaskView, 0, len(indexerMaintenanceTaskDefinitions))
	for _, def := range indexerMaintenanceTaskDefinitions {
		view := maintenanceTaskViewFromSettings(def, runtime)
		if run, ok := lastRunByStage[maintenanceTaskStageName(def.TaskKey)]; ok {
			runCopy := run
			view.LastRun = &runCopy
		}
		items = append(items, view)
	}
	return items, nil
}

func (s *runtimeIndexerService) DryRunMaintenanceTask(ctx context.Context, taskKey string) (*indexerMaintenanceTaskRunView, error) {
	def, err := parseMaintenanceTask(taskKey)
	if err != nil {
		return nil, err
	}
	result, err := s.executeMaintenanceTask(ctx, def.TaskKey, true)
	if err != nil {
		return nil, err
	}
	s.enrichMaintenanceTaskStorageSnapshots(ctx, result)
	if s.settingsAdmin != nil {
		current, err := s.settingsAdmin.Get(ctx)
		if err == nil {
			next := app.CloneRuntimeSettings(current)
			if next.Indexing == nil {
				next.Indexing = &app.IndexingRuntimeSettings{}
			}
			next.Indexing.MaintenanceTasks = app.WithRuntimeDefaults(next).Indexing.MaintenanceTasks
			cfg := next.Indexing.MaintenanceTasks[def.TaskKey]
			cfg.LastDryRunAt = time.Now().UTC().Format(time.RFC3339)
			next.Indexing.MaintenanceTasks[def.TaskKey] = cfg
			_, _ = s.settingsAdmin.Update(ctx, &app.RuntimeSettingsPatch{Indexing: next.Indexing})
		}
	}
	return maintenanceTaskRunView(result), nil
}

func (s *runtimeIndexerService) RunMaintenanceTask(ctx context.Context, taskKey string) (*indexerMaintenanceTaskRunView, error) {
	def, err := parseMaintenanceTask(taskKey)
	if err != nil {
		return nil, err
	}
	if def.Destructive {
		runtime, err := s.loadRuntimeSettings(ctx)
		if err != nil {
			return nil, err
		}
		cfg := maintenanceTaskConfig(runtime, def.TaskKey)
		lastDryRun, err := time.Parse(time.RFC3339, strings.TrimSpace(cfg.LastDryRunAt))
		if err != nil || time.Since(lastDryRun) > 30*time.Minute {
			return nil, fmt.Errorf("destructive task %q requires a fresh dry-run within 30 minutes", def.TaskKey)
		}
	}
	result, err := s.runMaintenanceTaskWithStageRecord(ctx, def.TaskKey)
	if err != nil {
		return nil, err
	}
	s.enrichMaintenanceTaskStorageSnapshots(ctx, result)
	return maintenanceTaskRunView(result), nil
}

func (s *runtimeIndexerService) UpdateMaintenanceTask(ctx context.Context, taskKey string, patch indexerMaintenanceTaskPatch) (*indexerMaintenanceTaskView, error) {
	def, err := parseMaintenanceTask(taskKey)
	if err != nil {
		return nil, err
	}
	if s == nil || s.settingsAdmin == nil {
		return nil, settingsadmin.ErrUnavailable
	}
	current, err := s.settingsAdmin.Get(ctx)
	if err != nil {
		return nil, err
	}
	next := app.CloneRuntimeSettings(current)
	if next.Indexing == nil {
		next.Indexing = &app.IndexingRuntimeSettings{}
	}
	next.Indexing.MaintenanceTasks = app.WithRuntimeDefaults(next).Indexing.MaintenanceTasks
	cfg := next.Indexing.MaintenanceTasks[def.TaskKey]
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.ScheduleEnabled != nil {
		cfg.ScheduleEnabled = *patch.ScheduleEnabled
	}
	if patch.IntervalHours != nil {
		cfg.IntervalHours = *patch.IntervalHours
	}
	if patch.BatchSize != nil && def.UsesBatchSize {
		cfg.BatchSize = *patch.BatchSize
	}
	if cfg.ScheduleEnabled && cfg.IntervalHours < maintenanceTaskMinIntervalHours(def) {
		return nil, fmt.Errorf("maintenance task %q scheduled interval must be at least %d hours", def.TaskKey, maintenanceTaskMinIntervalHours(def))
	}
	next.Indexing.MaintenanceTasks[def.TaskKey] = cfg
	if _, err := s.settingsAdmin.Update(ctx, &app.RuntimeSettingsPatch{Indexing: next.Indexing}); err != nil {
		return nil, err
	}
	updated, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	view := maintenanceTaskViewFromSettings(def, updated)
	return &view, nil
}

type maintenanceTaskStore interface {
	DryRunReleaseSourcePurge(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) (*pgindex.MaintenanceTaskResult, error)
	RunReleaseSourcePurge(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) (*pgindex.MaintenanceTaskResult, error)
	DryRunSimpleMaintenanceTask(ctx context.Context, taskKey string, batchSize int) (*pgindex.MaintenanceTaskResult, error)
	RunSimpleMaintenanceTask(ctx context.Context, taskKey string, batchSize int) (*pgindex.MaintenanceTaskResult, error)
	DryRunRawStageRetentionTask(ctx context.Context, batchSize int, policy pgindex.RawStageRetentionPolicy) (*pgindex.MaintenanceTaskResult, error)
	RunRawStageRetentionTask(ctx context.Context, batchSize int, policy pgindex.RawStageRetentionPolicy) (*pgindex.MaintenanceTaskResult, error)
	RefreshIndexerGroupProfiles(ctx context.Context) (int64, error)
	RunIndexerMaintenance(ctx context.Context) (*pgindex.IndexerMaintenanceResult, error)
	ClaimIndexerStage(ctx context.Context, req pgindex.IndexerStageClaimRequest) (*pgindex.IndexerStageClaimResult, error)
	CompleteIndexerStageRun(ctx context.Context, req pgindex.IndexerStageFinishRequest) error
	FailIndexerStageRun(ctx context.Context, req pgindex.IndexerStageFinishRequest) error
}

func (s *runtimeIndexerService) executeMaintenanceTask(ctx context.Context, taskKey string, dryRun bool) (*pgindex.MaintenanceTaskResult, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	store, ok := s.store.(maintenanceTaskStore)
	if !ok {
		return nil, errIndexerUnavailable
	}
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	cfg := maintenanceTaskConfig(runtime, taskKey)
	if !cfg.Enabled {
		return nil, fmt.Errorf("maintenance task %q is disabled", taskKey)
	}
	switch taskKey {
	case "dashboard_stats_refresh":
		if dryRun {
			stats, err := s.store.GetIndexerDashboardStats(ctx)
			if err != nil {
				return nil, err
			}
			count := int64(0)
			if stats != nil {
				count = int64(stats.Count)
			}
			return &pgindex.MaintenanceTaskResult{
				TaskKey:              taskKey,
				DryRun:               true,
				EstimatedRowsByTable: map[string]int64{"indexer_dashboard_stats": count},
				Warnings:             []string{"dry-run reports cached stat rows without recomputing counts"},
			}, nil
		}
		stats, err := s.store.RefreshIndexerDashboardStats(ctx)
		if err != nil {
			return nil, err
		}
		count := int64(0)
		if stats != nil {
			count = int64(stats.Count)
		}
		return &pgindex.MaintenanceTaskResult{
			TaskKey:              taskKey,
			DryRun:               false,
			EstimatedRowsByTable: map[string]int64{"indexer_dashboard_stats": count},
		}, nil
	case "release_source_purge":
		if dryRun {
			return store.DryRunReleaseSourcePurge(ctx, cfg.BatchSize, s.releaseReadyPolicy(ctx))
		}
		return store.RunReleaseSourcePurge(ctx, cfg.BatchSize, s.releaseReadyPolicy(ctx))
	case "group_profile_refresh":
		if dryRun {
			profiles, err := s.store.ListIndexerGroupProfiles(ctx, 1000)
			if err != nil {
				return nil, err
			}
			return &pgindex.MaintenanceTaskResult{TaskKey: taskKey, DryRun: true, EstimatedRowsByTable: map[string]int64{"indexer_group_profiles": int64(len(profiles))}, Warnings: []string{"dry-run reports visible profile count without recalculating scores"}}, nil
		}
		updated, err := store.RefreshIndexerGroupProfiles(ctx)
		if err != nil {
			return nil, err
		}
		return &pgindex.MaintenanceTaskResult{TaskKey: taskKey, DryRun: false, EstimatedRowsByTable: map[string]int64{"indexer_group_profiles_scored": updated}}, nil
	case "raw_stage_retention":
		if dryRun {
			return store.DryRunRawStageRetentionTask(ctx, cfg.BatchSize, rawStageRetentionPolicyFromRuntime(runtime))
		}
		return store.RunRawStageRetentionTask(ctx, cfg.BatchSize, rawStageRetentionPolicyFromRuntime(runtime))
	case "readiness_cleanup":
		if dryRun {
			return &pgindex.MaintenanceTaskResult{TaskKey: taskKey, DryRun: true, EstimatedRowsByTable: map[string]int64{}, Warnings: []string{"dry-run is not exact for the existing combined readiness cleanup path"}}, nil
		}
		out, err := store.RunIndexerMaintenance(ctx)
		if err != nil {
			return nil, err
		}
		return &pgindex.MaintenanceTaskResult{
			TaskKey: taskKey,
			DryRun:  false,
			DeletedRowsByTable: map[string]int64{
				"release_family_readiness_summaries": out.PurgedReadinessSummaries,
				"releases":                           out.PurgedOrphanReleases,
			},
			Warnings: []string{"v1 delegates to the existing combined indexer maintenance cleanup path"},
		}, nil
	case "inspect_workspace_cleanup":
		return &pgindex.MaintenanceTaskResult{TaskKey: taskKey, DryRun: dryRun, EstimatedRowsByTable: map[string]int64{}, Warnings: []string{"inspect workspace cleanup is currently executed by the scheduled indexer_maintenance service"}}, nil
	default:
		if dryRun {
			return store.DryRunSimpleMaintenanceTask(ctx, taskKey, cfg.BatchSize)
		}
		return store.RunSimpleMaintenanceTask(ctx, taskKey, cfg.BatchSize)
	}
}

func (s *runtimeIndexerService) runMaintenanceTaskWithStageRecord(ctx context.Context, taskKey string) (*pgindex.MaintenanceTaskResult, error) {
	store, ok := s.store.(maintenanceTaskStore)
	if !ok {
		return nil, errIndexerUnavailable
	}
	owner := "maintenance-api"
	stageName := maintenanceTaskStageName(taskKey)
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	cfg := maintenanceTaskConfig(runtime, taskKey)
	claim, err := store.ClaimIndexerStage(ctx, pgindex.IndexerStageClaimRequest{
		StageName:     stageName,
		TriggerKind:   "manual",
		Owner:         owner,
		Enabled:       true,
		Interval:      time.Duration(cfg.IntervalHours) * time.Hour,
		BatchSize:     cfg.BatchSize,
		Concurrency:   1,
		LeaseDuration: 30 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	if claim == nil || !claim.Claimed || claim.Run == nil {
		reason := "not claimed"
		if claim != nil && claim.Reason != "" {
			reason = claim.Reason
		}
		return nil, fmt.Errorf("maintenance task %q skipped: %s", taskKey, reason)
	}
	result, runErr := s.executeMaintenanceTask(ctx, taskKey, false)
	s.enrichMaintenanceTaskStorageSnapshots(ctx, result)
	metrics, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		metrics = json.RawMessage(`{"metrics_error":"marshal_failed"}`)
	}
	finish := pgindex.IndexerStageFinishRequest{RunID: claim.Run.ID, Owner: owner, MetricsJSON: metrics}
	if runErr != nil {
		finish.ErrorText = runErr.Error()
		if err := store.FailIndexerStageRun(context.Background(), finish); err != nil {
			return nil, fmt.Errorf("%v (also failed to mark maintenance task failed: %w)", runErr, err)
		}
		return nil, runErr
	}
	if err := store.CompleteIndexerStageRun(context.Background(), finish); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *runtimeIndexerService) enrichMaintenanceTaskStorageSnapshots(ctx context.Context, result *pgindex.MaintenanceTaskResult) {
	if s == nil || result == nil || s.settingsAdmin == nil {
		return
	}
	runtime, err := s.loadRuntimeSettings(ctx)
	if err != nil || runtime == nil || runtime.Indexing == nil {
		return
	}
	dataDirectory := strings.TrimSpace(runtime.Indexing.StorageGuard.DataDirectory)
	if dataDirectory == "" {
		return
	}
	for _, snapshot := range []*pgindex.MaintenanceTaskStorageSnapshot{result.BeforeStorage, result.AfterStorage} {
		if snapshot == nil {
			continue
		}
		status := &pgindex.DatabaseStorageStatus{DatabaseBytes: snapshot.DatabaseBytes}
		if err := pgindex.PopulateDatabaseStorageFilesystemStatus(status, dataDirectory); err != nil {
			continue
		}
		snapshot.DataDirectory = status.DataDirectory
		snapshot.FilesystemFreeBytes = status.FilesystemFreeBytes
		snapshot.FilesystemTotalBytes = status.FilesystemTotalBytes
		snapshot.FilesystemFreePercent = status.FilesystemFreePercent
		snapshot.FilesystemVisible = status.FilesystemVisible
	}
}

func (s *runtimeIndexerService) ListReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, errIndexerUnavailable
	}
	params.Query = strings.TrimSpace(params.Query)
	params.ReadyPolicy = s.releaseReadyPolicy(ctx)
	return s.store.ListPublicIndexerReleases(ctx, params)
}

func (s *runtimeIndexerService) GetRelease(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	detail, err := s.store.GetPublicIndexerReleaseDetailWithPolicy(ctx, strings.TrimSpace(releaseID), s.releaseReadyPolicy(ctx))
	if err != nil || detail == nil {
		return detail, err
	}
	detail.Capabilities.CanSendToDownloader = s.downloaderReady
	return detail, nil
}

func (s *runtimeIndexerService) releaseReadyPolicy(ctx context.Context) pgindex.ReleaseReadyPolicy {
	policy := pgindex.DefaultReleaseReadyPolicy()
	if s == nil || s.settingsAdmin == nil {
		return policy
	}
	runtime, err := s.settingsAdmin.Get(ctx)
	if err != nil || runtime == nil || runtime.Indexing == nil {
		return policy
	}
	release := runtime.Indexing.Release
	return pgindex.NormalizeReleaseReadyPolicy(pgindex.ReleaseReadyPolicy{
		MinMatchConfidence:                   release.PublicMinMatchConfidence,
		MinCompletionPct:                     release.PublicMinCompletionPct,
		MinIdentityStatus:                    release.PublicMinIdentityStatus,
		RequireInspection:                    release.PublicRequireInspection,
		RequireEnrichment:                    release.PublicRequireEnrichment,
		RequirePayloadComplete:               release.PublicRequirePayloadComplete,
		RequireExpectedFileCountComplete:     release.PublicRequireExpectedFileCountComplete,
		RequirePAR2:                          release.PublicRequirePAR2,
		RequireNFO:                           release.PublicRequireNFO,
		RequireSFV:                           release.PublicRequireSFV,
		RetainUntilExpectedFileCountComplete: release.RetainUntilExpectedFileCountComplete,
		RetainRequirePAR2:                    release.RetainRequirePAR2,
		RetainRequireNFO:                     release.RetainRequireNFO,
		RetainRequireSFV:                     release.RetainRequireSFV,
	})
}

func (s *runtimeIndexerService) ListAdminReleases(ctx context.Context, params pgindex.AdminIndexerReleaseListParams) ([]pgindex.IndexerReleaseSummary, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, errIndexerUnavailable
	}
	params.Query = strings.TrimSpace(params.Query)
	return s.store.ListIndexerReleases(ctx, params)
}

func (s *runtimeIndexerService) GetAdminRelease(ctx context.Context, releaseID string) (*indexerAdminReleaseView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	release, err := s.store.GetIndexerReleaseDetail(ctx, strings.TrimSpace(releaseID))
	if err != nil || release == nil {
		return nil, err
	}
	override, err := s.store.GetReleaseOverride(ctx, strings.TrimSpace(releaseID))
	if err != nil {
		return nil, err
	}

	return &indexerAdminReleaseView{
		Release:  release,
		Override: override,
		Files:    []*pgindex.IndexerFileDetail{},
		Binaries: []*pgindex.IndexerBinaryDetail{},
	}, nil
}

func (s *runtimeIndexerService) UpdateReleaseOverride(ctx context.Context, releaseID string, patch indexerReleaseOverridePatch) (*pgindex.ReleaseOverrideRecord, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	current, err := s.store.GetReleaseOverride(ctx, strings.TrimSpace(releaseID))
	if err != nil {
		return nil, err
	}
	if current == nil {
		current = &pgindex.ReleaseOverrideRecord{ReleaseID: strings.TrimSpace(releaseID)}
	}
	applyReleaseOverridePatch(current, patch)
	if err := s.store.UpsertReleaseOverride(ctx, *current); err != nil {
		return nil, err
	}
	return s.store.GetReleaseOverride(ctx, strings.TrimSpace(releaseID))
}

func (s *runtimeIndexerService) ReinspectRelease(ctx context.Context, releaseID string) error {
	if s == nil || s.store == nil || s.currentIndexer() == nil {
		return errIndexerUnavailable
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	if _, err := s.store.GetIndexerReleaseDetail(ctx, releaseID); err != nil {
		return err
	}
	if err := s.store.ResetReleaseInspectionState(ctx, releaseID); err != nil {
		return err
	}
	go s.runStageSequence(
		"admin release reinspect",
		[]string{
			string(supervisor.StageInspectDiscovery),
			string(supervisor.StageInspectPAR2),
			string(supervisor.StageInspectNFO),
			string(supervisor.StageInspectArchive),
			string(supervisor.StageInspectPassword),
			string(supervisor.StageInspectMedia),
		},
	)
	return nil
}

func (s *runtimeIndexerService) ReenrichRelease(ctx context.Context, releaseID string) error {
	if s == nil || s.store == nil || s.currentIndexer() == nil {
		return errIndexerUnavailable
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	if _, err := s.store.GetIndexerReleaseDetail(ctx, releaseID); err != nil {
		return err
	}
	if err := s.store.ResetReleaseEnrichmentState(ctx, releaseID); err != nil {
		return err
	}
	go s.runStageSequence(
		"admin release reenrich",
		[]string{
			string(supervisor.StageEnrichPreDB),
			string(supervisor.StageEnrichTMDB),
		},
	)
	return nil
}

func (s *runtimeIndexerService) GetBinary(ctx context.Context, binaryID int64) (*pgindex.IndexerBinaryDetail, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerBinaryDetail(ctx, binaryID)
}

func (s *runtimeIndexerService) GetFile(ctx context.Context, fileID int64) (*pgindex.IndexerFileDetail, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerFileDetail(ctx, fileID)
}

func (s *runtimeIndexerService) getStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	items, err := s.ListStages(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.StageName == stageName {
			stage := item
			return &stage, nil
		}
	}
	return nil, fmt.Errorf("unknown stage %q", stageName)
}

func (s *runtimeIndexerService) runStageSequence(reason string, stages []string) {
	indexer := s.currentIndexer()
	if indexer == nil {
		return
	}
	for _, stage := range stages {
		if err := indexer.RunStageOnce(context.Background(), stage); err != nil && s.log != nil {
			s.log.Error("%s failed stage=%s err=%v", reason, stage, err)
		}
	}
}

func (s *runtimeIndexerService) currentIndexer() app.UsenetIndexerService {
	if s == nil || s.appCtx == nil {
		return nil
	}
	return s.appCtx.UsenetIndexer
}

func overlayStageState(view *indexerStageView, state pgindex.IndexerStageState, ok bool) {
	if view == nil || !ok {
		return
	}

	view.Paused = state.Paused
	view.LeaseOwner = state.LeaseOwner
	view.LastRunID = state.LastRunID
	view.LastError = state.LastError
	updatedAt := state.UpdatedAt.UTC()
	view.UpdatedAt = &updatedAt
	if state.LeaseExpiresAt != nil {
		t := state.LeaseExpiresAt.UTC()
		view.LeaseExpiresAt = &t
	}
	if state.LastHeartbeatAt != nil {
		t := state.LastHeartbeatAt.UTC()
		view.LastHeartbeatAt = &t
	}
	if state.LastSuccessAt != nil {
		t := state.LastSuccessAt.UTC()
		view.LastSuccessAt = &t
	}
}

func stageViewFromSettings(stageName string, runtime *app.RuntimeSettings) indexerStageView {
	view := indexerStageView{StageName: stageName}
	stageConfig, ok := stageSettingsForName(runtime, stageName)
	if !ok {
		return view
	}
	view.Enabled = stageConfig.Enabled
	view.IntervalSeconds = int(stageConfig.IntervalMinutes * 60)
	view.BatchSize = stageConfig.BatchSize
	view.SupportsConcurrency = stageSupportsConcurrency(stageName)
	if view.SupportsConcurrency {
		view.Concurrency = stageConfig.Concurrency
	}
	view.BackoffSeconds = stageConfig.BackoffSeconds
	return view
}

func (s *runtimeIndexerService) loadRuntimeSettings(ctx context.Context) (*app.RuntimeSettings, error) {
	if s == nil || s.settingsAdmin == nil {
		return nil, settingsadmin.ErrUnavailable
	}
	return s.settingsAdmin.Get(ctx)
}

func stageSettingsForName(runtime *app.RuntimeSettings, stageName string) (app.IndexingStageRuntimeSettings, bool) {
	if runtime == nil || runtime.Indexing == nil {
		return app.IndexingStageRuntimeSettings{}, false
	}
	switch stageName {
	case string(supervisor.StageScrapeLatest):
		return runtime.Indexing.ScrapeLatest, true
	case string(supervisor.StageScrapeBackfill):
		return runtime.Indexing.ScrapeBackfill, true
	case string(supervisor.StagePosterMaterialize):
		return runtime.Indexing.PosterMaterialize, true
	case string(supervisor.StageCrosspostPopularityRefresh):
		return runtime.Indexing.CrosspostPopularityRefresh, true
	case string(supervisor.StageAssemble):
		return runtime.Indexing.Assemble, true
	case string(supervisor.StageRecoverYEnc):
		return runtime.Indexing.RecoverYEnc, true
	case string(supervisor.StageReleaseSummaryRefresh):
		return runtime.Indexing.ReleaseSummaryRefresh, true
	case string(supervisor.StageRelease):
		return app.IndexingStageRuntimeSettings{
			Enabled:         runtime.Indexing.Release.Enabled,
			IntervalMinutes: runtime.Indexing.Release.IntervalMinutes,
			BatchSize:       runtime.Indexing.Release.BatchSize,
			BackoffSeconds:  runtime.Indexing.Release.BackoffSeconds,
		}, true
	case string(supervisor.StageReleaseGenerateNZB):
		return runtime.Indexing.ReleaseGenerateNZB, true
	case string(supervisor.StageReleaseArchiveNZB):
		return runtime.Indexing.ReleaseArchiveNZB, true
	case string(supervisor.StageReleasePurgeArchivedSources):
		return runtime.Indexing.ReleasePurgeArchivedSources, true
	case string(supervisor.StageInspectDiscoveryReadyRefresh):
		return runtime.Indexing.InspectDiscoveryReadyRefresh, true
	case string(supervisor.StageInspectPAR2ReadyRefresh):
		return runtime.Indexing.InspectPAR2ReadyRefresh, true
	case string(supervisor.StageInspectArchiveReadyRefresh):
		return runtime.Indexing.InspectArchiveReadyRefresh, true
	case string(supervisor.StageInspectMediaReadyRefresh):
		return runtime.Indexing.InspectMediaReadyRefresh, true
	case string(supervisor.StageInspectDiscovery):
		return runtime.Indexing.InspectDiscovery, true
	case string(supervisor.StageInspectPAR2):
		return runtime.Indexing.InspectPAR2, true
	case string(supervisor.StageInspectNFO):
		return runtime.Indexing.InspectNFO, true
	case string(supervisor.StageInspectArchive):
		return runtime.Indexing.InspectArchive, true
	case string(supervisor.StageInspectPassword):
		return runtime.Indexing.InspectPassword, true
	case string(supervisor.StageInspectMedia):
		return runtime.Indexing.InspectMedia, true
	case string(supervisor.StageEnrichPreDB):
		return app.IndexingStageRuntimeSettings{
			Enabled:         runtime.Indexing.EnrichPreDB.Enabled,
			IntervalMinutes: runtime.Indexing.EnrichPreDB.IntervalMinutes,
			BatchSize:       runtime.Indexing.EnrichPreDB.BatchSize,
			BackoffSeconds:  runtime.Indexing.EnrichPreDB.BackoffSeconds,
		}, true
	case string(supervisor.StageEnrichTMDB):
		return app.IndexingStageRuntimeSettings{
			Enabled:         runtime.Indexing.EnrichTMDB.Enabled,
			IntervalMinutes: runtime.Indexing.EnrichTMDB.IntervalMinutes,
			BatchSize:       runtime.Indexing.EnrichTMDB.BatchSize,
			BackoffSeconds:  runtime.Indexing.EnrichTMDB.BackoffSeconds,
		}, true
	case string(supervisor.StageMaintenance):
		return app.IndexingStageRuntimeSettings{
			Enabled:         true,
			IntervalMinutes: 360,
			BatchSize:       0,
			BackoffSeconds:  0,
		}, true
	default:
		return app.IndexingStageRuntimeSettings{}, false
	}
}

var allIndexerStages = []string{
	string(supervisor.StageScrapeLatest),
	string(supervisor.StageScrapeBackfill),
	string(supervisor.StagePosterMaterialize),
	string(supervisor.StageCrosspostPopularityRefresh),
	string(supervisor.StageAssemble),
	string(supervisor.StageRecoverYEnc),
	string(supervisor.StageReleaseSummaryRefresh),
	string(supervisor.StageRelease),
	string(supervisor.StageReleaseGenerateNZB),
	string(supervisor.StageReleaseArchiveNZB),
	string(supervisor.StageInspectDiscoveryReadyRefresh),
	string(supervisor.StageInspectPAR2ReadyRefresh),
	string(supervisor.StageInspectArchiveReadyRefresh),
	string(supervisor.StageInspectMediaReadyRefresh),
	string(supervisor.StageInspectDiscovery),
	string(supervisor.StageInspectPAR2),
	string(supervisor.StageInspectNFO),
	string(supervisor.StageInspectArchive),
	string(supervisor.StageInspectPassword),
	string(supervisor.StageInspectMedia),
	string(supervisor.StageEnrichPreDB),
	string(supervisor.StageEnrichTMDB),
	string(supervisor.StageMaintenance),
}

type maintenanceTaskDefinition struct {
	TaskKey          string
	Label            string
	Purpose          string
	Risk             string
	SpaceEffect      string
	SupervisorEffect string
	DataEffect       string
	ReleaseSafety    string
	Destructive      bool
	UsesBatchSize    bool
	MinIntervalHours int
	Warnings         []string
}

var indexerMaintenanceTaskDefinitions = []maintenanceTaskDefinition{
	{TaskKey: "dashboard_stats_refresh", Label: "Dashboard Stats Refresh", Purpose: "Refreshes exact admin dashboard backlog counts into the cached dashboard stats table.", Risk: "low", SpaceEffect: "No storage reclaim; updates cached dashboard counters.", SupervisorEffect: "No pipeline work is enqueued.", DataEffect: "Rewrites dashboard stat cache rows only.", ReleaseSafety: "No source, binary, catalog, or NZB data is deleted.", Destructive: false, UsesBatchSize: false, MinIntervalHours: 1},
	{TaskKey: "vacuum_dead_tuple_tables", Label: "Vacuum Dead Tuple Tables", Purpose: "Runs plain VACUUM (ANALYZE) on tables with meaningful dead-tuple counts.", Risk: "low", SpaceEffect: "Makes dead-tuple space reusable inside PostgreSQL and refreshes planner stats; OS space is not returned without VACUUM FULL, pg_repack, CLUSTER, or a table rewrite.", SupervisorEffect: "No stage behavior changes and no pipeline work is enqueued; it can add I/O while running.", DataEffect: "Deletes no application rows and changes only PostgreSQL maintenance metadata/free-space maps/statistics.", ReleaseSafety: "No source, binary, catalog, or NZB data is deleted.", Destructive: false, UsesBatchSize: true, MinIntervalHours: 6, Warnings: []string{"batch size is the maximum number of tables vacuumed per run"}},
	{TaskKey: "release_source_purge", Label: "Release Source Purge", Purpose: "Purges archived release binary/source lineage after durable NZB archival.", Risk: "high", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Terminal cleanup after release archive; source rows cannot rebuild the purged release.", DataEffect: "Deletes release lineage, safe binary source rows, article source rows, and legacy NZB cache rows.", ReleaseSafety: "Requires durable archived NZB state, release catalog files, and release readiness gates before source deletion.", Destructive: true, UsesBatchSize: false, MinIntervalHours: 6},
	{TaskKey: "poster_queue_done_cleanup", Label: "Poster Queue Done Cleanup", Purpose: "Deletes poster materialization queue rows that have already completed.", Risk: "low", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Poster materialize keeps only pending queue state.", DataEffect: "Deletes completed poster queue rows only.", ReleaseSafety: "Does not delete headers, binaries, release files, catalog data, or archived NZBs.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6},
	{TaskKey: "inspect_ready_queue_cleanup", Label: "Inspect Ready Queue Cleanup", Purpose: "Deletes completed or blocked inspect ready-queue rows once inspection history exists.", Risk: "low", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Inspect refresh stages may repopulate only if source inspections require work.", DataEffect: "Deletes ready-queue rows after durable inspection history exists.", ReleaseSafety: "Keeps binary_inspections, release details, catalog files, and archive metadata intact.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6},
	{TaskKey: "assembly_queue_stale_cleanup", Label: "Assembly Queue Stale Cleanup", Purpose: "Deletes assembly queue rows already represented by binary parts.", Risk: "low", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Assemble skips queue entries already represented by binary parts.", DataEffect: "Deletes queue residue only; raw headers and payloads remain.", ReleaseSafety: "Does not delete source headers, payloads, binaries, or release lineage.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6},
	{TaskKey: "readiness_cleanup", Label: "Readiness Cleanup", Purpose: "Cleans processed stale release readiness residue.", Risk: "medium", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Readiness summaries may be recomputed from retained source/projection rows.", DataEffect: "Deletes stale derived readiness rows and orphan release shells.", ReleaseSafety: "Safe only while raw source, binary projections, public release detail, and archive state are retained.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6, Warnings: []string{"v1 run delegates to the existing bounded indexer maintenance cleanup path"}},
	{TaskKey: "runtime_history_cleanup", Label: "Runtime History Cleanup", Purpose: "Purges old stage, scrape, and inspection run history.", Risk: "low", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "No stage behavior changes; old run/debug history is shortened.", DataEffect: "Deletes old operational history rows only.", ReleaseSafety: "No source, binary, catalog, or NZB data is deleted.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6},
	{TaskKey: "grouping_evidence_cleanup", Label: "Grouping Evidence Cleanup", Purpose: "Purges stale stable grouping evidence side-table rows.", Risk: "medium", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Grouping/release-family debug evidence is reduced; current identity projections remain.", DataEffect: "Deletes older stable side-table evidence rows.", ReleaseSafety: "Does not delete current binary identity, source headers, catalog files, or NZBs.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6},
	{TaskKey: "crosspost_group_raw_purge", Label: "Crosspost Raw Group Purge", Purpose: "Purges raw Xref crosspost observations after the popularity summary watermark has consumed them.", Risk: "medium", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Crosspost popularity refresh keeps summary and queue state; historical raw Xref observations older than 72h are no longer available for recompute/debug.", DataEffect: "Deletes only article_header_crosspost_groups rows older than 72h whose group queue is done and whose article id is at or below the summary watermark.", ReleaseSafety: "Current release formation uses binary identity/release family evidence and persists release_newsgroups from binaries; this task does not delete source headers, binaries, releases, catalog files, or NZBs.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6, Warnings: []string{"medium risk because raw Xref forensic telemetry is removed after summary consumption"}},
	{TaskKey: "yenc_done_work_item_cleanup", Label: "yEnc Done Work Item Cleanup", Purpose: "Purges completed recover_yenc work receipts after durable yEnc recovery projection exists.", Risk: "medium", SpaceEffect: "DB-internal cleanup followed by normal VACUUM (ANALYZE); OS space is not returned until vacuum full/table rewrite.", SupervisorEffect: "recover_yenc keeps ready/running backlog intact; completed receipts older than 72h are no longer available for queue audit.", DataEffect: "Deletes only yenc_recovery_work_items rows with status done, older than 72h, backed by binary_recovery_current recovered_source yenc_header, and not referenced by release/archive/running inspect work.", ReleaseSafety: "Does not delete article headers, payloads, binary roots, recovery projections, release files, archive lineage, catalog data, or NZBs.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6, Warnings: []string{"medium risk because completed yEnc queue audit receipts are removed after durable projection exists"}},
	{TaskKey: "group_profile_refresh", Label: "Group Profile Refresh", Purpose: "Scores provider/newsgroup yield and refreshes automatic hot/warm/cold tiering when no manual tier override is set.", Risk: "low", SpaceEffect: "No storage reclaim; updates group profile metrics and tiers.", SupervisorEffect: "Scrape/yEnc admission uses refreshed tiers for capacity decisions.", DataEffect: "Updates indexer_group_profiles metrics, score, tier, and last_scored_at only.", ReleaseSafety: "No source, binary, release, catalog, or NZB data is deleted.", Destructive: false, UsesBatchSize: false, MinIntervalHours: 1},
	{TaskKey: "raw_stage_retention", Label: "Raw Stage Retention", Purpose: "Purges old terminal raw-stage residue using tier-aware hot/warm/cold source windows and conservative relationship guards.", Risk: "high", SpaceEffect: "DB-internal cleanup followed by normal VACUUM (ANALYZE); OS space is not returned until vacuum full/table rewrite.", SupervisorEffect: "Reduces stale raw source/yEnc audit backlog without touching ready/running work.", DataEffect: "Deletes terminal yEnc receipts and fully orphaned old article headers with associated payload/crosspost/poster rows.", ReleaseSafety: "Skips ready/running yEnc work, assembly queue rows, binary parts, release files, archive lineage, and running inspections.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 24, Warnings: []string{"high risk: use dry-run first", "retention thresholds come from runtime indexer retention settings"}},
	{TaskKey: "inspect_workspace_cleanup", Label: "Inspect Workspace Cleanup", Purpose: "Cleans stale inspect workspaces.", Risk: "low", SpaceEffect: "Filesystem cleanup for stale inspect workspaces when scheduled maintenance runs it.", SupervisorEffect: "No DB queue population; stale workspaces may be recreated by future inspect jobs.", DataEffect: "Deletes stale temporary inspect workspaces, not release/catalog DB rows.", ReleaseSafety: "No source, binary, catalog, or NZB database rows are deleted.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6, Warnings: []string{"filesystem cleanup is executed by the scheduled indexer_maintenance service in this build"}},
	{TaskKey: "stale_nonrelease_source_purge", Label: "Stale Non-Release Source Purge", Purpose: "Purges old source headers outside the default 7-day window only when no downstream relationship still references them.", Risk: "high", SpaceEffect: "DB-internal cleanup followed by normal VACUUM (ANALYZE); OS space is not returned until vacuum full/table rewrite.", SupervisorEffect: "Deletes source rows that are outside the active scrape window and not queued for assemble or yEnc; future releases cannot be formed from those purged source rows.", DataEffect: "Deletes eligible article_headers and cascades their ingest payload, crosspost, poster-ref, and poster queue rows.", ReleaseSafety: "Skips any header with assembly queue, binary_parts, yEnc work item, or archive lineage. Dry-run estimates only the next batch.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 24, Warnings: []string{"high risk: use dry-run first and keep batch sizes conservative", "does not delete binaries, release files, catalog files, archive metadata, or NZBs directly"}},
	{TaskKey: "emergency_source_window_reset", Label: "Emergency Source Window Reset", Purpose: "Discards old unreleased binary/source work outside the 7-day active window when a scrape backlog bypassed retention and disk pressure requires an emergency reset.", Risk: "high", SpaceEffect: "DB-internal cleanup followed by normal VACUUM (ANALYZE); OS space is returned only by CLI/DBA table rewrite commands such as VACUUM FULL or pg_repack.", SupervisorEffect: "Removes old unformed binary/yEnc/source work so assemble, recover_yenc, inspect, and release formation stop spending time on stale backlog.", DataEffect: "Deletes eligible binary_core roots and cascades binary parts, yEnc receipts, binary identity/current projections, inspection rows, and other binary-derived rows; then deletes fully orphaned old article headers and their payload/crosspost/poster rows.", ReleaseSafety: "Skips binaries referenced by release_files or release archive lineage, skips running inspect/yEnc work, and does not delete release detail, release catalog files, archive metadata, or archived NZBs.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 24, Warnings: []string{"high risk: future releases cannot be formed from purged old source/binary rows", "dry-run first; dry-run uses a rollback-only transaction to estimate cascades", "manual or emergency scheduling only"}},
	{TaskKey: "header_payload_purge", Label: "Header Payload Purge", Purpose: "Runs the existing aged article-header payload purge.", Risk: "high", SpaceEffect: "DB-internal cleanup; OS space is not returned until vacuum/table rewrite.", SupervisorEffect: "Can reduce yEnc recovery and forensic source detail for already assembled rows.", DataEffect: "Deletes retained article_header_ingest_payloads matching the aged assembled-row predicate.", ReleaseSafety: "Keep manual until archive/source lineage gates are verified for the affected window.", Destructive: true, UsesBatchSize: true, MinIntervalHours: 6, Warnings: []string{"disabled by default"}},
}

func parseMaintenanceTask(taskKey string) (maintenanceTaskDefinition, error) {
	taskKey = strings.ToLower(strings.TrimSpace(taskKey))
	for _, def := range indexerMaintenanceTaskDefinitions {
		if def.TaskKey == taskKey {
			return def, nil
		}
	}
	return maintenanceTaskDefinition{}, fmt.Errorf("unknown maintenance task %q", taskKey)
}

func maintenanceTaskStageName(taskKey string) string {
	return "maintenance." + taskKey
}

func maintenanceTaskConfig(runtime *app.RuntimeSettings, taskKey string) app.IndexingMaintenanceTaskRuntimeSettings {
	defaults := app.DefaultRuntimeSettings()
	cfg := defaults.Indexing.MaintenanceTasks[taskKey]
	if runtime != nil && runtime.Indexing != nil {
		withDefaults := app.WithRuntimeDefaults(runtime)
		if withDefaults != nil && withDefaults.Indexing != nil {
			if override, ok := withDefaults.Indexing.MaintenanceTasks[taskKey]; ok {
				cfg = override
			}
		}
	}
	return cfg
}

func rawStageRetentionPolicyFromRuntime(runtime *app.RuntimeSettings) pgindex.RawStageRetentionPolicy {
	defaults := app.DefaultRuntimeSettings()
	retention := defaults.Indexing.Retention
	if runtime != nil && runtime.Indexing != nil {
		retention = runtime.Indexing.Retention
	}
	return pgindex.RawStageRetentionPolicy{
		HotHours:         retention.RawStageHotHours,
		WarmHours:        retention.RawStageWarmHours,
		ColdHours:        retention.RawStageColdHours,
		FailedProbeHours: retention.FailedProbeHours,
		DoneYEncHours:    retention.RawStageWarmHours,
	}
}

func maintenanceTaskViewFromSettings(def maintenanceTaskDefinition, runtime *app.RuntimeSettings) indexerMaintenanceTaskView {
	cfg := maintenanceTaskConfig(runtime, def.TaskKey)
	return indexerMaintenanceTaskView{
		TaskKey:          def.TaskKey,
		Label:            def.Label,
		Purpose:          def.Purpose,
		Risk:             def.Risk,
		SpaceEffect:      def.SpaceEffect,
		SupervisorEffect: def.SupervisorEffect,
		DataEffect:       def.DataEffect,
		ReleaseSafety:    def.ReleaseSafety,
		Destructive:      def.Destructive,
		Enabled:          cfg.Enabled,
		ScheduleEnabled:  cfg.ScheduleEnabled,
		IntervalHours:    cfg.IntervalHours,
		MinIntervalHours: maintenanceTaskMinIntervalHours(def),
		UsesBatchSize:    def.UsesBatchSize,
		BatchSize:        cfg.BatchSize,
		LastDryRunAt:     cfg.LastDryRunAt,
		Warnings:         append([]string(nil), def.Warnings...),
	}
}

func maintenanceTaskMinIntervalHours(def maintenanceTaskDefinition) int {
	if def.MinIntervalHours > 0 {
		return def.MinIntervalHours
	}
	return 6
}

func maintenanceTaskRunView(result *pgindex.MaintenanceTaskResult) *indexerMaintenanceTaskRunView {
	if result == nil {
		return nil
	}
	return &indexerMaintenanceTaskRunView{
		TaskKey:              result.TaskKey,
		DryRun:               result.DryRun,
		EstimatedRowsByTable: result.EstimatedRowsByTable,
		DeletedRowsByTable:   result.DeletedRowsByTable,
		VacuumedTables:       result.VacuumedTables,
		EstimatedBytes:       result.EstimatedBytes,
		BeforeStorage:        result.BeforeStorage,
		AfterStorage:         result.AfterStorage,
		Blockers:             result.Blockers,
		Warnings:             result.Warnings,
	}
}

func parseIndexerStage(stageName string) (supervisor.StageName, error) {
	stageName = normalizeStageName(stageName)
	for _, allowed := range allIndexerStages {
		if stageName == allowed {
			return supervisor.StageName(stageName), nil
		}
	}
	return "", fmt.Errorf("unknown stage %q", stageName)
}

func normalizeStageName(stageName string) string {
	return strings.ToLower(strings.TrimSpace(stageName))
}

func applyIndexerStageConfigPatch(indexing *app.IndexingRuntimeSettings, stageName string, patch indexerStageConfigPatch) error {
	if indexing == nil {
		return fmt.Errorf("indexing settings are required")
	}
	if patch.Concurrency != nil && !stageSupportsConcurrency(stageName) {
		return fmt.Errorf("stage %q does not support concurrency settings", stageName)
	}
	switch stageName {
	case string(supervisor.StageScrapeLatest):
		applyStagePatch(&indexing.ScrapeLatest, patch)
	case string(supervisor.StageScrapeBackfill):
		applyStagePatch(&indexing.ScrapeBackfill, patch)
	case string(supervisor.StagePosterMaterialize):
		applyStagePatch(&indexing.PosterMaterialize, patch)
	case string(supervisor.StageCrosspostPopularityRefresh):
		applyStagePatch(&indexing.CrosspostPopularityRefresh, patch)
	case string(supervisor.StageAssemble):
		applyStagePatch(&indexing.Assemble, patch)
	case string(supervisor.StageRecoverYEnc):
		applyStagePatch(&indexing.RecoverYEnc, patch)
	case string(supervisor.StageReleaseSummaryRefresh):
		applyStagePatch(&indexing.ReleaseSummaryRefresh, patch)
	case string(supervisor.StageRelease):
		applyReleaseStagePatch(&indexing.Release, patch)
	case string(supervisor.StageReleaseGenerateNZB):
		applyStagePatch(&indexing.ReleaseGenerateNZB, patch)
	case string(supervisor.StageReleaseArchiveNZB):
		applyStagePatch(&indexing.ReleaseArchiveNZB, patch)
	case string(supervisor.StageReleasePurgeArchivedSources):
		applyStagePatch(&indexing.ReleasePurgeArchivedSources, patch)
	case string(supervisor.StageInspectDiscoveryReadyRefresh):
		applyStagePatch(&indexing.InspectDiscoveryReadyRefresh, patch)
	case string(supervisor.StageInspectPAR2ReadyRefresh):
		applyStagePatch(&indexing.InspectPAR2ReadyRefresh, patch)
	case string(supervisor.StageInspectArchiveReadyRefresh):
		applyStagePatch(&indexing.InspectArchiveReadyRefresh, patch)
	case string(supervisor.StageInspectMediaReadyRefresh):
		applyStagePatch(&indexing.InspectMediaReadyRefresh, patch)
	case string(supervisor.StageInspectDiscovery):
		applyStagePatch(&indexing.InspectDiscovery, patch)
	case string(supervisor.StageInspectPAR2):
		applyStagePatch(&indexing.InspectPAR2, patch)
	case string(supervisor.StageInspectNFO):
		applyStagePatch(&indexing.InspectNFO, patch)
	case string(supervisor.StageInspectArchive):
		applyStagePatch(&indexing.InspectArchive, patch)
	case string(supervisor.StageInspectPassword):
		applyStagePatch(&indexing.InspectPassword, patch)
	case string(supervisor.StageInspectMedia):
		applyStagePatch(&indexing.InspectMedia, patch)
	case string(supervisor.StageEnrichPreDB):
		applyPreDBStagePatch(&indexing.EnrichPreDB, patch)
	case string(supervisor.StageEnrichTMDB):
		applyTMDBStagePatch(&indexing.EnrichTMDB, patch)
	case string(supervisor.StageMaintenance):
		return fmt.Errorf("stage %q is not runtime configurable", stageName)
	default:
		return fmt.Errorf("unknown stage %q", stageName)
	}
	return nil
}

func stageSupportsConcurrency(stageName string) bool {
	switch stageName {
	case string(supervisor.StageAssemble), string(supervisor.StageRecoverYEnc), string(supervisor.StageInspectPAR2), string(supervisor.StageInspectArchive), string(supervisor.StageInspectMedia):
		return true
	default:
		return false
	}
}

func applyStagePatch(dst *app.IndexingStageRuntimeSettings, patch indexerStageConfigPatch) {
	if patch.Enabled != nil {
		dst.Enabled = *patch.Enabled
	}
	if patch.IntervalMinutes != nil {
		dst.IntervalMinutes = *patch.IntervalMinutes
	}
	if patch.BatchSize != nil {
		dst.BatchSize = *patch.BatchSize
	}
	if patch.Concurrency != nil {
		dst.Concurrency = *patch.Concurrency
	}
	if patch.MaxEffectiveConcurrency != nil {
		dst.MaxEffectiveConcurrency = *patch.MaxEffectiveConcurrency
	}
	if patch.BackoffSeconds != nil {
		dst.BackoffSeconds = *patch.BackoffSeconds
	}
	if patch.BinaryUpsertDBChunkSize != nil {
		dst.BinaryUpsertDBChunkSize = *patch.BinaryUpsertDBChunkSize
	}
	if patch.LaneATargetPct != nil {
		dst.LaneATargetPct = *patch.LaneATargetPct
	}
	if patch.LaneBMinPct != nil {
		dst.LaneBMinPct = *patch.LaneBMinPct
	}
	if patch.LaneATimeWindowMinutes != nil {
		dst.LaneATimeWindowMinutes = *patch.LaneATimeWindowMinutes
	}
	if patch.TargetWindowEnabled != nil {
		dst.TargetWindowEnabled = *patch.TargetWindowEnabled
	}
	if patch.TargetWindowStart != nil {
		dst.TargetWindowStart = *patch.TargetWindowStart
	}
	if patch.TargetWindowEnd != nil {
		dst.TargetWindowEnd = *patch.TargetWindowEnd
	}
	if patch.TargetWindowPct != nil {
		dst.TargetWindowPct = *patch.TargetWindowPct
	}
	if patch.NewestPct != nil {
		dst.NewestPct = *patch.NewestPct
	}
}

func applyReleaseStagePatch(dst *app.IndexingReleaseRuntimeSettings, patch indexerStageConfigPatch) {
	if patch.Enabled != nil {
		dst.Enabled = *patch.Enabled
	}
	if patch.IntervalMinutes != nil {
		dst.IntervalMinutes = *patch.IntervalMinutes
	}
	if patch.BatchSize != nil {
		dst.BatchSize = *patch.BatchSize
	}
	if patch.BackoffSeconds != nil {
		dst.BackoffSeconds = *patch.BackoffSeconds
	}
}

func applyPreDBStagePatch(dst *app.IndexingPreDBRuntimeSettings, patch indexerStageConfigPatch) {
	if patch.Enabled != nil {
		dst.Enabled = *patch.Enabled
	}
	if patch.IntervalMinutes != nil {
		dst.IntervalMinutes = *patch.IntervalMinutes
	}
	if patch.BatchSize != nil {
		dst.BatchSize = *patch.BatchSize
	}
	if patch.BackoffSeconds != nil {
		dst.BackoffSeconds = *patch.BackoffSeconds
	}
}

func applyTMDBStagePatch(dst *app.IndexingTMDBRuntimeSettings, patch indexerStageConfigPatch) {
	if patch.Enabled != nil {
		dst.Enabled = *patch.Enabled
	}
	if patch.IntervalMinutes != nil {
		dst.IntervalMinutes = *patch.IntervalMinutes
	}
	if patch.BatchSize != nil {
		dst.BatchSize = *patch.BatchSize
	}
	if patch.BackoffSeconds != nil {
		dst.BackoffSeconds = *patch.BackoffSeconds
	}
}

func applyReleaseOverridePatch(dst *pgindex.ReleaseOverrideRecord, patch indexerReleaseOverridePatch) {
	if patch.DisplayTitle != nil {
		dst.DisplayTitle = strings.TrimSpace(*patch.DisplayTitle)
	}
	if patch.ClassificationOverride != nil {
		dst.ClassificationOverride = strings.TrimSpace(*patch.ClassificationOverride)
	}
	if patch.TMDBIDOverride != nil {
		dst.TMDBIDOverride = *patch.TMDBIDOverride
	}
	if patch.TVDBIDOverride != nil {
		dst.TVDBIDOverride = *patch.TVDBIDOverride
	}
	if patch.IMDBIDOverride != nil {
		dst.IMDBIDOverride = strings.TrimSpace(*patch.IMDBIDOverride)
	}
	if patch.Hidden != nil {
		dst.Hidden = *patch.Hidden
	}
	if patch.Notes != nil {
		dst.Notes = *patch.Notes
	}
	if patch.Tags != nil {
		dst.Tags = append([]string(nil), (*patch.Tags)...)
	}
}

func indexerErrorStatus(err error) int {
	switch {
	case errors.Is(err, errIndexerUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, settingsadmin.ErrUnavailable):
		return http.StatusServiceUnavailable
	case err == nil:
		return http.StatusOK
	default:
		if strings.HasPrefix(err.Error(), "unknown stage") || strings.HasSuffix(err.Error(), "is required") {
			return http.StatusBadRequest
		}
		return http.StatusInternalServerError
	}
}
