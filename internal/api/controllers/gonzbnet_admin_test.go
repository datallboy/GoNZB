package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
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
