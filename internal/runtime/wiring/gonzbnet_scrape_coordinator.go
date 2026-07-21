package wiring

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/activity"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type gonzbnetScrapeCoordinatorStore interface {
	CheckCoverageRangeBlock(ctx context.Context, params pgindex.CoverageRangeBlockParams) (pgindex.CoverageRangeBlock, error)
	SuggestCoverageWork(ctx context.Context, params pgindex.CoverageWorkSuggestionParams) ([]pgindex.CoverageWorkSuggestion, error)
	UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error
	NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error)
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error
	ListActivePoolIDsForNodeCapabilities(ctx context.Context, nodeID string, required []string) ([]string, error)
}

type gonzbnetMultiPoolScrapeRangeCoordinator struct {
	identity            *identity.Identity
	store               gonzbnetScrapeCoordinatorStore
	preferredPoolID     string
	enabledPoolIDs      map[string]struct{}
	claimTTL            time.Duration
	minBlockingTrust    float64
	providerScopeHash   string
	allowAssigned       bool
	allowUnassigned     bool
	respectRemoteClaims bool
	mu                  sync.Mutex
	coordinators        map[string]*gonzbnetScrapeRangeCoordinator
}

type gonzbnetScrapeRangeCoordinator struct {
	identity            *identity.Identity
	store               gonzbnetScrapeCoordinatorStore
	poolID              string
	nodeID              string
	claimTTL            time.Duration
	minBlockingTrust    float64
	providerScopeHash   string
	allowAssigned       bool
	allowUnassigned     bool
	respectRemoteClaims bool
	now                 func() time.Time
	mu                  sync.Mutex
}

// ObserveScrapeRun publishes node-level scanner health and capacity from the
// existing indexer stage metrics. It is intentionally optional so the indexer
// remains independent of GoNZBNet.
func (c *gonzbnetScrapeRangeCoordinator) ObserveScrapeRun(ctx context.Context, metrics map[string]any, runErr error) {
	if c == nil {
		return
	}
	items := int64(0)
	if value, ok := metrics["article_headers_seen"].(int64); ok {
		items = value
	}
	activity.Default.Record(activity.ComponentScanner, c.poolID, activity.Result{ItemsIn: items, ItemsOut: items, Err: runErr})
	nodeID, err := c.localNode(ctx)
	if err != nil {
		return
	}
	now := c.now().UTC().Format(time.RFC3339)
	status := "healthy"
	if runErr != nil {
		status = "degraded"
	}
	maxGroups := 0
	if value, ok := metrics["groups_total"].(int); ok {
		maxGroups = value
	}
	maxArticles := int64(0)
	if value, ok := metrics["article_headers_seen"].(int64); ok {
		maxArticles = value
	}
	capacity := coverage.ScannerCapacity{
		SchemaVersion: "1.0", Type: coverage.TypeScannerCapacity,
		NodeID: nodeID, PoolID: c.poolID, CreatedAt: now,
		MaxGroups: maxGroups, MaxArticlesPerHour: maxArticles,
		SupportsArticleRangeScan: true, SupportsTimeWindowScan: true,
		ProviderScope: c.providerScopeHash,
	}
	heartbeat := coverage.ScannerHeartbeat{
		SchemaVersion: "1.0", Type: coverage.TypeScannerHeartbeat,
		NodeID: nodeID, PoolID: c.poolID, CreatedAt: now,
		QueueDepth: 0, CurrentArticlesPerMinute: maxArticles, Status: status,
	}
	if err := c.signAppendProject(ctx, coverage.TypeScannerCapacity, capacity); err != nil {
		return
	}
	_ = c.signAppendProject(ctx, coverage.TypeScannerHeartbeat, heartbeat)
}

