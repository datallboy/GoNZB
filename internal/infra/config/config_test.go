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
