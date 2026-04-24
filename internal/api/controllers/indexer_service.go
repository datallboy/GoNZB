package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	ListStages(ctx context.Context) ([]indexerStageView, error)
	GetStage(ctx context.Context, stageName string) (*indexerStageView, error)
	ListRuns(ctx context.Context, stageName string, limit int) ([]pgindex.IndexerStageRun, error)
	RunStage(ctx context.Context, stageName string) error
	PauseStage(ctx context.Context, stageName string) (*indexerStageView, error)
	ResumeStage(ctx context.Context, stageName string) (*indexerStageView, error)
	UpdateStageConfig(ctx context.Context, stageName string, patch indexerStageConfigPatch) (*indexerStageView, error)
	ListReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error)
	GetRelease(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error)
	ListAdminReleases(ctx context.Context, query string, limit, offset int) ([]pgindex.IndexerReleaseSummary, int, error)
	GetAdminRelease(ctx context.Context, releaseID string) (*indexerAdminReleaseView, error)
	UpdateReleaseOverride(ctx context.Context, releaseID string, patch indexerReleaseOverridePatch) (*pgindex.ReleaseOverrideRecord, error)
	ReinspectRelease(ctx context.Context, releaseID string) error
	ReenrichRelease(ctx context.Context, releaseID string) error
	GetBinary(ctx context.Context, binaryID int64) (*pgindex.IndexerBinaryDetail, error)
	GetFile(ctx context.Context, fileID int64) (*pgindex.IndexerFileDetail, error)
}

type indexerStageView struct {
	StageName       string                   `json:"stage_name"`
	Enabled         bool                     `json:"enabled"`
	Paused          bool                     `json:"paused"`
	IntervalSeconds int                      `json:"interval_seconds"`
	BatchSize       int                      `json:"batch_size"`
	Concurrency     int                      `json:"concurrency"`
	BackoffSeconds  int                      `json:"backoff_seconds"`
	LeaseOwner      string                   `json:"lease_owner"`
	LeaseExpiresAt  *time.Time               `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt *time.Time               `json:"last_heartbeat_at,omitempty"`
	LastRunID       int64                    `json:"last_run_id"`
	LastSuccessAt   *time.Time               `json:"last_success_at,omitempty"`
	LastError       string                   `json:"last_error"`
	UpdatedAt       *time.Time               `json:"updated_at,omitempty"`
	LatestRun       *pgindex.IndexerStageRun `json:"latest_run,omitempty"`
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

func (s *runtimeIndexerService) ListStages(ctx context.Context) ([]indexerStageView, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}

	states, err := s.store.ListIndexerStageStates(ctx)
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
		view := stageViewFromState(stageName, state, ok)
		if run, ok := latestRunByStage[stageName]; ok {
			runCopy := run
			view.LatestRun = &runCopy
		}
		items = append(items, view)
	}

	return items, nil
}

func (s *runtimeIndexerService) ListRuns(ctx context.Context, stageName string, limit int) ([]pgindex.IndexerStageRun, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}

	stageName = normalizeStageName(stageName)
	if stageName != "" {
		if _, err := parseIndexerStage(stageName); err != nil {
			return nil, err
		}
	}

	return s.store.ListIndexerStageRuns(ctx, stageName, limit)
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
	return s.store.ListPublicIndexerReleases(ctx, params)
}

func (s *runtimeIndexerService) GetRelease(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error) {
	if s == nil || s.store == nil {
		return nil, errIndexerUnavailable
	}
	detail, err := s.store.GetPublicIndexerReleaseDetail(ctx, strings.TrimSpace(releaseID))
	if err != nil || detail == nil {
		return detail, err
	}
	detail.Capabilities.CanSendToDownloader = s.downloaderReady
	return detail, nil
}

func (s *runtimeIndexerService) ListAdminReleases(ctx context.Context, query string, limit, offset int) ([]pgindex.IndexerReleaseSummary, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, errIndexerUnavailable
	}
	return s.store.ListIndexerReleases(ctx, strings.TrimSpace(query), limit, offset)
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

func stageViewFromState(stageName string, state pgindex.IndexerStageState, ok bool) indexerStageView {
	view := indexerStageView{
		StageName: stageName,
	}
	if !ok {
		return view
	}

	view.Enabled = state.Enabled
	view.Paused = state.Paused
	view.IntervalSeconds = state.IntervalSeconds
	view.BatchSize = state.BatchSize
	view.Concurrency = state.Concurrency
	view.BackoffSeconds = state.BackoffSeconds
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

	return view
}

var allIndexerStages = []string{
	string(supervisor.StageScrapeLatest),
	string(supervisor.StageScrapeBackfill),
	string(supervisor.StageAssemble),
	string(supervisor.StageRelease),
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
	switch stageName {
	case string(supervisor.StageScrapeLatest):
		applyStagePatch(&indexing.ScrapeLatest, patch)
	case string(supervisor.StageScrapeBackfill):
		applyStagePatch(&indexing.ScrapeBackfill, patch)
	case string(supervisor.StageAssemble):
		applyStagePatch(&indexing.Assemble, patch)
	case string(supervisor.StageRelease):
		applyReleaseStagePatch(&indexing.Release, patch)
	case string(supervisor.StageInspectDiscovery):
		applyStagePatch(&indexing.InspectArchive, patch)
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
	if patch.Concurrency != nil {
		dst.Concurrency = *patch.Concurrency
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
	if patch.Concurrency != nil {
		dst.Concurrency = *patch.Concurrency
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
	if patch.Concurrency != nil {
		dst.Concurrency = *patch.Concurrency
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