func goNZBNetScrapeRangeCoordinatorFor(appCtx *app.Context) (scrape.RangeCoordinator, error) {
	if appCtx == nil || appCtx.Config == nil {
		return nil, nil
	}
	if !appCtx.Config.Modules.GoNZBNet.Enabled {
		return nil, nil
	}
	cfg := appCtx.Config.GoNZBNet
	if !cfg.ScannerEnabled || !cfg.CoverageEnabled {
		return nil, nil
	}
	allowAssigned := strings.EqualFold(cfg.CoverageMode, "scheduler") || strings.EqualFold(cfg.CoverageMode, "automatic")
	if !cfg.ScannerAllowUnassignedWork && !allowAssigned {
		return nil, nil
	}
	store, ok := appCtx.PGIndexStore.(gonzbnetScrapeCoordinatorStore)
	if !ok {
		return nil, fmt.Errorf("gonzbnet scrape coordinator store is unavailable")
	}
	nodeIdentity, err := identity.LoadOrCreateWithPassword(cfg.KeysDir, cfg.KeyPassword)
	if err != nil {
		return nil, err
	}
	return newGoNZBNetMultiPoolScrapeRangeCoordinator(
		nodeIdentity,
		store,
		cfg.LocalPoolID,
		cfg.PublishPoolIDs,
		time.Duration(cfg.ScannerClaimTTLMinutes)*time.Minute,
		cfg.CoverageMinTrustForClaim,
		providerBackboneHashForIndexer(appCtx),
		allowAssigned,
		cfg.ScannerAllowUnassignedWork,
		cfg.ScannerRespectRemoteClaims,
	)
}

func newGoNZBNetMultiPoolScrapeRangeCoordinator(nodeIdentity *identity.Identity, store gonzbnetScrapeCoordinatorStore, preferredPoolID string, enabledPoolIDs []string, claimTTL time.Duration, minBlockingTrust float64, providerScopeHash string, allowAssigned, allowUnassigned, respectRemoteClaims bool) (*gonzbnetMultiPoolScrapeRangeCoordinator, error) {
	if nodeIdentity == nil || store == nil {
		return nil, fmt.Errorf("gonzbnet scrape coordinator dependencies are required")
	}
	enabled := make(map[string]struct{}, len(enabledPoolIDs))
	for _, poolID := range enabledPoolIDs {
		if poolID = strings.TrimSpace(poolID); poolID != "" {
			enabled[poolID] = struct{}{}
		}
	}
	return &gonzbnetMultiPoolScrapeRangeCoordinator{
		identity: nodeIdentity, store: store,
		preferredPoolID: strings.TrimSpace(preferredPoolID), enabledPoolIDs: enabled,
		claimTTL: claimTTL, minBlockingTrust: minBlockingTrust,
		providerScopeHash: strings.TrimSpace(providerScopeHash),
		allowAssigned:     allowAssigned, allowUnassigned: allowUnassigned,
		respectRemoteClaims: respectRemoteClaims,
		coordinators:        map[string]*gonzbnetScrapeRangeCoordinator{},
	}, nil
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) activeCoordinators(ctx context.Context) ([]*gonzbnetScrapeRangeCoordinator, error) {
	nodeID, err := c.identity.NodeID(ctx)
	if err != nil {
		return nil, err
	}
	publicKey, err := c.identity.PublicKey(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return nil, err
	}
	poolIDs, err := c.store.ListActivePoolIDsForNodeCapabilities(ctx, nodeID, []string{capability.Scanner, capability.Coverage})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	items := make([]*gonzbnetScrapeRangeCoordinator, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		poolID = strings.TrimSpace(poolID)
		if poolID == "" {
			continue
		}
		if len(c.enabledPoolIDs) > 0 {
			if _, ok := c.enabledPoolIDs[poolID]; !ok {
				continue
			}
		}
		coordinator := c.coordinators[poolID]
		if coordinator == nil {
			coordinator, err = newGoNZBNetScrapeRangeCoordinator(c.identity, c.store, poolID, c.claimTTL, c.minBlockingTrust, c.providerScopeHash, c.allowAssigned, c.allowUnassigned, c.respectRemoteClaims)
			if err != nil {
				return nil, err
			}
			c.coordinators[poolID] = coordinator
		}
		items = append(items, coordinator)
	}
	return items, nil
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) coordinatorFor(ctx context.Context, poolID string) (*gonzbnetScrapeRangeCoordinator, error) {
	items, err := c.activeCoordinators(ctx)
	if err != nil {
		return nil, err
	}
	poolID = strings.TrimSpace(poolID)
	explicitPool := poolID != ""
	if poolID == "" {
		poolID = c.preferredPoolID
	}
	for _, item := range items {
		if item.poolID == poolID {
			return item, nil
		}
	}
	if len(items) > 0 && !explicitPool {
		return items[0], nil
	}
	return nil, nil
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) AssignedScrapeRanges(ctx context.Context, mode string, limit int) ([]scrape.RangeRequest, error) {
	items, err := c.activeCoordinators(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]scrape.RangeRequest, 0, limit)
	for _, coordinator := range items {
		remaining := limit - len(out)
		if remaining <= 0 {
			break
		}
		ranges, err := coordinator.AssignedScrapeRanges(ctx, mode, remaining)
		if err != nil {
			return nil, err
		}
		for i := range ranges {
			ranges[i].PoolID = coordinator.poolID
		}
		out = append(out, ranges...)
	}
	return out, nil
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) BeginScrapeRange(ctx context.Context, request scrape.RangeRequest) (scrape.RangeDecision, error) {
	coordinator, err := c.coordinatorFor(ctx, request.PoolID)
	if err != nil || coordinator == nil {
		return scrape.RangeDecision{}, err
	}
	decision, err := coordinator.BeginScrapeRange(ctx, request)
	decision.PoolID = coordinator.poolID
	return decision, err
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) CompleteScrapeRange(ctx context.Context, decision scrape.RangeDecision, result scrape.RangeResult) error {
	c.mu.Lock()
	coordinator := c.coordinators[strings.TrimSpace(decision.PoolID)]
	c.mu.Unlock()
	if coordinator == nil {
		return fmt.Errorf("coverage coordinator for pool %q is unavailable", decision.PoolID)
	}
	return coordinator.CompleteScrapeRange(ctx, decision, result)
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) FailScrapeRange(ctx context.Context, decision scrape.RangeDecision, cause error) error {
	c.mu.Lock()
	coordinator := c.coordinators[strings.TrimSpace(decision.PoolID)]
	c.mu.Unlock()
	if coordinator == nil {
		return fmt.Errorf("coverage coordinator for pool %q is unavailable", decision.PoolID)
	}
	return coordinator.FailScrapeRange(ctx, decision, cause)
}

