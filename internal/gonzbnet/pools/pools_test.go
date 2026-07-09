package pools

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestValidateMemberApprovalRequiresThreshold(t *testing.T) {
	ctx := context.Background()
	admin1 := testIdentity(t)
	admin2 := testIdentity(t)
	admin3 := testIdentity(t)
	adminKeys := map[string]ed25519.PublicKey{}
	for _, admin := range []*identity.Identity{admin1, admin2, admin3} {
		nodeID, _ := admin.NodeID(ctx)
		publicKey, _ := admin.PublicKey(ctx)
		adminKeys[nodeID] = publicKey
	}
	body := MemberApproved{
		PoolID:            "pool.private.movies",
		SubjectNodeID:     "node_subject",
		Role:              RoleMember,
		ProposalEventID:   "evt_join",
		ApprovalsRequired: 2,
	}
	body.Approvals = []Approval{
		signApproval(t, admin1, body, "2026-07-09T12:00:00Z"),
		signApproval(t, admin2, body, "2026-07-09T12:01:00Z"),
	}
	if err := ValidateMemberApproval(body, adminKeys); err != nil {
		t.Fatalf("expected approval threshold to pass: %v", err)
	}

	body.Approvals = body.Approvals[:1]
	if err := ValidateMemberApproval(body, adminKeys); err == nil {
		t.Fatalf("expected approval threshold to fail")
	}
}

func TestAuthorizeEventRejectsNonMemberAndRevokedMember(t *testing.T) {
	policy := PoolPolicy{
		PoolID:             "pool.private.movies",
		AcceptMode:         "pool_member",
		AcceptedEventTypes: []string{"ReleaseCard"},
	}
	if ok, reason := AuthorizeEvent(policy, false, 1, "ReleaseCard"); ok || reason != "not_pool_member" {
		t.Fatalf("expected non-member rejection, ok=%v reason=%q", ok, reason)
	}
	if ok, reason := AuthorizeEvent(policy, false, 1, "ReleaseCard"); ok || reason != "not_pool_member" {
		t.Fatalf("expected revoked member to be rejected when active membership is false, ok=%v reason=%q", ok, reason)
	}
	if ok, reason := AuthorizeEvent(policy, true, 1, "ReleaseCard"); !ok || reason != "" {
		t.Fatalf("expected active member acceptance, ok=%v reason=%q", ok, reason)
	}
	if ok, reason := AuthorizeEvent(policy, true, 1, "PoolCheckpoint"); ok || reason != "event_type_not_allowed" {
		t.Fatalf("expected event type rejection, ok=%v reason=%q", ok, reason)
	}
	policy.MinNodeTrustScore = 0.5
	if ok, reason := AuthorizeEvent(policy, true, 0.25, "ReleaseCard"); ok || reason != "node_trust_below_pool_minimum" {
		t.Fatalf("expected trust rejection, ok=%v reason=%q", ok, reason)
	}
}

func testIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return node
}

func signApproval(t *testing.T, signer *identity.Identity, body MemberApproved, approvedAt string) Approval {
	t.Helper()
	nodeID, _ := signer.NodeID(context.Background())
	payload, err := canonical.Marshal(map[string]any{
		"pool_id":           body.PoolID,
		"proposal_event_id": body.ProposalEventID,
		"subject_node_id":   body.SubjectNodeID,
		"role":              body.Role,
		"approved_at":       approvedAt,
	})
	if err != nil {
		t.Fatalf("canonical approval: %v", err)
	}
	signature, err := signer.Sign(context.Background(), payload)
	if err != nil {
		t.Fatalf("sign approval: %v", err)
	}
	return Approval{
		NodeID:     nodeID,
		ApprovedAt: approvedAt,
		Signature:  canonical.Base64URL(signature),
	}
}
