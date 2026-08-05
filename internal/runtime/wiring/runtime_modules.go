package wiring

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/uploader"
)

const (
	moduleNameAggregator    = "aggregator"
	moduleNameUsenetIndexer = "usenet_indexer"
	moduleNameUploader      = "uploader"
)

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
		runtimeBoolCheck("indexer_sources", aggregatorHasSource(m.appCtx.Config), "at least one aggregator source must be configured"),
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

func aggregatorHasSource(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return len(cfg.Indexers) > 0 ||
		cfg.Modules.Uploader.Enabled ||
		cfg.Aggregator.Sources.LocalBlob.Enabled ||
		cfg.Aggregator.Sources.UsenetIndexer.Enabled ||
		cfg.Aggregator.Sources.GoNZBNet.Enabled
}

type uploaderRuntimeModule struct {
	appCtx  *app.Context
	scanner *uploader.InboxScanner
	cancel  context.CancelFunc
}

func (m *uploaderRuntimeModule) Name() string { return moduleNameUploader }

func (m *uploaderRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.Uploader.Enabled
}

func (m *uploaderRuntimeModule) Build(context.Context) error {
	if !m.Enabled() {
		m.appCtx.Uploader = nil
		m.appCtx.UploaderFederation = nil
		m.scanner = nil
		return nil
	}
	if m.appCtx.UploaderStore == nil {
		return fmt.Errorf("uploader store is required")
	}
	cfg := m.appCtx.Config.Uploader
	m.appCtx.Uploader = uploader.NewService(m.appCtx.UploaderStore, nzb.Limits{
		MaxBytes:          cfg.MaxNZBBytes,
		MaxFiles:          cfg.MaxFiles,
		MaxSegments:       cfg.MaxSegments,
		MaxXMLDepth:       cfg.MaxXMLDepth,
		MaxMetadataLength: cfg.MaxMetadataLength,
	}, uploader.IntakeLimits{MaxArtifactBytes: cfg.MaxArtifactBytes, MaxSubmissionBytes: cfg.MaxSubmissionBytes})
	if m.appCtx.UploaderFederationBackend != nil {
		m.appCtx.UploaderFederation = uploader.NewFederationService(m.appCtx.Uploader, m.appCtx.UploaderFederationBackend)
	} else {
		m.appCtx.UploaderFederation = nil
	}
	if cfg.Inbox.Enabled {
		m.scanner = uploader.NewInboxScanner(m.appCtx.Uploader, uploader.InboxOptions{
			Root:         cfg.Inbox.Path,
			ScanInterval: time.Duration(cfg.Inbox.ScanIntervalSeconds) * time.Second,
			SettleAge:    time.Duration(cfg.Inbox.SettleAgeSeconds) * time.Second,
			MaxNZBBytes:  cfg.MaxNZBBytes,
			Logger:       m.appCtx.Logger,
		})
	} else {
		m.scanner = nil
	}
	return nil
}

func (m *uploaderRuntimeModule) Start(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	if m.scanner != nil {
		if err := m.scanner.Check(); err != nil {
			return err
		}
	}
	if m.scanner == nil && m.appCtx.UploaderFederation == nil {
		return nil
	}
	child, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	if m.scanner != nil {
		go func() {
			if err := m.scanner.Start(child); err != nil && child.Err() == nil && m.appCtx.Logger != nil {
				m.appCtx.Logger.Error("uploader inbox stopped: %v", err)
			}
		}()
	}
	if m.appCtx.UploaderFederation != nil {
		go m.appCtx.UploaderFederation.Run(child, 30*time.Second)
	}
	return nil
}

func (m *uploaderRuntimeModule) Reload(ctx context.Context) error {
	wasRunning := m.cancel != nil
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if err := m.Build(ctx); err != nil {
		return err
	}
	if wasRunning {
		return m.Start(ctx)
	}
	return nil
}

func (m *uploaderRuntimeModule) Close() error {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	return nil
}

func (m *uploaderRuntimeModule) ReadinessChecks(ctx context.Context) []app.RuntimeCheck {
	if !m.Enabled() {
		return nil
	}
	checks := []app.RuntimeCheck{
		runtimeBoolCheck("uploader_service", m.appCtx.Uploader != nil, "uploader service is required"),
		runtimeBoolCheck("uploader_store", m.appCtx.UploaderStore != nil, "uploader store is required"),
	}
	if m.appCtx.UploaderStore != nil {
		checks = append(checks, runtimeErrorCheck("uploader_store_ping", m.appCtx.UploaderStore.Ping(ctx)))
		checks = append(checks, runtimeErrorCheck("uploader_store_schema", m.appCtx.UploaderStore.ValidateSchema(ctx)))
	}
	if m.scanner != nil {
		checks = append(checks, runtimeErrorCheck("uploader_inbox", m.scanner.Check()))
	}
	return checks
}

type usenetIndexerRuntimeModule struct {
	appCtx                   *app.Context
	current                  io.Closer
	telemetry                io.Closer
	runParent                context.Context
	runCancel                context.CancelFunc
	running                  bool
	stageOwner               string
	nntpStats                func() app.NNTPRuntimeStats
	partitionCreateDaysAhead int
	partitionDDLLockTimeout  time.Duration
}

func (m *usenetIndexerRuntimeModule) Name() string { return moduleNameUsenetIndexer }