func (c *gonzbnetMultiPoolScrapeRangeCoordinator) ObserveScrapeRun(ctx context.Context, metrics map[string]any, runErr error) {
	items, err := c.activeCoordinators(ctx)
	if err != nil {
		return
	}
	for _, coordinator := range items {
		coordinator.ObserveScrapeRun(ctx, metrics, runErr)
	}
}

func newGoNZBNetScrapeRangeCoordinator(nodeIdentity *identity.Identity, store gonzbnetScrapeCoordinatorStore, poolID string, claimTTL time.Duration, minBlockingTrust float64, providerScopeHash string, allowAssigned, allowUnassigned, respectRemoteClaims bool) (*gonzbnetScrapeRangeCoordinator, error) {
	if nodeIdentity == nil || store == nil {
		return nil, fmt.Errorf("gonzbnet scrape coordinator dependencies are required")
	}
	if claimTTL <= 0 {
		claimTTL = 30 * time.Minute
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		poolID = "pool.local"
	}
	return &gonzbnetScrapeRangeCoordinator{
		identity:            nodeIdentity,
		store:               store,
		poolID:              poolID,
		claimTTL:            claimTTL,
		minBlockingTrust:    minBlockingTrust,
		providerScopeHash:   strings.TrimSpace(providerScopeHash),
		allowAssigned:       allowAssigned,
		allowUnassigned:     allowUnassigned,
		respectRemoteClaims: respectRemoteClaims,
		now:                 time.Now,
	}, nil
}

