package manifestresolver

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestResolveNZBFetchesSignedManifestWithoutUserContext(t *testing.T) {
	ctx := context.Background()
	localIdentity, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("local identity: %v", err)
	}
	localNodeID, _ := localIdentity.NodeID(ctx)
	localPublicKey, _ := localIdentity.PublicKey(ctx)
	_, manifestEvent := testManifestEvent(t)
	requestStore := &fakeRequestStore{
		keys:   map[string]ed25519.PublicKey{localNodeID: localPublicKey},
		nonces: map[string]bool{},
	}
	var remoteBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		remoteBody = body
		if _, err := requestauth.Verify(ctx, requestStore, r.Header.Get("Authorization"), r.Method, r.URL.Path, r.URL.RawQuery, body, time.Now(), 2*time.Minute, 10*time.Minute); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(manifest.Response{
			SchemaVersion: "1.0",
			Type:          "ManifestResponse",
			RequestID:     "req_test",
			Status:        "ok",
			ManifestEvent: manifestEvent,
		})
	}))
	defer server.Close()

	var body manifest.ResolutionManifest
	if err := json.Unmarshal(manifestEvent.Body, &body); err != nil {
		t.Fatalf("manifest body: %v", err)
	}
	store := &fakeResolverStore{
		source: &pgindex.FederatedManifestSource{
			ManifestID:   body.ManifestID,
			ReleaseID:    body.ReleaseID,
			SourceNodeID: manifestEvent.AuthorNodeID,
			PoolID:       "pool.local",
			BaseURL:      server.URL,
			TrustScore:   1,
		},
	}
	reader, err := New(localIdentity, store).ResolveNZB(ctx, body.ReleaseID)
	if err != nil {
		t.Fatalf("resolve nzb: %v", err)
	}
	payload, _ := io.ReadAll(reader)
	_ = reader.Close()
	if !strings.Contains(string(payload), "<nzb") {
		t.Fatalf("expected generated nzb, got %q", string(payload))
	}
	if store.stored == nil || len(store.stored.GeneratedNZB) == 0 {
		t.Fatalf("expected manifest and nzb to be cached")
	}
	if strings.Contains(strings.ToLower(string(remoteBody)), "username") || strings.Contains(strings.ToLower(string(remoteBody)), "api_key") || strings.Contains(strings.ToLower(string(remoteBody)), "apikey") {
		t.Fatalf("manifest request leaked user/API context: %s", string(remoteBody))
	}
}

func testManifestEvent(t *testing.T) (*identity.Identity, *events.SignedEvent) {
	t.Helper()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("remote identity: %v", err)
	}
	core := manifest.ManifestCore{
		Groups:   []string{"alt.binaries.example"},
		Poster:   "poster@example.invalid",
		PostedAt: "2026-07-09T12:00:00Z",
		Files: []manifest.ManifestFile{{
			Name:      "example.rar",
			Subject:   "Example example.rar yEnc",
			Date:      "2026-07-09T12:01:00Z",
			SizeBytes: 1000,
			Segments:  []manifest.ManifestSegment{{Number: 1, Bytes: 1000, MessageID: "<seg1@example.invalid>"}},
		}},
		NZB: manifest.NZBInfo{Generator: "GoNZBNet", XMLCharset: "utf-8"},
	}
	manifestID, _, err := manifest.ComputeID(core)
	if err != nil {
		t.Fatalf("compute manifest id: %v", err)
	}
	body := manifest.ResolutionManifest{
		SchemaVersion: "1.0",
		Type:          manifest.Type,
		ManifestID:    manifestID,
		ReleaseID:     "rel_1",
		ManifestCore:  core,
	}
	event, validation, err := events.Create(context.Background(), node, events.CreateOptions{
		EventType:  manifest.Type,
		Sequence:   1,
		CreatedAt:  time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		PoolIDs:    []string{"pool.local"},
		Visibility: "pool",
		BodySchema: manifest.BodySchema,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("create manifest event: %v", err)
	}
	if !validation.OK {
		t.Fatalf("manifest event validation failed: %s", validation.Reason)
	}
	return node, event
}

type fakeResolverStore struct {
	source *pgindex.FederatedManifestSource
	stored *pgindex.ResolutionManifestRecord
}

func (s *fakeResolverStore) GetCachedFederatedNZBByReleaseID(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (s *fakeResolverStore) FindFederatedManifestSource(context.Context, string) (*pgindex.FederatedManifestSource, error) {
	return s.source, nil
}

func (s *fakeResolverStore) AppendVerifiedFederationEvent(context.Context, *events.SignedEvent, *events.ValidationResult) error {
	return nil
}

func (s *fakeResolverStore) StoreResolutionManifest(_ context.Context, record pgindex.ResolutionManifestRecord) error {
	cp := record
	s.stored = &cp
	return nil
}

func (s *fakeResolverStore) RecordFederatedManifestSourceSuccess(context.Context, pgindex.FederatedManifestSource) error {
	return nil
}

func (s *fakeResolverStore) RecordFederatedManifestSourceFailure(context.Context, pgindex.FederatedManifestSource) error {
	return nil
}

type fakeRequestStore struct {
	keys   map[string]ed25519.PublicKey
	nonces map[string]bool
}

func (s *fakeRequestStore) GetFederationNodePublicKey(_ context.Context, nodeID string) (ed25519.PublicKey, error) {
	return s.keys[nodeID], nil
}

func (s *fakeRequestStore) StoreFederationNonce(_ context.Context, nodeID, nonce string, _ time.Time) (bool, error) {
	key := nodeID + ":" + nonce
	if s.nonces[key] {
		return false, nil
	}
	s.nonces[key] = true
	return true, nil
}
