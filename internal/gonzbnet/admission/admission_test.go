package admission

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
)

type testIdentity struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

func newTestIdentity(t *testing.T) *testIdentity {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &testIdentity{public: public, private: private}
}

func (i *testIdentity) NodeID(context.Context) (string, error) {
	return identity.NodeIDFromPublicKey(i.public), nil
}
func (i *testIdentity) PublicKey(context.Context) (ed25519.PublicKey, error) { return i.public, nil }
func (i *testIdentity) Sign(_ context.Context, payload []byte) ([]byte, error) {
	return ed25519.Sign(i.private, payload), nil
}

func TestInvitationRoundTripAndTamper(t *testing.T) {
	ctx := context.Background()
	signer := newTestIdentity(t)
	expires := time.Now().UTC().Add(time.Hour)
	invite, err := NewInvitation(ctx, signer, "pool.test", "evt_genesis", "https://node.example/gonzbnet/v1", &expires)
	if err != nil {
		t.Fatal(err)
	}
	link, err := invite.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseInvitation(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.Verify(time.Now().UTC()); err != nil {
		t.Fatalf("verify invitation: %v", err)
	}
	decoded.PoolID = "pool.other"
	if err := decoded.Verify(time.Now().UTC()); err == nil {
		t.Fatal("expected tampered invitation to fail")
	}
}

func TestApprovalFragmentUsesFinalApprovalSignature(t *testing.T) {
	ctx := context.Background()
	signer := newTestIdentity(t)
	body := pools.MemberApproved{
		PoolID: "pool.test", SubjectNodeID: "node_candidate", Role: pools.RoleMember,
		ProposalEventID: "evt_join", AllowedCapabilities: []string{"consumer"},
	}
	fragment, err := NewApprovalFragment(ctx, signer, body, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fragment.Verify(); err != nil {
		t.Fatalf("verify fragment: %v", err)
	}
	final := fragment.MemberApproved([]ApprovalFragment{fragment}, 1)
	if err := pools.ValidateMemberApproval(final, map[string]ed25519.PublicKey{fragment.AdminNodeID: signer.public}); err != nil {
		t.Fatalf("validate final approval: %v", err)
	}
	fragment.SubjectNodeID = "node_other"
	if _, err := fragment.Verify(); err == nil {
		t.Fatal("expected tampered approval to fail")
	}
}

func TestApprovalFragmentsAggregateDeterministically(t *testing.T) {
	ctx := context.Background()
	firstSigner := newTestIdentity(t)
	secondSigner := newTestIdentity(t)
	body := pools.MemberApproved{
		PoolID: "pool.test", SubjectNodeID: "node_candidate", Role: pools.RoleMember,
		ProposalEventID: "evt_join", AllowedCapabilities: []string{"consumer"},
	}
	first, err := NewApprovalFragment(ctx, firstSigner, body, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewApprovalFragment(ctx, secondSigner, body, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	final := first.MemberApproved([]ApprovalFragment{second, first}, 2)
	if final.Approvals[0].NodeID > final.Approvals[1].NodeID {
		t.Fatalf("approval fragments were not canonically ordered: %+v", final.Approvals)
	}
	adminKeys := map[string]ed25519.PublicKey{first.AdminNodeID: firstSigner.public, second.AdminNodeID: secondSigner.public}
	if err := pools.ValidateMemberApproval(final, adminKeys); err != nil {
		t.Fatalf("validate aggregate approval: %v", err)
	}
}

func TestRejectionFragmentTamper(t *testing.T) {
	ctx := context.Background()
	signer := newTestIdentity(t)
	fragment, err := NewRejectionFragment(ctx, signer, "pool.test", "evt_join", "node_candidate", "not approved", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fragment.Verify(time.Now().UTC()); err != nil {
		t.Fatalf("verify rejection: %v", err)
	}
	fragment.SubjectNodeID = "node_other"
	if _, err := fragment.Verify(time.Now().UTC()); err == nil {
		t.Fatal("expected tampered rejection to fail")
	}
}

func TestNormalizeLocator(t *testing.T) {
	if got, err := NormalizeLocator("node.example:8443", false); err != nil || got != "https://node.example:8443" {
		t.Fatalf("normalize hostname: got=%q err=%v", got, err)
	}
	if _, err := NormalizeLocator("http://node.example", true); err == nil {
		t.Fatal("expected non-loopback insecure address rejection")
	}
}
