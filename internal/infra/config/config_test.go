package config

import (
	"strings"
	"testing"
)

func TestAggregatorRequiresAtLeastOneSource(t *testing.T) {
	cfg := minimalAggregatorConfig()

	err := cfg.ValidateEffective()
	if err == nil || !strings.Contains(err.Error(), "at least one aggregator source") {
		t.Fatalf("expected aggregator source validation error, got %v", err)
	}
}

func TestAggregatorAllowsLocalBlobSourceWithoutIndexerModule(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.Aggregator.Sources.LocalBlob.Enabled = true

	if err := cfg.ValidateEffective(); err != nil {
		t.Fatalf("expected local blob source config to validate, got %v", err)
	}
}

func TestAggregatorUsenetIndexerSourceRequiresUsenetIndexerModule(t *testing.T) {
	cfg := minimalAggregatorConfig()
	cfg.Aggregator.Sources.UsenetIndexer.Enabled = true

	err := cfg.ValidateEffective()
	if err == nil || !strings.Contains(err.Error(), "requires modules.usenet_indexer.enabled") {
		t.Fatalf("expected usenet indexer source dependency error, got %v", err)
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
