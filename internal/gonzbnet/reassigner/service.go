package reassigner

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Store interface {
	ListStaleCoverageRangeClaims(ctx context.Context, poolID string, limit int) ([]pgindex.CoverageClaimRecord, error)
	ListStaleCoverageTimeWindowClaims(ctx context.Context, poolID string, limit int) ([]pgindex.CoverageClaimRecord, error)
	ListCoverageScannerNodes(ctx context.Context, poolID string, minTrustScore float64) ([]pgindex.CoverageScannerNode, error)
	CoverageAssignmentExists(ctx context.Context, assignmentID string) (bool, error)
	UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error
	NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error)
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error
}

type Service struct {
	identity      *identity.Identity
	store         Store
	poolID        string
	minTrustScore float64
	now           func() time.Time
}

type Result struct {
	StaleClaims        int `json:"stale_claims"`
	AssignmentsCreated int `json:"assignments_created"`
	SkippedNoNode      int `json:"skipped_no_node"`
	SkippedDuplicate   int `json:"skipped_duplicate"`
}

func New(nodeIdentity *identity.Identity, store Store, poolID string, minTrustScore float64) (*Service, error) {
	if nodeIdentity == nil || store == nil {
		return nil, fmt.Errorf("reassigner dependencies are required")
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		poolID = "pool.local"
	}
	return &Service{
		identity:      nodeIdentity,
		store:         store,
		poolID:        poolID,
		minTrustScore: minTrustScore,
		now:           time.Now,
	}, nil
}

func (s *Service) Run(ctx context.Context, interval time.Duration, limit int) error {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if _, err := s.RunOnce(ctx, limit); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.RunOnce(ctx, limit); err != nil {
				return err
			}
		}
	}
}

func (s *Service) RunOnce(ctx context.Context, limit int) (Result, error) {
	var result Result
	if limit <= 0 {
		limit = 25
	}
	staleClaims, err := s.staleClaims(ctx, limit)
	if err != nil {
		return result, err
	}
	result.StaleClaims = len(staleClaims)
	if len(staleClaims) == 0 {
		return result, nil
	}
	nodes, err := s.store.ListCoverageScannerNodes(ctx, s.poolID, s.minTrustScore)
	if err != nil {
		return result, err
	}
	nodeID, err := s.identity.NodeID(ctx)
	if err != nil {
		return result, err
	}
	publicKey, err := s.identity.PublicKey(ctx)
	if err != nil {
		return result, err
	}
	if err := s.store.UpsertFederationNodeIdentity(ctx, nodeID, publicKey); err != nil {
		return result, err
	}
	for _, claim := range staleClaims {
		assignmentID := replacementAssignmentID(claim)
		exists, err := s.store.CoverageAssignmentExists(ctx, assignmentID)
		if err != nil {
			return result, err
		}
		if exists {
			result.SkippedDuplicate++
			continue
		}
		assignedNodeID := selectReplacementNode(claim, nodes)
		if assignedNodeID == "" {
			result.SkippedNoNode++
			continue
		}
		now := s.now().UTC()
		body := replacementAssignmentForClaim(claim, assignmentID, assignedNodeID, now)
		if err := s.signAppendProject(ctx, body); err != nil {
			return result, err
		}
		result.AssignmentsCreated++
	}
	return result, nil
}

