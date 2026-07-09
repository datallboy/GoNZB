package controllers

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifestresolver"
	"github.com/datallboy/gonzb/internal/gonzbnet/moderation"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	gonzbnetsync "github.com/datallboy/gonzb/internal/gonzbnet/sync"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type GoNZBNetAdminController struct {
	appCtx *app.Context
}

type gonzbnetAdminStore interface {
	ListTrustPools(ctx context.Context) ([]pgindex.TrustPoolRecord, error)
	ListPoolMembers(ctx context.Context, poolID string) ([]pgindex.PoolMemberRecord, error)
	UpsertTrustPool(ctx context.Context, pool pgindex.TrustPoolRecord) error
	UpsertPoolMember(ctx context.Context, member pgindex.PoolMemberRecord) error
	RevokePoolMember(ctx context.Context, poolID, nodeID, eventID string, effectiveAt *time.Time) error
	UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error
	NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error)
	FindFederationEventByBodyHash(ctx context.Context, authorNodeID, eventType, bodyHash string) (string, error)
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	ProjectTombstone(ctx context.Context, projection pgindex.TombstoneProjection) error
	ListTombstones(ctx context.Context, activeOnly bool) ([]pgindex.TombstoneRecord, error)
	ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error
	SetFederationNodeStatus(ctx context.Context, nodeID, status string) (bool, error)
	UpsertFederationPeerURL(ctx context.Context, peerURL string) (int64, error)
	SetFederationPeerEnabled(ctx context.Context, peerID int64, enabled bool) error
	DeleteFederationPeer(ctx context.Context, peerID int64) (bool, error)
	ListCoverageDashboard(ctx context.Context, poolID string) (pgindex.CoverageDashboard, error)
	SuggestCoverageWork(ctx context.Context, params pgindex.CoverageWorkSuggestionParams) ([]pgindex.CoverageWorkSuggestion, error)
	BuildCoverageSchedulerPlan(ctx context.Context, params pgindex.CoverageWorkSuggestionParams) (pgindex.CoverageSchedulerPlan, error)
	ListFederationNodeCapabilities(ctx context.Context) ([]pgindex.NodeCapabilityView, error)
	ListCoverageGroupCatalog(ctx context.Context, poolID string) ([]pgindex.CoverageGroupCatalogItem, error)
	ListValidationGaps(ctx context.Context, poolID string, limit int) ([]pgindex.ValidationGap, error)
	MaterializeCoverageStaleClaimPenalties(ctx context.Context, poolID string) (int64, error)
	ListFederationPeerDiagnostics(ctx context.Context, limit int) ([]pgindex.FederationPeerDiagnostic, error)
	ListFederationEventDiagnostics(ctx context.Context, limit int) ([]pgindex.FederationEventDiagnostic, error)
	ListFederationRejectedEventDiagnostics(ctx context.Context, limit int) ([]pgindex.FederationRejectedEventDiagnostic, error)
	ListFederationPeerDeliveryDiagnostics(ctx context.Context, limit int) ([]pgindex.FederationPeerDeliveryDiagnostic, error)
	ListValidationTaskDiagnostics(ctx context.Context, limit int) ([]pgindex.ValidationTaskDiagnostic, error)
	ListFederatedReleaseSourceDiagnostics(ctx context.Context, poolID string, limit int) ([]pgindex.FederatedReleaseSourceDiagnostic, error)
	ListFederatedManifestSourceDiagnostics(ctx context.Context, poolID string, limit int) ([]pgindex.FederatedManifestSourceDiagnostic, error)
	ListHealthAttestationDiagnostics(ctx context.Context, poolID string, limit int) ([]pgindex.HealthAttestationDiagnostic, error)
	ListReputationDiagnostics(ctx context.Context, limit int) ([]pgindex.ReputationDiagnostic, error)
}

type trustPoolRequest struct {
	PoolID                     string   `json:"pool_id"`
	DisplayName                string   `json:"display_name"`
	Description                string   `json:"description"`
	MembershipThreshold        int      `json:"membership_threshold"`
	ModerationThreshold        int      `json:"moderation_threshold"`
	CheckpointWitnessThreshold int      `json:"checkpoint_witness_threshold"`
	AcceptMode                 string   `json:"accept_mode"`
	MinNodeTrustScore          float64  `json:"min_node_trust_score"`
	AcceptedEventTypes         []string `json:"accepted_event_types"`
	Enabled                    *bool    `json:"enabled"`
}

type poolMemberRequest struct {
	NodeID              string          `json:"node_id"`
	Role                string          `json:"role"`
	Status              string          `json:"status"`
	AllowedCapabilities []string        `json:"allowed_capabilities"`
	Limits              json.RawMessage `json:"limits"`
}

type peerRequest struct {
	PeerURL string `json:"peer_url"`
}

type manifestResolveRequest struct {
	ReleaseID string `json:"release_id"`
}

type manifestResolveResponse struct {
	Status    string `json:"status"`
	ReleaseID string `json:"release_id"`
	NZBBytes  int    `json:"nzb_bytes"`
	Resolved  bool   `json:"resolved"`
}

type keyExportRequest struct {
	BackupPassword string `json:"backup_password"`
	Confirmation   string `json:"confirmation"`
}

