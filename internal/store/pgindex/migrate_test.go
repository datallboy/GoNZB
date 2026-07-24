package pgindex

import (
	"context"
	"database/sql"
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
	store := openPostgresTestStore(t)

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
	var hasFederationChainIssues bool
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = 'federation_event_chain_issues'
		)`).Scan(&hasFederationChainIssues); err != nil {
		t.Fatalf("check federation chain issue table: %v", err)
	}
	if !hasFederationChainIssues {
		t.Fatalf("fresh schema must create federation_event_chain_issues")
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

	redundantIndexes := []string{
		"idx_binary_grouping_evidence_source_posted",
		"idx_federation_events_author_sequence",
		"idx_release_archive_detail_subtitle_release",
		"idx_release_family_readiness_source_posted",
		"idx_release_ready_candidates_source_posted",
		"idx_release_recovered_file_set_candidates_source_posted",
		"idx_release_stage_dirty_families_source_posted",
		"idx_scrape_checkpoints_provider_newsgroup",
	}
	for _, indexName := range redundantIndexes {
		var exists bool
		if err := store.DB().QueryRowContext(context.Background(), `
			SELECT to_regclass('public.' || $1) IS NOT NULL`, indexName).Scan(&exists); err != nil {
			t.Fatalf("check redundant index %s: %v", indexName, err)
		}
		if exists {
			t.Fatalf("fresh schema must not retain redundant index %s", indexName)
		}
	}

	var baseStemPredicate string
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT pg_get_expr(i.indpred, i.indrelid)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relname = 'idx_binary_identity_base_stem_lookup'`,
	).Scan(&baseStemPredicate); err != nil {
		t.Fatalf("load release base-stem lookup predicate: %v", err)
	}
	if strings.Contains(baseStemPredicate, "expected_file_count") ||
		strings.Contains(baseStemPredicate, "expected_archive_file_count") {
		t.Fatalf("release base-stem lookup must cover singleton families, predicate=%q", baseStemPredicate)
	}
}
