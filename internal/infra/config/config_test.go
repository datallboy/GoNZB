package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAggregatorBootstrapDoesNotRequireSource(t *testing.T) {
	cfg := minimalAggregatorConfig()

	if err := cfg.ValidateEffective(); err != nil {
		t.Fatalf("expected unconfigured aggregator bootstrap to validate, got %v", err)
	}
}

func TestDownloaderBootstrapDoesNotRequireNNTPServer(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.Modules.Downloader.Enabled = true

	if err := cfg.ValidateEffective(); err != nil {
		t.Fatalf("expected unconfigured downloader bootstrap to validate, got %v", err)
	}
}

func TestReleaseExpectedFileCoverageValidation(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.Modules.UsenetIndexer.Enabled = true
	cfg.Store.PGDSN = "postgres://gonzb:gonzb@localhost:5432/gonzb?sslmode=disable"
	cfg.Indexing.Release.MinExpectedFileCoveragePct = func() *float64 { v := 101.0; return &v }()

	if err := cfg.ValidateEffective(); err == nil {
		t.Fatal("expected min_expected_file_coverage_pct validation error")
	}
}

func TestGoNZBNetBootstrapRequiresPostgres(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.Modules.GoNZBNet.Enabled = true

	if err := cfg.ValidateEffective(); err == nil {
		t.Fatal("expected gonzbnet postgres validation error")
	}
}

func TestGoNZBNetRejectsLiveQueryEnabled(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.GoNZBNet.LiveQueryEnabled = true

	if err := cfg.ValidateEffective(); err == nil {
		t.Fatal("expected live query validation error")
	}
}

func TestGoNZBNetRejectsLiveQueryEnvAlias(t *testing.T) {
	cfgPath := writeMinimalConfig(t, `
modules:
  downloader:
    enabled: false
  aggregator:
    enabled: true
  usenet_indexer:
    enabled: false
  api:
    enabled: true
  web_ui:
    enabled: false
`)
	t.Setenv("GONZBNET_LIVE_QUERY_ENABLED", "true")

	if _, err := Load(cfgPath); err == nil {
		t.Fatal("expected live query env alias validation error")
	}
}

func TestGoNZBNetOnlyBootstrapCountsAsEnabledModule(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.Modules = ModulesConfig{
		GoNZBNet: ModuleToggle{Enabled: true},
	}
	cfg.Store.PGDSN = "postgres://gonzb:gonzb@localhost:5432/gonzb?sslmode=disable"

	if err := cfg.ValidateEffective(); err != nil {
		t.Fatalf("expected gonzbnet-only bootstrap to validate, got %v", err)
	}
}

func TestGoNZBNetEnabledEnvAliasMapsToModuleGate(t *testing.T) {
	cfgPath := writeMinimalConfig(t, `
store:
  pg_dsn: postgres://gonzb:gonzb@localhost:5432/gonzb?sslmode=disable
modules:
  downloader:
    enabled: false
  aggregator:
    enabled: false
  usenet_indexer:
    enabled: false
  api:
    enabled: true
  web_ui:
    enabled: false
`)
	t.Setenv("GONZBNET_ENABLED", "true")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Modules.GoNZBNet.Enabled {
		t.Fatal("expected GONZBNET_ENABLED to enable modules.gonzbnet")
	}
}

func TestProjectPrefixedGoNZBNetModuleEnvTakesPrecedenceOverAlias(t *testing.T) {
	cfgPath := writeMinimalConfig(t, `
store:
  pg_dsn: postgres://gonzb:gonzb@localhost:5432/gonzb?sslmode=disable
modules:
  downloader:
    enabled: false
  aggregator:
    enabled: false
  usenet_indexer:
    enabled: false
  api:
    enabled: true
  web_ui:
    enabled: false
`)
	t.Setenv("GONZBNET_ENABLED", "true")
	t.Setenv("GONZB_MODULES_GONZBNET_ENABLED", "false")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Modules.GoNZBNet.Enabled {
		t.Fatal("expected project-prefixed module env to override GONZBNET_ENABLED alias")
	}
}

