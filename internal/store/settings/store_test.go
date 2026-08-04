package settings

import (
	"context"
	"database/sql"
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

//go:embed migrations_archive/pre_v0_8_0_squash/*.up.sql
var preV080SettingsMigrationFS embed.FS

func TestPreV080SettingsDatabaseUpgradesWithoutLosingData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "settings.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open pre-v0.8 settings database: %v", err)
	}
	entries, err := preV080SettingsMigrationFS.ReadDir("migrations_archive/pre_v0_8_0_squash")
	if err != nil {
		t.Fatalf("read pre-v0.8 settings migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		migration, err := preV080SettingsMigrationFS.ReadFile("migrations_archive/pre_v0_8_0_squash/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE module_schema_version (
			module_name TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO module_schema_version(module_name, version) VALUES ('settings', 8);
		INSERT INTO auth_users(id, username, password_hash) VALUES ('pre08-user', 'pre08-admin', 'sentinel-hash');
		INSERT INTO settings_nntp_servers(id, host, port, username, password_ciphertext, tls, max_connections, priority, scope)
		VALUES ('pre08-provider', 'news.example.test', 563, 'reader', 'sentinel-secret', 1, 12, 1, 'shared');
	`); err != nil {
		t.Fatalf("seed pre-v0.8 settings database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-v0.8 settings database: %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("upgrade pre-v0.8 settings database: %v", err)
	}
	defer store.Close()

	version, err := store.SchemaVersion(t.Context())
	if err != nil {
		t.Fatalf("read upgraded settings version: %v", err)
	}
	if version != expectedSchemaVersion {
		t.Fatalf("upgraded settings version = %d, want %d", version, expectedSchemaVersion)
	}
	var username, host string
	if err := store.db.QueryRowContext(t.Context(), `SELECT username FROM auth_users WHERE id = 'pre08-user'`).Scan(&username); err != nil {
		t.Fatalf("read preserved pre-v0.8 user: %v", err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT host FROM settings_nntp_servers WHERE id = 'pre08-provider'`).Scan(&host); err != nil {
		t.Fatalf("read preserved pre-v0.8 provider: %v", err)
	}
	if username != "pre08-admin" || host != "news.example.test" {
		t.Fatalf("pre-v0.8 settings were not preserved: username=%q host=%q", username, host)
	}
	for _, table := range []string{"settings_download", "settings_arr_integrations"} {
		var exists bool
		if err := store.db.QueryRowContext(t.Context(), `
			SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
			t.Fatalf("inspect retired table %s: %v", table, err)
		}
		if exists {
			t.Fatalf("retired table %s survived upgrade", table)
		}
	}
}

func TestNewStoreRestrictsDatabasePermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "settings.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat settings database: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("expected settings database permissions 0600, got %04o", got)
	}
}

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

func TestGetRuntimeSettingsUsesBootstrapConfigForFreshStore(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	base := &config.Config{
		Aggregator: config.AggregatorConfig{
			Sources: config.AggregatorSourcesConfig{
				GoNZBNet: config.ModuleToggle{Enabled: true},
			},
		},
	}
	runtime, err := store.GetRuntimeSettings(context.Background(), base)
	if err != nil {
		t.Fatalf("get runtime settings: %v", err)
	}
	if runtime.Aggregator == nil || !runtime.Aggregator.Sources.GoNZBNet.Enabled {
		t.Fatalf("expected bootstrap gonzbnet source to remain enabled, got %+v", runtime.Aggregator)
	}
}

func TestIndexerOutboundPolicyRoundTrips(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	runtime := DefaultRuntimeSettings()
	runtime.Indexers = []IndexerRuntimeSettings{{
		ID:                    "private-newznab",
		BaseURL:               "http://192.168.1.20:9696",
		APIPath:               "/api",
		APIKey:                "secret",
		AllowPrivateAddresses: false,
		AllowedCIDRs:          []string{"192.168.1.20/32"},
	}}
	if err := store.UpdateSettings(t.Context(), runtime); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(t.Context())
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	if len(reloaded.Indexers) != 1 {
		t.Fatalf("expected one indexer, got %d", len(reloaded.Indexers))
	}
	got := reloaded.Indexers[0]
	if got.AllowPrivateAddresses || len(got.AllowedCIDRs) != 1 || got.AllowedCIDRs[0] != "192.168.1.20/32" {
		t.Fatalf("unexpected outbound policy after reload: %+v", got)
	}
}

func TestExternalDownloadClientsRoundTripAndReplaceLegacySettings(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	runtime := DefaultRuntimeSettings()
	runtime.DownloadClients = []DownloadClientRuntimeSettings{{
		ID: "sab", Name: "SABnzbd", Enabled: true, Default: true,
		BaseURL: "https://sab.example.test", APIKey: "secret", Category: "movies", Priority: 1,
	}}
	if err := store.UpdateSettings(t.Context(), runtime); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(t.Context())
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	if len(reloaded.DownloadClients) != 1 {
		t.Fatalf("expected one download client, got %+v", reloaded.DownloadClients)
	}
	got := reloaded.DownloadClients[0]
	if got.ID != "sab" || got.APIKey != "secret" || !got.Default || got.Priority != 1 {
		t.Fatalf("unexpected download client after reload: %+v", got)
	}

	for _, legacyTable := range []string{"settings_download", "settings_arr_integrations"} {
		var count int
		if err := store.db.QueryRowContext(t.Context(), `
			SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, legacyTable).Scan(&count); err != nil {
			t.Fatalf("inspect legacy table %s: %v", legacyTable, err)
		}
		if count != 0 {
			t.Fatalf("expected legacy table %s to be removed", legacyTable)
		}
	}
}

func TestUpdateSettingsPreservesExplicitlyEmptyScrapeGroupsAcrossReload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.Indexing = &IndexingRuntimeSettings{
		Newsgroups: []string{"alt.binaries.test"},
		BackfillUntilDateByGroup: map[string]string{
			"alt.binaries.test": "2024-01-01",
		},
		ExplicitGroups: []app.IndexingScrapeGroupRuntimeSettings{
			{
				GroupName:         "alt.binaries.test",
				Enabled:           true,
				BackfillUntilDate: "2024-01-01",
				Source:            "explicit",
			},
		},
		WildcardRules:          []app.IndexingWildcardRuleRuntimeSettings{},
		ProviderGroupInventory: []app.IndexingProviderGroupInventoryRuntimeSettings{},
		MaterializedGroups:     []app.IndexingMaterializedGroupRuntimeSettings{},
	}
	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("seed runtime settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload seeded runtime settings: %v", err)
	}
	reloaded.Indexing.ExplicitGroups = []app.IndexingScrapeGroupRuntimeSettings{}
	reloaded.Indexing.WildcardRules = []app.IndexingWildcardRuleRuntimeSettings{}
	reloaded.Indexing.ProviderGroupInventory = []app.IndexingProviderGroupInventoryRuntimeSettings{}
	reloaded.Indexing.MaterializedGroups = []app.IndexingMaterializedGroupRuntimeSettings{}
	reloaded.Indexing.Newsgroups = []string{}
	reloaded.Indexing.BackfillUntilDateByGroup = map[string]string{}

	if err := store.UpdateSettings(ctx, reloaded); err != nil {
		t.Fatalf("persist empty scrape settings: %v", err)
	}

	finalRuntime, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload emptied runtime settings: %v", err)
	}
	if finalRuntime.Indexing == nil {
		t.Fatalf("expected indexing settings to be present")
	}
	if finalRuntime.Indexing.ExplicitGroups == nil {
		t.Fatalf("expected explicit_groups to round-trip as an intentional empty list")
	}
	if len(finalRuntime.Indexing.ExplicitGroups) != 0 {
		t.Fatalf("expected zero explicit groups after reload, got %+v", finalRuntime.Indexing.ExplicitGroups)
	}
	if len(finalRuntime.Indexing.Newsgroups) != 0 {
		t.Fatalf("expected zero derived newsgroups after reload, got %+v", finalRuntime.Indexing.Newsgroups)
	}
	if len(finalRuntime.Indexing.BackfillUntilDateByGroup) != 0 {
		t.Fatalf("expected zero backfill cutoffs after reload, got %+v", finalRuntime.Indexing.BackfillUntilDateByGroup)
	}
}

