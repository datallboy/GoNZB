package pgindex

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExpectedMigrationVersionTracksLatestEmbeddedMigration(t *testing.T) {
	migs, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations() error = %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected embedded migrations")
	}

	latest := migs[len(migs)-1].version
	if got := expectedMigrationVersion(); got != latest {
		t.Fatalf("expectedMigrationVersion() = %d, want %d", got, latest)
	}
}

func TestFreshBaselineMigration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set GONZB_TEST_PG_DSN to run fresh pgindex migration smoke test")
	}

	store, err := NewStore(dsn)
	if err != nil {
		t.Fatalf("open fresh migrated store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close fresh migrated store: %v", err)
		}
	})

	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != expectedMigrationVersion() {
		t.Fatalf("schema version = %d, want %d", version, expectedMigrationVersion())
	}

	var hasLegacyBinaries bool
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND c.relname = 'binaries'
			  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
		)`).Scan(&hasLegacyBinaries); err != nil {
		t.Fatalf("check retired binaries relation: %v", err)
	}
	if hasLegacyBinaries {
		t.Fatalf("fresh v0.8.0 baseline must not create retired public.binaries")
	}
}
