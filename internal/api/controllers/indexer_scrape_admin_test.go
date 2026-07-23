package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/labstack/echo/v5"
)

func TestSanitizeProviderInventoryTextRepairsInvalidUTF8(t *testing.T) {
	got := sanitizeProviderInventoryText(" alt.binaries.\xe8quebec\x00 ")

	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8 text, got %q", got)
	}
	if strings.Contains(got, "\xe8") || strings.Contains(got, "\x00") {
		t.Fatalf("expected invalid bytes and NULs removed, got %q", got)
	}
	if got != "alt.binaries.quebec" {
		t.Fatalf("unexpected sanitized text %q", got)
	}
}

func TestPreviewWildcardGroupsPageFiltersBookPattern(t *testing.T) {
	indexing := &app.IndexingRuntimeSettings{
		WildcardRules: []app.IndexingWildcardRuleRuntimeSettings{
			{ID: "books", Pattern: "*book*", Enabled: true},
		},
		ProviderGroupInventory: []app.IndexingProviderGroupInventoryRuntimeSettings{
			{ProviderID: "p1", GroupName: "alt.binaries.ebooks"},
			{ProviderID: "p1", GroupName: "alt.binaries.movies"},
			{ProviderID: "p2", GroupName: "alt.binaries.audio.books"},
		},
	}

	items, total := previewWildcardGroupsPage(context.Background(), nil, indexing, "book", 1, 0)
	if total != 2 {
		t.Fatalf("expected 2 book matches, got %d (%+v)", total, items)
	}
	if len(items) != 1 {
		t.Fatalf("expected one paged item, got %+v", items)
	}
	if items[0].GroupName != "alt.binaries.audio.books" {
		t.Fatalf("expected sorted first match, got %+v", items[0])
	}

	items, total = previewWildcardGroupsPage(context.Background(), nil, indexing, "book", 1, 1)
	if total != 2 {
		t.Fatalf("expected 2 book matches on second page, got %d", total)
	}
	if len(items) != 1 || items[0].GroupName != "alt.binaries.ebooks" {
		t.Fatalf("expected second sorted match, got %+v", items)
	}
}

type scrapeAdminSettingsStub struct {
	runtime *app.RuntimeSettings
}

func (s *scrapeAdminSettingsStub) Get(context.Context) (*app.RuntimeSettings, error) {
	return app.CloneRuntimeSettings(s.runtime), nil
}

func (s *scrapeAdminSettingsStub) Capabilities(context.Context) (*app.ControlPlaneCapabilities, error) {
	return &app.ControlPlaneCapabilities{}, nil
}

func (s *scrapeAdminSettingsStub) Update(_ context.Context, patch *app.RuntimeSettingsPatch) (*app.RuntimeSettings, error) {
	s.runtime = app.ApplyPatch(s.runtime, patch)
	return app.CloneRuntimeSettings(s.runtime), nil
}

func TestIndexerScrapeAdminCanRemoveFinalActiveGroup(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Indexing.Newsgroups = []string{"alt.binaries.test"}
	runtime.Indexing.ExplicitGroups = []app.IndexingScrapeGroupRuntimeSettings{{
		GroupName: "alt.binaries.test",
		Enabled:   true,
		Source:    "explicit",
	}}
	settings := &scrapeAdminSettingsStub{runtime: runtime}
	ctrl := NewIndexerScrapeAdminController(&app.Context{SettingsAdmin: settings})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/indexer/scrape", strings.NewReader(`{
		"explicit_groups": [],
		"scrape_timeframes": [],
		"wildcard_rules": [],
		"materialized_groups": []
	}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := ctrl.UpdateConfig(c); err != nil {
		t.Fatalf("update scrape config: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		ExplicitGroups  []app.IndexingScrapeGroupRuntimeSettings `json:"explicit_groups"`
		EffectiveGroups []app.IndexingScrapeGroupRuntimeSettings `json:"effective_groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.ExplicitGroups) != 0 || len(response.EffectiveGroups) != 0 {
		t.Fatalf("expected the final group to stay removed, got explicit=%+v effective=%+v", response.ExplicitGroups, response.EffectiveGroups)
	}
	if settings.runtime.Indexing.ExplicitGroups == nil {
		t.Fatal("expected an intentional empty explicit_groups list")
	}
	if len(settings.runtime.Indexing.Newsgroups) != 0 {
		t.Fatalf("expected derived newsgroups to be empty, got %+v", settings.runtime.Indexing.Newsgroups)
	}
}
