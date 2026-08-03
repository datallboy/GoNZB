package wiring

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

type fakeSettingsStore struct {
	runtime *app.RuntimeSettings
}

func (f fakeSettingsStore) LoadEffectiveSettings(context.Context, *config.Config) (*config.Config, error) {
	return nil, nil
}

func (f fakeSettingsStore) GetRuntimeSettings(context.Context, ...*config.Config) (*app.RuntimeSettings, error) {
	return f.runtime, nil
}

func (f fakeSettingsStore) UpdateSettings(context.Context, *app.RuntimeSettings) error {
	return nil
}

func (f fakeSettingsStore) WatchSettingsChanges(context.Context) (<-chan struct{}, error) {
	return nil, nil
}

func (f fakeSettingsStore) Ping(context.Context) error {
	return nil
}

func (f fakeSettingsStore) SchemaVersion(context.Context) (int, error) {
	return 0, nil
}

func (f fakeSettingsStore) ExpectedSchemaVersion() int {
	return 0
}

func (f fakeSettingsStore) ValidateSchema(context.Context) error {
	return nil
}

func TestDeriveUsenetIndexerConfigUsesExpandedRuntimeSettings(t *testing.T) {
	enabled := true
	interval := 1.5
	batch := 64
	concurrency := 2
	backoff := 9
	matchHigh := 0.9
	matchProbable := 0.7
	articleBucket := int64(12000)
	predbTimeout := 22
	tmdbTimeout := 33

	cfg := &config.Config{
		Servers: []config.ServerConfig{{
			ID:            "primary",
			Host:          "news.example.com",
			Port:          563,
			Username:      "user",
			Password:      "pass",
			TLS:           true,
			MaxConnection: 10,
			Priority:      1,
		}},
		Store: config.StoreConfig{
			PGDSN: "postgres://gonzb:gonzb@localhost:5432/gonzb?sslmode=disable",
		},
		Modules: config.ModulesConfig{
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
		Indexing: config.IndexingConfig{
			Newsgroups: []string{"alt.binaries.test"},
		},
	}
	cfg.Indexing.ScrapeLatest = config.IndexingStageConfig{
		Enabled:         &enabled,
		IntervalMinutes: &interval,
		BatchSize:       &batch,
		Concurrency:     &concurrency,
		BackoffSeconds:  &backoff,
	}
	cfg.Indexing.Assemble = config.IndexingStageConfig{
		Concurrency: &concurrency,
	}
	cfg.Indexing.Match = config.IndexingMatchConfig{
		HighConfidenceThreshold:     &matchHigh,
		ProbableConfidenceThreshold: &matchProbable,
		ArticleBucketSize:           &articleBucket,
	}
	cfg.Indexing.Release = config.IndexingReleaseConfig{
		Enabled:                    &enabled,
		IntervalMinutes:            &interval,
		BatchSize:                  &batch,
		BackoffSeconds:             &backoff,
		MinConfidence:              &matchHigh,
		MinCompletionPct:           func() *float64 { v := 25.0; return &v }(),
		MinExpectedFileCoveragePct: func() *float64 { v := 92.0; return &v }(),
		RequireExpectedFileCountForContextualObfuscated: func() *bool { v := true; return &v }(),
		PublicRequirePayloadComplete:                    func() *bool { v := true; return &v }(),
		PublicRequireExpectedFileCountComplete:          func() *bool { v := true; return &v }(),
		RetainUntilExpectedFileCountComplete:            func() *bool { v := true; return &v }(),
		ReopenArchivedNZBOnReleaseChange:                func() *bool { v := true; return &v }(),
	}
	cfg.Indexing.InspectMedia = config.IndexingStageConfig{
		Enabled:     &enabled,
		BatchSize:   &batch,
		Concurrency: &concurrency,
	}
	cfg.Indexing.InspectPAR2 = config.IndexingStageConfig{
		Enabled:     &enabled,
		BatchSize:   &batch,
		Concurrency: &concurrency,
	}
	cfg.Indexing.EnrichPreDB = config.IndexingPreDBConfig{
		Enabled:            &enabled,
		IntervalMinutes:    &interval,
		BatchSize:          &batch,
		BackoffSeconds:     &backoff,
		Provider:           "club",
		BaseURL:            "https://predb.example/api",
		FeedURL:            "https://predb.example/rss",
		HTTPTimeoutSeconds: &predbTimeout,
	}
	cfg.Indexing.EnrichTMDB = config.IndexingTMDBConfig{
		Enabled:            &enabled,
		IntervalMinutes:    &interval,
		BatchSize:          &batch,
		BackoffSeconds:     &backoff,
		HTTPTimeoutSeconds: &tmdbTimeout,
		TMDBAPIKey:         "tmdb-key",
		TMDBAccessToken:    "tmdb-token",
	}
	cfg.Indexing.Inspect = config.IndexingInspectConfig{
		WorkDir:                  "/tmp/inspect",
		WorkspaceBackend:         "memory",
		MemoryWorkDir:            "/dev/shm/gonzb-inspect-test",
		FFProbePath:              "ffprobe",
		SevenZipPath:             "7z",
		UnrarPath:                "unrar",
		PAR2Path:                 "par2",
		MaxBytes:                 1024,
		MaxArchiveDepth:          2,
		ToolTimeoutSecs:          15,
		RequireExpectedFileCount: true,
	}

	got, err := deriveUsenetIndexerConfig(cfg)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}

	if got.ScrapeLatest.Interval != 90*time.Second || got.ScrapeLatest.BatchSize != batch {
		t.Fatalf("unexpected scrape_latest stage config: %+v", got.ScrapeLatest)
	}
	if got.ScrapeLatest.Backoff != 9*time.Second || got.ScrapeLatest.Concurrency != concurrency || got.Assemble.Concurrency != concurrency {
		t.Fatalf("unexpected scrape/latest or assemble concurrency: scrape=%+v assemble=%+v", got.ScrapeLatest, got.Assemble)
	}
	if got.Match.ArticleBucketSize != articleBucket || got.Match.HighConfidenceThreshold != matchHigh {
		t.Fatalf("unexpected match config: %+v", got.Match)
	}
	if got.ReleaseMinConfidence != matchHigh || got.ReleaseMinCompletion != 25 || got.ReleaseMinExpectedFileCoveragePct != 92 || !got.RequireExpectedFileCountForContextualObfuscated {
		t.Fatalf("unexpected release thresholds: min_confidence=%v min_completion=%v min_expected_file_coverage_pct=%v require_expected=%v", got.ReleaseMinConfidence, got.ReleaseMinCompletion, got.ReleaseMinExpectedFileCoveragePct, got.RequireExpectedFileCountForContextualObfuscated)
	}
	if !got.ReopenArchivedNZBOnReleaseChange || !got.ReleaseReadyPolicy.RequirePayloadComplete || !got.ReleaseReadyPolicy.RequireExpectedFileCountComplete || !got.ReleaseReadyPolicy.RetainUntilExpectedFileCountComplete {
		t.Fatalf("expected release policy toggles to reach runtime config, got %+v / %+v", got.ReleaseReadyPolicy, got)
	}
	if !got.ReleaseSummaryRefreshStage.Enabled || got.ReleaseSummaryRefreshStage.BatchSize != 10000 || got.ReleaseSummaryRefreshStage.MaxBatches != 10 || got.ReleaseSummaryRefreshStage.Interval != 2*time.Minute {
		t.Fatalf("unexpected release summary refresh stage config: %+v", got.ReleaseSummaryRefreshStage)
	}
	if got.InspectMedia.BatchSize != batch {
		t.Fatalf("expected inspect_media batch size %d, got %+v", batch, got.InspectMedia)
	}
	if got.InspectMedia.Concurrency != concurrency {
		t.Fatalf("expected inspect_media concurrency %d, got %+v", concurrency, got.InspectMedia)
	}
	if got.InspectPAR2.BatchSize != batch || got.InspectPAR2.Concurrency != concurrency {
		t.Fatalf("expected inspect_par2 batch/concurrency from config, got %+v", got.InspectPAR2)
	}
	if got.Inspect.WorkspaceBackend != "memory" || got.Inspect.MemoryWorkDir != "/dev/shm/gonzb-inspect-test" {
		t.Fatalf("expected inspect workspace backend settings, got %+v", got.Inspect)
	}
	if !got.Inspect.RequireExpectedFileCount {
		t.Fatalf("expected inspect expected-file gate to reach runtime options, got %+v", got.Inspect)
	}
	if got.EnrichPreDB.Limit != batch || got.EnrichPreDB.HTTPTimeout != 22*time.Second {
		t.Fatalf("unexpected predb options: %+v", got.EnrichPreDB)
	}
	if got.EnrichTMDB.Limit != batch || got.EnrichTMDB.HTTPTimeout != 33*time.Second {
		t.Fatalf("unexpected tmdb options: %+v", got.EnrichTMDB)
	}
	if got.EnrichPreDBStage.Interval != 90*time.Second || !got.EnrichTMDBStage.Enabled {
		t.Fatalf("unexpected enrich stage config: predb=%+v tmdb=%+v", got.EnrichPreDBStage, got.EnrichTMDBStage)
	}
}