func TestGoNZBNetMaxEventAgeDefault(t *testing.T) {
	cfgPath := writeMinimalConfig(t, `
modules:
  downloader:
    enabled: false
  aggregator:
    enabled: true
  usenet_indexer:
    enabled: false
  api:
    enabled: true
  web_ui:
    enabled: false
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.GoNZBNet.MaxEventAgeHours != 720 {
		t.Fatalf("expected default max event age 720 hours, got %d", cfg.GoNZBNet.MaxEventAgeHours)
	}
}

func TestGoNZBNetAddendumConfigDefaults(t *testing.T) {
	cfgPath := writeMinimalConfig(t, `
modules:
  downloader:
    enabled: false
  aggregator:
    enabled: true
  usenet_indexer:
    enabled: false
  api:
    enabled: true
  web_ui:
    enabled: false
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.GoNZBNet.ScannerMaxGroups != 25 {
		t.Fatalf("expected default scanner max groups 25, got %d", cfg.GoNZBNet.ScannerMaxGroups)
	}
	if cfg.GoNZBNet.CoverageMinTrustForClaim != 0.65 {
		t.Fatalf("expected default coverage min trust 0.65, got %f", cfg.GoNZBNet.CoverageMinTrustForClaim)
	}
	if cfg.GoNZBNet.ManifestCacheMaxBytes != 10737418240 {
		t.Fatalf("expected default manifest cache bytes, got %d", cfg.GoNZBNet.ManifestCacheMaxBytes)
	}
	if len(cfg.GoNZBNet.ValidationTiers) != 3 {
		t.Fatalf("expected default validation tiers, got %#v", cfg.GoNZBNet.ValidationTiers)
	}
}

func TestGoNZBNetAddendumEnvAliases(t *testing.T) {
	cfgPath := writeMinimalConfig(t, `
modules:
  downloader:
    enabled: false
  aggregator:
    enabled: true
  usenet_indexer:
    enabled: false
  api:
    enabled: true
  web_ui:
    enabled: false
`)
	t.Setenv("GONZBNET_SCANNER_MAX_GROUPS", "7")
	t.Setenv("GONZBNET_COVERAGE_MIN_TRUST_FOR_CLAIM", "0.8")
	t.Setenv("GONZBNET_MANIFEST_CACHE_MAX_BYTES", "1024")
	t.Setenv("GONZBNET_SCANNER_PUBLISH_RELEASE_CARDS", "true")
	t.Setenv("GONZBNET_MANIFEST_CACHE_ENABLED", "false")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.GoNZBNet.ScannerMaxGroups != 7 {
		t.Fatalf("expected scanner max groups env alias, got %d", cfg.GoNZBNet.ScannerMaxGroups)
	}
	if cfg.GoNZBNet.CoverageMinTrustForClaim != 0.8 {
		t.Fatalf("expected coverage trust env alias, got %f", cfg.GoNZBNet.CoverageMinTrustForClaim)
	}
	if cfg.GoNZBNet.ManifestCacheMaxBytes != 1024 {
		t.Fatalf("expected manifest cache bytes env alias, got %d", cfg.GoNZBNet.ManifestCacheMaxBytes)
	}
	if !cfg.GoNZBNet.PublishReleaseCardsEnabled {
		t.Fatal("expected scanner publish release cards alias to enable publisher")
	}
	if cfg.GoNZBNet.ManifestCacheEnabled {
		t.Fatal("expected manifest cache env alias to disable manifest cache")
	}
}

func TestGoNZBNetAddendumConfigValidation(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.GoNZBNet.CoverageMinTrustForClaim = 1.1

	if err := cfg.ValidateEffective(); err == nil {
		t.Fatal("expected coverage min trust validation error")
	}
}

func minimalAggregatorConfig() *Config {
	return &Config{
		Modules: ModulesConfig{
			Aggregator: ModuleToggle{Enabled: true},
			API:        ModuleToggle{Enabled: true},
		},
		Download: DownloadConfig{
			OutDir: "./downloads",
		},
	}
}

func writeMinimalConfig(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
