package controllers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

func TestGoNZBNetAdminNodeProfileReturnsPublicIdentity(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gonzbnet/node/profile", nil)
	req.Host = "example.test"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: testGoNZBNetAdminConfig(t)})

	if err := ctrl.NodeProfile(c); err != nil {
		t.Fatalf("NodeProfile returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body gonzbnetAdminNodeProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.NodeID == "" || body.PublicKey == "" {
		t.Fatalf("expected node identity in response: %+v", body)
	}
	if body.Profile.NodeID != body.NodeID || body.Profile.PublicKey != body.PublicKey {
		t.Fatalf("expected profile identity to match top-level identity: %+v", body)
	}
	if body.Profile.Endpoints.Base == "" || !strings.Contains(body.Profile.Endpoints.Base, "/gonzbnet/v1") {
		t.Fatalf("expected fallback base endpoint, got %q", body.Profile.Endpoints.Base)
	}
}

func TestGoNZBNetAdminConfigValidationRedactsSensitiveValues(t *testing.T) {
	cfg := testGoNZBNetAdminConfig(t)
	cfg.GoNZBNet.SendUserContext = true
	cfg.GoNZBNet.KeyPassword = "secret-password"
	cfg.GoNZBNet.RelayAPIKey = "relay-secret"
	cfg.GoNZBNet.ManualPeers = []string{"https://peer.example/gonzbnet/v1"}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gonzbnet/config/validation", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: cfg})

	if err := ctrl.ConfigValidation(c); err != nil {
		t.Fatalf("ConfigValidation returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body gonzbnetAdminConfigValidationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Valid {
		t.Fatalf("expected invalid config when send_user_context is true")
	}
	if body.Summary.ManualPeers != 1 {
		t.Fatalf("expected peer count only, got %d", body.Summary.ManualPeers)
	}
	raw := rec.Body.String()
	for _, secret := range []string{"secret-password", "relay-secret", "peer.example"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("response leaked sensitive value %q: %s", secret, raw)
		}
	}
}

func TestGoNZBNetAdminResolveManifestRequiresReleaseID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gonzbnet/manifests/resolve", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: testGoNZBNetAdminConfig(t)})

	if err := ctrl.ResolveManifest(c); err != nil {
		t.Fatalf("ResolveManifest returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "release_id is required") {
		t.Fatalf("expected release_id validation error, got %s", rec.Body.String())
	}
}