func providerBackboneHashForIndexer(appCtx *app.Context) string {
	if appCtx == nil || appCtx.Config == nil || !appCtx.Config.GoNZBNet.ShareProviderBackbone {
		return ""
	}
	servers := scopedIndexerServers(appCtx)
	if len(servers) == 0 {
		servers = appCtx.Config.Servers
	}
	parts := make([]string, 0, len(servers))
	for _, server := range servers {
		host := strings.TrimSpace(server.Host)
		if host == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%t", host, server.Port, server.TLS))
	}
	return profile.ProviderBackboneHash(parts)
}

func (c *gonzbnetScrapeRangeCoordinator) AssignedScrapeRanges(ctx context.Context, mode string, limit int) ([]scrape.RangeRequest, error) {
	if c == nil || !c.allowAssigned {
		return nil, nil
	}
	nodeID, err := c.localNode(ctx)
	if err != nil {
		return nil, err
	}
	items, err := c.store.SuggestCoverageWork(ctx, pgindex.CoverageWorkSuggestionParams{
		PoolID:                c.poolID,
		NodeID:                nodeID,
		Mode:                  "scanner",
		Limit:                 limit,
		MinBlockingTrustScore: c.minBlockingTrust,
	})
	if err != nil {
		return nil, err
	}
	out := make([]scrape.RangeRequest, 0, len(items))
	for _, item := range items {
		assignment := item.Assignment
		if strings.TrimSpace(assignment.AssignmentID) == "" || strings.TrimSpace(assignment.Group) == "" {
			continue
		}
		request := scrape.RangeRequest{
			Mode:         mode,
			AssignmentID: assignment.AssignmentID,
			Group:        assignment.Group,
		}
		if assignment.RangeStart > 0 && assignment.RangeEnd >= assignment.RangeStart {
			request.RangeStart = assignment.RangeStart
			request.RangeEnd = assignment.RangeEnd
		} else if assignment.WindowStart != nil && assignment.WindowEnd != nil && assignment.WindowEnd.After(*assignment.WindowStart) {
			windowStart := assignment.WindowStart.UTC()
			windowEnd := assignment.WindowEnd.UTC()
			request.WindowStart = &windowStart
			request.WindowEnd = &windowEnd
		} else {
			continue
		}
		out = append(out, request)
	}
	return out, nil
}

