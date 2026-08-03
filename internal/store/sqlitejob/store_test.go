package sqlitejob

import (
	"path/filepath"
	"testing"
)

func TestDownloaderQueueTablesAreRemovedByMigration(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	version, err := store.SchemaVersion(t.Context())
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != expectedSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, expectedSchemaVersion)
	}

	for _, table := range []string{"queue_items", "queue_item_events", "queue_file_sets", "queue_file_set_items"} {
		var count int
		if err := store.db.QueryRowContext(t.Context(), `
			SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("inspect table %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected downloader table %s to be removed", table)
		}
	}
}
