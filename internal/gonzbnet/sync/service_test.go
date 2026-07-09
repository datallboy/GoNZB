package sync

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestSyncOnceAcceptsAndProjectsRemoteReleaseCard(t *testing.T) {
	ctx := context.Background()
	localIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("local identity: %v", err)
	}
	remoteIdentity, event := testRemoteReleaseCardEvent(t)
	server := testPeerServer(t, remoteIdentity, []events.SignedEvent{*event})
	store := &fakeSyncStore{
		peers: []pgindex.FederationPeerRecord{{ID: 1, PeerURL: server.URL}},
	}

	result, err := New(localIdentity, store, nil).SyncOnce(ctx)
	if err != nil {
		t.Fatalf("sync once: %v", err)
	}
	if result.Accepted != 1 || result.Projected != 1 || result.Rejected != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.events) != 1 {
		t.Fatalf("expected stored event")
	}
	if len(store.projections) != 1 {
		t.Fatalf("expected release card projection")
	}
	if store.successCursor == "" {
		t.Fatalf("expected cursor to be stored")
	}
}

func TestSyncOnceRejectsTamperedRemoteEvent(t *testing.T) {
	ctx := context.Background()
	localIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("local identity: %v", err)
	}
	remoteIdentity, event := testRemoteReleaseCardEvent(t)
	event.Body = []byte(`{"schema_version":"1.0","type":"ReleaseCard","release_id":"rel_tampered"}`)
	server := testPeerServer(t, remoteIdentity, []events.SignedEvent{*event})
	store := &fakeSyncStore{
		peers: []pgindex.FederationPeerRecord{{ID: 1, PeerURL: server.URL}},
	}

	result, err := New(localIdentity, store, nil).SyncOnce(ctx)
	if err != nil {
		t.Fatalf("sync once: %v", err)
	}
	if result.Accepted != 0 || result.Projected != 0 || result.Rejected != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.rejected) != 1 {
		t.Fatalf("expected rejected event")
	}
}

func TestPushOnceSendsSignedEventBatchAndRecordsDelivery(t *testing.T) {
	ctx := context.Background()
	localIdentity, event := testRemoteReleaseCardEvent(t)
	localNodeID, _ := localIdentity.NodeID(ctx)
	localPublicKey, _ := localIdentity.PublicKey(ctx)
	remoteIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("remote identity: %v", err)
	}
	authStore := &fakeRequestAuthStore{
		keys:   map[string]ed25519.PublicKey{localNodeID: localPublicKey},
		nonces: map[string]bool{},
	}
	server := testPushPeerServer(t, remoteIdentity, authStore)
	store := &fakeSyncStore{
		peers:       []pgindex.FederationPeerRecord{{ID: 7, PeerURL: server.URL}},
		undelivered: []*events.SignedEvent{event},
	}

	result, err := New(localIdentity, store, nil).PushOnce(ctx, 10)
	if err != nil {
		t.Fatalf("push once: %v", err)
	}
	if result.Accepted != 1 || result.Duplicate != 0 || result.Rejected != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.deliveries) != 1 {
		t.Fatalf("expected delivery record")
	}
	if store.deliveries[0].PeerID != 7 || store.deliveries[0].EventID != event.EventID || store.deliveries[0].Status != "accepted" {
		t.Fatalf("unexpected delivery: %+v", store.deliveries[0])
	}
}

func testPeerServer(t *testing.T, nodeIdentity *identity.Identity, outbox []events.SignedEvent) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	baseURL := server.URL + "/gonzbnet/v1"
	nodeProfile, err := profile.NodeProfileFor(context.Background(), nodeIdentity, profile.Config{
		AdvertiseURL:     baseURL,
		PrivateNetwork:   true,
		MaxEventBytes:    262144,
		MaxManifestBytes: 10485760,
		MaxBatchEvents:   100,
	}, time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	wellKnown, err := profile.WellKnownFor(context.Background(), nodeIdentity, baseURL)
	if err != nil {
		t.Fatalf("well known: %v", err)
	}
	mux.HandleFunc("/.well-known/gonzbnet", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wellKnown)
	})
	mux.HandleFunc("/gonzbnet/v1/node", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(nodeProfile)
	})
	mux.HandleFunc("/gonzbnet/v1/caps", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(profile.CapsFor(262144, 10485760))
	})
	mux.HandleFunc("/gonzbnet/v1/handshake", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})
	mux.HandleFunc("/gonzbnet/v1/outbox", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OutboxPage{
			SchemaVersion: "1.0",
			Type:          "OutboxPage",
			Events:        outbox,
			NextCursor:    outbox[len(outbox)-1].EventID,
			HasMore:       false,
		})
	})
	t.Cleanup(server.Close)
	return server
}

