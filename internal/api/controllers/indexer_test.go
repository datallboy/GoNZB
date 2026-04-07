package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type stubIndexerService struct {
	overview     *pgindex.IndexerOverview
	stages       []indexerStageView
	runs         []pgindex.IndexerStageRun
	releases     []pgindex.IndexerReleaseSummary
	releaseTotal int
	release      *pgindex.IndexerReleaseDetail
	binary       *pgindex.IndexerBinaryDetail
	file         *pgindex.IndexerFileDetail
	runErr       error
}

func (s *stubIndexerService) Overview(ctx context.Context) (*pgindex.IndexerOverview, error) {
	return s.overview, nil
}

func (s *stubIndexerService) ListStages(ctx context.Context) ([]indexerStageView, error) {
	return s.stages, nil
}

func (s *stubIndexerService) ListRuns(ctx context.Context, stageName string, limit int) ([]pgindex.IndexerStageRun, error) {
	return s.runs, nil
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

func (s *stubIndexerService) ListReleases(ctx context.Context, query string, limit, offset int) ([]pgindex.IndexerReleaseSummary, int, error) {
	return s.releases, s.releaseTotal, nil
}

func (s *stubIndexerService) GetRelease(ctx context.Context, releaseID string) (*pgindex.IndexerReleaseDetail, error) {
	return s.release, nil
}

func (s *stubIndexerService) GetBinary(ctx context.Context, binaryID int64) (*pgindex.IndexerBinaryDetail, error) {
	return s.binary, nil
}

func (s *stubIndexerService) GetFile(ctx context.Context, fileID int64) (*pgindex.IndexerFileDetail, error) {
	return s.file, nil
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
	if !strings.Contains(rec.Body.String(), `"stage_name":"inspect_archive"`) {
		t.Fatalf("expected stage_name in response, got %s", rec.Body.String())
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
