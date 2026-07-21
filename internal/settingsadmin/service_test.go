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

func TestValidateRuntimeSettingsAllowsEnabledIndexerStageWithoutNewsgroup(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Servers = []app.ServerRuntimeSettings{{ID: "primary", Host: "news.example.com", Port: 563}}
	runtime.Indexing.ScrapeLatest.Enabled = true

	err := ValidateRuntimeSettings(&config.Config{}, runtime)
	if err != nil {
		t.Fatalf("expected zero-group scrape config to save, got %v", err)
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

func TestValidateRuntimeSettingsReportsGoNZBNetModuleGate(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Aggregator.Sources.GoNZBNet.Enabled = true

	err := ValidateRuntimeSettings(&config.Config{
		Modules: config.ModulesConfig{Aggregator: config.ModuleToggle{Enabled: true}},
	}, runtime)
	if err == nil || !strings.Contains(err.Error(), "aggregator.sources.gonzbnet.enabled requires modules.gonzbnet.enabled in config.yaml") {
		t.Fatalf("expected GoNZBNet module gate detail, got %v", err)
	}
}

func TestBuildCapabilitiesReportsIndexerNeedsScrapeGroup(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.IndexerServers = []app.ServerRuntimeSettings{{ID: "primary", Host: "news.example.com", Port: 563}}

	caps := BuildCapabilities(&config.Config{
		Modules: config.ModulesConfig{UsenetIndexer: config.ModuleToggle{Enabled: true}},
	}, runtime)

	indexer := caps.Modules["usenet_indexer"]
	if indexer.Ready || len(indexer.Requirements) == 0 {
		t.Fatalf("expected scrape-group requirement, got %+v", indexer)
	}
	if !strings.Contains(strings.Join(indexer.Requirements, " "), "scrape group") {
		t.Fatalf("expected scrape-group detail, got %+v", indexer.Requirements)
	}
}

func TestBuildCapabilitiesIncludesGoNZBNetModule(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	caps := BuildCapabilities(&config.Config{
		Modules: config.ModulesConfig{GoNZBNet: config.ModuleToggle{Enabled: true}},
		Store:   config.StoreConfig{PGDSN: "postgres://gonzb:test@localhost/gonzb"},
	}, runtime)

	gonzbnet := caps.Modules["gonzbnet"]
	if !gonzbnet.Visible || !gonzbnet.Enabled || !gonzbnet.Configured || !gonzbnet.Ready {
		t.Fatalf("expected ready GoNZBNet capability, got %+v", gonzbnet)
	}
}

func TestBuildCapabilitiesCountsGoNZBNetAsAggregatorSource(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Aggregator.Sources.GoNZBNet.Enabled = true
	caps := BuildCapabilities(&config.Config{
		Modules: config.ModulesConfig{
			Aggregator: config.ModuleToggle{Enabled: true},
			GoNZBNet:   config.ModuleToggle{Enabled: true},
		},
		Store: config.StoreConfig{PGDSN: "postgres://gonzb:test@localhost/gonzb"},
	}, runtime)

	if aggregator := caps.Modules["aggregator"]; !aggregator.Configured || !aggregator.Ready {
		t.Fatalf("expected GoNZBNet-backed aggregator to be ready, got %+v", aggregator)
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

func TestValidateRuntimeSettingsAllowsDashboardMaintenanceOneHourInterval(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	task := runtime.Indexing.MaintenanceTasks["dashboard_stats_refresh"]
	task.ScheduleEnabled = true
	task.IntervalHours = 1
	runtime.Indexing.MaintenanceTasks["dashboard_stats_refresh"] = task

	if err := ValidateRuntimeSettingsMutation(&config.Config{}, runtime, runtime); err != nil {
		t.Fatalf("expected dashboard stats refresh one-hour interval to be valid, got %v", err)
	}
}

func TestValidateRuntimeSettingsRejectsSourcePurgeBelowMinimumInterval(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	task := runtime.Indexing.MaintenanceTasks["release_source_purge"]
	task.ScheduleEnabled = true
	task.IntervalHours = 1
	runtime.Indexing.MaintenanceTasks["release_source_purge"] = task

	err := ValidateRuntimeSettingsMutation(&config.Config{}, runtime, runtime)
	if err == nil {
		t.Fatalf("expected source purge interval validation error")
	}
	if !strings.Contains(err.Error(), "at least 6 hours") {
		t.Fatalf("expected minimum interval detail, got %v", err)
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