func (c *gonzbnetScrapeRangeCoordinator) BeginScrapeRange(ctx context.Context, request scrape.RangeRequest) (scrape.RangeDecision, error) {
	if c == nil {
		return scrape.RangeDecision{}, nil
	}
	assignmentID := strings.TrimSpace(request.AssignmentID)
	if assignmentID == "" && !c.allowUnassigned {
		return scrape.RangeDecision{}, nil
	}
	nodeID, err := c.localNode(ctx)
	if err != nil {
		return scrape.RangeDecision{}, err
	}
	block, err := c.store.CheckCoverageRangeBlock(ctx, pgindex.CoverageRangeBlockParams{
		PoolID:                c.poolID,
		NodeID:                nodeID,
		Group:                 request.Group,
		RangeStart:            request.RangeStart,
		RangeEnd:              request.RangeEnd,
		ProviderBackboneHash:  c.providerScopeHash,
		RequireProviderScope:  true,
		MinBlockingTrustScore: c.minBlockingTrust,
		RespectRemoteClaims:   c.respectRemoteClaims,
		SkipCompleted:         true,
	})
	if err != nil {
		return scrape.RangeDecision{}, err
	}
	if block.Blocked {
		return scrape.RangeDecision{
			Skipped:           true,
			AdvanceCheckpoint: block.AdvanceCheckpoint,
			Reason:            block.Reason,
			AssignmentID:      assignmentID,
			Group:             request.Group,
			RangeStart:        request.RangeStart,
			RangeEnd:          request.RangeEnd,
			WindowStart:       request.WindowStart,
			WindowEnd:         request.WindowEnd,
		}, nil
	}
	now := c.now().UTC()
	claimID := "claim-" + uuid.NewString()
	if request.WindowStart != nil && request.WindowEnd != nil && request.WindowEnd.After(*request.WindowStart) {
		body := coverage.TimeWindowClaim{
			SchemaVersion: "1.0",
			Type:          coverage.TypeTimeWindowClaim,
			ClaimID:       claimID,
			AssignmentID:  assignmentID,
			PoolID:        c.poolID,
			Group:         strings.TrimSpace(request.Group),
			NodeID:        nodeID,
			ProviderScope: c.providerScopeHash,
			WindowStart:   request.WindowStart.UTC().Format(time.RFC3339),
			WindowEnd:     request.WindowEnd.UTC().Format(time.RFC3339),
			ClaimedAt:     now.Format(time.RFC3339),
			ExpiresAt:     now.Add(c.claimTTL).Format(time.RFC3339),
			ClaimMode:     "primary_scan",
		}
		if err := c.signAppendProject(ctx, coverage.TypeTimeWindowClaim, body); err != nil {
			return scrape.RangeDecision{}, err
		}
		return scrape.RangeDecision{
			ClaimID:      claimID,
			AssignmentID: assignmentID,
			Group:        request.Group,
			RangeStart:   request.RangeStart,
			RangeEnd:     request.RangeEnd,
			WindowStart:  request.WindowStart,
			WindowEnd:    request.WindowEnd,
		}, nil
	}
	body := coverage.RangeClaim{
		SchemaVersion:                     "1.0",
		Type:                              coverage.TypeRangeClaim,
		ClaimID:                           claimID,
		AssignmentID:                      assignmentID,
		PoolID:                            c.poolID,
		Group:                             strings.TrimSpace(request.Group),
		NodeID:                            nodeID,
		ProviderScope:                     c.providerScopeHash,
		RangeStart:                        request.RangeStart,
		RangeEnd:                          request.RangeEnd,
		ClaimedAt:                         now.Format(time.RFC3339),
		ExpiresAt:                         now.Add(c.claimTTL).Format(time.RFC3339),
		ClaimMode:                         "primary_scan",
		ExpectedCheckpointIntervalSeconds: 300,
	}
	if err := c.signAppendProject(ctx, coverage.TypeRangeClaim, body); err != nil {
		return scrape.RangeDecision{}, err
	}
	return scrape.RangeDecision{
		ClaimID:      claimID,
		AssignmentID: assignmentID,
		Group:        request.Group,
		RangeStart:   request.RangeStart,
		RangeEnd:     request.RangeEnd,
	}, nil
}

func (c *gonzbnetScrapeRangeCoordinator) CompleteScrapeRange(ctx context.Context, decision scrape.RangeDecision, result scrape.RangeResult) error {
	if c == nil || strings.TrimSpace(decision.ClaimID) == "" {
		return nil
	}
	nodeID, err := c.localNode(ctx)
	if err != nil {
		return err
	}
	now := c.now().UTC()
	observation := coverage.GroupObservation{
		SchemaVersion:        "1.0",
		Type:                 coverage.TypeGroupObservation,
		ObservationID:        decision.ClaimID + "-observation",
		NodeID:               nodeID,
		PoolID:               c.poolID,
		Group:                strings.TrimSpace(result.Group),
		ProviderScope:        c.providerScopeHash,
		ObservedAt:           now.Format(time.RFC3339),
		LowWatermark:         result.RangeStart,
		HighWatermark:        result.RangeEnd,
		EstimatedCount:       max(0, result.RangeEnd-result.RangeStart+1),
		PostsPerHourEstimate: float64(max(0, result.ArticlesInserted)),
		ScanSupported:        true,
		Confidence:           1,
	}
	if err := c.signAppendProject(ctx, coverage.TypeGroupObservation, observation); err != nil {
		return err
	}
	checkpoint := coverage.CoverageCheckpoint{
		SchemaVersion:       "1.0",
		Type:                coverage.TypeCoverageCheckpoint,
		CheckpointID:        decision.ClaimID + "-checkpoint",
		PoolID:              c.poolID,
		NodeID:              nodeID,
		Group:               strings.TrimSpace(result.Group),
		ProviderScope:       c.providerScopeHash,
		ClaimID:             decision.ClaimID,
		RangeStart:          result.RangeStart,
		RangeCurrent:        result.RangeEnd,
		RangeEnd:            result.RangeEnd,
		ReleaseCardsEmitted: 0,
		ManifestsEmitted:    0,
		Errors:              0,
		CheckedAt:           now.Format(time.RFC3339),
	}
	if err := c.signAppendProject(ctx, coverage.TypeCoverageCheckpoint, checkpoint); err != nil {
		return err
	}
	body := coverage.RangeComplete{
		SchemaVersion:    "1.0",
		Type:             coverage.TypeRangeComplete,
		OutcomeID:        decision.ClaimID + "-complete",
		ClaimID:          decision.ClaimID,
		AssignmentID:     strings.TrimSpace(decision.AssignmentID),
		PoolID:           c.poolID,
		Group:            strings.TrimSpace(result.Group),
		NodeID:           nodeID,
		ProviderScope:    c.providerScopeHash,
		RangeStart:       result.RangeStart,
		RangeEnd:         result.RangeEnd,
		ArticlesSeen:     max(0, result.RangeEnd-result.RangeStart+1),
		HeadersProcessed: int64(result.ArticleHeaders),
		CompletedAt:      now.Format(time.RFC3339),
	}
	return c.signAppendProject(ctx, coverage.TypeRangeComplete, body)
}