func TestGoNZBNetAdminExportKeyRequiresExplicitConfirmation(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gonzbnet/keys/export", bytes.NewReader([]byte(`{"backup_password":"backup"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: testGoNZBNetAdminConfig(t)})

	if err := ctrl.ExportKey(c); err != nil {
		t.Fatalf("ExportKey returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "confirmation") {
		t.Fatalf("expected confirmation validation error, got %s", rec.Body.String())
	}
}

func TestGoNZBNetAdminExportKeyReturnsEncryptedBackupOnly(t *testing.T) {
	cfg := testGoNZBNetAdminConfig(t)
	cfg.GoNZBNet.KeyPassword = "configured-key-password"
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gonzbnet/keys/export", bytes.NewReader([]byte(`{"backup_password":"backup-password","confirmation":"export-gonzbnet-node-key"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: cfg})

	if err := ctrl.ExportKey(c); err != nil {
		t.Fatalf("ExportKey returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body keyExportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.NodeID == "" || body.PublicKey == "" || body.EncryptedKey == "" {
		t.Fatalf("expected encrypted backup response, got %+v", body)
	}
	if !strings.Contains(body.EncryptedKey, "gonzbnet.ed25519.private.v1") {
		t.Fatalf("expected encrypted key envelope, got %q", body.EncryptedKey)
	}
	raw := rec.Body.String()
	for _, secret := range []string{"configured-key-password", "backup-password"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("response leaked sensitive value %q: %s", secret, raw)
		}
	}
}

func TestGoNZBNetAdminRotateKeyRequiresExplicitConfirmation(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gonzbnet/keys/rotate", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: testGoNZBNetAdminConfig(t)})

	if err := ctrl.RotateKey(c); err != nil {
		t.Fatalf("RotateKey returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "confirmation") {
		t.Fatalf("expected confirmation validation error, got %s", rec.Body.String())
	}
}

func TestGoNZBNetAdminRotateKeyChangesNodeIDAndDoesNotReturnSecrets(t *testing.T) {
	cfg := testGoNZBNetAdminConfig(t)
	cfg.GoNZBNet.KeyPassword = "configured-key-password"
	original, err := identity.LoadOrCreateWithPassword(cfg.GoNZBNet.KeysDir, cfg.GoNZBNet.KeyPassword)
	if err != nil {
		t.Fatalf("create original identity: %v", err)
	}
	originalID, _ := original.NodeID(t.Context())
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gonzbnet/keys/rotate", bytes.NewReader([]byte(`{"confirmation":"rotate-gonzbnet-node-key"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	ctrl := NewGoNZBNetAdminController(&app.Context{Config: cfg})

	if err := ctrl.RotateKey(c); err != nil {
		t.Fatalf("RotateKey returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body keyRotateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.OldNodeID != originalID {
		t.Fatalf("expected old node id %q, got %q", originalID, body.OldNodeID)
	}
	if body.NewNodeID == "" || body.NewNodeID == originalID {
		t.Fatalf("expected new node id, got %+v", body)
	}
	if body.OldPublicKey == "" || body.NewPublicKey == "" || body.OldPublicKey == body.NewPublicKey {
		t.Fatalf("expected changed public keys, got %+v", body)
	}
	if body.BackupPath == "" || body.Warning == "" {
		t.Fatalf("expected backup path and warning, got %+v", body)
	}
	reloaded, err := identity.LoadOrCreateWithPassword(cfg.GoNZBNet.KeysDir, cfg.GoNZBNet.KeyPassword)
	if err != nil {
		t.Fatalf("reload rotated identity: %v", err)
	}
	reloadedID, _ := reloaded.NodeID(t.Context())
	if reloadedID != body.NewNodeID {
		t.Fatalf("expected persisted rotated node id %q, got %q", body.NewNodeID, reloadedID)
	}
	raw := rec.Body.String()
	for _, secret := range []string{"configured-key-password", "encrypted_key", encryptedKeyEnvelopeMarker} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(secret)) {
			t.Fatalf("response leaked sensitive value %q: %s", secret, raw)
		}
	}
}

func TestGoNZBNetAdminRequestPoolJoinSignsAndStoresEvent(t *testing.T) {
	cfg := testGoNZBNetAdminConfig(t)
	store := &fakeGoNZBNetAdminStore{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gonzbnet/pools/pool.remote/join-requests", bytes.NewReader([]byte(`{"requested_roles":["member","validator","member"],"message":" please add "}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "pool_id", Value: "pool.remote"}})
	ctrl := &GoNZBNetAdminController{appCtx: &app.Context{Config: cfg}, storeOverride: store}

	if err := ctrl.RequestPoolJoin(c); err != nil {
		t.Fatalf("RequestPoolJoin returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.appended == nil {
		t.Fatal("expected signed event to be appended")
	}
	if store.nodeID == "" || len(store.publicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected local node identity to be upserted, node_id=%q key_size=%d", store.nodeID, len(store.publicKey))
	}
	validation, err := events.Verify(store.appended)
	if err != nil {
		t.Fatalf("verify event: %v", err)
	}
	if validation == nil || !validation.OK {
		t.Fatalf("expected event signature to verify, got %+v", validation)
	}
	if store.appended.EventType != pools.EventTypePoolJoinRequest {
		t.Fatalf("expected PoolJoinRequest event, got %q", store.appended.EventType)
	}
	if store.appended.BodySchema != pools.BodySchema(pools.EventTypePoolJoinRequest) {
		t.Fatalf("unexpected body schema %q", store.appended.BodySchema)
	}
	if len(store.appended.PoolIDs) != 1 || store.appended.PoolIDs[0] != "pool.remote" {
		t.Fatalf("expected event pool_id pool.remote, got %+v", store.appended.PoolIDs)
	}
	var body pools.JoinRequest
	if err := json.Unmarshal(store.appended.Body, &body); err != nil {
		t.Fatalf("decode event body: %v", err)
	}
	if body.PoolID != "pool.remote" || body.CandidateNodeID != store.nodeID {
		t.Fatalf("unexpected join request body: %+v", body)
	}
	if body.Message != "please add" {
		t.Fatalf("expected trimmed message, got %q", body.Message)
	}
	if len(body.RequestedRoles) != 2 || body.RequestedRoles[0] != "member" || body.RequestedRoles[1] != "validator" {
		t.Fatalf("expected deduplicated roles, got %+v", body.RequestedRoles)
	}
	var response poolJoinResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.EventID != store.appended.EventID || response.CandidateNodeID != store.nodeID {
		t.Fatalf("expected response to reference appended event and node, got %+v", response)
	}
}

const encryptedKeyEnvelopeMarker = "gonzbnet.ed25519.private.v1"

func testGoNZBNetAdminConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		GoNZBNet: config.GoNZBNetConfig{
			KeysDir:                        t.TempDir(),
			HTTPEnabled:                    true,
			HTTPBasePath:                   "/gonzbnet/v1",
			PrivateNetwork:                 true,
			NetworkID:                      "default",
			LocalPoolID:                    "pool.local",
			ConsumerEnabled:                true,
			IndexProjectionEnabled:         true,
			ManifestCacheEnabled:           true,
			PublishReleaseCardsBatchSize:   50,
			PublishReleaseCardsIntervalMin: 10,
			HealthAttestationsBatchSize:    50,
			HealthAttestationsIntervalMin:  30,
			ValidationBatchSize:            25,
			ValidationIntervalMin:          15,
			PushSyncBatchSize:              100,
			GossipBatchSize:                100,
			GossipTTL:                      4,
			GossipFanout:                   4,
			MaxEventBytes:                  262144,
			MaxManifestBytes:               10485760,
			MaxBatchEvents:                 100,
			RateLimitEventsPerMinute:       120,
			TimeToleranceSeconds:           120,
			NonceTTLSeconds:                600,
		},
		Modules: config.ModulesConfig{
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
		Aggregator: config.AggregatorConfig{
			Sources: config.AggregatorSourcesConfig{
				GoNZBNet: config.ModuleToggle{Enabled: true},
			},
		},
	}
}

type fakeGoNZBNetAdminStore struct {
	nodeID          string
	publicKey       ed25519.PublicKey
	nextSequence    int64
	previousEventID *string
	appended        *events.SignedEvent
}

func (s *fakeGoNZBNetAdminStore) ListTrustPools(context.Context) ([]pgindex.TrustPoolRecord, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListPoolMembers(context.Context, string) ([]pgindex.PoolMemberRecord, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) UpsertTrustPool(context.Context, pgindex.TrustPoolRecord) error {
	return nil
}

func (s *fakeGoNZBNetAdminStore) UpsertPoolMember(context.Context, pgindex.PoolMemberRecord) error {
	return nil
}

func (s *fakeGoNZBNetAdminStore) RevokePoolMember(context.Context, string, string, string, *time.Time) error {
	return nil
}

func (s *fakeGoNZBNetAdminStore) UpsertFederationNodeIdentity(_ context.Context, nodeID string, publicKey ed25519.PublicKey) error {
	s.nodeID = nodeID
	s.publicKey = append(ed25519.PublicKey(nil), publicKey...)
	return nil
}

func (s *fakeGoNZBNetAdminStore) NextFederationEventSequence(context.Context, string) (int64, *string, error) {
	if s.nextSequence <= 0 {
		return 1, s.previousEventID, nil
	}
	return s.nextSequence, s.previousEventID, nil
}

func (s *fakeGoNZBNetAdminStore) FindFederationEventByBodyHash(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (s *fakeGoNZBNetAdminStore) ValidateFederationPoolControlEvent(_ context.Context, event *events.SignedEvent) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if event.EventType != pools.EventTypePoolJoinRequest {
		return fmt.Errorf("unexpected event type %q", event.EventType)
	}
	var body pools.JoinRequest
	if err := json.Unmarshal(event.Body, &body); err != nil {
		return err
	}
	if body.PoolID == "" || body.CandidateNodeID == "" {
		return fmt.Errorf("join request requires pool_id and candidate_node_id")
	}
	return nil
}

func (s *fakeGoNZBNetAdminStore) AppendVerifiedFederationEvent(_ context.Context, event *events.SignedEvent, validation *events.ValidationResult) error {
	if validation == nil || !validation.OK {
		return fmt.Errorf("validation failed")
	}
	s.appended = event
	return nil
}

func (s *fakeGoNZBNetAdminStore) ProjectTombstone(context.Context, pgindex.TombstoneProjection) error {
	return nil
}

func (s *fakeGoNZBNetAdminStore) ListTombstones(context.Context, bool) ([]pgindex.TombstoneRecord, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ProjectCoverageEvent(context.Context, *events.SignedEvent) error {
	return nil
}

func (s *fakeGoNZBNetAdminStore) SetFederationNodeStatus(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *fakeGoNZBNetAdminStore) UpsertFederationPeerURL(context.Context, string) (int64, error) {
	return 1, nil
}

func (s *fakeGoNZBNetAdminStore) SetFederationPeerEnabled(context.Context, int64, bool) error {
	return nil
}

func (s *fakeGoNZBNetAdminStore) DeleteFederationPeer(context.Context, int64) (bool, error) {
	return true, nil
}

func (s *fakeGoNZBNetAdminStore) ListCoverageDashboard(context.Context, string) (pgindex.CoverageDashboard, error) {
	return pgindex.CoverageDashboard{}, nil
}

func (s *fakeGoNZBNetAdminStore) SuggestCoverageWork(context.Context, pgindex.CoverageWorkSuggestionParams) ([]pgindex.CoverageWorkSuggestion, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) BuildCoverageSchedulerPlan(context.Context, pgindex.CoverageWorkSuggestionParams) (pgindex.CoverageSchedulerPlan, error) {
	return pgindex.CoverageSchedulerPlan{}, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederationNodeCapabilities(context.Context) ([]pgindex.NodeCapabilityView, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListCoverageGroupCatalog(context.Context, string) ([]pgindex.CoverageGroupCatalogItem, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListValidationGaps(context.Context, string, int) ([]pgindex.ValidationGap, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) MaterializeCoverageStaleClaimPenalties(context.Context, string) (int64, error) {
	return 0, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederationPeerDiagnostics(context.Context, int) ([]pgindex.FederationPeerDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederationEventDiagnostics(context.Context, int) ([]pgindex.FederationEventDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederationRejectedEventDiagnostics(context.Context, int) ([]pgindex.FederationRejectedEventDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederationPeerDeliveryDiagnostics(context.Context, int) ([]pgindex.FederationPeerDeliveryDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListValidationTaskDiagnostics(context.Context, int) ([]pgindex.ValidationTaskDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederatedReleaseSourceDiagnostics(context.Context, string, int) ([]pgindex.FederatedReleaseSourceDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListFederatedManifestSourceDiagnostics(context.Context, string, int) ([]pgindex.FederatedManifestSourceDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListHealthAttestationDiagnostics(context.Context, string, int) ([]pgindex.HealthAttestationDiagnostic, error) {
	return nil, nil
}

func (s *fakeGoNZBNetAdminStore) ListReputationDiagnostics(context.Context, int) ([]pgindex.ReputationDiagnostic, error) {
	return nil, nil
}
