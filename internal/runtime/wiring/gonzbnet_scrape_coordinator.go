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
	return newGoNZBNetScrapeRangeCoordinator(
		nodeIdentity,
		store,
		cfg.LocalPoolID,
		time.Duration(cfg.ScannerClaimTTLMinutes)*time.Minute,
		cfg.CoverageMinTrustForClaim,
		providerBackboneHashForIndexer(appCtx),
		allowAssigned,
		cfg.ScannerAllowUnassignedWork,
		cfg.ScannerRespectRemoteClaims,
	)
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
		if strings.TrimSpace(assignment.AssignmentID) == "" || strings.TrimSpace(assignment.Group) == "" || assignment.RangeStart <= 0 || assignment.RangeEnd < assignment.RangeStart {
			continue
		}
		out = append(out, scrape.RangeRequest{
			Mode:         mode,
			AssignmentID: assignment.AssignmentID,
			Group:        assignment.Group,
			RangeStart:   assignment.RangeStart,
			RangeEnd:     assignment.RangeEnd,
		})
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
		}, nil
	}
	now := c.now().UTC()
	claimID := "claim-" + uuid.NewString()
	body := coverage.RangeClaim{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeClaim,
		ClaimID:       claimID,
		AssignmentID:  assignmentID,
		PoolID:        c.poolID,
		Group:         strings.TrimSpace(request.Group),
		NodeID:        nodeID,
		RangeStart:    request.RangeStart,
		RangeEnd:      request.RangeEnd,
		ClaimedAt:     now.Format(time.RFC3339),
		ExpiresAt:     now.Add(c.claimTTL).Format(time.RFC3339),
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
	body := coverage.RangeComplete{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeComplete,
		OutcomeID:     decision.ClaimID + "-complete",
		ClaimID:       decision.ClaimID,
		AssignmentID:  strings.TrimSpace(decision.AssignmentID),
		PoolID:        c.poolID,
		Group:         strings.TrimSpace(result.Group),
		NodeID:        nodeID,
		RangeStart:    result.RangeStart,
		RangeEnd:      result.RangeEnd,
		CompletedAt:   now.Format(time.RFC3339),
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
		RangeStart:    decision.RangeStart,
		RangeEnd:      decision.RangeEnd,
		Reason:        reason,
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
