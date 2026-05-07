package config

import "testing"

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
	cfg.Store.PGDSN = "postgres://postgres:postgres@localhost:5432/gonzb?sslmode=disable"
	cfg.Indexing.Release.MinExpectedFileCoveragePct = func() *float64 { v := 101.0; return &v }()

	if err := cfg.ValidateEffective(); err == nil {
		t.Fatal("expected min_expected_file_coverage_pct validation error")
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
