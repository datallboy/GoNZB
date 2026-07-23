package wiring

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/activity"
	"github.com/datallboy/gonzb/internal/gonzbnet/admission"
	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/eventbody"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/publisher"
	"github.com/datallboy/gonzb/internal/gonzbnet/reassigner"
	gonzbnetsync "github.com/datallboy/gonzb/internal/gonzbnet/sync"
	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const moduleNameGoNZBNet = "gonzbnet"

type gonzbnetRuntimeModule struct {
	appCtx         *app.Context
	identity       *identity.Identity
	publisherStore publisher.Store
	poolStore      interface {
		ListActivePoolIDsForNode(context.Context, string) ([]string, error)
		ListActivePoolIDsForNodeCapabilities(context.Context, string, []string) ([]string, error)
	}
	reassignStore  reassigner.Store
	admissionStore interface {
		ListFederationAdmissions(context.Context, string, string, int) ([]pgindex.FederationAdmissionRecord, error)
		FinalizeFederationAdmission(context.Context, string, string, string, string) error
		FederationEventExists(context.Context, string) (bool, error)
		UpsertFederationNodeIdentity(context.Context, string, ed25519.PublicKey) error
		ValidateFederationPoolControlEvent(context.Context, *events.SignedEvent) error
		AppendVerifiedFederationEvent(context.Context, *events.SignedEvent, *events.ValidationResult) error
		ProjectFederationPoolEvent(context.Context, *events.SignedEvent) error
		UpsertFederationPeerURL(context.Context, string) (int64, error)
	}
	pullSync      *gonzbnetsync.Service
	activityStore interface {
		UpsertFederationActivityRollups(context.Context, []activity.Rollup) error
		CompactFederationActivityRollups(context.Context, time.Time) error
	}
	cancel  context.CancelFunc
	running bool
}

func (m *gonzbnetRuntimeModule) Name() string { return moduleNameGoNZBNet }

func (m *gonzbnetRuntimeModule) Enabled() bool {
	return m.appCtx != nil && m.appCtx.Config != nil && m.appCtx.Config.Modules.GoNZBNet.Enabled
}

func (m *gonzbnetRuntimeModule) Build(ctx context.Context) error {
	m.stop()
	activity.Default.Configure(goNZBNetActivityDefinitions(m.appCtx))
	m.identity = nil
	m.publisherStore = nil
	m.poolStore = nil
	m.reassignStore = nil
	m.admissionStore = nil
	m.pullSync = nil
	m.activityStore = nil
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
	nodeID, err := nodeIdentity.NodeID(ctx)
	if err != nil {
		return fmt.Errorf("load gonzbnet node ID: %w", err)
	}
	publicKey, err := nodeIdentity.PublicKey(ctx)
	if err != nil {
		return fmt.Errorf("load gonzbnet public key: %w", err)
	}
	if err := store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return fmt.Errorf("persist local gonzbnet node identity: %w", err)
	}
	poolStore, ok := m.appCtx.PGIndexStore.(interface {
		ListActivePoolIDsForNode(context.Context, string) ([]string, error)
		ListActivePoolIDsForNodeCapabilities(context.Context, string, []string) ([]string, error)
	})
	if !ok {
		return fmt.Errorf("pgindex store does not support gonzbnet pool membership selection")
	}
	m.identity = nodeIdentity
	m.publisherStore = store
	m.poolStore = poolStore
	if policyStore, ok := m.appCtx.PGIndexStore.(interface {
		SetGoNZBNetManifestCachePolicy(int64, int)
	}); ok {
		policyStore.SetGoNZBNetManifestCachePolicy(
			m.appCtx.Config.GoNZBNet.ManifestCacheMaxBytes,
			m.appCtx.Config.GoNZBNet.ManifestCacheTTLDays,
		)
	}
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
	m.reassignStore = reassignStore
	admissionStore, ok := m.appCtx.PGIndexStore.(interface {
		ListFederationAdmissions(context.Context, string, string, int) ([]pgindex.FederationAdmissionRecord, error)
		FinalizeFederationAdmission(context.Context, string, string, string, string) error
		FederationEventExists(context.Context, string) (bool, error)
		UpsertFederationNodeIdentity(context.Context, string, ed25519.PublicKey) error
		ValidateFederationPoolControlEvent(context.Context, *events.SignedEvent) error
		AppendVerifiedFederationEvent(context.Context, *events.SignedEvent, *events.ValidationResult) error
		ProjectFederationPoolEvent(context.Context, *events.SignedEvent) error
		UpsertFederationPeerURL(context.Context, string) (int64, error)
	})
	if !ok {
		return fmt.Errorf("pgindex store does not support gonzbnet admission polling")
	}
	m.admissionStore = admissionStore
	if activityStore, ok := m.appCtx.PGIndexStore.(interface {
		UpsertFederationActivityRollups(context.Context, []activity.Rollup) error
		CompactFederationActivityRollups(context.Context, time.Time) error
	}); ok {
		m.activityStore = activityStore
	}
	return nil
}

