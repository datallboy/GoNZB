package wiring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/publisher"
	"github.com/datallboy/gonzb/internal/gonzbnet/reassigner"
	gonzbnetsync "github.com/datallboy/gonzb/internal/gonzbnet/sync"
)

const moduleNameGoNZBNet = "gonzbnet"

type gonzbnetRuntimeModule struct {
	appCtx    *app.Context
	publisher *publisher.Service
	pullSync  *gonzbnetsync.Service
	reassign  *reassigner.Service
	cancel    context.CancelFunc
	running   bool
}

func (m *gonzbnetRuntimeModule) Name() string { return moduleNameGoNZBNet }

func (m *gonzbnetRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.GoNZBNet.Enabled
}

func (m *gonzbnetRuntimeModule) Build(ctx context.Context) error {
	m.stop()
	m.publisher = nil
	m.pullSync = nil
	m.reassign = nil
	if !m.Enabled() {
		return nil
	}
	if m.appCtx.PGIndexStore == nil {
		return fmt.Errorf("gonzbnet module requires pgindex store")
	}
	store, ok := m.appCtx.PGIndexStore.(publisher.Store)
	if !ok {
		return fmt.Errorf("pgindex store does not support gonzbnet publishing")
	}
	nodeIdentity, err := identity.LoadOrCreateWithPassword(m.appCtx.Config.GoNZBNet.KeysDir, m.appCtx.Config.GoNZBNet.KeyPassword)
	if err != nil {
		return err
	}
	m.publisher = publisher.New(nodeIdentity, store, m.appCtx.Config.GoNZBNet.LocalPoolID)
	m.publisher.SetManifestAvailabilityPublishing(m.appCtx.Config.GoNZBNet.ManifestAvailabilityEnabled)
	syncStore, ok := m.appCtx.PGIndexStore.(gonzbnetsync.Store)
	if !ok {
		return fmt.Errorf("pgindex store does not support gonzbnet pull sync")
	}
	m.pullSync = gonzbnetsync.NewWithOptions(nodeIdentity, syncStore, m.appCtx.Logger, gonzbnetsync.Options{
		AllowInsecurePeerHTTP: m.appCtx.Config.GoNZBNet.AllowInsecurePeerHTTP,
		EventTimeTolerance:    time.Duration(m.appCtx.Config.GoNZBNet.TimeToleranceSeconds) * time.Second,
		MaxEventAge:           time.Duration(m.appCtx.Config.GoNZBNet.MaxEventAgeHours) * time.Hour,
	})
	if err := m.pullSync.UpsertManualPeers(ctx, m.appCtx.Config.GoNZBNet.ManualPeers); err != nil {
		return err
	}
	reassignStore, ok := m.appCtx.PGIndexStore.(reassigner.Store)
	if !ok {
		return fmt.Errorf("pgindex store does not support gonzbnet stale claim reassignment")
	}
	m.reassign, err = reassigner.New(nodeIdentity, reassignStore, m.appCtx.Config.GoNZBNet.LocalPoolID, m.appCtx.Config.GoNZBNet.CoverageMinTrustForClaim)
	if err != nil {
		return err
	}
	return nil
}