func TestDeriveUsenetIndexerConfigDoesNotInventSourceWindowBackfillCutoff(t *testing.T) {
	cfg := &config.Config{
		Modules: config.ModulesConfig{
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
		Indexing: config.IndexingConfig{
			Newsgroups: []string{"alt.binaries.test"},
		},
	}

	got, err := deriveUsenetIndexerConfig(cfg)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}
	if len(got.BackfillUntilDateByGroup) != 0 {
		t.Fatalf("expected no implicit source-window cutoff, got %+v", got.BackfillUntilDateByGroup)
	}
}

func TestDeriveUsenetIndexerConfigKeepsExplicitBackfillCutoff(t *testing.T) {
	cfg := &config.Config{
		Modules: config.ModulesConfig{
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
		Indexing: config.IndexingConfig{
			Newsgroups: []string{"alt.binaries.test"},
			BackfillUntilDateByGroup: map[string]string{
				"alt.binaries.test": "2026-06-01",
			},
		},
	}

	got, err := deriveUsenetIndexerConfig(cfg)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}

	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if got.BackfillUntilDateByGroup["alt.binaries.test"] != want {
		t.Fatalf("expected explicit cutoff %s, got %+v", want, got.BackfillUntilDateByGroup)
	}
}

func TestDeriveUsenetIndexerConfigUsesPersistedHistoricalScrapeSettings(t *testing.T) {
	cfg := &config.Config{
		Indexing: config.IndexingConfig{
			Newsgroups: []string{"alt.binaries.bootstrap"},
		},
	}
	runtime := app.DefaultRuntimeSettings().Indexing
	runtime.Newsgroups = []string{}
	runtime.ExplicitGroups = []app.IndexingScrapeGroupRuntimeSettings{}
	runtime.MaterializedGroups = []app.IndexingMaterializedGroupRuntimeSettings{}
	runtime.ScrapeLatest.Enabled = false
	runtime.ScrapeBackfill.Enabled = false
	runtime.ScrapeTimeframe.Enabled = true
	runtime.ScrapeTimeframe.BatchSize = 1234
	runtime.ScrapeTimeframes = []app.IndexingScrapeTimeframeRuntimeSettings{{
		ID:        "january-test",
		GroupName: "alt.binaries.history",
		StartDate: "2026-01-12",
		EndDate:   "2026-01-18",
		Enabled:   true,
	}}

	got, err := deriveUsenetIndexerConfig(cfg, runtime)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}
	if len(got.Newsgroups) != 0 {
		t.Fatalf("expected persisted empty active groups to override bootstrap groups, got %+v", got.Newsgroups)
	}
	if !got.ScrapeTimeframe.Enabled || got.ScrapeTimeframe.BatchSize != 1234 {
		t.Fatalf("expected persisted timeframe stage settings, got %+v", got.ScrapeTimeframe)
	}
	if got.ScrapeLatest.Enabled || got.ScrapeBackfill.Enabled {
		t.Fatalf("expected latest and backfill disabled, got latest=%+v backfill=%+v", got.ScrapeLatest, got.ScrapeBackfill)
	}
	if len(got.ScrapeTimeframes) != 1 {
		t.Fatalf("expected one historical timeframe, got %+v", got.ScrapeTimeframes)
	}
	if got.ScrapeTimeframes[0].Group != "alt.binaries.history" {
		t.Fatalf("unexpected timeframe group %+v", got.ScrapeTimeframes[0])
	}
	wantEnd := time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)
	if !got.ScrapeTimeframes[0].End.Equal(wantEnd) {
		t.Fatalf("expected inclusive end date to become %s exclusive, got %s", wantEnd, got.ScrapeTimeframes[0].End)
	}
}