func (m *gonzbnetRuntimeModule) Start(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	publishEnabled := m.publisherStore != nil &&
		m.appCtx.Config.GoNZBNet.PublishReleaseCardsEnabled &&
		m.appCtx.Config.GoNZBNet.ScannerEnabled
	healthEnabled := m.publisherStore != nil &&
		m.appCtx.Config.GoNZBNet.HealthAttestationsEnabled &&
		m.appCtx.Config.GoNZBNet.HealthCheckerEnabled
	validationEnabled := m.publisherStore != nil && m.appCtx.Config.GoNZBNet.ValidatorEnabled
	pullEnabled := m.pullSync != nil && m.appCtx.Config.GoNZBNet.PullSyncEnabled
	pushEnabled := m.pullSync != nil && m.appCtx.Config.GoNZBNet.PushSyncEnabled
	gossipEnabled := m.pullSync != nil && m.appCtx.Config.GoNZBNet.WebSocketGossipEnabled
	reassignEnabled := m.reassignStore != nil &&
		m.appCtx.Config.GoNZBNet.CoverageEnabled &&
		m.appCtx.Config.GoNZBNet.SchedulerEnabled &&
		strings.EqualFold(m.appCtx.Config.GoNZBNet.CoverageMode, "automatic")
	admissionEnabled := m.admissionStore != nil
	if !publishEnabled && !healthEnabled && !validationEnabled && !pullEnabled && !pushEnabled && !gossipEnabled && !reassignEnabled && !admissionEnabled {
		return nil
	}
	if m.running {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.running = true
	if m.activityStore != nil {
		go m.runActivityFlush(childCtx)
	}
	if admissionEnabled {
		go m.runAdmissionPolling(childCtx, 30*time.Second)
	}

	if publishEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.PublishReleaseCardsIntervalMin * float64(time.Minute))
		batchSize := m.appCtx.Config.GoNZBNet.PublishReleaseCardsBatchSize
		m.appCtx.Logger.Info("starting gonzbnet release-card publisher interval=%s batch_size=%d", interval, batchSize)
		go func() {
			if err := m.runAcrossPools(childCtx, interval, capability.RequiredForEvent(pools.EventTypeReleaseCard), func(ctx context.Context, poolID string, service *publisher.Service) error {
				finish := activity.Default.Begin(activity.ComponentReleasePublisher, poolID)
				result, err := service.PublishOnce(ctx, batchSize)
				finish(activity.Result{ItemsIn: int64(result.Scanned), ItemsOut: int64(result.Published), Err: err})
				return err
			}); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet release-card publisher failed: %v", err)
			}
		}()
	}
	if healthEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.HealthAttestationsIntervalMin * float64(time.Minute))
		batchSize := m.appCtx.Config.GoNZBNet.HealthAttestationsBatchSize
		m.appCtx.Logger.Info("starting gonzbnet health attestation publisher interval=%s batch_size=%d", interval, batchSize)
		go func() {
			if err := m.runAcrossPools(childCtx, interval, capability.RequiredForEvent(pools.EventTypeHealthAttestation), func(ctx context.Context, poolID string, service *publisher.Service) error {
				finish := activity.Default.Begin(activity.ComponentHealthPublisher, poolID)
				result, err := service.PublishHealthOnce(ctx, batchSize)
				finish(activity.Result{ItemsIn: int64(result.Scanned), ItemsOut: int64(result.Published), Err: err})
				return err
			}); err != nil && childCtx.Err() == nil {
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
			if err := m.runAcrossPools(childCtx, interval, []string{capability.Validator}, func(ctx context.Context, poolID string, service *publisher.Service) error {
				finish := activity.Default.Begin(activity.ComponentValidator, poolID)
				result, err := service.PublishValidationOnce(ctx, batchSize, opts)
				finish(activity.Result{ItemsIn: int64(result.Claimed), ItemsOut: int64(result.Published), Backlog: int64(result.Failed), Err: err})
				return err
			}); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet validator failed: %v", err)
			}
		}()
	}
	if pullEnabled {
		interval := time.Duration(m.appCtx.Config.GoNZBNet.PullSyncIntervalMin * float64(time.Minute))
		m.appCtx.Logger.Info("starting gonzbnet pull sync interval=%s", interval)
		go func() {
			if resolved, err := m.pullSync.RetryPendingProjections(childCtx, 100); err != nil && childCtx.Err() == nil {
				m.appCtx.Logger.Error("gonzbnet pending projection retry failed: %v", err)
			} else if resolved > 0 {
				m.appCtx.Logger.Info("gonzbnet resolved pending projections=%d", resolved)
			}
		}()
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
			if err := m.runReassignAcrossPools(childCtx, interval, limit); err != nil && childCtx.Err() == nil {
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
		runtimeBoolCheck("gonzbnet_publisher", m.publisherStore != nil, "gonzbnet publisher is required"),
		runtimeBoolCheck("gonzbnet_pull_sync", m.pullSync != nil, "gonzbnet pull sync is required"),
		runtimeBoolCheck("gonzbnet_reassigner", m.reassignStore != nil, "gonzbnet stale claim reassigner is required"),
	}
	return checks
}