func TestUpdateSettingsPreservesZeroNewestPctAcrossReload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.Indexing.RecoverYEnc.Enabled = true
	runtime.Indexing.RecoverYEnc.BatchSize = 5000
	runtime.Indexing.RecoverYEnc.Concurrency = 100
	runtime.Indexing.RecoverYEnc.TargetWindowPct = 100
	runtime.Indexing.RecoverYEnc.NewestPct = 0

	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("persist runtime settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload runtime settings: %v", err)
	}
	if got := reloaded.Indexing.RecoverYEnc.TargetWindowPct; got != 100 {
		t.Fatalf("expected target window pct 100, got %d", got)
	}
	if got := reloaded.Indexing.RecoverYEnc.NewestPct; got != 0 {
		t.Fatalf("expected newest pct 0, got %d", got)
	}
}

func TestUpdateSettingsPreservesGoNZBNetOptionsAcrossReload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.GoNZBNet.NodeAlias = "runtime-node"
	runtime.GoNZBNet.ScannerEnabled = true
	runtime.GoNZBNet.ManualPeers = []string{"https://peer.example"}
	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("persist runtime settings: %v", err)
	}

	reloaded, err := store.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("reload runtime settings: %v", err)
	}
	if reloaded.GoNZBNet == nil || reloaded.GoNZBNet.NodeAlias != "runtime-node" || !reloaded.GoNZBNet.ScannerEnabled {
		t.Fatalf("expected GoNZBNet options to round-trip, got %+v", reloaded.GoNZBNet)
	}
	if len(reloaded.GoNZBNet.ManualPeers) != 1 || reloaded.GoNZBNet.ManualPeers[0] != "https://peer.example" {
		t.Fatalf("expected GoNZBNet manual peer to round-trip, got %+v", reloaded.GoNZBNet.ManualPeers)
	}
}

func TestOlderStructuredSettingsUseBootstrapGoNZBNetOptions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runtime := DefaultRuntimeSettings()
	runtime.GoNZBNet = nil
	if err := store.UpdateSettings(ctx, runtime); err != nil {
		t.Fatalf("persist older-shaped runtime settings: %v", err)
	}
	base := &config.Config{GoNZBNet: config.GoNZBNetConfig{NodeAlias: "bootstrap-node", ScannerEnabled: true}}

	reloaded, err := store.GetRuntimeSettings(ctx, base)
	if err != nil {
		t.Fatalf("reload runtime settings: %v", err)
	}
	if reloaded.GoNZBNet == nil || reloaded.GoNZBNet.NodeAlias != "bootstrap-node" || !reloaded.GoNZBNet.ScannerEnabled {
		t.Fatalf("expected missing GoNZBNet options to inherit bootstrap config, got %+v", reloaded.GoNZBNet)
	}
}