type keyExportResponse struct {
	Status       string `json:"status"`
	NodeID       string `json:"node_id"`
	PublicKey    string `json:"public_key"`
	Format       string `json:"format"`
	EncryptedKey string `json:"encrypted_key"`
	CreatedAt    string `json:"created_at"`
}

type gonzbnetAdminNodeProfileResponse struct {
	NodeID    string              `json:"node_id"`
	PublicKey string              `json:"public_key"`
	Profile   profile.NodeProfile `json:"profile"`
}

type gonzbnetAdminConfigValidationResponse struct {
	Valid   bool                       `json:"valid"`
	Summary gonzbnetAdminConfigSummary `json:"summary"`
	Issues  []gonzbnetAdminConfigIssue `json:"issues"`
}

type gonzbnetAdminConfigSummary struct {
	Mode                         string          `json:"mode"`
	HTTPEnabled                  bool            `json:"http_enabled"`
	AdvertiseURL                 string          `json:"advertise_url"`
	HTTPBasePath                 string          `json:"http_base_path"`
	PrivateNetwork               bool            `json:"private_network"`
	NetworkID                    string          `json:"network_id"`
	LocalPoolID                  string          `json:"local_pool_id"`
	ManualPeers                  int             `json:"manual_peers"`
	ModuleEnabled                map[string]bool `json:"module_enabled"`
	Limits                       map[string]int  `json:"limits"`
	Privacy                      map[string]bool `json:"privacy"`
	Publisher                    map[string]any  `json:"publisher"`
	Sync                         map[string]any  `json:"sync"`
	Gossip                       map[string]any  `json:"gossip"`
	RedactedSensitiveConfigNames []string        `json:"redacted_sensitive_config_names"`
}

type gonzbnetAdminConfigIssue struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

type tombstoneRequest struct {
	TargetType       string   `json:"target_type"`
	TargetID         string   `json:"target_id"`
	PoolID           string   `json:"pool_id"`
	Reason           string   `json:"reason"`
	Severity         string   `json:"severity"`
	EvidenceEventIDs []string `json:"evidence_event_ids"`
	EffectiveAt      string   `json:"effective_at"`
	ExpiresAt        *string  `json:"expires_at"`
}

type coverageAssignmentRequest struct {
	AssignmentID   string `json:"assignment_id"`
	PlanID         string `json:"plan_id"`
	PoolID         string `json:"pool_id"`
	Group          string `json:"group"`
	AssignedNodeID string `json:"assigned_node_id"`
	RangeStart     int64  `json:"range_start"`
	RangeEnd       int64  `json:"range_end"`
	WindowStart    string `json:"window_start"`
	WindowEnd      string `json:"window_end"`
	Priority       int    `json:"priority"`
	DueAt          string `json:"due_at"`
}

type coverageClaimRequest struct {
	ClaimID      string `json:"claim_id"`
	AssignmentID string `json:"assignment_id"`
	PoolID       string `json:"pool_id"`
	Group        string `json:"group"`
	RangeStart   int64  `json:"range_start"`
	RangeEnd     int64  `json:"range_end"`
	WindowStart  string `json:"window_start"`
	WindowEnd    string `json:"window_end"`
	ExpiresAt    string `json:"expires_at"`
}

type coverageOutcomeRequest struct {
	OutcomeID    string `json:"outcome_id"`
	ClaimID      string `json:"claim_id"`
	AssignmentID string `json:"assignment_id"`
	PoolID       string `json:"pool_id"`
	Group        string `json:"group"`
	RangeStart   int64  `json:"range_start"`
	RangeEnd     int64  `json:"range_end"`
	ReleaseCount int    `json:"release_count"`
	Reason       string `json:"reason"`
}

func NewGoNZBNetAdminController(appCtx *app.Context) *GoNZBNetAdminController {
	return &GoNZBNetAdminController{appCtx: appCtx}
}

func (ctrl *GoNZBNetAdminController) NodeProfile(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.Config == nil {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin controller is unavailable")
	}
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	cfg := ctrl.adminProfileConfig(c)
	nodeProfile, err := profile.NodeProfileFor(c.Request().Context(), nodeIdentity, cfg, time.Now())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, gonzbnetAdminNodeProfileResponse{
		NodeID:    nodeProfile.NodeID,
		PublicKey: nodeProfile.PublicKey,
		Profile:   nodeProfile,
	})
}