func (m *gonzbnetRuntimeModule) activePoolIDs(ctx context.Context, requiredCapabilities []string) ([]string, error) {
	if m.identity == nil || m.poolStore == nil {
		return nil, nil
	}
	nodeID, err := m.identity.NodeID(ctx)
	if err != nil {
		return nil, err
	}
	active, err := m.poolStore.ListActivePoolIDsForNodeCapabilities(ctx, nodeID, requiredCapabilities)
	if err != nil {
		return nil, err
	}
	requested := map[string]struct{}{}
	for _, poolID := range m.appCtx.Config.GoNZBNet.PublishPoolIDs {
		if poolID = strings.TrimSpace(poolID); poolID != "" {
			requested[poolID] = struct{}{}
		}
	}
	result := make([]string, 0, len(active))
	for _, poolID := range active {
		poolID = strings.TrimSpace(poolID)
		if poolID == "" {
			continue
		}
		if len(requested) > 0 {
			if _, ok := requested[poolID]; !ok {
				continue
			}
		}
		result = append(result, poolID)
	}
	return result, nil
}

func (m *gonzbnetRuntimeModule) publisherForPool(poolID string) *publisher.Service {
	service := publisher.New(m.identity, m.publisherStore, poolID)
	service.SetReleaseReadyPolicy(gonzbnetReleaseReadyPolicy(m.appCtx))
	service.SetManifestAvailabilityPublishing(m.appCtx.Config.GoNZBNet.ManifestAvailabilityEnabled)
	service.SetManifestBuilding(m.appCtx.Config.GoNZBNet.ManifestBuilderEnabled)
	if checker, ok := m.appCtx.NNTP.(interface {
		FetchBodyPrefixForScope(context.Context, string, string, []string, int64) ([]byte, error)
	}); ok {
		service.SetArticleChecker(func(ctx context.Context, messageID string, groups []string) error {
			_, err := checker.FetchBodyPrefixForScope(ctx, "gonzbnet_validator", messageID, groups, 1)
			return err
		})
	}
	return service
}

func gonzbnetReleaseReadyPolicy(appCtx *app.Context) pgindex.ReleaseReadyPolicy {
	policy := pgindex.DefaultReleaseReadyPolicy()
	runtime := indexerRuntimeSettings(appCtx)
	if runtime == nil || runtime.Indexing == nil {
		return policy
	}
	release := runtime.Indexing.Release
	return pgindex.NormalizeReleaseReadyPolicy(pgindex.ReleaseReadyPolicy{
		MinMatchConfidence:                   release.PublicMinMatchConfidence,
		MinCompletionPct:                     release.PublicMinCompletionPct,
		MinIdentityStatus:                    release.PublicMinIdentityStatus,
		RequireInspection:                    release.PublicRequireInspection,
		RequireEnrichment:                    release.PublicRequireEnrichment,
		RequireClearTitle:                    release.PublicRequireClearTitle,
		RequirePayloadComplete:               release.PublicRequirePayloadComplete,
		RequireExpectedFileCountComplete:     release.PublicRequireExpectedFileCountComplete,
		RequirePAR2:                          release.PublicRequirePAR2,
		RequireNFO:                           release.PublicRequireNFO,
		RequireSFV:                           release.PublicRequireSFV,
		RetainUntilExpectedFileCountComplete: release.RetainUntilExpectedFileCountComplete,
		RetainRequirePAR2:                    release.RetainRequirePAR2,
		RetainRequireNFO:                     release.RetainRequireNFO,
		RetainRequireSFV:                     release.RetainRequireSFV,
	})
}