func testPushPeerServer(t *testing.T, nodeIdentity *identity.Identity, authStore *fakeRequestAuthStore) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	baseURL := server.URL + "/gonzbnet/v1"
	nodeProfile, err := profile.NodeProfileFor(context.Background(), nodeIdentity, profile.Config{
		AdvertiseURL:     baseURL,
		PrivateNetwork:   true,
		MaxEventBytes:    262144,
		MaxManifestBytes: 10485760,
		MaxBatchEvents:   100,
	}, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("node profile: %v", err)
	}
	wellKnown, err := profile.WellKnownFor(context.Background(), nodeIdentity, baseURL)
	if err != nil {
		t.Fatalf("well known: %v", err)
	}
	mux.HandleFunc("/.well-known/gonzbnet", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wellKnown)
	})
	mux.HandleFunc("/gonzbnet/v1/node", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(nodeProfile)
	})
	mux.HandleFunc("/gonzbnet/v1/inbox", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if _, err := requestauth.Verify(context.Background(), authStore, r.Header.Get("Authorization"), r.Method, r.URL.Path, r.URL.RawQuery, body, time.Now(), 2*time.Minute, 10*time.Minute); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		var batch EventBatch
		if err := json.Unmarshal(body, &batch); err != nil || batch.Type != "EventBatch" {
			http.Error(w, "invalid event batch", http.StatusBadRequest)
			return
		}
		resp := InboxResponse{
			SchemaVersion: "1.0",
			Type:          "InboxResponse",
			Accepted:      make([]InboxEventResult, 0, len(batch.Events)),
			Duplicate:     []InboxEventResult{},
			Rejected:      []InboxEventResult{},
		}
		for _, event := range batch.Events {
			resp.Accepted = append(resp.Accepted, InboxEventResult{EventID: event.EventID, Status: "accepted"})
			resp.Cursor = event.EventID
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	t.Cleanup(server.Close)
	return server
}

func testRemoteReleaseCardEvent(t *testing.T) (*identity.Identity, *events.SignedEvent) {
	t.Helper()
	nodeIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("remote identity: %v", err)
	}
	card, err := releasecard.MapLocalRelease(testSyncLocalRelease())
	if err != nil {
		t.Fatalf("map release: %v", err)
	}
	event, validation, err := events.Create(context.Background(), nodeIdentity, events.CreateOptions{
		EventType:  "ReleaseCard",
		Sequence:   1,
		CreatedAt:  time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
		PoolIDs:    []string{"pool.local"},
		Visibility: "pool",
		BodySchema: releasecard.BodySchema,
		Body:       card,
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if !validation.OK {
		t.Fatalf("event validation failed: %s", validation.Reason)
	}
	return nodeIdentity, event
}

func testSyncLocalRelease() releasecard.LocalRelease {
	posted := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	return releasecard.LocalRelease{
		LocalReleaseID: "release-1",
		Title:          "Example.Release.2026.1080p.WEB-DL",
		Category:       "movies",
		CategoryID:     2040,
		SizeBytes:      1000,
		PostedAt:       &posted,
		FileCount:      1,
		Groups:         []string{"alt.binaries.example"},
		PasswordState:  "not_passworded",
		Availability:   1,
		Files: []releasecard.LocalFile{{
			Name:      "example.mkv",
			Subject:   "Example.Release.2026.1080p.WEB-DL example.mkv yEnc",
			PostedAt:  &posted,
			SizeBytes: 1000,
			FileIndex: 1,
			Segments: []releasecard.LocalSegment{{
				Number:    1,
				Bytes:     1000,
				MessageID: "<seg1@example.invalid>",
			}},
		}},
	}
}

type fakeSyncStore struct {
	peers         []pgindex.FederationPeerRecord
	nodes         []pgindex.FederationNodeRecord
	events        []*events.SignedEvent
	rejected      []string
	projections   []releasecard.Projection
	undelivered   []*events.SignedEvent
	deliveries    []pgindex.FederationDeliveryResult
	successCursor string
	failures      []string
}

func (s *fakeSyncStore) UpsertFederationPeerURL(context.Context, string) (int64, error) {
	return 1, nil
}

func (s *fakeSyncStore) ListEnabledFederationPeers(context.Context) ([]pgindex.FederationPeerRecord, error) {
	return s.peers, nil
}

func (s *fakeSyncStore) UpsertFederationNode(_ context.Context, node pgindex.FederationNodeRecord) error {
	s.nodes = append(s.nodes, node)
	return nil
}

func (s *fakeSyncStore) AppendVerifiedFederationEvent(_ context.Context, event *events.SignedEvent, _ *events.ValidationResult) error {
	s.events = append(s.events, event)
	return nil
}

func (s *fakeSyncStore) AppendRejectedFederationEvent(_ context.Context, _, _, _ string, _ []byte, reason string) error {
	s.rejected = append(s.rejected, reason)
	return nil
}

func (s *fakeSyncStore) UpsertFederatedReleaseCardProjection(_ context.Context, projection releasecard.Projection) error {
	s.projections = append(s.projections, projection)
	return nil
}

func (s *fakeSyncStore) MarkFederationPeerSyncSuccess(_ context.Context, _ int64, _, cursor, _ string) error {
	s.successCursor = cursor
	return nil
}

func (s *fakeSyncStore) MarkFederationPeerSyncFailure(_ context.Context, _ int64, errText string) error {
	s.failures = append(s.failures, errText)
	return nil
}

func (s *fakeSyncStore) ListUndeliveredFederationEvents(_ context.Context, _ int64, _ int) ([]*events.SignedEvent, error) {
	return s.undelivered, nil
}

func (s *fakeSyncStore) RecordFederationPeerDelivery(_ context.Context, result pgindex.FederationDeliveryResult) error {
	s.deliveries = append(s.deliveries, result)
	return nil
}

type fakeRequestAuthStore struct {
	keys   map[string]ed25519.PublicKey
	nonces map[string]bool
}

func (s *fakeRequestAuthStore) GetFederationNodePublicKey(_ context.Context, nodeID string) (ed25519.PublicKey, error) {
	return s.keys[nodeID], nil
}

func (s *fakeRequestAuthStore) StoreFederationNonce(_ context.Context, nodeID, nonce string, _ time.Time) (bool, error) {
	key := nodeID + ":" + nonce
	if s.nonces[key] {
		return false, nil
	}
	s.nonces[key] = true
	return true, nil
}

var _ Store = (*fakeSyncStore)(nil)
