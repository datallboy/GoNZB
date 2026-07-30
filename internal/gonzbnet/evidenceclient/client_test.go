package evidenceclient

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/evidence"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type exchangeStore struct {
	mu       sync.Mutex
	keys     map[string]ed25519.PublicKey
	peers    []pgindex.BinaryEvidencePeer
	evidence map[string]pgindex.YEncEvidenceRecord
	nonces   map[string]struct{}
}

func (s *exchangeStore) FindAcceptedYEncEvidence(_ context.Context, ids []string, _ bool, _ int) ([]pgindex.YEncEvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []pgindex.YEncEvidenceRecord{}
	for _, id := range ids {
		if item, ok := s.evidence[id]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s *exchangeStore) UpsertYEncHeaderEvidence(_ context.Context, items []pgindex.YEncEvidenceRecord) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		s.evidence[item.MessageID] = item
	}
	return len(items), 0, nil
}
func (s *exchangeStore) ListBinaryEvidencePeers(context.Context, string, int) ([]pgindex.BinaryEvidencePeer, error) {
	return s.peers, nil
}
func (s *exchangeStore) GetFederationNodePublicKey(_ context.Context, nodeID string) (ed25519.PublicKey, error) {
	return s.keys[nodeID], nil
}
func (*exchangeStore) RecordBinaryEvidenceDiagnostic(context.Context, pgindex.BinaryEvidenceDiagnostic) error {
	return nil
}
func (*exchangeStore) ImportPeerSegments(context.Context, int64, string, string, string, []evidence.Segment) (int, int, error) {
	return 0, 0, nil
}
func (s *exchangeStore) StoreFederationNonce(_ context.Context, nodeID, nonce string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := nodeID + ":" + nonce
	if _, exists := s.nonces[key]; exists {
		return false, nil
	}
	s.nonces[key] = struct{}{}
	return true, nil
}

func TestLookupYEncUsesSignedPeerBundle(t *testing.T) {
	requester, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	requesterID, _ := requester.NodeID(ctx)
	requesterKey, _ := requester.PublicKey(ctx)
	providerID, _ := provider.NodeID(ctx)
	providerKey, _ := provider.PublicKey(ctx)
	store := &exchangeStore{
		keys:     map[string]ed25519.PublicKey{requesterID: requesterKey, providerID: providerKey},
		evidence: map[string]pgindex.YEncEvidenceRecord{}, nonces: map[string]struct{}{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query evidence.YEncQuery
		body, _ := io.ReadAll(r.Body)
		if _, err := requestauth.Verify(ctx, store, r.Header.Get("Authorization"), r.Method, r.URL.Path, r.URL.RawQuery, body, time.Now(), time.Minute, time.Minute); err != nil {
			t.Errorf("verify request: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := json.Unmarshal(body, &query); err != nil {
			t.Errorf("decode query: %v", err)
			return
		}
		now := time.Now().UTC()
		bundle := evidence.Bundle{
			PoolID: query.PoolID, RequestID: query.RequestID,
			RequestingNodeID: query.RequestingNodeID,
			YEncHeaders: []evidence.YEncHeader{{
				MessageID: "<part@example>", SourcePostedAt: now.Format(time.RFC3339),
				FileName: "movie.mkv", PartNumber: 2, TotalParts: 10, FileSize: 1234,
			}},
		}
		if err := evidence.SignBundle(ctx, provider, &bundle); err != nil {
			t.Errorf("sign bundle: %v", err)
			return
		}
		_ = json.NewEncoder(w).Encode(bundle)
	}))
	defer server.Close()
	store.peers = []pgindex.BinaryEvidencePeer{{PoolID: "pool.test", NodeID: providerID, BaseURL: server.URL}}

	client := New(requester, store, Options{
		Enabled: true, AllowInsecurePeerHTTP: true, PeerFanout: 1,
	})
	result, err := client.LookupYEnc(ctx, []string{"<part@example>"})
	if err != nil {
		t.Fatal(err)
	}
	if result.PeerHits != 1 || result.PeerRequests != 1 || result.Headers["<part@example>"].FileName != "movie.mkv" {
		t.Fatalf("unexpected lookup result: %+v", result)
	}
}
