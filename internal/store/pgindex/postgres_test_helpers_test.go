package pgindex

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const testProviderID int64 = 1
const testNewsgroupID int64 = 1

var postgresTestDatabaseMu sync.Mutex

func requireTestPostgresDSN(t *testing.T) string {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn != "" {
		return dsn
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" || strings.TrimSpace(os.Getenv("GONZB_REQUIRE_TEST_PG")) == "1" {
		t.Fatal("GONZB_TEST_PG_DSN is required for PostgreSQL tests in CI")
	}
	t.Skip("set GONZB_TEST_PG_DSN to run PostgreSQL integration tests")
	return ""
}

func openPostgresTestStore(t *testing.T) *Store {
	t.Helper()

	// The package-level integration tests share one disposable database. Serialize
	// them and clear application data so queue rows, projections, and identities
	// cannot leak between tests.
	postgresTestDatabaseMu.Lock()
	t.Cleanup(postgresTestDatabaseMu.Unlock)

	store, err := NewStore(requireTestPostgresDSN(t))
	if err != nil {
		t.Fatalf("open PostgreSQL test store: %v", err)
	}
	requireDisposableTestDatabase(t, store)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close PostgreSQL test store: %v", err)
		}
	})
	resetPostgresTestData(t, store)
	return store
}

func resetPostgresTestData(t *testing.T, store *Store) {
	t.Helper()

	var tables string
	if err := store.DB().QueryRow(`
		SELECT COALESCE(string_agg(format('%I.%I', n.nspname, c.relname), ', '), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relname <> 'module_schema_version'`).Scan(&tables); err != nil {
		t.Fatalf("list PostgreSQL test tables: %v", err)
	}
	if tables == "" {
		return
	}
	if _, err := store.DB().Exec(`TRUNCATE TABLE ` + tables + ` RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset PostgreSQL test data: %v", err)
	}
}

func ensureDefaultTestProvider(t *testing.T, store *Store) {
	t.Helper()

	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO usenet_providers (id, provider_key, display_name)
		VALUES ($1, 'integration-test-provider', 'Integration Test Provider')
		ON CONFLICT (id) DO UPDATE
		SET display_name = EXCLUDED.display_name`,
		testProviderID,
	); err != nil {
		t.Fatalf("ensure default PostgreSQL test provider: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO newsgroups (id, group_name)
		VALUES ($1, 'alt.test.integration-default')
		ON CONFLICT (id) DO NOTHING`,
		testNewsgroupID,
	); err != nil {
		t.Fatalf("ensure default PostgreSQL test newsgroup: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		SELECT setval('usenet_providers_id_seq', GREATEST((SELECT MAX(id) FROM usenet_providers), 1));
		SELECT setval('newsgroups_id_seq', GREATEST((SELECT MAX(id) FROM newsgroups), 1))`); err != nil {
		t.Fatalf("advance PostgreSQL test fixture sequences: %v", err)
	}
	now := time.Now().UTC()
	for _, bundle := range []partitionBundle{
		partitionBundleScrape,
		partitionBundleScheduler,
		partitionBundleAssemble,
		partitionBundleYEnc,
		partitionBundleInspect,
		partitionBundleRelease,
	} {
		if err := store.provisionPartitionBundleForDays(ctx, bundle, []time.Time{now}); err != nil {
			t.Fatalf("provision %s PostgreSQL test partitions: %v", bundle, err)
		}
	}
}

func requireDisposableTestDatabase(t *testing.T, store *Store) {
	t.Helper()

	var databaseName string
	if err := store.DB().QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("read PostgreSQL test database name: %v", err)
	}
	normalized := strings.ToLower(strings.TrimSpace(databaseName))
	if !strings.Contains(normalized, "test") && !strings.Contains(normalized, "ci") {
		t.Fatalf("PostgreSQL tests refuse database %q; use a disposable database whose name contains test or ci", databaseName)
	}
}