func (c *gonzbnetScrapeRangeCoordinator) FailScrapeRange(ctx context.Context, decision scrape.RangeDecision, cause error) error {
	if c == nil || strings.TrimSpace(decision.ClaimID) == "" {
		return nil
	}
	nodeID, err := c.localNode(ctx)
	if err != nil {
		return err
	}
	reason := "scrape_failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = strings.TrimSpace(cause.Error())
		if len(reason) > 240 {
			reason = reason[:240]
		}
	}
	now := c.now().UTC()
	body := coverage.RangeFailed{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeFailed,
		OutcomeID:     decision.ClaimID + "-failed",
		ClaimID:       decision.ClaimID,
		AssignmentID:  strings.TrimSpace(decision.AssignmentID),
		PoolID:        c.poolID,
		Group:         strings.TrimSpace(decision.Group),
		NodeID:        nodeID,
		ProviderScope: c.providerScopeHash,
		RangeStart:    decision.RangeStart,
		RangeEnd:      decision.RangeEnd,
		Reason:        reason,
		Retryable:     true,
		FailedAt:      now.Format(time.RFC3339),
	}
	return c.signAppendProject(ctx, coverage.TypeRangeFailed, body)
}

func (c *gonzbnetScrapeRangeCoordinator) localNode(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nodeID != "" {
		return c.nodeID, nil
	}
	nodeID, err := c.identity.NodeID(ctx)
	if err != nil {
		return "", err
	}
	publicKey, err := c.identity.PublicKey(ctx)
	if err != nil {
		return "", err
	}
	if err := c.store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return "", err
	}
	c.nodeID = nodeID
	return nodeID, nil
}

func (c *gonzbnetScrapeRangeCoordinator) signAppendProject(ctx context.Context, eventType string, body any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	nodeID, err := c.identity.NodeID(ctx)
	if err != nil {
		return err
	}
	sequence, previousEventID, err := c.store.NextFederationEventSequence(ctx, nodeID)
	if err != nil {
		return err
	}
	event, validation, err := events.Create(ctx, c.identity, events.CreateOptions{
		EventType:       eventType,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       c.now().UTC(),
		PoolIDs:         []string{c.poolID},
		Visibility:      "pool",
		BodySchema:      coverage.BodySchema(eventType),
		Body:            body,
	})
	if err != nil {
		return err
	}
	if validation == nil || !validation.OK {
		return fmt.Errorf("signed coverage event did not verify")
	}
	if err := c.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		return err
	}
	return c.store.ProjectCoverageEvent(ctx, event)
}

var _ scrape.RangeCoordinator = (*gonzbnetScrapeRangeCoordinator)(nil)
var _ scrape.RangeCoordinator = (*gonzbnetMultiPoolScrapeRangeCoordinator)(nil)