func (m *gonzbnetRuntimeModule) runAcrossPools(ctx context.Context, interval time.Duration, requiredCapabilities []string, run func(context.Context, string, *publisher.Service) error) error {
	for {
		poolIDs, err := m.activePoolIDs(ctx, requiredCapabilities)
		if err != nil {
			return err
		}
		for _, poolID := range poolIDs {
			if err := run(ctx, poolID, m.publisherForPool(poolID)); err != nil {
				return fmt.Errorf("pool %s: %w", poolID, err)
			}
		}
		if interval <= 0 {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *gonzbnetRuntimeModule) runReassignAcrossPools(ctx context.Context, interval time.Duration, limit int) error {
	for {
		poolIDs, err := m.activePoolIDs(ctx, []string{capability.CoverageCoordinator})
		if err != nil {
			return err
		}
		for _, poolID := range poolIDs {
			finish := activity.Default.Begin(activity.ComponentCoverageScheduler, poolID)
			service, err := reassigner.New(m.identity, m.reassignStore, poolID, m.appCtx.Config.GoNZBNet.CoverageMinTrustForClaim)
			if err != nil {
				finish(activity.Result{Err: err})
				return err
			}
			result, err := service.RunOnce(ctx, limit)
			finish(activity.Result{ItemsIn: int64(result.StaleClaims), ItemsOut: int64(result.AssignmentsCreated), Err: err})
			if err != nil {
				return fmt.Errorf("pool %s: %w", poolID, err)
			}
		}
		if interval <= 0 {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *gonzbnetRuntimeModule) runAdmissionPolling(ctx context.Context, interval time.Duration) {
	for {
		finish := activity.Default.Begin(activity.ComponentAdmissionPoller, "")
		err := m.refreshPendingAdmissions(ctx)
		finish(activity.Result{Err: err})
		if err != nil && ctx.Err() == nil {
			m.appCtx.Logger.Warn("gonzbnet admission refresh failed: %v", err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *gonzbnetRuntimeModule) runActivityFlush(ctx context.Context) {
	ticker := time.NewTicker(activity.RollupBucket)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.flushActivity(ctx); err != nil && ctx.Err() == nil {
				m.appCtx.Logger.Warn("gonzbnet activity rollup flush failed: %v", err)
			}
		}
	}
}

func (m *gonzbnetRuntimeModule) flushActivity(ctx context.Context) error {
	if m.activityStore == nil || m.identity == nil {
		return nil
	}
	nodeID, err := m.identity.NodeID(ctx)
	if err != nil {
		return err
	}
	items := activity.Default.DrainRollups(nodeID)
	if len(items) == 0 {
		return nil
	}
	if err := m.activityStore.UpsertFederationActivityRollups(ctx, items); err != nil {
		activity.Default.RestoreRollups(items)
		return err
	}
	return m.activityStore.CompactFederationActivityRollups(ctx, time.Now().UTC())
}

func goNZBNetActivityDefinitions(appCtx *app.Context) []activity.Definition {
	if appCtx == nil || appCtx.Config == nil {
		return nil
	}
	cfg := appCtx.Config.GoNZBNet
	return activity.Definitions(activity.Configuration{
		ModuleEnabled: appCtx.Config.Modules.GoNZBNet.Enabled, StoreReady: appCtx.PGIndexStore != nil,
		ConsumerEnabled: cfg.ConsumerEnabled, ScannerEnabled: cfg.ScannerEnabled,
		IndexProjectionEnabled: cfg.IndexProjectionEnabled, ManifestCacheEnabled: cfg.ManifestCacheEnabled,
		ValidatorEnabled: cfg.ValidatorEnabled, HealthCheckerEnabled: cfg.HealthCheckerEnabled,
		CoverageEnabled: cfg.CoverageEnabled, SchedulerEnabled: cfg.SchedulerEnabled,
		PublishReleaseCardsEnabled: cfg.PublishReleaseCardsEnabled, HealthAttestationsEnabled: cfg.HealthAttestationsEnabled,
		PullSyncEnabled: cfg.PullSyncEnabled, PushSyncEnabled: cfg.PushSyncEnabled,
		WebSocketGossipEnabled: cfg.WebSocketGossipEnabled, RelayEnabled: cfg.RelayEnabled,
		PeerExchangeEnabled: cfg.PeerExchangeEnabled, CoverageMode: cfg.CoverageMode,
		PublishReleaseCardsInterval: time.Duration(cfg.PublishReleaseCardsIntervalMin * float64(time.Minute)),
		HealthAttestationsInterval:  time.Duration(cfg.HealthAttestationsIntervalMin * float64(time.Minute)),
		ValidationInterval:          time.Duration(cfg.ValidationIntervalMin * float64(time.Minute)),
		PullSyncInterval:            time.Duration(cfg.PullSyncIntervalMin * float64(time.Minute)),
		PushSyncInterval:            time.Duration(cfg.PushSyncIntervalMin * float64(time.Minute)),
		GossipInterval:              time.Duration(cfg.GossipIntervalMin * float64(time.Minute)),
		CoverageSchedulerInterval:   time.Duration(cfg.ScannerCheckpointIntervalSecs) * time.Second,
	})
}

func (m *gonzbnetRuntimeModule) refreshPendingAdmissions(ctx context.Context) error {
	nodeID, err := m.identity.NodeID(ctx)
	if err != nil {
		return err
	}
	items, err := m.admissionStore.ListFederationAdmissions(ctx, "", "pending", 100)
	if err != nil {
		return err
	}
	client := admission.NewClient(m.identity, m.appCtx.Config.GoNZBNet.AllowInsecurePeerHTTP)
	for _, item := range items {
		if item.CandidateNodeID != nodeID || strings.TrimSpace(item.RelayURL) == "" {
			continue
		}
		status, err := client.FetchStatus(ctx, item.RelayURL, item.PoolID, item.ProposalEventID)
		if err != nil {
			m.appCtx.Logger.Warn("gonzbnet admission status unavailable pool=%s relay=%s: %v", item.PoolID, item.RelayURL, err)
			continue
		}
		if status.Status == "rejected" {
			if err := m.admissionStore.FinalizeFederationAdmission(ctx, item.ProposalEventID, "", "rejected", status.RejectionReason); err != nil {
				return err
			}
			continue
		}
		admissionEvents := make([]*events.SignedEvent, 0, len(status.TrustEvents)+2)
		admissionEvents = append(admissionEvents, status.GenesisEvent)
		admissionEvents = append(admissionEvents, status.TrustEvents...)
		admissionEvents = append(admissionEvents, status.ApprovalEvent)
		seenEvents := map[string]struct{}{}
		for _, event := range admissionEvents {
			if event == nil {
				continue
			}
			if _, seen := seenEvents[event.EventID]; seen {
				continue
			}
			seenEvents[event.EventID] = struct{}{}
			validation, err := events.VerifyWithin(event, time.Now().UTC(), time.Duration(m.appCtx.Config.GoNZBNet.TimeToleranceSeconds)*time.Second, 0)
			if err != nil || validation == nil || !validation.OK {
				return fmt.Errorf("admission event %s failed verification", event.EventID)
			}
			if err := eventbody.Validate(event, time.Now().UTC(), time.Duration(m.appCtx.Config.GoNZBNet.TimeToleranceSeconds)*time.Second); err != nil {
				return err
			}
			publicKey, err := canonical.DecodeBase64URL(event.AuthorPublicKey)
			if err != nil || len(publicKey) != ed25519.PublicKeySize {
				return fmt.Errorf("admission event %s has invalid author key", event.EventID)
			}
			if err := m.admissionStore.UpsertFederationNodeIdentity(ctx, event.AuthorNodeID, ed25519.PublicKey(publicKey)); err != nil {
				return err
			}
			if err := m.admissionStore.ValidateFederationPoolControlEvent(ctx, event); err != nil {
				return err
			}
			exists, err := m.admissionStore.FederationEventExists(ctx, event.EventID)
			if err != nil {
				return err
			}
			if !exists {
				if err := m.admissionStore.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
					return err
				}
			}
			if err := m.admissionStore.ProjectFederationPoolEvent(ctx, event); err != nil {
				return err
			}
		}
		if status.Status == "approved" {
			for _, endpoint := range status.MemberEndpoints {
				if strings.TrimSpace(endpoint.NodeID) == "" || endpoint.NodeID == nodeID || strings.TrimSpace(endpoint.BaseURL) == "" {
					continue
				}
				if err := transportpolicy.ValidateHTTPURL(endpoint.BaseURL, m.appCtx.Config.GoNZBNet.AllowInsecurePeerHTTP); err != nil {
					return fmt.Errorf("pool member endpoint failed transport policy: %w", err)
				}
				if _, err := m.admissionStore.UpsertFederationPeerURL(ctx, endpoint.BaseURL); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (m *gonzbnetRuntimeModule) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.running = false
}
