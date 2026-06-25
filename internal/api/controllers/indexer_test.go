package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type stubIndexerService struct {
	overview     *pgindex.IndexerOverview
	dashboard    *pgindex.IndexerDashboardStats
	backfill     *pgindex.IndexerBackfillProgress
	capacity     *pgindex.YEncRecoveryAdmissionSnapshot
	profiles     []pgindex.IndexerGroupProfileSummary
	deferred     []pgindex.DeferredArticleRangeSummary
	dailyBuckets []pgindex.IndexerDailyBucketSummary
	throughput   *pgindex.IndexerStageThroughput
	nntpStats    *app.NNTPRuntimeStats
	stages       []indexerStageView
	runs         []pgindex.IndexerStageRun
	run          *pgindex.IndexerStageRun
	releases     []pgindex.PublicIndexerReleaseSummary
	releaseTotal int
	release      *pgindex.PublicIndexerReleaseDetail
	adminRelease *indexerAdminReleaseView
	binary       *pgindex.IndexerBinaryDetail
	file         *pgindex.IndexerFileDetail
	runErr       error
	reinspectID  string
	reenrichID   string
}

func (s *stubIndexerService) Overview(ctx context.Context) (*pgindex.IndexerOverview, error) {
	return s.overview, nil
}

func (s *stubIndexerService) DashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error) {
	return s.dashboard, nil
}

func (s *stubIndexerService) RefreshDashboardStats(ctx context.Context) (*pgindex.IndexerDashboardStats, error) {
	return s.dashboard, nil
}

func (s *stubIndexerService) StorageStatus(ctx context.Context) (*indexerStorageStatusView, error) {
	return &indexerStorageStatusView{DataDirectory: "/pgdata", FilesystemVisible: true, GuardEnabled: true, MinFreePercent: 15}, nil
}

func (s *stubIndexerService) StorageAudit(ctx context.Context) (*pgindex.IndexerStorageAuditReport, error) {
	return &pgindex.IndexerStorageAuditReport{}, nil
}

func (s *stubIndexerService) BackfillProgress(ctx context.Context) (*pgindex.IndexerBackfillProgress, error) {
	return s.backfill, nil
}

func (s *stubIndexerService) RecoveryCapacity(ctx context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error) {
	return s.capacity, nil
}

func (s *stubIndexerService) GroupProfiles(ctx context.Context, limit int) ([]pgindex.IndexerGroupProfileSummary, error) {
	return s.profiles, nil
}

func (s *stubIndexerService) DeferredArticleRanges(ctx context.Context, state string, limit int) ([]pgindex.DeferredArticleRangeSummary, error) {
	return s.deferred, nil
}

func (s *stubIndexerService) DailyBucketStats(ctx context.Context, limit int) ([]pgindex.IndexerDailyBucketSummary, error) {
	return s.dailyBuckets, nil
}

func (s *stubIndexerService) StageThroughput(ctx context.Context) (*pgindex.IndexerStageThroughput, error) {
	return s.throughput, nil
}

func (s *stubIndexerService) NNTPStats(ctx context.Context) (*app.NNTPRuntimeStats, error) {
	return s.nntpStats, nil
}

func (s *stubIndexerService) ListStages(ctx context.Context) ([]indexerStageView, error) {
	return s.stages, nil
}

func (s *stubIndexerService) ListRuns(ctx context.Context, params pgindex.IndexerStageRunListParams) ([]pgindex.IndexerStageRun, error) {
	return s.runs, nil
}

func (s *stubIndexerService) GetRun(ctx context.Context, runID int64) (*pgindex.IndexerStageRun, error) {
	if s.run != nil {
		return s.run, nil
	}
	for i := range s.runs {
		if s.runs[i].ID == runID {
			run := s.runs[i]
			return &run, nil
		}
	}
	return nil, nil
}

func (s *stubIndexerService) GetStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	if len(s.stages) == 0 {
		return &indexerStageView{StageName: stageName}, nil
	}
	stage := s.stages[0]
	return &stage, nil
}

func (s *stubIndexerService) RunStage(ctx context.Context, stageName string) error {
	return s.runErr
}