func (ctrl *GoNZBNetAdminController) ConfigValidation(c *echo.Context) error {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.Config == nil {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin controller is unavailable")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	issues := []gonzbnetAdminConfigIssue{}
	if err := ctrl.appCtx.Config.ValidateEffective(); err != nil {
		issues = append(issues, gonzbnetAdminConfigIssue{
			Severity: "error",
			Field:    "config",
			Message:  err.Error(),
		})
	}
	issue := func(severity, field, message string) {
		issues = append(issues, gonzbnetAdminConfigIssue{
			Severity: severity,
			Field:    field,
			Message:  message,
		})
	}
	if cfg.SendUserContext {
		issue("error", "gonzbnet.send_user_context", "must remain false; federation must not send local user context")
	}
	if cfg.MaxEventBytes <= 0 {
		issue("error", "gonzbnet.max_event_bytes", "must be greater than 0")
	}
	if cfg.MaxManifestBytes <= 0 {
		issue("error", "gonzbnet.max_manifest_bytes", "must be greater than 0")
	}
	if cfg.MaxBatchEvents <= 0 {
		issue("error", "gonzbnet.max_batch_events", "must be greater than 0")
	}
	if cfg.TimeToleranceSeconds <= 0 {
		issue("error", "gonzbnet.time_tolerance_seconds", "must be greater than 0")
	}
	if cfg.NonceTTLSeconds <= 0 {
		issue("error", "gonzbnet.nonce_ttl_seconds", "must be greater than 0")
	}
	if cfg.HTTPEnabled && strings.TrimSpace(cfg.AdvertiseURL) == "" {
		issue("warning", "gonzbnet.advertise_url", "not set; public node profile falls back to the current request host")
	}
	if cfg.HTTPEnabled && strings.TrimSpace(cfg.HTTPBasePath) != "" && !strings.HasPrefix(strings.TrimSpace(cfg.HTTPBasePath), "/") {
		issue("warning", "gonzbnet.http_base_path", "should start with /")
	}
	if !cfg.HTTPEnabled && (cfg.PushSyncEnabled || cfg.WebSocketGossipEnabled || cfg.RelayEnabled) {
		issue("warning", "gonzbnet.http_enabled", "disabled while inbound push, gossip, or relay features are enabled")
	}
	if cfg.LiveQueryEnabled {
		issue("warning", "gonzbnet.live_query_enabled", "live query is enabled; user searches should normally use the local federated cache")
	}
	if cfg.PublishReleaseCardsEnabled && !ctrl.appCtx.Config.Modules.UsenetIndexer.Enabled && !cfg.ScannerEnabled {
		issue("warning", "gonzbnet.publish_release_cards_enabled", "enabled without the local indexer module or scanner capability")
	}
	if cfg.ValidatorEnabled && !cfg.ManifestCacheEnabled {
		issue("warning", "gonzbnet.manifest_cache_enabled", "disabled while validator is enabled; validation can be limited without cached manifests")
	}
	if cfg.HealthCheckerEnabled && !cfg.ManifestCacheEnabled {
		issue("warning", "gonzbnet.manifest_cache_enabled", "disabled while health checker is enabled; health checks can be limited without cached manifests")
	}
	if ctrl.appCtx.Config.Aggregator.Sources.GoNZBNet.Enabled && !cfg.ConsumerEnabled {
		issue("warning", "gonzbnet.consumer_enabled", "disabled while the GoNZBNet aggregator source is enabled")
	}
	valid := true
	for _, item := range issues {
		if item.Severity == "error" {
			valid = false
			break
		}
	}
	return c.JSON(http.StatusOK, gonzbnetAdminConfigValidationResponse{
		Valid:   valid,
		Summary: ctrl.adminConfigSummary(),
		Issues:  issues,
	})
}

func (ctrl *GoNZBNetAdminController) ResolveManifest(c *echo.Context) error {
	var req manifestResolveRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	releaseID := strings.TrimSpace(req.ReleaseID)
	if releaseID == "" {
		return jsonError(c, http.StatusBadRequest, "release_id is required")
	}
	resolver, err := ctrl.manifestResolver()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	reader, err := resolver.ResolveNZB(c.Request().Context(), releaseID)
	if err != nil {
		return jsonError(c, http.StatusBadGateway, err.Error())
	}
	defer reader.Close()
	payload, err := io.ReadAll(io.LimitReader(reader, int64(ctrl.appCtx.Config.GoNZBNet.MaxManifestBytes)))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, manifestResolveResponse{
		Status:    "ok",
		ReleaseID: releaseID,
		NZBBytes:  len(payload),
		Resolved:  len(payload) > 0,
	})
}