func TestDeriveUsenetIndexerConfigNarrowsHistoricalScrapeWithUTCTimes(t *testing.T) {
	cfg := &config.Config{}
	runtime := app.DefaultRuntimeSettings().Indexing
	runtime.ScrapeTimeframes = []app.IndexingScrapeTimeframeRuntimeSettings{{
		ID:        "upload-window",
		GroupName: "alt.binaries.history",
		StartDate: "2026-07-24",
		StartTime: "14:47:00",
		EndDate:   "2026-07-24",
		EndTime:   "14:49:30",
		Enabled:   true,
	}}

	got, err := deriveUsenetIndexerConfig(cfg, runtime)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}
	if len(got.ScrapeTimeframes) != 1 {
		t.Fatalf("expected one historical timeframe, got %+v", got.ScrapeTimeframes)
	}
	wantStart := time.Date(2026, 7, 24, 14, 47, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 7, 24, 14, 49, 30, 0, time.UTC)
	if !got.ScrapeTimeframes[0].Start.Equal(wantStart) || !got.ScrapeTimeframes[0].End.Equal(wantEnd) {
		t.Fatalf("expected exact UTC window %s-%s, got %+v", wantStart, wantEnd, got.ScrapeTimeframes[0])
	}
}

func TestScopedIndexerServersUsesSharedRuntimeServers(t *testing.T) {
	appCtx := &app.Context{
		BootstrapConfig: &config.Config{},
		SettingsStore: fakeSettingsStore{
			runtime: &app.RuntimeSettings{
				Servers: []app.ServerRuntimeSettings{{
					ID:       "shared",
					Host:     "shared.example.com",
					Port:     563,
					Username: "shared-user",
					Password: "shared-pass",
					TLS:      true,
				}},
				IndexerServers: []app.ServerRuntimeSettings{{
					ID:       "indexer",
					Host:     "indexer.example.com",
					Port:     563,
					Username: "indexer-user",
					Password: "indexer-pass",
					TLS:      true,
				}},
			},
		},
	}

	servers := scopedIndexerServers(appCtx)
	if len(servers) != 1 {
		t.Fatalf("expected one shared indexer server, got %+v", servers)
	}
	if servers[0].ID != "shared" || servers[0].Host != "shared.example.com" || servers[0].Username != "shared-user" {
		t.Fatalf("expected shared server selection, got %+v", servers[0])
	}
}