func (s *stubIndexerService) PauseStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	if len(s.stages) == 0 {
		return &indexerStageView{StageName: stageName, Paused: true}, nil
	}
	stage := s.stages[0]
	stage.Paused = true
	return &stage, nil
}

func (s *stubIndexerService) ResumeStage(ctx context.Context, stageName string) (*indexerStageView, error) {
	if len(s.stages) == 0 {
		return &indexerStageView{StageName: stageName}, nil
	}
	stage := s.stages[0]
	stage.Paused = false
	return &stage, nil
}

func (s *stubIndexerService) UpdateStageConfig(ctx context.Context, stageName string, patch indexerStageConfigPatch) (*indexerStageView, error) {
	if len(s.stages) == 0 {
		return &indexerStageView{StageName: stageName}, nil
	}
	stage := s.stages[0]
	if patch.Enabled != nil {
		stage.Enabled = *patch.Enabled
	}
	if patch.IntervalMinutes != nil {
		stage.IntervalSeconds = int(*patch.IntervalMinutes * 60)
	}
	if patch.BatchSize != nil {
		stage.BatchSize = *patch.BatchSize
	}
	if patch.Concurrency != nil {
		stage.Concurrency = *patch.Concurrency
	}
	if patch.BackoffSeconds != nil {
		stage.BackoffSeconds = *patch.BackoffSeconds
	}
	return &stage, nil
}

func (s *stubIndexerService) ListMaintenanceTasks(ctx context.Context) ([]indexerMaintenanceTaskView, error) {
	return []indexerMaintenanceTaskView{{TaskKey: "release_source_purge", Label: "Release Source Purge"}}, nil
}

func (s *stubIndexerService) DryRunMaintenanceTask(ctx context.Context, taskKey string) (*indexerMaintenanceTaskRunView, error) {
	return &indexerMaintenanceTaskRunView{TaskKey: taskKey, DryRun: true, EstimatedRowsByTable: map[string]int64{}}, nil
}

func (s *stubIndexerService) RunMaintenanceTask(ctx context.Context, taskKey string) (*indexerMaintenanceTaskRunView, error) {
	return &indexerMaintenanceTaskRunView{TaskKey: taskKey, DryRun: false, DeletedRowsByTable: map[string]int64{}}, nil
}

func (s *stubIndexerService) UpdateMaintenanceTask(ctx context.Context, taskKey string, patch indexerMaintenanceTaskPatch) (*indexerMaintenanceTaskView, error) {
	return &indexerMaintenanceTaskView{TaskKey: taskKey}, nil
}

func (s *stubIndexerService) ListReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error) {
	return s.releases, s.releaseTotal, nil
}

func (s *stubIndexerService) GetRelease(ctx context.Context, releaseID string) (*pgindex.PublicIndexerReleaseDetail, error) {
	return s.release, nil
}

func (s *stubIndexerService) ListAdminReleases(ctx context.Context, params pgindex.AdminIndexerReleaseListParams) ([]pgindex.IndexerReleaseSummary, int, error) {
	return nil, 0, nil
}

func (s *stubIndexerService) GetAdminRelease(ctx context.Context, releaseID string) (*indexerAdminReleaseView, error) {
	return s.adminRelease, nil
}

func (s *stubIndexerService) UpdateReleaseOverride(ctx context.Context, releaseID string, patch indexerReleaseOverridePatch) (*pgindex.ReleaseOverrideRecord, error) {
	return &pgindex.ReleaseOverrideRecord{ReleaseID: releaseID}, nil
}

func (s *stubIndexerService) ReinspectRelease(ctx context.Context, releaseID string) error {
	s.reinspectID = releaseID
	return nil
}

func (s *stubIndexerService) ReenrichRelease(ctx context.Context, releaseID string) error {
	s.reenrichID = releaseID
	return nil
}

func (s *stubIndexerService) GetBinary(ctx context.Context, binaryID int64) (*pgindex.IndexerBinaryDetail, error) {
	return s.binary, nil
}

func (s *stubIndexerService) ListBinaries(ctx context.Context, params pgindex.IndexerBinaryListParams) ([]pgindex.IndexerBinarySummary, int, error) {
	return nil, 0, nil
}

func (s *stubIndexerService) GetFile(ctx context.Context, fileID int64) (*pgindex.IndexerFileDetail, error) {
	return s.file, nil
}