func (ctrl *GoNZBNetAdminController) ExportKey(c *echo.Context) error {
	var req keyExportRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(req.Confirmation) != "export-gonzbnet-node-key" {
		return jsonError(c, http.StatusBadRequest, "confirmation must be export-gonzbnet-node-key")
	}
	if strings.TrimSpace(req.BackupPassword) == "" {
		return jsonError(c, http.StatusBadRequest, "backup_password is required")
	}
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	nodeID, err := nodeIdentity.NodeID(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	publicKey, err := nodeIdentity.PublicKeyBase64URL(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	encryptedKey, err := nodeIdentity.ExportEncryptedPrivateKey(req.BackupPassword)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, keyExportResponse{
		Status:       "ok",
		NodeID:       nodeID,
		PublicKey:    publicKey,
		Format:       "gonzbnet.ed25519.private.v1",
		EncryptedKey: encryptedKey,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

func (ctrl *GoNZBNetAdminController) ListPools(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListTrustPools(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) UpsertPool(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req trustPoolRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(req.PoolID) == "" || strings.TrimSpace(req.DisplayName) == "" {
		return jsonError(c, http.StatusBadRequest, "pool_id and display_name are required")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	acceptedTypes := req.AcceptedEventTypes
	if len(acceptedTypes) == 0 {
		acceptedTypes = []string{
			pools.EventTypeReleaseCard,
			pools.EventTypeHealthAttestation,
			pools.EventTypeTombstone,
			pools.EventTypeValidatorCapacity,
			pools.EventTypeArticleAvailabilityAttestation,
			pools.EventTypeChecksumAttestation,
			pools.EventTypeManifestAvailability,
			pools.EventTypeScannerCapacity,
			pools.EventTypeScannerHeartbeat,
			pools.EventTypeGroupObservation,
			pools.EventTypeCoveragePlan,
			pools.EventTypeCoverageAssignment,
			pools.EventTypeRangeClaim,
			pools.EventTypeTimeWindowClaim,
			pools.EventTypeCoverageCheckpoint,
			pools.EventTypeRangeComplete,
			pools.EventTypeRangeFailed,
		}
	}
	policy := pools.Policy{
		MembershipThreshold:        req.MembershipThreshold,
		ModerationThreshold:        req.ModerationThreshold,
		CheckpointWitnessThreshold: req.CheckpointWitnessThreshold,
		AcceptMode:                 req.AcceptMode,
		MinNodeTrustScore:          req.MinNodeTrustScore,
		AcceptedEventTypes:         acceptedTypes,
	}
	policy = pools.NormalizePolicy(policy, req.MembershipThreshold)
	policyJSON, _ := json.Marshal(policy)
	if err := store.UpsertTrustPool(c.Request().Context(), pgindex.TrustPoolRecord{
		PoolID:                     req.PoolID,
		DisplayName:                req.DisplayName,
		Description:                req.Description,
		PolicyJSON:                 policyJSON,
		MembershipThreshold:        policy.MembershipThreshold,
		ModerationThreshold:        policy.ModerationThreshold,
		CheckpointWitnessThreshold: policy.CheckpointWitnessThreshold,
		AcceptMode:                 policy.AcceptMode,
		MinNodeTrustScore:          policy.MinNodeTrustScore,
		AcceptedEventTypes:         policy.AcceptedEventTypes,
		Enabled:                    enabled,
	}); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) ListPoolMembers(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListPoolMembers(c.Request().Context(), pathParamTrimmed(c, "pool_id"))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) UpsertPoolMember(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req poolMemberRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(req.NodeID) == "" {
		return jsonError(c, http.StatusBadRequest, "node_id is required")
	}
	if len(req.Limits) > 0 && !json.Valid(req.Limits) {
		return jsonError(c, http.StatusBadRequest, "limits must be a JSON object")
	}
	if err := store.UpsertPoolMember(c.Request().Context(), pgindex.PoolMemberRecord{
		PoolID:              pathParamTrimmed(c, "pool_id"),
		NodeID:              req.NodeID,
		Role:                firstNonBlank(req.Role, pools.RoleMember),
		Status:              firstNonBlank(req.Status, pools.StatusActive),
		AllowedCapabilities: capability.Normalize(req.AllowedCapabilities),
		LimitsJSON:          req.Limits,
	}); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) RevokePoolMember(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	if err := store.RevokePoolMember(c.Request().Context(), pathParamTrimmed(c, "pool_id"), pathParamTrimmed(c, "node_id"), "", nil); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) ListTombstones(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	activeOnly := strings.EqualFold(queryParamTrimmed(c, "active"), "true")
	items, err := store.ListTombstones(c.Request().Context(), activeOnly)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) CreateTombstone(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req tombstoneRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	severity := firstNonBlank(req.Severity, moderation.SeverityReject)
	if strings.TrimSpace(req.PoolID) == "" && strings.TrimSpace(req.Severity) == "" {
		severity = moderation.SeverityLocalOnly
	}
	effectiveAt := firstNonBlank(req.EffectiveAt, now.Format(time.RFC3339))
	body := moderation.Tombstone{
		SchemaVersion:    "1.0",
		Type:             moderation.Type,
		TargetType:       strings.TrimSpace(req.TargetType),
		TargetID:         strings.TrimSpace(req.TargetID),
		PoolID:           strings.TrimSpace(req.PoolID),
		Reason:           strings.TrimSpace(req.Reason),
		Severity:         severity,
		EvidenceEventIDs: req.EvidenceEventIDs,
		EffectiveAt:      effectiveAt,
		ExpiresAt:        req.ExpiresAt,
	}
	if err := moderation.Validate(body, now, 2*time.Minute); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	nodeID, err := nodeIdentity.NodeID(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	publicKey, err := nodeIdentity.PublicKey(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.UpsertFederationNodeIdentity(c.Request().Context(), nodeID, publicKey); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	bodyHash, err := moderation.HashBody(body)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if existingEventID, err := store.FindFederationEventByBodyHash(c.Request().Context(), nodeID, moderation.Type, bodyHash); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	} else if existingEventID != "" {
		if err := store.ProjectTombstone(c.Request().Context(), pgindex.TombstoneProjection{
			Tombstone:    body,
			EventID:      existingEventID,
			AuthorNodeID: nodeID,
		}); err != nil {
			return jsonError(c, http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "event_id": existingEventID})
	}
	sequence, previousEventID, err := store.NextFederationEventSequence(c.Request().Context(), nodeID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolIDs := []string{}
	visibility := "local"
	if strings.TrimSpace(body.PoolID) != "" && body.Severity != moderation.SeverityLocalOnly {
		poolIDs = []string{body.PoolID}
		visibility = "pool"
	}
	event, validation, err := events.Create(c.Request().Context(), nodeIdentity, events.CreateOptions{
		EventType:       moderation.Type,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       now,
		PoolIDs:         poolIDs,
		Visibility:      visibility,
		BodySchema:      moderation.BodySchema,
		Body:            body,
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if validation == nil || !validation.OK {
		return jsonError(c, http.StatusInternalServerError, "signed tombstone did not verify")
	}
	if err := store.AppendVerifiedFederationEvent(c.Request().Context(), event, validation); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.ProjectTombstone(c.Request().Context(), pgindex.TombstoneProjection{
		Tombstone:    body,
		EventID:      event.EventID,
		AuthorNodeID: nodeID,
	}); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, map[string]string{"status": "ok", "event_id": event.EventID})
}

func (ctrl *GoNZBNetAdminController) CoverageDashboard(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	dashboard, err := store.ListCoverageDashboard(c.Request().Context(), poolID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, dashboard)
}

func (ctrl *GoNZBNetAdminController) ListNodeCapabilities(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederationNodeCapabilities(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) CoverageGroupCatalog(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	items, err := store.ListCoverageGroupCatalog(c.Request().Context(), poolID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) ValidationGaps(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	limit := parseIntDefault(queryParamTrimmed(c, "limit"), 100)
	items, err := store.ListValidationGaps(c.Request().Context(), poolID, limit)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) MaterializeStaleClaimPenalties(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	count, err := store.MaterializeCoverageStaleClaimPenalties(c.Request().Context(), poolID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "created": count})
}

func (ctrl *GoNZBNetAdminController) CoverageSuggestions(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	nodeID := queryParamTrimmed(c, "node_id")
	if nodeID == "" {
		var err error
		nodeID, err = ctrl.localNodeID(c)
		if err != nil {
			return jsonError(c, http.StatusInternalServerError, err.Error())
		}
	}
	limit := parseIntDefault(queryParamTrimmed(c, "limit"), 25)
	minTrust := parseCoverageFloatDefault(queryParamTrimmed(c, "min_blocking_trust"), 0.25)
	items, err := store.SuggestCoverageWork(c.Request().Context(), pgindex.CoverageWorkSuggestionParams{
		PoolID:                poolID,
		NodeID:                nodeID,
		Mode:                  firstNonBlank(queryParamTrimmed(c, "mode"), "scanner"),
		Limit:                 limit,
		MinBlockingTrustScore: minTrust,
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) CoverageSchedulerPlan(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	params, err := ctrl.coverageSuggestionParams(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	plan, err := store.BuildCoverageSchedulerPlan(c.Request().Context(), params)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, plan)
}

func (ctrl *GoNZBNetAdminController) UpsertPeer(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req peerRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	peerID, err := store.UpsertFederationPeerURL(c.Request().Context(), req.PeerURL)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "peer_id": peerID})
}

func (ctrl *GoNZBNetAdminController) EnablePeer(c *echo.Context) error {
	return ctrl.setPeerEnabled(c, true)
}

func (ctrl *GoNZBNetAdminController) DisablePeer(c *echo.Context) error {
	return ctrl.setPeerEnabled(c, false)
}

func (ctrl *GoNZBNetAdminController) DeletePeer(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	peerID, err := strconv.ParseInt(pathParamTrimmed(c, "peer_id"), 10, 64)
	if err != nil || peerID <= 0 {
		return jsonError(c, http.StatusBadRequest, "valid peer_id is required")
	}
	deleted, err := store.DeleteFederationPeer(c.Request().Context(), peerID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if !deleted {
		return jsonError(c, http.StatusNotFound, "peer not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "peer_id": peerID})
}

func (ctrl *GoNZBNetAdminController) BlockNode(c *echo.Context) error {
	return ctrl.setNodeStatus(c, "blocked")
}

func (ctrl *GoNZBNetAdminController) UnblockNode(c *echo.Context) error {
	return ctrl.setNodeStatus(c, "known")
}

func (ctrl *GoNZBNetAdminController) PullSync(c *echo.Context) error {
	service, err := ctrl.syncService()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	result, err := service.SyncOnce(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "result": result})
}

func (ctrl *GoNZBNetAdminController) PushSync(c *echo.Context) error {
	service, err := ctrl.syncService()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	limit := parseIntDefault(queryParamTrimmed(c, "limit"), ctrl.appCtx.Config.GoNZBNet.PushSyncBatchSize)
	result, err := service.PushOnce(c.Request().Context(), limit)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "result": result})
}

func (ctrl *GoNZBNetAdminController) GossipSync(c *echo.Context) error {
	service, err := ctrl.syncService()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	result, err := service.GossipOnce(c.Request().Context(), gonzbnetsync.GossipOptions{
		NetworkID:           cfg.NetworkID,
		TTL:                 cfg.GossipTTL,
		BatchSize:           cfg.GossipBatchSize,
		Fanout:              cfg.GossipFanout,
		PeerExchangeEnabled: cfg.PeerExchangeEnabled,
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "result": result})
}

func (ctrl *GoNZBNetAdminController) setPeerEnabled(c *echo.Context, enabled bool) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	peerID, err := strconv.ParseInt(pathParamTrimmed(c, "peer_id"), 10, 64)
	if err != nil || peerID <= 0 {
		return jsonError(c, http.StatusBadRequest, "peer_id is required")
	}
	if err := store.SetFederationPeerEnabled(c.Request().Context(), peerID, enabled); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) setNodeStatus(c *echo.Context, status string) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	nodeID := pathParamTrimmed(c, "node_id")
	if nodeID == "" {
		return jsonError(c, http.StatusBadRequest, "node_id is required")
	}
	localNodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if nodeID == localNodeID {
		return jsonError(c, http.StatusBadRequest, "local node status cannot be changed")
	}
	updated, err := store.SetFederationNodeStatus(c.Request().Context(), nodeID, status)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if !updated {
		return jsonError(c, http.StatusNotFound, "node not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "node_id": nodeID})
}

func (ctrl *GoNZBNetAdminController) PeerDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederationPeerDiagnostics(c.Request().Context(), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) EventDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederationEventDiagnostics(c.Request().Context(), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) RejectedEventDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederationRejectedEventDiagnostics(c.Request().Context(), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) PeerDeliveryDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederationPeerDeliveryDiagnostics(c.Request().Context(), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) ValidationTaskDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListValidationTaskDiagnostics(c.Request().Context(), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) ReleaseSourceDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederatedReleaseSourceDiagnostics(c.Request().Context(), queryParamTrimmed(c, "pool_id"), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) ManifestSourceDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListFederatedManifestSourceDiagnostics(c.Request().Context(), queryParamTrimmed(c, "pool_id"), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) HealthDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListHealthAttestationDiagnostics(c.Request().Context(), queryParamTrimmed(c, "pool_id"), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) ReputationDiagnostics(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListReputationDiagnostics(c.Request().Context(), parseIntDefault(queryParamTrimmed(c, "limit"), 100))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) coverageSuggestionParams(c *echo.Context) (pgindex.CoverageWorkSuggestionParams, error) {
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	nodeID := queryParamTrimmed(c, "node_id")
	if nodeID == "" {
		localNodeID, err := ctrl.localNodeID(c)
		if err != nil {
			return pgindex.CoverageWorkSuggestionParams{}, err
		}
		nodeID = localNodeID
	}
	return pgindex.CoverageWorkSuggestionParams{
		PoolID:                poolID,
		NodeID:                nodeID,
		Mode:                  firstNonBlank(queryParamTrimmed(c, "mode"), "scanner"),
		Limit:                 parseIntDefault(queryParamTrimmed(c, "limit"), 25),
		MinBlockingTrustScore: parseCoverageFloatDefault(queryParamTrimmed(c, "min_blocking_trust"), 0.25),
	}, nil
}

func (ctrl *GoNZBNetAdminController) CreateCoverageAssignment(c *echo.Context) error {
	var req coverageAssignmentRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	body := coverage.CoverageAssignment{
		SchemaVersion:  "1.0",
		Type:           coverage.TypeCoverageAssignment,
		AssignmentID:   firstNonBlank(req.AssignmentID, coverageID("assign", now)),
		PlanID:         strings.TrimSpace(req.PlanID),
		PoolID:         firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local"),
		Group:          strings.TrimSpace(req.Group),
		AssignedNodeID: strings.TrimSpace(req.AssignedNodeID),
		RangeStart:     req.RangeStart,
		RangeEnd:       req.RangeEnd,
		WindowStart:    strings.TrimSpace(req.WindowStart),
		WindowEnd:      strings.TrimSpace(req.WindowEnd),
		Priority:       req.Priority,
		DueAt:          strings.TrimSpace(req.DueAt),
		CreatedAt:      now.Format(time.RFC3339),
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeCoverageAssignment, body, body.PoolID)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageClaim(c *echo.Context) error {
	var req coverageClaimRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	nodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolID := firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	expiresAt := firstNonBlank(req.ExpiresAt, now.Add(30*time.Minute).Format(time.RFC3339))
	if strings.TrimSpace(req.WindowStart) != "" || strings.TrimSpace(req.WindowEnd) != "" {
		body := coverage.TimeWindowClaim{
			SchemaVersion: "1.0",
			Type:          coverage.TypeTimeWindowClaim,
			ClaimID:       firstNonBlank(req.ClaimID, coverageID("claim", now)),
			AssignmentID:  strings.TrimSpace(req.AssignmentID),
			PoolID:        poolID,
			Group:         strings.TrimSpace(req.Group),
			NodeID:        nodeID,
			WindowStart:   strings.TrimSpace(req.WindowStart),
			WindowEnd:     strings.TrimSpace(req.WindowEnd),
			ClaimedAt:     now.Format(time.RFC3339),
			ExpiresAt:     expiresAt,
		}
		return ctrl.signAndProjectCoverage(c, coverage.TypeTimeWindowClaim, body, poolID)
	}
	body := coverage.RangeClaim{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeClaim,
		ClaimID:       firstNonBlank(req.ClaimID, coverageID("claim", now)),
		AssignmentID:  strings.TrimSpace(req.AssignmentID),
		PoolID:        poolID,
		Group:         strings.TrimSpace(req.Group),
		NodeID:        nodeID,
		RangeStart:    req.RangeStart,
		RangeEnd:      req.RangeEnd,
		ClaimedAt:     now.Format(time.RFC3339),
		ExpiresAt:     expiresAt,
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeRangeClaim, body, poolID)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageComplete(c *echo.Context) error {
	var req coverageOutcomeRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	nodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolID := firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	body := coverage.RangeComplete{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeComplete,
		OutcomeID:     firstNonBlank(req.OutcomeID, coverageID("complete", now)),
		ClaimID:       strings.TrimSpace(req.ClaimID),
		AssignmentID:  strings.TrimSpace(req.AssignmentID),
		PoolID:        poolID,
		Group:         strings.TrimSpace(req.Group),
		NodeID:        nodeID,
		RangeStart:    req.RangeStart,
		RangeEnd:      req.RangeEnd,
		ReleaseCount:  req.ReleaseCount,
		CompletedAt:   now.Format(time.RFC3339),
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeRangeComplete, body, poolID)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageFailed(c *echo.Context) error {
	var req coverageOutcomeRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	nodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolID := firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	body := coverage.RangeFailed{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeFailed,
		OutcomeID:     firstNonBlank(req.OutcomeID, coverageID("failed", now)),
		ClaimID:       strings.TrimSpace(req.ClaimID),
		AssignmentID:  strings.TrimSpace(req.AssignmentID),
		PoolID:        poolID,
		Group:         strings.TrimSpace(req.Group),
		NodeID:        nodeID,
		RangeStart:    req.RangeStart,
		RangeEnd:      req.RangeEnd,
		Reason:        strings.TrimSpace(req.Reason),
		FailedAt:      now.Format(time.RFC3339),
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeRangeFailed, body, poolID)
}

func (ctrl *GoNZBNetAdminController) signAndProjectCoverage(c *echo.Context, eventType string, body any, poolID string) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	now := time.Now().UTC()
	if err := coverage.Validate(eventType, body, now, 2*time.Minute); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	nodeID, err := nodeIdentity.NodeID(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	publicKey, err := nodeIdentity.PublicKey(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.UpsertFederationNodeIdentity(c.Request().Context(), nodeID, publicKey); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	sequence, previousEventID, err := store.NextFederationEventSequence(c.Request().Context(), nodeID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	event, validation, err := events.Create(c.Request().Context(), nodeIdentity, events.CreateOptions{
		EventType:       eventType,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       now,
		PoolIDs:         []string{poolID},
		Visibility:      "pool",
		BodySchema:      coverage.BodySchema(eventType),
		Body:            body,
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if validation == nil || !validation.OK {
		return jsonError(c, http.StatusInternalServerError, "signed coverage event did not verify")
	}
	if err := store.AppendVerifiedFederationEvent(c.Request().Context(), event, validation); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.ProjectCoverageEvent(c.Request().Context(), event); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok", "event_id": event.EventID})
}

func (ctrl *GoNZBNetAdminController) localNodeID(c *echo.Context) (string, error) {
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return "", err
	}
	return nodeIdentity.NodeID(c.Request().Context())
}

func (ctrl *GoNZBNetAdminController) localIdentity() (*identity.Identity, error) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.Config == nil {
		return nil, fmt.Errorf("gonzbnet admin controller is not initialized")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	return identity.LoadOrCreateWithPassword(cfg.KeysDir, cfg.KeyPassword)
}

func (ctrl *GoNZBNetAdminController) syncService() (*gonzbnetsync.Service, error) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.PGIndexStore == nil {
		return nil, fmt.Errorf("gonzbnet admin store is unavailable")
	}
	syncStore, ok := ctrl.appCtx.PGIndexStore.(gonzbnetsync.Store)
	if !ok {
		return nil, fmt.Errorf("gonzbnet sync store is unavailable")
	}
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return nil, err
	}
	return gonzbnetsync.New(nodeIdentity, syncStore, ctrl.appCtx.Logger), nil
}

func (ctrl *GoNZBNetAdminController) manifestResolver() (*manifestresolver.Resolver, error) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.PGIndexStore == nil {
		return nil, fmt.Errorf("gonzbnet manifest resolver store is unavailable")
	}
	resolverStore, ok := ctrl.appCtx.PGIndexStore.(manifestresolver.Store)
	if !ok {
		return nil, fmt.Errorf("gonzbnet manifest resolver store is unavailable")
	}
	nodeIdentity, err := ctrl.localIdentity()
	if err != nil {
		return nil, err
	}
	return manifestresolver.New(nodeIdentity, resolverStore), nil
}

func coverageID(prefix string, now time.Time) string {
	return fmt.Sprintf("%s_%d", prefix, now.UnixNano())
}

func parseCoverageFloatDefault(raw string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return fallback
	}
	return value
}

func (ctrl *GoNZBNetAdminController) store() (gonzbnetAdminStore, bool) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.PGIndexStore == nil {
		return nil, false
	}
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetAdminStore)
	return store, ok
}

func (ctrl *GoNZBNetAdminController) adminProfileConfig(c *echo.Context) profile.Config {
	cfg := ctrl.appCtx.Config.GoNZBNet
	return profile.Config{
		Alias:            cfg.NodeAlias,
		AdvertiseURL:     ctrl.adminBaseURL(c),
		HTTPBasePath:     cfg.HTTPBasePath,
		PrivateNetwork:   cfg.PrivateNetwork,
		LiveQueryEnabled: cfg.LiveQueryEnabled,
		WebSocketGossip:  cfg.WebSocketGossipEnabled,
		PeerExchange:     cfg.PeerExchangeEnabled,
		RelayMode:        cfg.RelayEnabled,
		Consumer:         cfg.ConsumerEnabled,
		Scanner:          cfg.ScannerEnabled,
		Indexer:          ctrl.appCtx.Config.Modules.UsenetIndexer.Enabled,
		ManifestBuilder:  cfg.ManifestBuilderEnabled,
		ManifestCache:    cfg.ManifestCacheEnabled,
		Validator:        cfg.ValidatorEnabled,
		HealthChecker:    cfg.HealthCheckerEnabled,
		Coverage:         cfg.CoverageEnabled,
		Scheduler:        cfg.SchedulerEnabled,
		MaxEventBytes:    cfg.MaxEventBytes,
		MaxManifestBytes: cfg.MaxManifestBytes,
		MaxBatchEvents:   cfg.MaxBatchEvents,
	}
}

func (ctrl *GoNZBNetAdminController) adminBaseURL(c *echo.Context) string {
	cfg := ctrl.appCtx.Config.GoNZBNet
	if strings.TrimSpace(cfg.AdvertiseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.AdvertiseURL), "/")
	}
	basePath := strings.TrimRight(strings.TrimSpace(cfg.HTTPBasePath), "/")
	if basePath == "" {
		basePath = "/gonzbnet/v1"
	}
	return fmt.Sprintf("%s://%s%s", c.Scheme(), c.Request().Host, basePath)
}

func (ctrl *GoNZBNetAdminController) adminConfigSummary() gonzbnetAdminConfigSummary {
	cfg := ctrl.appCtx.Config.GoNZBNet
	return gonzbnetAdminConfigSummary{
		Mode:           cfg.Mode,
		HTTPEnabled:    cfg.HTTPEnabled,
		AdvertiseURL:   cfg.AdvertiseURL,
		HTTPBasePath:   cfg.HTTPBasePath,
		PrivateNetwork: cfg.PrivateNetwork,
		NetworkID:      cfg.NetworkID,
		LocalPoolID:    cfg.LocalPoolID,
		ManualPeers:    len(cfg.ManualPeers),
		ModuleEnabled: map[string]bool{
			"aggregator_gonzbnet":  ctrl.appCtx.Config.Aggregator.Sources.GoNZBNet.Enabled,
			"consumer":             cfg.ConsumerEnabled,
			"coverage":             cfg.CoverageEnabled,
			"health_checker":       cfg.HealthCheckerEnabled,
			"index_projection":     cfg.IndexProjectionEnabled,
			"manifest_builder":     cfg.ManifestBuilderEnabled,
			"manifest_cache":       cfg.ManifestCacheEnabled,
			"peer_exchange":        cfg.PeerExchangeEnabled,
			"publish_releasecards": cfg.PublishReleaseCardsEnabled,
			"relay":                cfg.RelayEnabled,
			"scanner":              cfg.ScannerEnabled,
			"scheduler":            cfg.SchedulerEnabled,
			"usenet_indexer":       ctrl.appCtx.Config.Modules.UsenetIndexer.Enabled,
			"validator":            cfg.ValidatorEnabled,
			"websocket_gossip":     cfg.WebSocketGossipEnabled,
		},
		Limits: map[string]int{
			"max_batch_events":       cfg.MaxBatchEvents,
			"max_event_bytes":        cfg.MaxEventBytes,
			"max_manifest_bytes":     cfg.MaxManifestBytes,
			"nonce_ttl_seconds":      cfg.NonceTTLSeconds,
			"time_tolerance_seconds": cfg.TimeToleranceSeconds,
		},
		Privacy: map[string]bool{
			"live_query_enabled":         cfg.LiveQueryEnabled,
			"manifest_trusted_pool_only": true,
			"private_network":            cfg.PrivateNetwork,
			"send_user_context":          cfg.SendUserContext,
			"share_provider_backbone":    cfg.ShareProviderBackbone,
			"share_source_indexer_hash":  cfg.ShareSourceIndexer,
		},
		Publisher: map[string]any{
			"checksum_validation_enabled":        cfg.ChecksumValidationEnabled,
			"health_attestations_batch_size":     cfg.HealthAttestationsBatchSize,
			"health_attestations_enabled":        cfg.HealthAttestationsEnabled,
			"health_attestations_interval_min":   cfg.HealthAttestationsIntervalMin,
			"manifest_availability_enabled":      cfg.ManifestAvailabilityEnabled,
			"publish_release_cards_batch_size":   cfg.PublishReleaseCardsBatchSize,
			"publish_release_cards_enabled":      cfg.PublishReleaseCardsEnabled,
			"publish_release_cards_interval_min": cfg.PublishReleaseCardsIntervalMin,
			"validation_batch_size":              cfg.ValidationBatchSize,
			"validation_interval_min":            cfg.ValidationIntervalMin,
		},
		Sync: map[string]any{
			"pull_sync_enabled":      cfg.PullSyncEnabled,
			"pull_sync_interval_min": cfg.PullSyncIntervalMin,
			"push_sync_batch_size":   cfg.PushSyncBatchSize,
			"push_sync_enabled":      cfg.PushSyncEnabled,
			"push_sync_interval_min": cfg.PushSyncIntervalMin,
		},
		Gossip: map[string]any{
			"gossip_batch_size":        cfg.GossipBatchSize,
			"gossip_fanout":            cfg.GossipFanout,
			"gossip_interval_min":      cfg.GossipIntervalMin,
			"gossip_ttl":               cfg.GossipTTL,
			"peer_exchange_enabled":    cfg.PeerExchangeEnabled,
			"websocket_gossip_enabled": cfg.WebSocketGossipEnabled,
		},
		RedactedSensitiveConfigNames: []string{
			"gonzbnet.key_password",
			"gonzbnet.manual_peers",
			"gonzbnet.relay_api_key",
		},
	}
}
