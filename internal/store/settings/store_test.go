package settings

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
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

func TestGetRuntimeSettingsUsesBootstrapConfigForFreshStore(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	base := &config.Config{
		Aggregator: config.AggregatorConfig{
			Sources: config.AggregatorSourcesConfig{
				GoNZBNet: config.ModuleToggle{Enabled: true},
			},
		},
	}
	runtime, err := store.GetRuntimeSettings(context.Background(), base)
	if err != nil {
		t.Fatalf("get runtime settings: %v", err)
	}
	if runtime.Aggregator == nil || !runtime.Aggregator.Sources.GoNZBNet.Enabled {
		t.Fatalf("expected bootstrap gonzbnet source to remain enabled, got %+v", runtime.Aggregator)
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

func TestUpdateSettingsPreservesZeroNewestPctAcrossReload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.Indexing.RecoverYEnc.Enabled = true
	runtime.Indexing.RecoverYEnc.BatchSize = 5000
	runtime.Indexing.RecoverYEnc.Concurrency = 100
	runtime.Indexing.RecoverYEnc.TargetWindowPct = 100
	runtime.Indexing.RecoverYEnc.NewestPct = 0

	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("persist runtime settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload runtime settings: %v", err)
	}
	if got := reloaded.Indexing.RecoverYEnc.TargetWindowPct; got != 100 {
		t.Fatalf("expected target window pct 100, got %d", got)
	}
	if got := reloaded.Indexing.RecoverYEnc.NewestPct; got != 0 {
		t.Fatalf("expected newest pct 0, got %d", got)
	}
}

func TestUpdateSettingsPreservesGoNZBNetOptionsAcrossReload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.GoNZBNet.NodeAlias = "runtime-node"
	runtime.GoNZBNet.ScannerEnabled = true
	runtime.GoNZBNet.ManualPeers = []string{"https://peer.example"}
	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("persist runtime settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload runtime settings: %v", err)
	}
	if reloaded.GoNZBNet == nil || reloaded.GoNZBNet.NodeAlias != "runtime-node" || !reloaded.GoNZBNet.ScannerEnabled {
		t.Fatalf("expected GoNZBNet options to round-trip, got %+v", reloaded.GoNZBNet)
	}
	if len(reloaded.GoNZBNet.ManualPeers) != 1 || reloaded.GoNZBNet.ManualPeers[0] != "https://peer.example" {
		t.Fatalf("expected GoNZBNet manual peer to round-trip, got %+v", reloaded.GoNZBNet.ManualPeers)
	}
}

func TestOlderStructuredSettingsUseBootstrapGoNZBNetOptions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.GoNZBNet = nil
	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("persist older-shaped runtime settings: %v", err)
	}
	base := &config.Config{GoNZBNet: config.GoNZBNetConfig{NodeAlias: "bootstrap-node", ScannerEnabled: true}}

	reloaded, err := store.GetRuntimeSettings(ctx, base)
	if err != nil {
		t.Fatalf("reload runtime settings: %v", err)
	}
	if reloaded.GoNZBNet == nil || reloaded.GoNZBNet.NodeAlias != "bootstrap-node" || !reloaded.GoNZBNet.ScannerEnabled {
		t.Fatalf("expected missing GoNZBNet options to inherit bootstrap config, got %+v", reloaded.GoNZBNet)
	}
}