func TestIndexerControllerGetOverviewMarksResponseAsInternalDebug(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/overview", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			overview: &pgindex.IndexerOverview{ReleaseCount: 5},
		},
	}

	if err := ctrl.GetOverview(c); err != nil {
		t.Fatalf("GetOverview returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	if !strings.Contains(rec.Body.String(), `"release_count":5`) {
		t.Fatalf("expected overview payload, got %s", rec.Body.String())
	}
}

func TestIndexerControllerListStages(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/stages", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			stages: []indexerStageView{
				{StageName: "inspect_archive", Enabled: true, Paused: false},
			},
		},
	}

	if err := ctrl.ListStages(c); err != nil {
		t.Fatalf("ListStages returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload struct {
		Count int `json:"count"`
		Items []struct {
			StageName string `json:"stage_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("expected count 1, got %d", payload.Count)
	}
	if len(payload.Items) != 1 || payload.Items[0].StageName != "inspect_archive" {
		t.Fatalf("unexpected items payload: %s", rec.Body.String())
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
}

func TestIndexerControllerListRunsMarksResponseAsInternalDebug(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/runs?stage=inspect_archive&limit=5", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			runs: []pgindex.IndexerStageRun{{ID: 11, StageName: "inspect_archive", Status: "completed"}},
		},
	}

	if err := ctrl.ListRuns(c); err != nil {
		t.Fatalf("ListRuns returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"count":1`, `"stage":"inspect_archive"`, `"stage_name":"inspect_archive"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerAdminControllerGetDashboardStats(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/indexer/overview/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerAdminController{
		Service: &stubIndexerService{
			dashboard: &pgindex.IndexerDashboardStats{
				Items: []pgindex.IndexerDashboardStat{{
					Key:       "unassembled_headers",
					Label:     "Unassembled Headers",
					Value:     42,
					Available: true,
					Exact:     true,
				}},
				Count: 1,
			},
		},
	}

	if err := ctrl.GetDashboardStats(c); err != nil {
		t.Fatalf("GetDashboardStats returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"count":1`, `"key":"unassembled_headers"`, `"value":42`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerAdminControllerGetBackfillProgress(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/indexer/overview/backfill-progress", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerAdminController{
		Service: &stubIndexerService{
			backfill: &pgindex.IndexerBackfillProgress{
				Items: []pgindex.IndexerBackfillProgressItem{{
					GroupName:                   "alt.binaries.wood",
					CutoffReached:               true,
					BackfillCursorArticleNumber: 12345,
					LatestArticleNumber:         67890,
					ProviderCount:               2,
				}},
				Count: 1,
			},
		},
	}

	if err := ctrl.GetBackfillProgress(c); err != nil {
		t.Fatalf("GetBackfillProgress returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"count":1`, `"group_name":"alt.binaries.wood"`, `"cutoff_reached":true`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerAdminControllerGetNNTPStats(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/indexer/overview/nntp", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerAdminController{
		Service: &stubIndexerService{
			nntpStats: &app.NNTPRuntimeStats{
				Scope:          "indexer",
				Policy:         "wait_queue",
				Capacity:       40,
				Active:         4,
				Waiting:        2,
				WaitDurationMS: 123,
				Providers: []app.NNTPProviderRuntimeStats{{
					ID:       "primary",
					Label:    "news.example",
					Capacity: 40,
					Active:   4,
					Dials:    8,
				}},
				Scopes: []app.NNTPScopeRuntimeStats{{
					Scope:   "inspect_par2",
					Active:  1,
					Waiting: 1,
					XOver:   0,
				}},
			},
		},
	}

	if err := ctrl.GetNNTPStats(c); err != nil {
		t.Fatalf("GetNNTPStats returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"scope":"indexer"`, `"policy":"wait_queue"`, `"capacity":40`, `"waiting":2`, `"label":"news.example"`, `"scope":"inspect_par2"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerAdminControllerStreamOverview(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/indexer/overview/stream", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerAdminController{
		Service: &stubIndexerService{
			nntpStats: &app.NNTPRuntimeStats{
				Scope:    "indexer",
				Policy:   "shared",
				Capacity: 40,
				Active:   6,
			},
			throughput: &pgindex.IndexerStageThroughput{
				Items: []pgindex.IndexerStageThroughputItem{{
					StageName: "scrape_latest",
					Label:     "Scrape Latest",
					ItemLabel: "headers",
					Windows: []pgindex.IndexerStageThroughputWindow{{
						WindowHours:      1,
						CompletedRuns:    3,
						ItemsProcessed:   12000,
						ItemsPerSecond:   100,
						AvgWorkersUsed:   8,
						MaxWorkersUsed:   8,
						MaxRangesFetched: 8,
					}},
				}},
				Count: 1,
			},
		},
	}

	cancelledCtx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(cancelledCtx)
	c.SetRequest(req)

	done := make(chan error, 1)
	go func() {
		done <- ctrl.StreamOverview(c)
	}()

	deadline := time.After(2 * time.Second)
	for {
		body := rec.Body.String()
		if strings.Contains(body, "event: overview") && strings.Contains(body, `"policy":"shared"`) {
			cancel()
			err := <-done
			if err != nil {
				t.Fatalf("StreamOverview returned error: %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			if got := rec.Header().Get(echo.HeaderContentType); got != "text/event-stream" {
				t.Fatalf("expected content type text/event-stream, got %q", got)
			}
			if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
				t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
			}
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("StreamOverview returned error before emitting event: %v", err)
			}
			t.Fatalf("StreamOverview exited before emitting event")
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("timed out waiting for overview stream event; body=%q", rec.Body.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestIndexerAdminControllerGetStageThroughput(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/indexer/overview/throughput", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerAdminController{
		Service: &stubIndexerService{
			throughput: &pgindex.IndexerStageThroughput{
				Items: []pgindex.IndexerStageThroughputItem{{
					StageName: "assemble",
					Label:     "Assemble",
					ItemLabel: "headers",
					Windows: []pgindex.IndexerStageThroughputWindow{{
						WindowHours:        1,
						CompletedRuns:      2,
						ItemsProcessed:     20000,
						ItemsPerSecond:     500,
						AvgWorkersUsed:     8,
						MaxWorkersUsed:     12,
						AvgGroupsScheduled: 10,
						MaxGroupsScheduled: 16,
						AvgRangesFetched:   10,
						MaxRangesFetched:   16,
					}},
				}},
				Count: 1,
			},
		},
	}

	if err := ctrl.GetStageThroughput(c); err != nil {
		t.Fatalf("GetStageThroughput returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"count":1`, `"stage_name":"assemble"`, `"items_per_second":500`, `"max_workers_used":12`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerAdminControllerReinspectReleaseAccepted(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/indexer/releases/rel-1/actions/reinspect", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/admin/indexer/releases/:id/actions/reinspect")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "rel-1"}})

	svc := &stubIndexerService{}
	ctrl := &IndexerAdminController{Service: svc}
	if err := ctrl.ReinspectRelease(c); err != nil {
		t.Fatalf("ReinspectRelease returned error: %v", err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", rec.Code)
	}
	if svc.reinspectID != "rel-1" {
		t.Fatalf("expected reinspect release id rel-1, got %q", svc.reinspectID)
	}
	if !strings.Contains(rec.Body.String(), `"action":"reinspect"`) {
		t.Fatalf("expected reinspect action response, got %s", rec.Body.String())
	}
}

func TestIndexerControllerRunStageAccepted(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/indexer/stages/inspect_archive/run", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/stages/:stage/run")
	c.SetPathValues(echo.PathValues{{Name: "stage", Value: "inspect_archive"}})

	ctrl := &IndexerController{Service: &stubIndexerService{}}
	if err := ctrl.RunStage(c); err != nil {
		t.Fatalf("RunStage returned error: %v", err)
	}

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	if !strings.Contains(rec.Body.String(), `"stage_name":"inspect_archive"`) {
		t.Fatalf("expected stage_name in response, got %s", rec.Body.String())
	}
}

func TestIndexerControllerPauseStageReturnsUpdatedState(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/indexer/stages/inspect_archive/pause", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/stages/:stage/pause")
	c.SetPathValues(echo.PathValues{{Name: "stage", Value: "inspect_archive"}})

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			stages: []indexerStageView{
				{StageName: "inspect_archive", Enabled: true, Paused: false},
			},
		},
	}

	if err := ctrl.PauseStage(c); err != nil {
		t.Fatalf("PauseStage returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"action":"pause"`) || !strings.Contains(body, `"paused":true`) {
		t.Fatalf("expected pause response payload, got %s", body)
	}
}

func TestIndexerControllerResumeStageReturnsUpdatedState(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/indexer/stages/inspect_archive/resume", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/stages/:stage/resume")
	c.SetPathValues(echo.PathValues{{Name: "stage", Value: "inspect_archive"}})

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			stages: []indexerStageView{
				{StageName: "inspect_archive", Enabled: true, Paused: true},
			},
		},
	}

	if err := ctrl.ResumeStage(c); err != nil {
		t.Fatalf("ResumeStage returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"action":"resume"`) || !strings.Contains(body, `"paused":false`) {
		t.Fatalf("expected resume response payload, got %s", body)
	}
}

func TestIndexerControllerGetBinaryRejectsInvalidID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/binaries/not-a-number", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/binaries/:id")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "not-a-number"}})

	ctrl := &IndexerController{Service: &stubIndexerService{}}
	if err := ctrl.GetBinary(c); err != nil {
		t.Fatalf("GetBinary returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestIndexerControllerListReleasesReturnsStablePublicContract(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/releases?q=example&limit=10&offset=5", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			releaseTotal: 1,
			releases: []pgindex.PublicIndexerReleaseSummary{{
				ReleaseID:         "rel-1",
				GUID:              "guid-1",
				Title:             "Example Feature 1963 1080p BluRay x265-GROUP",
				SizeBytes:         1_500_000_000,
				FileCount:         3,
				CompletionPct:     100,
				Classification:    "video",
				HasPAR2:           true,
				HasNFO:            true,
				PasswordState:     "passworded_known",
				AvailabilityScore: 100,
				AvailabilityTier:  "excellent",
				MediaQualityScore: 90,
				MediaQualityTier:  "premium",
				TMDBID:            123,
				TVDBID:            456,
				ExternalMediaType: "movie",
				ExternalTitle:     "Example Feature",
				ExternalYear:      1963,
			}},
		},
	}

	if err := ctrl.ListReleases(c); err != nil {
		t.Fatalf("ListReleases returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopePublic {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopePublic, got)
	}

	body := rec.Body.String()
	for _, needle := range []string{
		`"count":1`,
		`"total":1`,
		`"limit":10`,
		`"offset":5`,
		`"q":"example"`,
		`"sort":"posted_at_desc"`,
		`"release_id":"rel-1"`,
		`"guid":"guid-1"`,
		`"password_state":"passworded_known"`,
		`"tmdb_id":123`,
		`"external_media_type":"movie"`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
	for _, needle := range []string{
		`"release_key"`,
		`"matched_media_title"`,
		`"season_number"`,
		`"predb_matches"`,
		`"tmdb_matches"`,
		`"newsgroups"`,
		`"deobfuscated_title"`,
	} {
		if strings.Contains(body, needle) {
			t.Fatalf("did not expect %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerControllerGetReleaseReturnsStablePublicContract(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/releases/rel-1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/releases/:id")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "rel-1"}})

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			release: &pgindex.PublicIndexerReleaseDetail{
				Release: pgindex.PublicIndexerReleaseSummary{
					ReleaseID:         "rel-1",
					GUID:              "guid-1",
					Title:             "Example Feature 1963 1080p BluRay x265-GROUP",
					SizeBytes:         1_500_000_000,
					FileCount:         3,
					CompletionPct:     100,
					Classification:    "video",
					HasPAR2:           true,
					HasNFO:            true,
					PasswordState:     "passworded_known",
					AvailabilityScore: 100,
					AvailabilityTier:  "excellent",
					MediaQualityScore: 90,
					MediaQualityTier:  "premium",
					TMDBID:            123,
					TVDBID:            456,
					ExternalMediaType: "movie",
					ExternalTitle:     "Example Feature",
					ExternalYear:      1963,
				},
				Files: []pgindex.PublicIndexerReleaseFileSummary{{
					FileName:      "example.feature.1963.7z.001",
					SizeBytes:     500,
					FileIndex:     1,
					ArticleCount:  20,
					TotalParts:    20,
					ObservedParts: 20,
				}},
				Media: pgindex.PublicIndexerReleaseMediaSummary{
					RuntimeSeconds:    5400,
					PrimaryResolution: "1080p",
					PrimaryVideoCodec: "x265",
				},
				External: pgindex.PublicIndexerReleaseExternal{
					TMDBID:            123,
					TVDBID:            456,
					ExternalMediaType: "movie",
					ExternalTitle:     "Example Feature",
					ExternalYear:      1963,
				},
				Capabilities: pgindex.PublicIndexerReleaseCapabilities{
					CanSendToDownloader: true,
				},
			},
		},
	}

	if err := ctrl.GetRelease(c); err != nil {
		t.Fatalf("GetRelease returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopePublic {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopePublic, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{
		`"release_id":"rel-1"`,
		`"guid":"guid-1"`,
		`"password_state":"passworded_known"`,
		`"file_name":"example.feature.1963.7z.001"`,
		`"tmdb_id":123`,
		`"external_media_type":"movie"`,
		`"runtime_seconds":5400`,
		`"can_send_to_downloader":true`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
	for _, needle := range []string{
		`"release_key"`,
		`"matched_media_title"`,
		`"season_number"`,
		`"predb_matches"`,
		`"tmdb_matches"`,
		`"newsgroups"`,
		`"file_id"`,
		`"binary_id"`,
		`"subject"`,
	} {
		if strings.Contains(body, needle) {
			t.Fatalf("did not expect %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerControllerGetBinaryMarksResponseAsInternalDebug(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/binaries/42", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/binaries/:id")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "42"}})

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			binary: &pgindex.IndexerBinaryDetail{
				BinaryID:      42,
				ReleaseID:     "rel-1",
				ReleaseKey:    "debug-key",
				BinaryKey:     "binary-key",
				BinaryName:    "example.7z.001",
				FileID:        7,
				FileName:      "example.7z.001",
				MatchStatus:   "matched",
				PasswordState: "passworded_known",
			},
		},
	}

	if err := ctrl.GetBinary(c); err != nil {
		t.Fatalf("GetBinary returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"release_key":"debug-key"`, `"binary_key":"binary-key"`, `"file_id":7`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestIndexerControllerGetFileMarksResponseAsInternalDebug(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexer/files/7", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/indexer/files/:id")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "7"}})

	ctrl := &IndexerController{
		Service: &stubIndexerService{
			file: &pgindex.IndexerFileDetail{
				FileID:          7,
				ReleaseID:       "rel-1",
				BinaryID:        42,
				FileName:        "example.7z.001",
				Subject:         "Example Subject",
				MatchConfidence: 0.95,
			},
		},
	}

	if err := ctrl.GetFile(c); err != nil {
		t.Fatalf("GetFile returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(indexerContractScopeHeader); got != indexerContractScopeInternalDebug {
		t.Fatalf("expected %s header %q, got %q", indexerContractScopeHeader, indexerContractScopeInternalDebug, got)
	}
	body := rec.Body.String()
	for _, needle := range []string{`"file_id":7`, `"binary_id":42`, `"subject":"Example Subject"`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected %s in response, got %s", needle, body)
		}
	}
}

func TestRuntimeIndexerServiceNNTPStatsUsesCurrentRuntimeIndexer(t *testing.T) {
	appCtx := &app.Context{}
	service := newIndexerService(appCtx)

	first := &testRuntimeIndexerService{stats: &app.NNTPRuntimeStats{Scope: "indexer", Active: 1}}
	second := &testRuntimeIndexerService{stats: &app.NNTPRuntimeStats{Scope: "indexer", Active: 7}}
	appCtx.UsenetIndexer = first

	stats, err := service.NNTPStats(context.Background())
	if err != nil {
		t.Fatalf("NNTPStats first call error = %v", err)
	}
	if stats == nil || stats.Active != 1 {
		t.Fatalf("expected first runtime stats active=1, got %+v", stats)
	}

	appCtx.UsenetIndexer = second
	stats, err = service.NNTPStats(context.Background())
	if err != nil {
		t.Fatalf("NNTPStats second call error = %v", err)
	}
	if stats == nil || stats.Active != 7 {
		t.Fatalf("expected refreshed runtime stats active=7, got %+v", stats)
	}
}

type testSnapshotReader struct {
	snapshot *pgindex.NNTPRuntimeSnapshot
	items    []pgindex.NNTPRuntimeSnapshot
	err      error
}

func (t testSnapshotReader) GetLatestNNTPSnapshot(context.Context, string) (*pgindex.NNTPRuntimeSnapshot, error) {
	return t.snapshot, t.err
}

func (t testSnapshotReader) ListRecentNNTPSnapshots(context.Context, string, time.Time) ([]pgindex.NNTPRuntimeSnapshot, error) {
	return t.items, t.err
}

func TestRuntimeIndexerServiceNNTPStatsUsesSharedSnapshotWhenNoLocalRuntime(t *testing.T) {
	appCtx := &app.Context{}
	service := &runtimeIndexerService{
		appCtx:        appCtx,
		nntpSnapshots: testSnapshotReader{items: []pgindex.NNTPRuntimeSnapshot{{Payload: json.RawMessage(`{"scope":"indexer","active":9}`)}}},
	}

	stats, err := service.NNTPStats(context.Background())
	if err != nil {
		t.Fatalf("NNTPStats returned error: %v", err)
	}
	if stats == nil || stats.Active != 9 {
		t.Fatalf("expected shared snapshot active=9, got %+v", stats)
	}
}

func TestRuntimeIndexerServiceNNTPStatsPrefersLocalRuntimeWhenAvailable(t *testing.T) {
	appCtx := &app.Context{}
	service := &runtimeIndexerService{
		appCtx:        appCtx,
		nntpSnapshots: testSnapshotReader{items: []pgindex.NNTPRuntimeSnapshot{{Payload: json.RawMessage(`{"scope":"indexer","active":9}`)}}},
	}
	appCtx.UsenetIndexer = &testRuntimeIndexerService{stats: &app.NNTPRuntimeStats{Scope: "indexer", Active: 2}}

	stats, err := service.NNTPStats(context.Background())
	if err != nil {
		t.Fatalf("NNTPStats returned error: %v", err)
	}
	if stats == nil || stats.Active != 2 {
		t.Fatalf("expected local runtime stats active=2, got %+v", stats)
	}
}

func TestRuntimeIndexerServiceNNTPStatsAggregatesRecentSnapshots(t *testing.T) {
	appCtx := &app.Context{}
	service := &runtimeIndexerService{
		appCtx: appCtx,
		nntpSnapshots: testSnapshotReader{items: []pgindex.NNTPRuntimeSnapshot{
			{Payload: json.RawMessage(`{"scope":"indexer","policy":"wait_queue","capacity":40,"active":4,"providers":[{"id":"primary","label":"news","capacity":40,"active":4}],"scopes":[{"scope":"scrape","active":3}]}`)},
			{Payload: json.RawMessage(`{"scope":"indexer","policy":"wait_queue","capacity":40,"active":6,"providers":[{"id":"primary","label":"news","capacity":40,"active":6}],"scopes":[{"scope":"recover_yenc","active":2}]}`)},
		}},
	}

	stats, err := service.NNTPStats(context.Background())
	if err != nil {
		t.Fatalf("NNTPStats returned error: %v", err)
	}
	if stats == nil || stats.Active != 10 || stats.Capacity != 80 {
		t.Fatalf("expected aggregated stats active=10 capacity=80, got %+v", stats)
	}
	if len(stats.Providers) != 1 || stats.Providers[0].Active != 10 {
		t.Fatalf("expected merged provider stats, got %+v", stats.Providers)
	}
	if len(stats.Scopes) != 2 {
		t.Fatalf("expected merged scope stats, got %+v", stats.Scopes)
	}
}

func TestOverlayBackfillProgressFromRuntimeClearsStaleCutoff(t *testing.T) {
	progress := &pgindex.IndexerBackfillProgress{
		Items: []pgindex.IndexerBackfillProgressItem{{
			GroupName:            "alt.binaries.xray",
			ConfiguredCutoffDate: ptrTimeTest(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			CutoffReached:        true,
		}},
		Count: 1,
	}
	indexing := &app.IndexingRuntimeSettings{
		ExplicitGroups: []app.IndexingScrapeGroupRuntimeSettings{{
			GroupName:         "alt.binaries.xray",
			Enabled:           true,
			BackfillUntilDate: "2020-01-01",
		}},
	}

	overlayBackfillProgressFromRuntime(progress, indexing)

	if len(progress.Items) != 1 {
		t.Fatalf("expected one backfill item, got %+v", progress.Items)
	}
	item := progress.Items[0]
	if item.ConfiguredCutoffDate == nil || item.ConfiguredCutoffDate.Year() != 2020 {
		t.Fatalf("expected runtime cutoff date 2020, got %+v", item.ConfiguredCutoffDate)
	}
	if item.CutoffReached {
		t.Fatalf("expected stale cutoff reached state to clear after runtime cutoff change, got %+v", item)
	}
}

func ptrTimeTest(t time.Time) *time.Time { return &t }

type testRuntimeIndexerService struct {
	stats *app.NNTPRuntimeStats
}

func (t *testRuntimeIndexerService) ScrapeOnce(context.Context) error                { return nil }
func (t *testRuntimeIndexerService) ScrapeLatestOnce(context.Context) error          { return nil }
func (t *testRuntimeIndexerService) ScrapeBackfillOnce(context.Context) error        { return nil }
func (t *testRuntimeIndexerService) AssembleOnce(context.Context) error              { return nil }
func (t *testRuntimeIndexerService) RecoverYEncOnce(context.Context) error           { return nil }
func (t *testRuntimeIndexerService) ReleaseSummaryRefreshOnce(context.Context) error { return nil }
func (t *testRuntimeIndexerService) ReleaseOnce(context.Context) error               { return nil }
func (t *testRuntimeIndexerService) ReleaseGenerateNZBOnce(context.Context) error    { return nil }
func (t *testRuntimeIndexerService) ReleaseArchiveNZBOnce(context.Context) error     { return nil }
func (t *testRuntimeIndexerService) ReleasePurgeArchivedSourcesOnce(context.Context) error {
	return nil
}
func (t *testRuntimeIndexerService) ReformReleasesOnce(context.Context) error { return nil }
func (t *testRuntimeIndexerService) ReformSelectedReleasesOnce(context.Context, []string) error {
	return nil
}
func (t *testRuntimeIndexerService) InspectOnce(context.Context) error          { return nil }
func (t *testRuntimeIndexerService) InspectDiscoveryOnce(context.Context) error { return nil }
func (t *testRuntimeIndexerService) InspectPAR2Once(context.Context) error      { return nil }
func (t *testRuntimeIndexerService) InspectNFOOnce(context.Context) error       { return nil }
func (t *testRuntimeIndexerService) InspectArchiveOnce(context.Context) error   { return nil }
func (t *testRuntimeIndexerService) InspectPasswordOnce(context.Context) error  { return nil }
func (t *testRuntimeIndexerService) InspectMediaOnce(context.Context) error     { return nil }
func (t *testRuntimeIndexerService) EnrichPredbOnce(context.Context) error      { return nil }
func (t *testRuntimeIndexerService) EnrichPredbSceneNameRecoveryOnce(context.Context) error {
	return nil
}
func (t *testRuntimeIndexerService) EnrichPredbMetadataFallbackOnce(context.Context) error {
	return nil
}
func (t *testRuntimeIndexerService) EnrichPredbSyncFeedOnce(context.Context) error     { return nil }
func (t *testRuntimeIndexerService) EnrichPredbSyncBackfillOnce(context.Context) error { return nil }
func (t *testRuntimeIndexerService) EnrichTMDBOnce(context.Context) error              { return nil }
func (t *testRuntimeIndexerService) RunStageOnce(context.Context, string) error        { return nil }
func (t *testRuntimeIndexerService) RunPipelineOnce(context.Context) error             { return nil }
func (t *testRuntimeIndexerService) Start(context.Context, time.Duration) error        { return nil }
func (t *testRuntimeIndexerService) NNTPStats(context.Context) (*app.NNTPRuntimeStats, error) {
	return t.stats, nil
}
