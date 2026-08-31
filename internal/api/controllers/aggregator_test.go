package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/labstack/echo/v5"
)

type stubCatalogAggregatorService struct {
	results []*domain.Release
	lookup  *domain.Release
	request aggregatorSearchRequest
}

func (s *stubCatalogAggregatorService) Search(_ context.Context, request aggregatorSearchRequest) ([]*domain.Release, error) {
	s.request = request
	return s.results, nil
}

func (s *stubCatalogAggregatorService) Lookup(context.Context, string) (*domain.Release, error) {
	return s.lookup, nil
}

func (s *stubCatalogAggregatorService) PrepareDownload(context.Context, string) (*app.AggregatorDownloadResult, error) {
	return nil, nil
}

func TestListCatalogReleasesUsesMergedAggregatorContract(t *testing.T) {
	postedAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	service := &stubCatalogAggregatorService{results: []*domain.Release{
		{ID: "rel-1", GUID: "guid-1", Source: "gonzbnet", Title: "Federated Fixture", Size: 42, Category: "2040", PublishDate: postedAt},
	}}
	ctrl := &AggregatorController{Service: service}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/releases?limit=25", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := ctrl.ListCatalogReleases(c); err != nil {
		t.Fatal(err)
	}
	if service.request.Query != "" || service.request.Limit != 26 {
		t.Fatalf("unexpected search request: %+v", service.request)
	}
	var body struct {
		Items []struct {
			ReleaseID  string `json:"release_id"`
			SourceKind string `json:"source_kind"`
			Category   string `json:"category"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(body.Items) != 1 || body.Items[0].ReleaseID != "rel-1" || body.Items[0].SourceKind != "gonzbnet" || body.Items[0].Category == "" {
		t.Fatalf("unexpected catalog response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetCatalogReleaseReturnsViewerDetailContract(t *testing.T) {
	service := &stubCatalogAggregatorService{lookup: &domain.Release{
		ID: "rel-1", GUID: "guid-1", Source: "uploader", Title: "Uploader Fixture", Size: 84, Category: "2000",
	}}
	ctrl := &AggregatorController{Service: service}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/releases/rel-1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/catalog/releases/:id")
	c.SetPathValues(echo.PathValues{{Name: "id", Value: "rel-1"}})

	if err := ctrl.GetCatalogRelease(c); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Release struct {
			ReleaseID  string `json:"release_id"`
			SourceKind string `json:"source_kind"`
		} `json:"release"`
		Files []any `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || body.Release.ReleaseID != "rel-1" || body.Release.SourceKind != "uploader" || body.Files == nil {
		t.Fatalf("unexpected detail response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
