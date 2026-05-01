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
