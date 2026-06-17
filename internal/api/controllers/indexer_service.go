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
	BackfillProgress(ctx context.Context) (*pgindex.IndexerBackfillProgress, error)
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
	TaskKey         string                         `json:"task_key"`
	Label           string                         `json:"label"`
	Purpose         string                         `json:"purpose"`
	Destructive     bool                           `json:"destructive"`
	Enabled         bool                           `json:"enabled"`
	ScheduleEnabled bool                           `json:"schedule_enabled"`
	IntervalHours   int                            `json:"interval_hours"`
	BatchSize       int                            `json:"batch_size"`
	LastDryRunAt    string                         `json:"last_dry_run_at,omitempty"`
	LastRun         *pgindex.IndexerStageRun       `json:"last_run,omitempty"`
	LastDryRun      *indexerMaintenanceTaskRunView `json:"last_dry_run,omitempty"`
	Warnings        []string                       `json:"warnings,omitempty"`
	Blockers        []string                       `json:"blockers,omitempty"`
}

type indexerMaintenanceTaskRunView struct {
	TaskKey              string           `json:"task_key"`
	DryRun               bool             `json:"dry_run"`
	EstimatedRowsByTable map[string]int64 `json:"estimated_rows_by_table,omitempty"`
	DeletedRowsByTable   map[string]int64 `json:"deleted_rows_by_table,omitempty"`
	EstimatedBytes       int64            `json:"estimated_bytes,omitempty"`
	Blockers             []string         `json:"blockers,omitempty"`
	Warnings             []string         `json:"warnings,omitempty"`
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
		if run, ok := latestRunByStage[stageName]; ok {
			runCopy := run
			view.LatestRun = &runCopy
		}
		items = append(items, view)
	}

	return items, nil
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
	if patch.BatchSize != nil {
		cfg.BatchSize = *patch.BatchSize
	}
	if cfg.ScheduleEnabled && cfg.IntervalHours < 6 {
		return nil, fmt.Errorf("maintenance task %q scheduled interval must be at least 6 hours", def.TaskKey)
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
	DryRunSimpleMaintenanceTask(ctx context.Context, taskKey string) (*pgindex.MaintenanceTaskResult, error)
	RunSimpleMaintenanceTask(ctx context.Context, taskKey string) (*pgindex.MaintenanceTaskResult, error)
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
	case "release_source_purge":
		if dryRun {
			return store.DryRunReleaseSourcePurge(ctx, cfg.BatchSize, s.releaseReadyPolicy(ctx))
		}
		return store.RunReleaseSourcePurge(ctx, cfg.BatchSize, s.releaseReadyPolicy(ctx))
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
			return store.DryRunSimpleMaintenanceTask(ctx, taskKey)
		}
		return store.RunSimpleMaintenanceTask(ctx, taskKey)
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

	files := make([]*pgindex.IndexerFileDetail, 0, len(release.Files))
	binaries := make([]*pgindex.IndexerBinaryDetail, 0, len(release.Files))
	seenBinaryIDs := make(map[int64]struct{}, len(release.Files))
	for _, file := range release.Files {
		if file.FileID > 0 {
			fileDetail, err := s.store.GetIndexerFileDetail(ctx, file.FileID)
			if err != nil {
				return nil, err
			}
			if fileDetail != nil {
				files = append(files, fileDetail)
			}
		}
		if file.BinaryID <= 0 {
			continue
		}
		if _, ok := seenBinaryIDs[file.BinaryID]; ok {
			continue
		}
		seenBinaryIDs[file.BinaryID] = struct{}{}
		binaryDetail, err := s.store.GetIndexerBinaryDetail(ctx, file.BinaryID)
		if err != nil {
			return nil, err
		}
		if binaryDetail != nil {
			binaries = append(binaries, binaryDetail)
		}
	}

	return &indexerAdminReleaseView{
		Release:  release,
		Override: override,
		Files:    files,
		Binaries: binaries,
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
	TaskKey     string
	Label       string
	Purpose     string
	Destructive bool
	Warnings    []string
}

var indexerMaintenanceTaskDefinitions = []maintenanceTaskDefinition{
	{TaskKey: "release_source_purge", Label: "Release Source Purge", Purpose: "Purges archived release binary/source lineage after durable NZB archival.", Destructive: true},
	{TaskKey: "assembly_queue_stale_cleanup", Label: "Assembly Queue Stale Cleanup", Purpose: "Deletes assembly queue rows already represented by binary parts.", Destructive: true},
	{TaskKey: "readiness_cleanup", Label: "Readiness Cleanup", Purpose: "Cleans processed stale release readiness residue.", Destructive: true, Warnings: []string{"v1 run delegates to the existing bounded indexer maintenance cleanup path"}},
	{TaskKey: "runtime_history_cleanup", Label: "Runtime History Cleanup", Purpose: "Purges old stage, scrape, and inspection run history.", Destructive: true},
	{TaskKey: "grouping_evidence_cleanup", Label: "Grouping Evidence Cleanup", Purpose: "Purges stale stable grouping evidence side-table rows.", Destructive: true},
	{TaskKey: "inspect_workspace_cleanup", Label: "Inspect Workspace Cleanup", Purpose: "Cleans stale inspect workspaces.", Destructive: true, Warnings: []string{"filesystem cleanup is executed by the scheduled indexer_maintenance service in this build"}},
	{TaskKey: "header_payload_purge", Label: "Header Payload Purge", Purpose: "Runs the existing aged article-header payload purge.", Destructive: true, Warnings: []string{"disabled by default"}},
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

func maintenanceTaskViewFromSettings(def maintenanceTaskDefinition, runtime *app.RuntimeSettings) indexerMaintenanceTaskView {
	cfg := maintenanceTaskConfig(runtime, def.TaskKey)
	return indexerMaintenanceTaskView{
		TaskKey:         def.TaskKey,
		Label:           def.Label,
		Purpose:         def.Purpose,
		Destructive:     def.Destructive,
		Enabled:         cfg.Enabled,
		ScheduleEnabled: cfg.ScheduleEnabled,
		IntervalHours:   cfg.IntervalHours,
		BatchSize:       cfg.BatchSize,
		LastDryRunAt:    cfg.LastDryRunAt,
		Warnings:        append([]string(nil), def.Warnings...),
	}
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
		EstimatedBytes:       result.EstimatedBytes,
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
