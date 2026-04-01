package wiring

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/app"
)

const (
	moduleNameDownloader    = "downloader"
	moduleNameAggregator    = "aggregator"
	moduleNameUsenetIndexer = "usenet_indexer"
	moduleNameArrNotifier   = "arr_notifier"
)

type downloaderRuntimeModule struct {
	appCtx *app.Context
}

func (m *downloaderRuntimeModule) Name() string { return moduleNameDownloader }

func (m *downloaderRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.Downloader.Enabled
}

func (m *downloaderRuntimeModule) Build(context.Context) error {
	if !m.Enabled() {
		return BuildDownloader(m.appCtx)
	}
	return BuildDownloader(m.appCtx)
}

func (m *downloaderRuntimeModule) Start(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	if m.appCtx.Queue == nil {
		return fmt.Errorf("downloader module is enabled but queue manager is not initialized")
	}

	m.appCtx.Logger.Info("starting downloader queue manager")
	go m.appCtx.Queue.Start(ctx)
	return nil
}

func (m *downloaderRuntimeModule) Reload(ctx context.Context) error {
	if m.appCtx == nil {
		return nil
	}
	if !m.Enabled() {
		return BuildDownloader(m.appCtx)
	}
	if m.appCtx.Queue != nil {
		m.appCtx.Queue.ReloadRuntime(m.appCtx)
	}
	if err := ReloadDownloaderIfIdle(m.appCtx); err != nil {
		return err
	}
	BindApplicationModules(m.appCtx)
	return nil
}

func (m *downloaderRuntimeModule) Close() error {
	if m.appCtx == nil {
		return nil
	}
	if m.appCtx.Queue != nil {
		m.appCtx.Queue.Stop()
	}
	if m.appCtx.NNTP != nil {
		return m.appCtx.NNTP.Close()
	}
	return nil
}

func (m *downloaderRuntimeModule) ReadinessChecks(ctx context.Context) []app.RuntimeCheck {
	if !m.Enabled() {
		return nil
	}

	checks := []app.RuntimeCheck{
		runtimeBoolCheck("job_store", m.appCtx.JobStore != nil, "downloader job store is required"),
		runtimeBoolCheck("queue_file_store", m.appCtx.QueueFileStore != nil, "queue file store is required"),
		runtimeBoolCheck("queue_manager", m.appCtx.Queue != nil, "queue manager is required"),
		runtimeBoolCheck("downloader_runtime", m.appCtx.Downloader != nil, "downloader runtime is required"),
		runtimeBoolCheck("nntp_manager", m.appCtx.NNTP != nil, "NNTP manager is required"),
		runtimeBoolCheck("nzb_parser", m.appCtx.NZBParser != nil, "NZB parser is required"),
	}

	if m.appCtx.JobStore != nil {
		checks = append(checks, runtimeErrorCheck("job_store_ping", m.appCtx.JobStore.Ping(ctx)))
		checks = append(checks, runtimeErrorCheck("job_store_schema", m.appCtx.JobStore.ValidateSchema(ctx)))
	}

	return checks
}

type aggregatorRuntimeModule struct {
	appCtx *app.Context
}

func (m *aggregatorRuntimeModule) Name() string { return moduleNameAggregator }

func (m *aggregatorRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.Aggregator.Enabled
}

func (m *aggregatorRuntimeModule) Build(context.Context) error  { return nil }
func (m *aggregatorRuntimeModule) Start(context.Context) error  { return nil }
func (m *aggregatorRuntimeModule) Reload(context.Context) error { return nil }
func (m *aggregatorRuntimeModule) Close() error                 { return nil }

func (m *aggregatorRuntimeModule) ReadinessChecks(ctx context.Context) []app.RuntimeCheck {
	if !m.Enabled() {
		return nil
	}

	checks := []app.RuntimeCheck{
		runtimeBoolCheck("aggregator_runtime", m.appCtx.Aggregator != nil, "aggregator runtime is required"),
		runtimeBoolCheck("indexer_sources", len(m.appCtx.Config.Indexers) > 0, "at least one indexer source must be configured"),
		runtimeBoolCheck("payload_store", m.appCtx.BlobStore != nil, "payload store is required"),
	}

	if m.appCtx.Config.Store.SearchPersistenceEnabled {
		if m.appCtx.JobStore == nil {
			checks = append(checks, app.RuntimeCheck{
				Name:   "aggregator_cache_store",
				OK:     false,
				Detail: "job store is required when search persistence is enabled",
			})
		} else {
			checks = append(checks, runtimeErrorCheck("aggregator_cache_ping", m.appCtx.JobStore.Ping(ctx)))
			checks = append(checks, runtimeErrorCheck("aggregator_cache_schema", m.appCtx.JobStore.ValidateSchema(ctx)))
		}
	}

	return checks
}

