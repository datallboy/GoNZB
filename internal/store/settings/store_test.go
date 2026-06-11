package settings

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
)

func TestGetRuntimeSettingsReturnsDefaultsForFreshStore(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	runtime, err := store.GetRuntimeSettings(context.Background())
	if err != nil {
		t.Fatalf("get runtime settings: %v", err)
	}

	if len(runtime.Servers) != 0 || len(runtime.Indexers) != 0 {
		t.Fatalf("expected empty operational source defaults, got %+v", runtime)
	}
	if runtime.Aggregator == nil || runtime.Aggregator.Sources.LocalBlob.Enabled || runtime.Aggregator.Sources.UsenetIndexer.Enabled {
		t.Fatalf("expected disabled aggregator defaults, got %+v", runtime.Aggregator)
	}
	if runtime.Indexing == nil || runtime.Indexing.ScrapeLatest.Enabled || runtime.Indexing.Release.Enabled {
		t.Fatalf("expected disabled indexer stage defaults, got %+v", runtime.Indexing)
	}
}

func TestUpdateSettingsPreservesExplicitlyEmptyScrapeGroupsAcrossReload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.Indexing = &IndexingRuntimeSettings{
		Newsgroups: []string{"alt.binaries.test"},
		BackfillUntilDateByGroup: map[string]string{
			"alt.binaries.test": "2024-01-01",
		},
		ExplicitGroups: []app.IndexingScrapeGroupRuntimeSettings{
			{
				GroupName:         "alt.binaries.test",
				Enabled:           true,
				BackfillUntilDate: "2024-01-01",
				Source:            "explicit",
			},
		},
		WildcardRules:          []app.IndexingWildcardRuleRuntimeSettings{},
		ProviderGroupInventory: []app.IndexingProviderGroupInventoryRuntimeSettings{},
		MaterializedGroups:     []app.IndexingMaterializedGroupRuntimeSettings{},
	}
	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("seed runtime settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload seeded runtime settings: %v", err)
	}
	reloaded.Indexing.ExplicitGroups = []app.IndexingScrapeGroupRuntimeSettings{}
	reloaded.Indexing.WildcardRules = []app.IndexingWildcardRuleRuntimeSettings{}
	reloaded.Indexing.ProviderGroupInventory = []app.IndexingProviderGroupInventoryRuntimeSettings{}
	reloaded.Indexing.MaterializedGroups = []app.IndexingMaterializedGroupRuntimeSettings{}
	reloaded.Indexing.Newsgroups = []string{}
	reloaded.Indexing.BackfillUntilDateByGroup = map[string]string{}

	if err := store.UpdateSettings(ctx, reloaded); err != nil {
		t.Fatalf("persist empty scrape settings: %v", err)
	}

	finalRuntime, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload emptied runtime settings: %v", err)
	}
	if finalRuntime.Indexing == nil {
		t.Fatalf("expected indexing settings to be present")
	}
	if finalRuntime.Indexing.ExplicitGroups == nil {
		t.Fatalf("expected explicit_groups to round-trip as an intentional empty list")
	}
	if len(finalRuntime.Indexing.ExplicitGroups) != 0 {
		t.Fatalf("expected zero explicit groups after reload, got %+v", finalRuntime.Indexing.ExplicitGroups)
	}
	if len(finalRuntime.Indexing.Newsgroups) != 0 {
		t.Fatalf("expected zero derived newsgroups after reload, got %+v", finalRuntime.Indexing.Newsgroups)
	}
	if len(finalRuntime.Indexing.BackfillUntilDateByGroup) != 0 {
		t.Fatalf("expected zero backfill cutoffs after reload, got %+v", finalRuntime.Indexing.BackfillUntilDateByGroup)
	}
}
