package pgindex

import (
	"context"
	"database/sql"
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

	report, err := store.CheckCriticalIndexerIntegrity(context.Background(), false)
	if err != nil {
		t.Fatalf("check critical indexer integrity on fresh schema: %v", err)
	}
	if report.HasFailures() {
		t.Fatalf("fresh schema critical integrity failed: %s", report.FailureSummary())
	}
	var checkedPartitionParent bool
	for _, check := range report.Checks {
		if check.AccessMethod == "partitioned" {
			checkedPartitionParent = true
			break
		}
	}
	if !checkedPartitionParent {
		t.Fatalf("fresh schema integrity check did not cover partitioned parents: %+v", report.Checks)
	}

	idDefaultTables := []string{
		"binary_projection_events",
		"binary_inspections",
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_sets",
		"binary_par2_targets",
	}
	for _, table := range idDefaultTables {
		var columnDefault sql.NullString
		if err := store.DB().QueryRowContext(context.Background(), `
			SELECT column_default
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = $1
			  AND column_name = 'id'`, table).Scan(&columnDefault); err != nil {
			t.Fatalf("check %s id default: %v", table, err)
		}
		if !columnDefault.Valid || !strings.Contains(columnDefault.String, "nextval(") {
			t.Fatalf("%s.id must have sequence default, got %q", table, columnDefault.String)
		}
	}
}
