package controllers

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
)

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
