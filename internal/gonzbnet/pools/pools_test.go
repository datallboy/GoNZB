package pools

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

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

func TestEventTypeSupportedRejectsUnknownTypes(t *testing.T) {
	for _, eventType := range []string{
		EventTypeReleaseCard,
		EventTypeManifestAvailability,
		EventTypeCoverageAssignment,
		EventTypePoolMemberApproved,
		EventTypePoolCheckpoint,
		EventTypeTrustAttestation,
	} {
		if !EventTypeSupported(eventType) {
			t.Fatalf("expected %s to be supported", eventType)
		}
	}
	for _, eventType := range []string{"", "UnknownFutureEvent"} {
		if EventTypeSupported(eventType) {
			t.Fatalf("expected %s to be rejected", eventType)
		}
	}
}

func TestValidateCheckpointVerifiesMerkleRootAndWitnesses(t *testing.T) {
	ctx := context.Background()
	witness1 := testIdentity(t)
	witness2 := testIdentity(t)
	witnessKeys := map[string]ed25519.PublicKey{}
	for _, witness := range []*identity.Identity{witness1, witness2} {
		nodeID, _ := witness.NodeID(ctx)
		publicKey, _ := witness.PublicKey(ctx)
		witnessKeys[nodeID] = publicKey
	}
	leaves := []CheckpointLeaf{
		{
			EventID:      "evt_a",
			AuthorNodeID: "node_a",
			Sequence:     1,
			BodyHash:     "sha256:a",
			CreatedAt:    time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			EventID:      "evt_b",
			AuthorNodeID: "node_b",
			Sequence:     2,
			BodyHash:     "sha256:b",
			CreatedAt:    time.Date(2026, 7, 9, 12, 1, 0, 0, time.UTC),
		},
	}
	root, err := CheckpointMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("merkle root: %v", err)
	}
	body := Checkpoint{
		PoolID:      "pool.private.movies",
		Height:      1,
		EventCount:  int64(len(leaves)),
		FromEventID: leaves[0].EventID,
		ToEventID:   leaves[len(leaves)-1].EventID,
		MerkleRoot:  root,
		CreatedAt:   "2026-07-09T12:05:00Z",
	}
	body.Witnesses = []Approval{
		signCheckpoint(t, witness1, body, "2026-07-09T12:06:00Z"),
		signCheckpoint(t, witness2, body, "2026-07-09T12:07:00Z"),
	}
	if err := ValidateCheckpoint(body, witnessKeys, 2, leaves); err != nil {
		t.Fatalf("expected checkpoint to validate: %v", err)
	}

	tampered := body
	tampered.MerkleRoot = "sha256:deadbeef"
	if err := ValidateCheckpoint(tampered, witnessKeys, 2, leaves); err == nil {
		t.Fatalf("expected tampered merkle root to fail")
	}

	tampered = body
	tampered.Witnesses = tampered.Witnesses[:1]
	if err := ValidateCheckpoint(tampered, witnessKeys, 2, leaves); err == nil {
		t.Fatalf("expected insufficient witnesses to fail")
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

func signCheckpoint(t *testing.T, signer *identity.Identity, body Checkpoint, witnessedAt string) Approval {
	t.Helper()
	nodeID, _ := signer.NodeID(context.Background())
	payload, err := canonical.Marshal(map[string]any{
		"pool_id":       body.PoolID,
		"height":        body.Height,
		"event_count":   body.EventCount,
		"from_event_id": body.FromEventID,
		"to_event_id":   body.ToEventID,
		"merkle_root":   body.MerkleRoot,
		"created_at":    body.CreatedAt,
		"witnessed_at":  witnessedAt,
	})
	if err != nil {
		t.Fatalf("canonical checkpoint: %v", err)
	}
	signature, err := signer.Sign(context.Background(), payload)
	if err != nil {
		t.Fatalf("sign checkpoint: %v", err)
	}
	return Approval{
		NodeID:     nodeID,
		ApprovedAt: witnessedAt,
		Signature:  canonical.Base64URL(signature),
	}
}
