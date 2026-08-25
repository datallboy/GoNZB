package requestauth

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

func TestSignVerifyAndRejectReplay(t *testing.T) {
	ctx := context.Background()
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	nodeID, _ := nodeIdentity.NodeID(ctx)
	publicKey, _ := nodeIdentity.PublicKey(ctx)
	store := &fakeVerifierStore{
		keys:   map[string]ed25519.PublicKey{nodeID: publicKey},
		nonces: map[string]bool{},
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"type":"EventBatch","events":[]}`)

	header, err := Sign(ctx, nodeIdentity, "POST", "/gonzbnet/v1/inbox", "", body, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := Verify(ctx, store, header, "POST", "/gonzbnet/v1/inbox", "", body, now, 2*time.Minute, 10*time.Minute); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := Verify(ctx, store, header, "POST", "/gonzbnet/v1/inbox", "", body, now, 2*time.Minute, 10*time.Minute); err == nil {
		t.Fatalf("expected replay verification to fail")
	}
}

func TestParseAuthorizationRejectsAmbiguousFields(t *testing.T) {
	valid := `GoNZBNet node_id="node_a",timestamp="2026-07-09T12:00:00Z",nonce="nonce",signature="signature"`
	tests := map[string]string{
		"duplicate node id": strings.Replace(valid, `node_id="node_a"`, `node_id="node_a",node_id="node_b"`, 1),
		"unknown field":     valid + `,role="admin"`,
		"unquoted value":    strings.Replace(valid, `nonce="nonce"`, `nonce=nonce`, 1),
		"empty field":       strings.Replace(valid, `nonce="nonce"`, `nonce=""`, 1),
		"trailing comma":    valid + `,`,
	}
	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAuthorization(header); err == nil {
				t.Fatal("expected ambiguous authorization header to be rejected")
			}
		})
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	ctx := context.Background()
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	nodeID, _ := nodeIdentity.NodeID(ctx)
	publicKey, _ := nodeIdentity.PublicKey(ctx)
	store := &fakeVerifierStore{
		keys:   map[string]ed25519.PublicKey{nodeID: publicKey},
		nonces: map[string]bool{},
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	header, err := Sign(ctx, nodeIdentity, "POST", "/gonzbnet/v1/inbox", "", []byte(`{"ok":true}`), now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := Verify(ctx, store, header, "POST", "/gonzbnet/v1/inbox", "", []byte(`{"ok":false}`), now, 2*time.Minute, 10*time.Minute); err == nil {
		t.Fatalf("expected tampered body verification to fail")
	}
}

type fakeVerifierStore struct {
	keys   map[string]ed25519.PublicKey
	nonces map[string]bool
}

func (s *fakeVerifierStore) GetFederationNodePublicKey(_ context.Context, nodeID string) (ed25519.PublicKey, error) {
	return s.keys[nodeID], nil
}

func (s *fakeVerifierStore) StoreFederationNonce(_ context.Context, nodeID, nonce string, _ time.Time) (bool, error) {
	key := nodeID + ":" + nonce
	if s.nonces[key] {
		return false, nil
	}
	s.nonces[key] = true
	return true, nil
}