func TestDeriveUsenetIndexerConfigPreservesAllIndexerServers(t *testing.T) {
	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{ID: "easynews", Host: "easy.example.com", Port: 563, MaxConnection: 20},
			{ID: "newshosting", Host: "newshosting.example.com", Port: 563, MaxConnection: 30, Priority: 1},
		},
		Modules: config.ModulesConfig{
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
	}

	got, err := deriveUsenetIndexerConfig(cfg)
	if err != nil {
		t.Fatalf("deriveUsenetIndexerConfig: %v", err)
	}
	if got.ScrapeServer == nil || got.ScrapeServer.ID != "easynews" {
		t.Fatalf("expected first server retained as compatibility scrape server, got %+v", got.ScrapeServer)
	}
	if len(got.ScrapeServers) != 2 {
		t.Fatalf("expected all indexer servers to be preserved, got %+v", got.ScrapeServers)
	}
	if got.ScrapeServers[1].ID != "newshosting" {
		t.Fatalf("expected newshosting as second scrape server, got %+v", got.ScrapeServers)
	}
}

func startTestNNTPServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen nntp test server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestNNTPConn(conn)
		}
	}()

	return ln.Addr().String()
}

func handleTestNNTPConn(conn net.Conn) {
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "200 test server ready\r\n")
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case line == "DATE\r\n":
			_, _ = fmt.Fprintf(conn, "111 20260604120000\r\n")
		case line == "QUIT\r\n":
			_, _ = fmt.Fprintf(conn, "205 closing connection\r\n")
			return
		default:
			_, _ = fmt.Fprintf(conn, "500 unsupported\r\n")
		}
	}
}

func TestIndexerStageTargetWindow(t *testing.T) {
	start, end := indexerStageTargetWindow(indexerStageConfig{
		TargetWindowEnabled: true,
		TargetWindowStart:   "2026-07-24T14:05:00-04:00",
		TargetWindowEnd:     "2026-07-24T14:21:00-04:00",
	})
	if start == nil || end == nil {
		t.Fatal("expected valid target window")
	}
	if got := start.Format(time.RFC3339); got != "2026-07-24T18:05:00Z" {
		t.Fatalf("start = %s", got)
	}
	if got := end.Format(time.RFC3339); got != "2026-07-24T18:21:00Z" {
		t.Fatalf("end = %s", got)
	}

	start, end = indexerStageTargetWindow(indexerStageConfig{
		TargetWindowEnabled: true,
		TargetWindowStart:   "invalid",
		TargetWindowEnd:     "2026-07-24T14:21:00Z",
	})
	if start != nil || end != nil {
		t.Fatalf("expected invalid target window to be disabled, got %v..%v", start, end)
	}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port %q: %v", addr, err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("lookup port %q: %v", portText, err)
	}
	return host, port
}
