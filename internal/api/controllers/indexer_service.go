package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
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
	ListReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error)
	GetRelease(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error)
	ListAdminReleases(ctx context.Context, params pgindex.AdminIndexerReleaseListParams) ([]pgindex.IndexerReleaseSummary, int, error)
	GetAdminRelease(ctx context.Context, releaseID string) (*indexerAdminReleaseView, error)
	UpdateReleaseOverride(ctx context.Context, releaseID string, patch indexerReleaseOverridePatch) (*pgindex.ReleaseOverrideRecord, error)
	ReinspectRelease(ctx context.Context, releaseID string) error
	ReenrichRelease(ctx context.Context, releaseID string) error
	GetReleasePreview(ctx context.Context, releaseID string) (io.ReadCloser, string, error)
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
	Enabled         *bool    `json:"enabled,omitempty"`
	IntervalMinutes *float64 `json:"interval_minutes,omitempty"`
	BatchSize       *int     `json:"batch_size,omitempty"`
	Concurrency     *int     `json:"concurrency,omitempty"`
	BackoffSeconds  *int     `json:"backoff_seconds,omitempty"`
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

type indexerAdminReleaseView struct {
	Release  *pgindex.IndexerReleaseDetail  `json:"release"`
	Override *pgindex.ReleaseOverrideRecord `json:"override,omitempty"`
	Files    []*pgindex.IndexerFileDetail   `json:"files"`
	Binaries []*pgindex.IndexerBinaryDetail `json:"binaries"`
}

type runtimeIndexerService struct {
	store           app.UsenetIndexStore
	indexer         app.UsenetIndexerService
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
		store:           appCtx.PGIndexStore,
		indexer:         appCtx.UsenetIndexer,
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
	return s.store.GetIndexerBackfillProgress(ctx)
}

func (s *runtimeIndexerService) StageThroughput(ctx context.Context) (*pgindex.IndexerStageThroughput, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	return s.store.GetIndexerStageThroughput(ctx)
}

func (s *runtimeIndexerService) NNTPStats(ctx context.Context) (*app.NNTPRuntimeStats, error) {
	if s == nil || s.indexer == nil {
		return nil, errIndexerUnavailable
	}
	return s.indexer.NNTPStats(ctx)
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
	if s == nil || s.indexer == nil {
		return errIndexerUnavailable
	}

	stage, err := parseIndexerStage(stageName)
	if err != nil {
		return err
	}

	go func(stage string) {
		if err := s.indexer.RunStageOnce(context.Background(), stage); err != nil && s.log != nil {
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
	if strings.TrimSpace(detail.Preview.ObjectKey) != "" {
		detail.Preview.URL = fmt.Sprintf("/api/v1/indexer/releases/%s/preview", strings.TrimSpace(releaseID))
	}
	return detail, nil
}

func (s *runtimeIndexerService) GetReleasePreview(ctx context.Context, releaseID string) (io.ReadCloser, string, error) {
	if s == nil || s.store == nil || s.archiveStore == nil {
		return nil, "", errIndexerUnavailable
	}
	state, err := s.store.GetReleaseArchiveState(ctx, strings.TrimSpace(releaseID))
	if err != nil {
		return nil, "", err
	}
	if state == nil || strings.TrimSpace(state.PreviewObjectKey) == "" {
		return nil, "", nil
	}
	reader, err := s.archiveStore.GetObjectReader(state.PreviewObjectKey)
	if err != nil {
		return nil, "", err
	}
	return reader, inferPreviewContentType(strings.TrimSpace(state.PreviewContentType), state.PreviewObjectKey), nil
}

func inferPreviewContentType(contentType, objectKey string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType != "" {
		return contentType
	}
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(objectKey))) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".jpeg", ".jpg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
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
		MinMatchConfidence: release.PublicMinMatchConfidence,
		MinCompletionPct:   release.PublicMinCompletionPct,
		MinIdentityStatus:  release.PublicMinIdentityStatus,
		RequireInspection:  release.PublicRequireInspection,
		RequireEnrichment:  release.PublicRequireEnrichment,
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
	if s == nil || s.store == nil || s.indexer == nil {
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
	if s == nil || s.store == nil || s.indexer == nil {
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
	for _, stage := range stages {
		if err := s.indexer.RunStageOnce(context.Background(), stage); err != nil && s.log != nil {
			s.log.Error("%s failed stage=%s err=%v", reason, stage, err)
		}
	}
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
	case string(supervisor.StageAssemble):
		return runtime.Indexing.Assemble, true
	case string(supervisor.StageAssembleLaneA):
		return runtime.Indexing.AssembleLaneA, true
	case string(supervisor.StageAssembleLaneB):
		return runtime.Indexing.AssembleLaneB, true
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
	string(supervisor.StageAssemble),
	string(supervisor.StageAssembleLaneA),
	string(supervisor.StageAssembleLaneB),
	string(supervisor.StageRecoverYEnc),
	string(supervisor.StageReleaseSummaryRefresh),
	string(supervisor.StageRelease),
	string(supervisor.StageReleaseGenerateNZB),
	string(supervisor.StageReleaseArchiveNZB),
	string(supervisor.StageReleasePurgeArchivedSources),
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
	case string(supervisor.StageAssemble):
		applyStagePatch(&indexing.Assemble, patch)
	case string(supervisor.StageAssembleLaneA):
		applyStagePatch(&indexing.AssembleLaneA, patch)
	case string(supervisor.StageAssembleLaneB):
		applyStagePatch(&indexing.AssembleLaneB, patch)
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
	case string(supervisor.StageAssemble), string(supervisor.StageAssembleLaneA), string(supervisor.StageAssembleLaneB), string(supervisor.StageRecoverYEnc), string(supervisor.StageInspectPAR2), string(supervisor.StageInspectArchive), string(supervisor.StageInspectMedia):
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
	if patch.BackoffSeconds != nil {
		dst.BackoffSeconds = *patch.BackoffSeconds
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