func (s *Service) staleClaims(ctx context.Context, limit int) ([]pgindex.CoverageClaimRecord, error) {
	rangeClaims, err := s.store.ListStaleCoverageRangeClaims(ctx, s.poolID, limit)
	if err != nil {
		return nil, err
	}
	windowClaims, err := s.store.ListStaleCoverageTimeWindowClaims(ctx, s.poolID, limit)
	if err != nil {
		return nil, err
	}
	out := append(append([]pgindex.CoverageClaimRecord{}, rangeClaims...), windowClaims...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ExpiresAt.Before(out[j].ExpiresAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func replacementAssignmentForClaim(claim pgindex.CoverageClaimRecord, assignmentID, assignedNodeID string, now time.Time) coverage.CoverageAssignment {
	body := coverage.CoverageAssignment{
		SchemaVersion:  "1.0",
		Type:           coverage.TypeCoverageAssignment,
		AssignmentID:   assignmentID,
		PlanID:         "stale-" + strings.TrimSpace(claim.ClaimID),
		PoolID:         strings.TrimSpace(claim.PoolID),
		Group:          strings.TrimSpace(claim.Group),
		Mode:           "article_range",
		Role:           "primary_scanner",
		AssignedNodeID: strings.TrimSpace(assignedNodeID),
		Priority:       100,
		DueAt:          now.Add(30 * time.Minute).Format(time.RFC3339),
		ExpiresAt:      now.Add(30 * time.Minute).Format(time.RFC3339),
		CreatedAt:      now.Format(time.RFC3339),
	}
	if claim.WindowStart != nil && claim.WindowEnd != nil && claim.WindowEnd.After(*claim.WindowStart) {
		body.Mode = "time_window"
		body.WindowStart = claim.WindowStart.UTC().Format(time.RFC3339)
		body.WindowEnd = claim.WindowEnd.UTC().Format(time.RFC3339)
		return body
	}
	body.RangeStart = claim.RangeStart
	body.RangeEnd = claim.RangeEnd
	return body
}

func (s *Service) signAppendProject(ctx context.Context, body coverage.CoverageAssignment) error {
	nodeID, err := s.identity.NodeID(ctx)
	if err != nil {
		return err
	}
	sequence, previousEventID, err := s.store.NextFederationEventSequence(ctx, nodeID)
	if err != nil {
		return err
	}
	event, validation, err := events.Create(ctx, s.identity, events.CreateOptions{
		EventType:       coverage.TypeCoverageAssignment,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       s.now().UTC(),
		PoolIDs:         []string{body.PoolID},
		Visibility:      "pool",
		BodySchema:      coverage.CoverageAssignmentBodySchema,
		Body:            body,
	})
	if err != nil {
		return err
	}
	if validation == nil || !validation.OK {
		return fmt.Errorf("signed coverage assignment did not verify")
	}
	if err := s.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		return err
	}
	return s.store.ProjectCoverageEvent(ctx, event)
}

func selectReplacementNode(claim pgindex.CoverageClaimRecord, nodes []pgindex.CoverageScannerNode) string {
	schedulerNodes := make([]coverage.SchedulerNode, 0, len(nodes))
	for _, node := range nodes {
		nodeID := strings.TrimSpace(node.NodeID)
		if nodeID == "" || nodeID == strings.TrimSpace(claim.NodeID) {
			continue
		}
		weight := node.Weight
		if weight <= 0 {
			weight = float64(node.MaxRangesPerHour)
		}
		if weight <= 0 {
			weight = 1
		}
		schedulerNodes = append(schedulerNodes, coverage.SchedulerNode{NodeID: nodeID, Weight: weight})
	}
	workID := claimWorkID(claim)
	assignments := coverage.RendezvousAssignments([]coverage.SchedulerWorkItem{{
		WorkID: workID,
		SeenBy: []string{
			strings.TrimSpace(claim.NodeID),
		},
	}}, schedulerNodes)
	if len(assignments) == 0 {
		return ""
	}
	return assignments[0].NodeID
}

func claimWorkID(claim pgindex.CoverageClaimRecord) string {
	if claim.WindowStart != nil && claim.WindowEnd != nil {
		return fmt.Sprintf("%s:%s:%s:%s-%s",
			strings.TrimSpace(claim.PoolID),
			strings.TrimSpace(claim.Group),
			strings.TrimSpace(claim.ClaimType),
			claim.WindowStart.UTC().Format(time.RFC3339),
			claim.WindowEnd.UTC().Format(time.RFC3339),
		)
	}
	return fmt.Sprintf("%s:%s:%s:%d-%d",
		strings.TrimSpace(claim.PoolID),
		strings.TrimSpace(claim.Group),
		strings.TrimSpace(claim.ClaimType),
		claim.RangeStart,
		claim.RangeEnd,
	)
}

func replacementAssignmentID(claim pgindex.CoverageClaimRecord) string {
	payload := strings.Join([]string{
		strings.TrimSpace(claim.PoolID),
		strings.TrimSpace(claim.ClaimID),
		strings.TrimSpace(claim.ClaimType),
		strings.TrimSpace(claim.Group),
		fmt.Sprintf("%d", claim.RangeStart),
		fmt.Sprintf("%d", claim.RangeEnd),
		formatClaimWindowTime(claim.WindowStart),
		formatClaimWindowTime(claim.WindowEnd),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return "assign_stale_" + canonical.Base64URL(sum[:])[:32]
}

func formatClaimWindowTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