func (m *usenetIndexerRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.UsenetIndexer.Enabled
}

func (m *usenetIndexerRuntimeModule) Build(ctx context.Context) error {
	return m.rebuild(ctx)
}

func (m *usenetIndexerRuntimeModule) Start(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	if m.appCtx == nil || m.appCtx.UsenetIndexer == nil {
		return fmt.Errorf("usenet indexer runtime is required")
	}
	if m.running {
		return nil
	}

	m.runParent = ctx
	m.running = true
	m.startCurrentRuntime()
	return nil
}

func (m *usenetIndexerRuntimeModule) Reload(ctx context.Context) error {
	return m.rebuild(ctx)
}

func (m *usenetIndexerRuntimeModule) Close() error {
	m.stopRuntime()
	if m.telemetry != nil {
		_ = m.telemetry.Close()
		m.telemetry = nil
	}
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

func (m *usenetIndexerRuntimeModule) rebuild(parent context.Context) error {
	if m.appCtx == nil {
		return nil
	}
	if !m.Enabled() {
		m.stopRuntime()
		m.appCtx.UsenetIndexer = nil
		return m.Close()
	}
	if m.stageOwner == "" {
		m.stageOwner = newIndexerStageOwner()
	}

	rt, err := buildUsenetIndexerRuntime(m.appCtx, m.stageOwner)
	if err != nil {
		return err
	}

	oldCloser := m.current
	wasRunning := m.running
	if parent != nil {
		m.runParent = parent
	}
	m.appCtx.UsenetIndexer = rt.service
	m.current = rt.scrapeProvider
	m.nntpStats = rt.nntpStats
	m.partitionCreateDaysAhead = rt.partitionCreateDaysAhead
	m.partitionDDLLockTimeout = rt.partitionDDLLockTimeout
	if wasRunning {
		m.stopRuntime()
		m.running = true
		m.startCurrentRuntime()
	}

	if oldCloser != nil {
		if closeErr := oldCloser.Close(); closeErr != nil {
			m.appCtx.Logger.Warn("failed to close previous usenet indexer scrape provider: %v", closeErr)
		}
	}
	return nil
}

func (m *usenetIndexerRuntimeModule) startCurrentRuntime() {
	if m.appCtx == nil || m.appCtx.UsenetIndexer == nil {
		return
	}

	parent := m.runParent
	if parent == nil {
		parent = context.Background()
	}

	childCtx, childCancel := context.WithCancel(parent)
	m.runCancel = childCancel
	if m.telemetry != nil {
		_ = m.telemetry.Close()
		m.telemetry = nil
	}
	if store, ok := m.appCtx.PGIndexStore.(nntpSnapshotStore); ok {
		m.telemetry = startIndexerNNTPSnapshotPublisher(childCtx, m.appCtx.Logger, store, m.stageOwner, m.nntpStats)
	}

	service := m.appCtx.UsenetIndexer
	if m.appCtx.PGIndexStore != nil {
		m.appCtx.PGIndexStore.ConfigurePartitionProvisioning(m.partitionDDLLockTimeout)
		m.appCtx.Logger.Info("pre-provisioning scrape partitions current_day=true days_ahead=%d ddl_lock_timeout=%s", m.partitionCreateDaysAhead, m.partitionDDLLockTimeout)
		if err := m.appCtx.PGIndexStore.ProvisionSourceWorkPartitions(childCtx, 0, m.partitionCreateDaysAhead); err != nil {
			childCancel()
			m.runCancel = nil
			m.running = false
			m.appCtx.Logger.Error("usenet indexer partition provisioning failed: %v", err)
			return
		}
	}
	m.appCtx.Logger.Info("starting usenet indexer supervisor")
	go m.runPartitionProvisioner(childCtx, m.partitionCreateDaysAhead)
	go func() {
		if err := service.Start(childCtx, 0); err != nil && childCtx.Err() == nil {
			m.appCtx.Logger.Error("usenet indexer supervisor failed: %v", err)
		}
	}()
}

func (m *usenetIndexerRuntimeModule) runPartitionProvisioner(ctx context.Context, daysAhead int) {
	for {
		now := time.Now().UTC()
		nextUTCRefresh := now.Truncate(24 * time.Hour).Add(24*time.Hour + time.Minute)
		timer := time.NewTimer(time.Until(nextUTCRefresh))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if m.appCtx == nil || m.appCtx.PGIndexStore == nil {
				continue
			}
			if err := m.appCtx.PGIndexStore.ProvisionSourceWorkPartitions(ctx, 0, daysAhead); err != nil && ctx.Err() == nil {
				m.appCtx.Logger.Error("usenet indexer UTC rollover partition provisioning failed: %v", err)
			}
		}
	}
}

func (m *usenetIndexerRuntimeModule) stopRuntime() {
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
	if m.telemetry != nil {
		_ = m.telemetry.Close()
		m.telemetry = nil
	}
	m.running = false
}

func registerRuntimeModules(appCtx *app.Context) {
	if appCtx == nil {
		return
	}

	appCtx.RegisterRuntimeModules(
		&aggregatorRuntimeModule{appCtx: appCtx},
		&usenetIndexerRuntimeModule{appCtx: appCtx},
		&gonzbnetRuntimeModule{appCtx: appCtx},
		&uploaderRuntimeModule{appCtx: appCtx},
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
