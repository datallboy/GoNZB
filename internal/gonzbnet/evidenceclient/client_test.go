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
	mu               sync.Mutex
	keys             map[string]ed25519.PublicKey
	peers            []pgindex.BinaryEvidencePeer
	evidence         map[string]pgindex.YEncEvidenceRecord
	nonces           map[string]struct{}
	importedSegments []evidence.Segment
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
func (s *exchangeStore) ImportPeerSegments(_ context.Context, _ int64, _, _, _ string, segments []evidence.Segment) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.importedSegments = append(s.importedSegments, segments...)
	return len(segments), 0, nil
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

func TestLookupSegmentsImportsSignedPeerHeaders(t *testing.T) {
	ctx := context.Background()
	requester, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requesterID, _ := requester.NodeID(ctx)
	requesterKey, _ := requester.PublicKey(ctx)
	providerID, _ := provider.NodeID(ctx)
	providerKey, _ := provider.PublicKey(ctx)
	store := &exchangeStore{
		keys: map[string]ed25519.PublicKey{
			requesterID: requesterKey,
			providerID:  providerKey,
		},
		evidence: map[string]pgindex.YEncEvidenceRecord{},
		nonces:   map[string]struct{}{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query evidence.SegmentQuery
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Errorf("decode segment query: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bundle := evidence.Bundle{
			PoolID: query.PoolID, RequestID: query.RequestID,
			RequestingNodeID: query.RequestingNodeID,
			Segments: []evidence.Segment{{
				PartNumber: 4, TotalParts: 10, MessageID: "<part-four@example>",
				Bytes: 1000, SourcePostedAt: time.Now().UTC().Format(time.RFC3339),
				Groups: []string{"alt.test"}, FileName: "movie.mkv",
			}},
		}
		if err := evidence.SignBundle(ctx, provider, &bundle); err != nil {
			t.Errorf("sign segment bundle: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(bundle)
	}))
	defer server.Close()
	store.peers = []pgindex.BinaryEvidencePeer{{
		PoolID: "pool.test", NodeID: providerID, BaseURL: server.URL,
	}}

	client := New(requester, store, Options{
		Enabled: true, AllowInsecurePeerHTTP: true, PeerFanout: 1,
	})
	result, err := client.LookupSegments(
		ctx, 42, "yenc_v1", "match_123", []int{4}, []string{"<anchor@example>"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.PeerRequests != 1 || result.Imported != 1 || len(store.importedSegments) != 1 {
		t.Fatalf("expected one imported peer segment, result=%+v imported=%+v", result, store.importedSegments)
	}
	if store.importedSegments[0].MessageID != "<part-four@example>" {
		t.Fatalf("unexpected imported segment: %+v", store.importedSegments[0])
	}
}

func TestLookupYEncCombinesMultiplePeersAndSkipsFailedPeer(t *testing.T) {
	ctx := context.Background()
	requester, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	badProvider, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providerA, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providerB, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requesterID, _ := requester.NodeID(ctx)
	requesterKey, _ := requester.PublicKey(ctx)
	badProviderID, _ := badProvider.NodeID(ctx)
	badProviderKey, _ := badProvider.PublicKey(ctx)
	providerAID, _ := providerA.NodeID(ctx)
	providerAKey, _ := providerA.PublicKey(ctx)
	providerBID, _ := providerB.NodeID(ctx)
	providerBKey, _ := providerB.PublicKey(ctx)
	store := &exchangeStore{
		keys: map[string]ed25519.PublicKey{
			requesterID: requesterKey, badProviderID: badProviderKey,
			providerAID: providerAKey, providerBID: providerBKey,
		},
		evidence: map[string]pgindex.YEncEvidenceRecord{},
		nonces:   map[string]struct{}{},
	}

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(evidence.Bundle{
			SchemaVersion: evidence.SchemaVersion,
			Type:          evidence.BundleType,
			SourceNodeID:  badProviderID,
			Signature:     "tampered",
		})
	}))
	defer badServer.Close()
	newPeerServer := func(provider *identity.Identity, messageID, fileName string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var query evidence.YEncQuery
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
				t.Errorf("decode query: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			bundle := evidence.Bundle{
				PoolID: query.PoolID, RequestID: query.RequestID,
				RequestingNodeID: query.RequestingNodeID,
				YEncHeaders: []evidence.YEncHeader{{
					MessageID: messageID, SourcePostedAt: time.Now().UTC().Format(time.RFC3339),
					FileName: fileName, PartNumber: 1, TotalParts: 1, FileSize: 100,
				}},
			}
			if err := evidence.SignBundle(ctx, provider, &bundle); err != nil {
				t.Errorf("sign bundle: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(bundle)
		}))
	}
	serverA := newPeerServer(providerA, "<part-a@example>", "part-a.mkv")
	defer serverA.Close()
	serverB := newPeerServer(providerB, "<part-b@example>", "part-b.mkv")
	defer serverB.Close()

	store.peers = []pgindex.BinaryEvidencePeer{
		{PoolID: "pool.test", NodeID: badProviderID, BaseURL: badServer.URL},
		{PoolID: "pool.test", NodeID: providerAID, BaseURL: serverA.URL},
		{PoolID: "pool.test", NodeID: providerBID, BaseURL: serverB.URL},
	}
	client := New(requester, store, Options{
		Enabled: true, AllowInsecurePeerHTTP: true, PeerFanout: 3,
	})
	result, err := client.LookupYEnc(ctx, []string{"<part-a@example>", "<part-b@example>"})
	if err != nil {
		t.Fatal(err)
	}
	if result.PeerRequests != 3 || result.PeerHits != 2 {
		t.Fatalf("expected one failed and two useful peer requests, got %+v", result)
	}
	if result.Headers["<part-a@example>"].FileName != "part-a.mkv" ||
		result.Headers["<part-b@example>"].FileName != "part-b.mkv" {
		t.Fatalf("expected combined peer evidence, got %+v", result.Headers)
	}
}

func TestLookupYEncUsesLocalEvidenceWithoutPeerRequest(t *testing.T) {
	ctx := context.Background()
	requester, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &exchangeStore{
		keys: map[string]ed25519.PublicKey{},
		evidence: map[string]pgindex.YEncEvidenceRecord{
			"<local@example>": {
				SourcePostedAt: time.Now().UTC(), MessageID: "<local@example>",
				FileName: "local.mkv", Provenance: "local_body",
			},
		},
		nonces: map[string]struct{}{},
	}
	client := New(requester, store, Options{Enabled: true, PeerFanout: 3})
	result, err := client.LookupYEnc(ctx, []string{"<local@example>"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheHits != 1 || result.PeerRequests != 0 || result.Headers["<local@example>"].FileName != "local.mkv" {
		t.Fatalf("expected local evidence to avoid peer traffic, got %+v", result)
	}
}
