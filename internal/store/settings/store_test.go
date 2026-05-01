package settings

import (
	"context"
	"path/filepath"
	"testing"
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