func (m *gonzbnetRuntimeModule) Start(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	publishEnabled := m.publisher != nil &&
		m.appCtx.Config.GoNZBNet.PublishReleaseCardsEnabled &&
		m.appCtx.Config.GoNZBNet.ScannerEnabled
	healthEnabled := m.publisher != nil &&
		m.appCtx.Config.GoNZBNet.HealthAttestationsEnabled &&
		m.appCtx.Config.GoNZBNet.HealthCheckerEnabled
	validationEnabled := m.publisher != nil && m.appCtx.Config.GoNZBNet.ValidatorEnabled
	pullEnabled := m.pullSync != nil && m.appCtx.Config.GoNZBNet.PullSyncEnabled
	pushEnabled := m.pullSync != nil && m.appCtx.Config.GoNZBNet.PushSyncEnabled
	gossipEnabled := m.pullSync != nil && m.appCtx.Config.GoNZBNet.WebSocketGossipEnabled
	reassignEnabled := m.reassign != nil &&
		m.appCtx.Config.GoNZBNet.CoverageEnabled &&
		m.appCtx.Config.GoNZBNet.SchedulerEnabled &&
		strings.EqualFold(m.appCtx.Config.GoNZBNet.CoverageMode, "automatic")
	if !publishEnabled && !healthEnabled && !validationEnabled && !pullEnabled && !pushEnabled && !gossipEnabled && !reassignEnabled {
		return nil
	}
	if m.running {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.running = true

	if publishEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.PublishReleaseCardsIntervalMin * float64(time.Minute))
		batchSize := m.appCtx.Config.GoNZBNet.PublishReleaseCardsBatchSize
		m.appCtx.Logger.Info("starting gonzbnet release-card publisher interval=%s batch_size=%d", interval, batchSize)
		go func() {
			if err := m.publisher.Run(childCtx, interval, batchSize); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet release-card publisher failed: %v", err)
			}
		}()
	}
	if healthEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.HealthAttestationsIntervalMin * float64(time.Minute))
		batchSize := m.appCtx.Config.GoNZBNet.HealthAttestationsBatchSize
		m.appCtx.Logger.Info("starting gonzbnet health attestation publisher interval=%s batch_size=%d", interval, batchSize)
		go func() {
			if err := m.publisher.RunHealth(childCtx, interval, batchSize); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet health attestation publisher failed: %v", err)
			}
		}()
	}
	if validationEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.ValidationIntervalMin * float64(time.Minute))
		batchSize := m.appCtx.Config.GoNZBNet.ValidationBatchSize
		opts := publisher.ValidationOptions{
			ChecksumEnabled: m.appCtx.Config.GoNZBNet.ChecksumValidationEnabled,
			MaxTasksPerHour: batchSize,
		}
		m.appCtx.Logger.Info("starting gonzbnet validator interval=%s batch_size=%d checksum_validation=%v", interval, batchSize, opts.ChecksumEnabled)
		go func() {
			if err := m.publisher.RunValidation(childCtx, interval, batchSize, opts); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet validator failed: %v", err)
			}
		}()
	}
	if pullEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.PullSyncIntervalMin * float64(time.Minute))
		m.appCtx.Logger.Info("starting gonzbnet pull sync interval=%s", interval)
		go func() {
			if err := m.pullSync.Run(childCtx, interval); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet pull sync failed: %v", err)
			}
		}()
	}
	if pushEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.PushSyncIntervalMin * float64(time.Minute))
		batchSize := m.appCtx.Config.GoNZBNet.PushSyncBatchSize
		m.appCtx.Logger.Info("starting gonzbnet push sync interval=%s batch_size=%d", interval, batchSize)
		go func() {
			if err := m.pullSync.RunPush(childCtx, interval, batchSize); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet push sync failed: %v", err)
			}
		}()
	}
	if gossipEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.GossipIntervalMin * float64(time.Minute))
		opts := gonzbnetsync.GossipOptions{
			NetworkID:           m.appCtx.Config.GoNZBNet.NetworkID,
			TTL:                 m.appCtx.Config.GoNZBNet.GossipTTL,
			BatchSize:           m.appCtx.Config.GoNZBNet.GossipBatchSize,
			Fanout:              m.appCtx.Config.GoNZBNet.GossipFanout,
			PeerExchangeEnabled: m.appCtx.Config.GoNZBNet.PeerExchangeEnabled,
		}
		m.appCtx.Logger.Info("starting gonzbnet websocket gossip interval=%s batch_size=%d ttl=%d fanout=%d peer_exchange=%v", interval, opts.BatchSize, opts.TTL, opts.Fanout, opts.PeerExchangeEnabled)
		go func() {
			if err := m.pullSync.RunGossip(childCtx, interval, opts); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet websocket gossip failed: %v", err)
			}
		}()
	}
	if reassignEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.ScannerCheckpointIntervalSecs) * time.Second
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		limit := m.appCtx.Config.GoNZBNet.ScannerMaxGroups
		if limit <= 0 {
			limit = 25
		}
		m.appCtx.Logger.Info("starting gonzbnet stale claim reassigner interval=%s limit=%d", interval, limit)
		go func() {
			if err := m.reassign.Run(childCtx, interval, limit); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet stale claim reassigner failed: %v", err)
			}
		}()
	}
	return nil
}

func (m *gonzbnetRuntimeModule) Reload(ctx context.Context) error {
	wasRunning := m.running
	m.stop()
	if err := m.Build(ctx); err != nil {
		return err
	}
	if wasRunning {
		return m.Start(ctx)
	}
	return nil
}

func (m *gonzbnetRuntimeModule) Close() error {
	m.stop()
	return nil
}

func (m *gonzbnetRuntimeModule) ReadinessChecks(context.Context) []app.RuntimeCheck {
	if !m.Enabled() {
		return nil
	}
	checks := []app.RuntimeCheck{
		runtimeBoolCheck("gonzbnet_pgindex_store", m.appCtx.PGIndexStore != nil, "pgindex store is required"),
		runtimeBoolCheck("gonzbnet_publisher", m.publisher != nil, "gonzbnet publisher is required"),
		runtimeBoolCheck("gonzbnet_pull_sync", m.pullSync != nil, "gonzbnet pull sync is required"),
		runtimeBoolCheck("gonzbnet_reassigner", m.reassign != nil, "gonzbnet stale claim reassigner is required"),
	}
	return checks
}

func (m *gonzbnetRuntimeModule) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.running = false
}