type usenetIndexerRuntimeModule struct {
	appCtx  *app.Context
	current io.Closer
}

func (m *usenetIndexerRuntimeModule) Name() string { return moduleNameUsenetIndexer }

func (m *usenetIndexerRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.UsenetIndexer.Enabled
}

func (m *usenetIndexerRuntimeModule) Build(context.Context) error {
	return m.rebuild()
}

func (m *usenetIndexerRuntimeModule) Start(context.Context) error { return nil }

func (m *usenetIndexerRuntimeModule) Reload(context.Context) error {
	return m.rebuild()
}

func (m *usenetIndexerRuntimeModule) Close() error {
	if m.current == nil {
		return nil
	}
	err := m.current.Close()
	m.current = nil
	return err
}

func (m *usenetIndexerRuntimeModule) ReadinessChecks(ctx context.Context) []app.RuntimeCheck {
	if !m.Enabled() {
		return nil
	}

	checks := []app.RuntimeCheck{
		runtimeBoolCheck("usenet_indexer_runtime", m.appCtx.UsenetIndexer != nil, "usenet indexer runtime is required"),
		runtimeBoolCheck("pgindex_store", m.appCtx.PGIndexStore != nil, "pgindex store is required"),
	}

	if m.appCtx.PGIndexStore != nil {
		checks = append(checks, runtimeErrorCheck("pgindex_ping", m.appCtx.PGIndexStore.Ping(ctx)))
		checks = append(checks, runtimeErrorCheck("pgindex_schema", m.appCtx.PGIndexStore.ValidateSchema(ctx)))
	}

	if m.appCtx.SettingsStore != nil {
		checks = append(checks, runtimeErrorCheck("settings_ping", m.appCtx.SettingsStore.Ping(ctx)))
		checks = append(checks, runtimeErrorCheck("settings_schema", m.appCtx.SettingsStore.ValidateSchema(ctx)))
	}

	return checks
}

func (m *usenetIndexerRuntimeModule) rebuild() error {
	if m.appCtx == nil {
		return nil
	}
	if !m.Enabled() {
		m.appCtx.UsenetIndexer = nil
		return m.Close()
	}

	rt, err := buildUsenetIndexerRuntime(m.appCtx)
	if err != nil {
		return err
	}

	if m.current != nil {
		if closeErr := m.current.Close(); closeErr != nil {
			m.appCtx.Logger.Warn("failed to close previous usenet indexer scrape provider: %v", closeErr)
		}
	}

	m.appCtx.UsenetIndexer = rt.service
	m.current = rt.scrapeProvider
	return nil
}

type arrNotifierRuntimeModule struct {
	appCtx *app.Context
}

func (m *arrNotifierRuntimeModule) Name() string { return moduleNameArrNotifier }

func (m *arrNotifierRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.SettingsStore != nil
}

func (m *arrNotifierRuntimeModule) Build(ctx context.Context) error {
	return BuildArrNotifier(ctx, m.appCtx)
}
func (m *arrNotifierRuntimeModule) Start(context.Context) error { return nil }
func (m *arrNotifierRuntimeModule) Reload(ctx context.Context) error {
	return BuildArrNotifier(ctx, m.appCtx)
}
func (m *arrNotifierRuntimeModule) Close() error { return nil }
func (m *arrNotifierRuntimeModule) ReadinessChecks(context.Context) []app.RuntimeCheck {
	return nil
}

func registerRuntimeModules(appCtx *app.Context) {
	if appCtx == nil {
		return
	}

	appCtx.RegisterRuntimeModules(
		&downloaderRuntimeModule{appCtx: appCtx},
		&aggregatorRuntimeModule{appCtx: appCtx},
		&usenetIndexerRuntimeModule{appCtx: appCtx},
		&arrNotifierRuntimeModule{appCtx: appCtx},
	)
}

func runtimeBoolCheck(name string, ok bool, detail string) app.RuntimeCheck {
	return app.RuntimeCheck{Name: name, OK: ok, Detail: detailIfFalse(ok, detail)}
}

func runtimeErrorCheck(name string, err error) app.RuntimeCheck {
	if err == nil {
		return app.RuntimeCheck{Name: name, OK: true}
	}
	return app.RuntimeCheck{Name: name, OK: false, Detail: err.Error()}
}

func detailIfFalse(ok bool, detail string) string {
	if ok {
		return ""
	}
	return detail
}
