package settingsadmin

import (
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

func TestValidateRuntimeSettingsRejectsEnabledIndexerStageWithoutServer(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Indexing.Newsgroups = []string{"alt.binaries.test"}
	runtime.Indexing.ScrapeLatest.Enabled = true

	err := ValidateRuntimeSettings(&config.Config{}, runtime)
	if err == nil || !strings.Contains(err.Error(), "indexing stages require at least one NNTP server in servers") {
		t.Fatalf("expected NNTP validation error, got %v", err)
	}
}

func TestValidateRuntimeSettingsRejectsEnabledIndexerStageWithoutNewsgroup(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.IndexerServers = []app.ServerRuntimeSettings{{ID: "primary", Host: "news.example.com", Port: 563}}
	runtime.Indexing.ScrapeLatest.Enabled = true

	err := ValidateRuntimeSettings(&config.Config{}, runtime)
	if err == nil || !strings.Contains(err.Error(), "indexing stages require at least one newsgroup in indexing.newsgroups") {
		t.Fatalf("expected newsgroup validation error, got %v", err)
	}
}

func TestValidateRuntimeSettingsReportsIncompleteNewznabSource(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Indexers = []app.IndexerRuntimeSettings{{ID: "external"}}

	err := ValidateRuntimeSettings(&config.Config{}, runtime)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "indexers[0].base_url is required") {
		t.Fatalf("expected base_url detail, got %v", err)
	}
	if !strings.Contains(err.Error(), "indexers[0].api_path is required") {
		t.Fatalf("expected api_path detail, got %v", err)
	}
}

func TestValidateRuntimeSettingsReportsLocalIndexerModuleGate(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Aggregator.Sources.UsenetIndexer.Enabled = true

	err := ValidateRuntimeSettings(&config.Config{
		Modules: config.ModulesConfig{Aggregator: config.ModuleToggle{Enabled: true}},
	}, runtime)
	if err == nil || !strings.Contains(err.Error(), "aggregator.sources.usenet_indexer.enabled requires modules.usenet_indexer.enabled in config.yaml") {
		t.Fatalf("expected local indexer module gate detail, got %v", err)
	}
}

func TestValidateRuntimeSettingsAllowsLocalIndexerAggregatorSourceWhenModuleEnabled(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Aggregator.Sources.UsenetIndexer.Enabled = true

	err := ValidateRuntimeSettings(&config.Config{
		Modules: config.ModulesConfig{
			Aggregator:    config.ModuleToggle{Enabled: true},
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
	}, runtime)
	if err != nil {
		t.Fatalf("expected local indexer source to save without NNTP/newsgroup prerequisites, got %v", err)
	}
}

func TestValidateRuntimeSettingsRejectsInvalidStorageGuardThresholds(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Indexing.StorageGuard.MinFreeBytes = -1
	runtime.Indexing.StorageGuard.MinFreePercent = 101

	err := ValidateRuntimeSettings(&config.Config{}, runtime)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "indexing.storage_guard.min_free_bytes") {
		t.Fatalf("expected min_free_bytes detail, got %v", err)
	}
	if !strings.Contains(err.Error(), "indexing.storage_guard.min_free_percent") {
		t.Fatalf("expected min_free_percent detail, got %v", err)
	}
}

func TestValidateRuntimeSettingsRejectsInvalidMemoryGuardThresholds(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Indexing.MemoryGuard.MinAvailableBytes = -1
	runtime.Indexing.MemoryGuard.MinAvailablePercent = 101
	runtime.Indexing.MemoryGuard.MinSwapFreeBytes = -1

	err := ValidateRuntimeSettings(&config.Config{}, runtime)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "indexing.memory_guard.min_available_bytes") {
		t.Fatalf("expected min_available_bytes detail, got %v", err)
	}
	if !strings.Contains(err.Error(), "indexing.memory_guard.min_available_percent") {
		t.Fatalf("expected min_available_percent detail, got %v", err)
	}
	if !strings.Contains(err.Error(), "indexing.memory_guard.min_swap_free_bytes") {
		t.Fatalf("expected min_swap_free_bytes detail, got %v", err)
	}
}

func TestBuildCapabilitiesReportsAggregatorMissingSource(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	caps := BuildCapabilities(&config.Config{
		Modules: config.ModulesConfig{Aggregator: config.ModuleToggle{Enabled: true}},
	}, runtime)

	agg := caps.Modules["aggregator"]
	if agg.Ready || len(agg.Requirements) == 0 {
		t.Fatalf("expected aggregator missing source requirement, got %+v", agg)
	}
}
