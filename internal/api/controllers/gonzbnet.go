package controllers

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/eventbody"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/gossip"
	"github.com/datallboy/gonzb/internal/gonzbnet/health"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifestavailability"
	"github.com/datallboy/gonzb/internal/gonzbnet/moderation"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/gonzbnet/trust"
	gonzbnetvalidation "github.com/datallboy/gonzb/internal/gonzbnet/validation"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
	"golang.org/x/net/websocket"
)

type GoNZBNetController struct {
	appCtx   *app.Context
	identity *identity.Identity
}

type gonzbnetStore interface {
	ListFederationOutboxEvents(ctx context.Context, params pgindex.FederationOutboxParams) (pgindex.FederationOutboxPage, error)
	GetFederationEvent(ctx context.Context, eventID string) (*events.SignedEvent, error)
	FederationEventExists(ctx context.Context, eventID string) (bool, error)
	GetFederationNodePublicKey(ctx context.Context, nodeID string) (ed25519.PublicKey, error)
	StoreFederationNonce(ctx context.Context, nodeID, nonce string, expiresAt time.Time) (bool, error)
	UpsertFederationNode(ctx context.Context, node pgindex.FederationNodeRecord) error
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	AppendRejectedFederationEvent(ctx context.Context, eventID, authorNodeID, eventType string, rawEventJSON []byte, reason string) error
	GetPoolCheckpointEvent(ctx context.Context, poolID string) (*events.SignedEvent, error)
	ListPoolMembers(ctx context.Context, poolID string) ([]pgindex.PoolMemberRecord, error)
	UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error
	ValidateFederationPoolControlEvent(ctx context.Context, event *events.SignedEvent) error
	ProjectFederationPoolEvent(ctx context.Context, event *events.SignedEvent) error
	CanAcceptFederationEventForPools(ctx context.Context, authorNodeID string, poolIDs []string, eventType string) (pgindex.PoolAuthorizationResult, error)
	IsActivePoolMember(ctx context.Context, poolID, nodeID string) (bool, error)
	IsActiveFederationPoolMember(ctx context.Context, nodeID string) (bool, error)
	ListFederationNodeCapabilities(ctx context.Context) ([]pgindex.NodeCapabilityView, error)
	ListCoverageGroupCatalog(ctx context.Context, poolID string) ([]pgindex.CoverageGroupCatalogItem, error)
	SuggestCoverageWork(ctx context.Context, params pgindex.CoverageWorkSuggestionParams) ([]pgindex.CoverageWorkSuggestion, error)
	BuildCoverageSchedulerPlan(ctx context.Context, params pgindex.CoverageWorkSuggestionParams) (pgindex.CoverageSchedulerPlan, error)
	GetResolutionManifest(ctx context.Context, manifestID string) (*manifest.ResolutionManifest, error)
	GetResolutionManifestEvent(ctx context.Context, manifestID string) (*events.SignedEvent, error)
	CanFetchResolutionManifest(ctx context.Context, manifestID, nodeID string) (bool, error)
	ProjectHealthAttestation(ctx context.Context, projection pgindex.HealthAttestationProjection) error
	ProjectTrustAttestation(ctx context.Context, projection pgindex.TrustAttestationProjection) error
	ProjectValidatorCapacity(ctx context.Context, projection pgindex.ValidatorCapacityProjection) error
	ProjectArticleAvailabilityAttestation(ctx context.Context, projection pgindex.ArticleAvailabilityProjection) error
	ProjectChecksumAttestation(ctx context.Context, projection pgindex.ChecksumAttestationProjection) error
	EnqueueFederationValidationTask(ctx context.Context, request pgindex.ValidationTaskRequest) (bool, error)
	ProjectManifestAvailability(ctx context.Context, projection pgindex.ManifestAvailabilityProjection) error
	ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error
	ProjectTombstone(ctx context.Context, projection pgindex.TombstoneProjection) error
	ListEnabledFederationPeers(ctx context.Context) ([]pgindex.FederationPeerRecord, error)
	UpsertFederationPeerURL(ctx context.Context, peerURL string) (int64, error)
}

type outboxResponse struct {
	SchemaVersion string                `json:"schema_version"`
	Type          string                `json:"type"`
	Events        []*events.SignedEvent `json:"events"`
	NextCursor    string                `json:"next_cursor"`
	HasMore       bool                  `json:"has_more"`
}

type poolMembersResponse struct {
	SchemaVersion string                     `json:"schema_version"`
	Type          string                     `json:"type"`
	PoolID        string                     `json:"pool_id"`
	Members       []pgindex.PoolMemberRecord `json:"members"`
}

type poolCheckpointResponse struct {
	SchemaVersion   string              `json:"schema_version"`
	Type            string              `json:"type"`
	PoolID          string              `json:"pool_id"`
	CheckpointEvent *events.SignedEvent `json:"checkpoint_event"`
}

type peersResponse struct {
	SchemaVersion string   `json:"schema_version"`
	Type          string   `json:"type"`
	Peers         []string `json:"peers"`
}

type coverageGroupsResponse struct {
	SchemaVersion string                             `json:"schema_version"`
	Type          string                             `json:"type"`
	PoolID        string                             `json:"pool_id"`
	Items         []pgindex.CoverageGroupCatalogItem `json:"items"`
	Count         int                                `json:"count"`
}

type coverageWorkResponse struct {
	SchemaVersion string                           `json:"schema_version"`
	Type          string                           `json:"type"`
	PoolID        string                           `json:"pool_id"`
	NodeID        string                           `json:"node_id"`
	Items         []pgindex.CoverageWorkSuggestion `json:"items"`
	Count         int                              `json:"count"`
}

type nodeCapabilitiesResponse struct {
	SchemaVersion string                       `json:"schema_version"`
	Type          string                       `json:"type"`
	PoolID        string                       `json:"pool_id"`
	Items         []pgindex.NodeCapabilityView `json:"items"`
	Count         int                          `json:"count"`
}

type inboxEventBatch struct {
	SchemaVersion string               `json:"schema_version"`
	Type          string               `json:"type"`
	Events        []events.SignedEvent `json:"events"`
}

type inboxEventResult struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type inboxResponse struct {
	SchemaVersion string             `json:"schema_version"`
	Type          string             `json:"type"`
	Accepted      []inboxEventResult `json:"accepted"`
	Duplicate     []inboxEventResult `json:"duplicate"`
	Rejected      []inboxEventResult `json:"rejected"`
	Cursor        string             `json:"cursor,omitempty"`
}

type federationErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func federationJSONError(c *echo.Context, status int, code, message string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "internal_error"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = code
	}
	return c.JSON(status, federationErrorResponse{
		Error:   code,
		Code:    code,
		Message: message,
	})
}

func federationAuthErrorCode(err error) string {
	if err == nil {
		return "invalid_signature"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "nonce replay"):
		return "replayed_nonce"
	case strings.Contains(msg, "future"):
		return "future_timestamp"
	case strings.Contains(msg, "expired"), strings.Contains(msg, "outside tolerance"):
		return "expired_event"
	case strings.Contains(msg, "public key"):
		return "unknown_node"
	default:
		return "invalid_signature"
	}
}

func federationVerificationCode(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(reason, "unsupported spec_version"):
		return "unsupported_spec_version"
	case strings.Contains(reason, "event_id"):
		return "invalid_event_id"
	case strings.Contains(reason, "sequence_conflict"):
		return "sequence_conflict"
	case strings.Contains(reason, "fork"):
		return "fork_detected"
	case strings.Contains(reason, "body_hash"):
		return "invalid_body_hash"
	case strings.Contains(reason, "signature"):
		return "invalid_signature"
	case strings.Contains(reason, "event_type"):
		return "unsupported_event_type"
	case strings.Contains(reason, "future"):
		return "future_timestamp"
	case strings.Contains(reason, "expired"):
		return "expired_event"
	case strings.Contains(reason, "too old"):
		return "stale_event"
	default:
		return "invalid_schema"
	}
}

func federationPoolErrorCode(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(reason, "blocked"):
		return "node_blocked"
	case strings.Contains(reason, "revoked"):
		return "node_revoked"
	case strings.Contains(reason, "role"), strings.Contains(reason, "capability"):
		return "insufficient_pool_role"
	case strings.Contains(reason, "quorum"), strings.Contains(reason, "threshold"):
		return "insufficient_pool_quorum"
	case strings.Contains(reason, "member"), strings.Contains(reason, "pool"), reason == "missing_pool":
		return "not_pool_member"
	default:
		return "not_pool_member"
	}
}

func federationBodyReadErrorCode(err error) string {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return "payload_too_large"
	}
	return "invalid_json"
}

func NewGoNZBNetController(appCtx *app.Context) *GoNZBNetController {
	return &GoNZBNetController{appCtx: appCtx}
}

func (ctrl *GoNZBNetController) WellKnown(c *echo.Context) error {
	id, err := ctrl.localIdentity()
	if err != nil {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", err.Error())
	}
	resp, err := profile.WellKnownFor(c.Request().Context(), id, ctrl.baseURL(c))
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, resp)
}

func (ctrl *GoNZBNetController) Node(c *echo.Context) error {
	id, err := ctrl.localIdentity()
	if err != nil {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", err.Error())
	}
	resp, err := profile.NodeProfileFor(c.Request().Context(), id, ctrl.profileConfig(c), time.Now())
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, resp)
}

func (ctrl *GoNZBNetController) Caps(c *echo.Context) error {
	cfg := ctrl.appCtx.Config.GoNZBNet
	return c.JSON(http.StatusOK, profile.CapsFor(cfg.MaxEventBytes, cfg.MaxManifestBytes))
}

func (ctrl *GoNZBNetController) Outbox(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	verified, ok := ctrl.verifyNodeRead(c, store)
	if !ok {
		return nil
	}
	active, err := store.IsActiveFederationPoolMember(c.Request().Context(), verified.NodeID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if !active {
		return federationJSONError(c, http.StatusForbidden, "not_pool_member", "requesting node is not an active pool member")
	}
	page, err := store.ListFederationOutboxEvents(c.Request().Context(), pgindex.FederationOutboxParams{
		Since:            queryParamTrimmed(c, "since"),
		PoolID:           queryParamTrimmed(c, "pool"),
		EventType:        queryParamTrimmed(c, "type"),
		RequestingNodeID: verified.NodeID,
		Limit:            parseIntDefault(queryParamTrimmed(c, "limit"), 100),
	})
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, outboxResponse{
		SchemaVersion: "1.0",
		Type:          "OutboxPage",
		Events:        page.Events,
		NextCursor:    page.NextCursor,
		HasMore:       page.HasMore,
	})
}

func (ctrl *GoNZBNetController) Event(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	verified, ok := ctrl.verifyNodeRead(c, store)
	if !ok {
		return nil
	}
	event, err := store.GetFederationEvent(c.Request().Context(), pathParamTrimmed(c, "event_id"))
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if event == nil {
		return federationJSONError(c, http.StatusNotFound, "invalid_event_id", "event not found")
	}
	if strings.TrimSpace(event.Visibility) != "public" {
		allowed := false
		for _, poolID := range event.PoolIDs {
			active, err := store.IsActivePoolMember(c.Request().Context(), poolID, verified.NodeID)
			if err != nil {
				return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
			}
			if active {
				allowed = true
				break
			}
		}
		if !allowed {
			return federationJSONError(c, http.StatusForbidden, "not_pool_member", "requesting node cannot read this event")
		}
	}
	return c.JSON(http.StatusOK, event)
}

func (ctrl *GoNZBNetController) PoolMembers(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	poolID := pathParamTrimmed(c, "pool_id")
	if poolID == "" {
		return federationJSONError(c, http.StatusBadRequest, "invalid_schema", "pool_id is required")
	}
	if _, ok := ctrl.authorizePathPoolRead(c, store, poolID); !ok {
		return nil
	}
	members, err := store.ListPoolMembers(c.Request().Context(), poolID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, poolMembersResponse{
		SchemaVersion: "1.0",
		Type:          "PoolMembers",
		PoolID:        poolID,
		Members:       members,
	})
}

func (ctrl *GoNZBNetController) PoolCheckpoint(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	poolID := pathParamTrimmed(c, "pool_id")
	if poolID == "" {
		return federationJSONError(c, http.StatusBadRequest, "invalid_schema", "pool_id is required")
	}
	if _, ok := ctrl.authorizePathPoolRead(c, store, poolID); !ok {
		return nil
	}
	event, err := store.GetPoolCheckpointEvent(c.Request().Context(), poolID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if event == nil {
		return federationJSONError(c, http.StatusNotFound, "checkpoint_not_found", "pool checkpoint not found")
	}
	return c.JSON(http.StatusOK, poolCheckpointResponse{
		SchemaVersion:   "1.0",
		Type:            "PoolCheckpoint",
		PoolID:          poolID,
		CheckpointEvent: event,
	})
}

func (ctrl *GoNZBNetController) Peers(c *echo.Context) error {
	cfg := ctrl.appCtx.Config.GoNZBNet
	if !cfg.PeerExchangeEnabled {
		return c.JSON(http.StatusOK, peersResponse{
			SchemaVersion: "1.0",
			Type:          "PeerList",
			Peers:         []string{},
		})
	}
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	verified, ok := ctrl.verifyNodeRead(c, store)
	if !ok {
		return nil
	}
	active, err := store.IsActiveFederationPoolMember(c.Request().Context(), verified.NodeID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if !active {
		return federationJSONError(c, http.StatusForbidden, "not_pool_member", "requesting node is not an active pool member")
	}
	records, err := store.ListEnabledFederationPeers(c.Request().Context())
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	peers := make([]string, 0, len(records))
	for _, record := range records {
		peers = append(peers, record.PeerURL)
	}
	return c.JSON(http.StatusOK, peersResponse{
		SchemaVersion: "1.0",
		Type:          "PeerList",
		Peers:         gossip.FilterPeers(peers, true, cfg.GossipFanout),
	})
}

func (ctrl *GoNZBNetController) CoverageGroups(c *echo.Context) error {
	store, verified, poolID, ok := ctrl.authorizedPoolRead(c)
	if !ok {
		return nil
	}
	_ = verified
	items, err := store.ListCoverageGroupCatalog(c.Request().Context(), poolID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, coverageGroupsResponse{
		SchemaVersion: "1.0",
		Type:          "CoverageGroups",
		PoolID:        poolID,
		Items:         items,
		Count:         len(items),
	})
}

func (ctrl *GoNZBNetController) CoveragePlan(c *echo.Context) error {
	store, verified, poolID, ok := ctrl.authorizedPoolRead(c)
	if !ok {
		return nil
	}
	params := ctrl.coverageWorkParams(c, poolID, verified.NodeID)
	plan, err := store.BuildCoverageSchedulerPlan(c.Request().Context(), params)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"schema_version": "1.0",
		"type":           "CoveragePlanView",
		"pool_id":        poolID,
		"node_id":        verified.NodeID,
		"plan":           plan,
	})
}

func (ctrl *GoNZBNetController) CoverageWork(c *echo.Context) error {
	store, verified, poolID, ok := ctrl.authorizedPoolRead(c)
	if !ok {
		return nil
	}
	params := ctrl.coverageWorkParams(c, poolID, verified.NodeID)
	items, err := store.SuggestCoverageWork(c.Request().Context(), params)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusOK, coverageWorkResponse{
		SchemaVersion: "1.0",
		Type:          "CoverageWork",
		PoolID:        poolID,
		NodeID:        verified.NodeID,
		Items:         items,
		Count:         len(items),
	})
}

func (ctrl *GoNZBNetController) CoverageClaim(c *echo.Context) error {
	return ctrl.acceptConstrainedFederationEvents(c, map[string]struct{}{
		coverage.TypeRangeClaim:      {},
		coverage.TypeTimeWindowClaim: {},
	})
}

func (ctrl *GoNZBNetController) CoverageCheckpoint(c *echo.Context) error {
	return ctrl.acceptConstrainedFederationEvents(c, map[string]struct{}{
		coverage.TypeCoverageCheckpoint: {},
		coverage.TypeRangeComplete:      {},
		coverage.TypeRangeFailed:        {},
	})
}

func (ctrl *GoNZBNetController) ValidationRequest(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	if !ctrl.appCtx.Config.GoNZBNet.ValidatorEnabled {
		return c.JSON(http.StatusForbidden, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			Status:        "error",
			Code:          "validator_disabled",
			Message:       "local validator module is disabled",
		})
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		code := federationBodyReadErrorCode(err)
		status := http.StatusBadRequest
		if code == "payload_too_large" {
			status = http.StatusRequestEntityTooLarge
		}
		return federationJSONError(c, status, code, "read validation request body")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	verified, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		body,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	)
	if err != nil {
		return federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
	}
	var req gonzbnetvalidation.Request
	if err := decodeFederationJSON(body, &req); err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_json", "invalid validation request json")
	}
	if err := gonzbnetvalidation.ValidateRequest(req, time.Now().UTC(), time.Duration(cfg.TimeToleranceSeconds)*time.Second); err != nil {
		return c.JSON(http.StatusBadRequest, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "invalid_schema",
			Message:       err.Error(),
		})
	}
	if req.RequestingNodeID != verified.NodeID {
		return c.JSON(http.StatusForbidden, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "requesting_node_mismatch",
			Message:       "requesting node does not match request signature",
		})
	}
	if strings.TrimSpace(req.TargetNodeID) != "" {
		id, err := ctrl.localIdentity()
		if err != nil {
			return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", err.Error())
		}
		localNodeID, err := id.NodeID(c.Request().Context())
		if err != nil {
			return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", err.Error())
		}
		if req.TargetNodeID != localNodeID {
			return c.JSON(http.StatusForbidden, gonzbnetvalidation.Response{
				SchemaVersion: "1.0",
				Type:          "ValidationResponse",
				RequestID:     req.RequestID,
				Status:        "error",
				Code:          "target_node_mismatch",
				Message:       "target node does not match local node",
			})
		}
	}
	active, err := store.IsActivePoolMember(c.Request().Context(), req.PoolID, verified.NodeID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if !active {
		return c.JSON(http.StatusForbidden, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "not_pool_member",
			Message:       "requesting node is not authorized for this pool",
		})
	}
	allowed, err := store.CanFetchResolutionManifest(c.Request().Context(), req.ManifestID, verified.NodeID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if !allowed {
		return c.JSON(http.StatusForbidden, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "not_pool_member",
			Message:       "requesting node is not authorized for this manifest",
		})
	}
	item, err := store.GetResolutionManifest(c.Request().Context(), req.ManifestID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if item == nil {
		return c.JSON(http.StatusNotFound, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "manifest_not_found",
			Message:       "manifest not found",
		})
	}
	if item.ReleaseID != req.ReleaseID {
		return c.JSON(http.StatusBadRequest, gonzbnetvalidation.Response{
			SchemaVersion: "1.0",
			Type:          "ValidationResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "release_id_mismatch",
			Message:       "release_id does not match cached manifest",
		})
	}
	var dueAt *time.Time
	if strings.TrimSpace(req.DueAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.DueAt))
		if err != nil {
			return c.JSON(http.StatusBadRequest, gonzbnetvalidation.Response{
				SchemaVersion: "1.0",
				Type:          "ValidationResponse",
				RequestID:     req.RequestID,
				Status:        "error",
				Code:          "invalid_schema",
				Message:       "due_at must be RFC3339",
			})
		}
		dueAt = &parsed
	}
	queued, err := store.EnqueueFederationValidationTask(c.Request().Context(), pgindex.ValidationTaskRequest{
		ManifestID: req.ManifestID,
		ReleaseID:  req.ReleaseID,
		PoolID:     req.PoolID,
		Priority:   req.Priority,
		DueAt:      dueAt,
	})
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	return c.JSON(http.StatusAccepted, gonzbnetvalidation.Response{
		SchemaVersion: "1.0",
		Type:          "ValidationResponse",
		RequestID:     req.RequestID,
		Status:        "accepted",
		Queued:        queued,
	})
}

func (ctrl *GoNZBNetController) NodeCapabilities(c *echo.Context) error {
	store, _, poolID, ok := ctrl.authorizedPoolRead(c)
	if !ok {
		return nil
	}
	members, err := store.ListPoolMembers(c.Request().Context(), poolID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	activeMembers := map[string]struct{}{}
	for _, member := range members {
		if strings.TrimSpace(member.Status) == pools.StatusActive {
			activeMembers[member.NodeID] = struct{}{}
		}
	}
	items, err := store.ListFederationNodeCapabilities(c.Request().Context())
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	filtered := make([]pgindex.NodeCapabilityView, 0, len(items))
	for _, item := range items {
		if _, ok := activeMembers[item.NodeID]; ok {
			filtered = append(filtered, item)
		}
	}
	return c.JSON(http.StatusOK, nodeCapabilitiesResponse{
		SchemaVersion: "1.0",
		Type:          "NodeCapabilities",
		PoolID:        poolID,
		Items:         filtered,
		Count:         len(filtered),
	})
}

func (ctrl *GoNZBNetController) acceptConstrainedFederationEvents(c *echo.Context, allowed map[string]struct{}) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		code := federationBodyReadErrorCode(err)
		status := http.StatusBadRequest
		if code == "payload_too_large" {
			status = http.StatusRequestEntityTooLarge
		}
		return federationJSONError(c, status, code, "read federation event body")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	verified, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		body,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	)
	if err != nil {
		return federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
	}
	eventsIn, err := decodeInboxEvents(body)
	if err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_json", err.Error())
	}
	maxBatch := cfg.MaxBatchEvents
	if maxBatch <= 0 {
		maxBatch = 100
	}
	if len(eventsIn) > maxBatch {
		return federationJSONError(c, http.StatusBadRequest, "invalid_schema", "event batch exceeds max_batch_events")
	}
	resp := inboxResponse{
		SchemaVersion: "1.0",
		Type:          "InboxResponse",
		Accepted:      []inboxEventResult{},
		Duplicate:     []inboxEventResult{},
		Rejected:      []inboxEventResult{},
	}
	for i := range eventsIn {
		event := eventsIn[i]
		if _, ok := allowed[event.EventType]; !ok {
			resp.Rejected = append(resp.Rejected, inboxEventResult{
				EventID: event.EventID,
				Status:  "rejected",
				Code:    "unsupported_event_type",
				Message: "event_type is not accepted by this endpoint",
			})
			continue
		}
		if event.AuthorNodeID != verified.NodeID {
			resp.Rejected = append(resp.Rejected, inboxEventResult{
				EventID: event.EventID,
				Status:  "rejected",
				Code:    "request_author_mismatch",
				Message: "requesting node does not match event author",
			})
			continue
		}
		item := ctrl.acceptInboxEvent(c.Request().Context(), store, &event)
		switch item.Status {
		case "accepted":
			resp.Accepted = append(resp.Accepted, item)
			resp.Cursor = item.EventID
		case "duplicate":
			resp.Duplicate = append(resp.Duplicate, item)
			resp.Cursor = item.EventID
		default:
			resp.Rejected = append(resp.Rejected, item)
		}
	}
	return c.JSON(http.StatusOK, resp)
}

func (ctrl *GoNZBNetController) authorizedPoolRead(c *echo.Context) (gonzbnetStore, requestauth.VerificationResult, string, bool) {
	var empty requestauth.VerificationResult
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		_ = federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
		return nil, empty, "", false
	}
	poolID := queryParamTrimmed(c, "pool_id")
	if poolID == "" {
		_ = federationJSONError(c, http.StatusBadRequest, "invalid_schema", "pool_id is required")
		return nil, empty, "", false
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	verified, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		nil,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	)
	if err != nil {
		_ = federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
		return nil, empty, "", false
	}
	active, err := store.IsActivePoolMember(c.Request().Context(), poolID, verified.NodeID)
	if err != nil {
		_ = federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
		return nil, empty, "", false
	}
	if !active {
		_ = federationJSONError(c, http.StatusForbidden, "not_pool_member", "requesting node is not authorized for this pool")
		return nil, empty, "", false
	}
	return store, verified, poolID, true
}

func (ctrl *GoNZBNetController) verifyNodeRead(c *echo.Context, store gonzbnetStore) (requestauth.VerificationResult, bool) {
	var empty requestauth.VerificationResult
	if store == nil {
		_ = federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
		return empty, false
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	verified, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		nil,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	)
	if err != nil {
		_ = federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
		return empty, false
	}
	return verified, true
}

func (ctrl *GoNZBNetController) authorizePathPoolRead(c *echo.Context, store gonzbnetStore, poolID string) (requestauth.VerificationResult, bool) {
	verified, ok := ctrl.verifyNodeRead(c, store)
	if !ok {
		return requestauth.VerificationResult{}, false
	}
	active, err := store.IsActivePoolMember(c.Request().Context(), poolID, verified.NodeID)
	if err != nil {
		_ = federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
		return requestauth.VerificationResult{}, false
	}
	if !active {
		_ = federationJSONError(c, http.StatusForbidden, "not_pool_member", "requesting node is not authorized for this pool")
		return requestauth.VerificationResult{}, false
	}
	return verified, true
}

func (ctrl *GoNZBNetController) coverageWorkParams(c *echo.Context, poolID, nodeID string) pgindex.CoverageWorkSuggestionParams {
	return pgindex.CoverageWorkSuggestionParams{
		PoolID:                poolID,
		NodeID:                nodeID,
		Mode:                  firstNonBlank(queryParamTrimmed(c, "role"), queryParamTrimmed(c, "mode"), "scanner"),
		Limit:                 parseIntDefault(queryParamTrimmed(c, "limit"), 25),
		MinBlockingTrustScore: 0.25,
	}
}

func (ctrl *GoNZBNetController) Handshake(c *echo.Context) error {
	id, err := ctrl.localIdentity()
	if err != nil {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", err.Error())
	}
	bodyBytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_json", "read handshake json")
	}
	var req map[string]any
	if err := decodeFederationJSON(bodyBytes, &req); err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_json", "invalid handshake json")
	}
	signatureValue, _ := req["signature"].(string)
	delete(req, "signature")
	if strings.TrimSpace(signatureValue) == "" {
		return federationJSONError(c, http.StatusBadRequest, "invalid_signature", "missing signature")
	}
	nodeID, _ := req["node_id"].(string)
	publicKeyValue, _ := req["public_key"].(string)
	nonce, _ := req["nonce"].(string)
	publicKey, err := canonical.DecodeBase64URL(publicKeyValue)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return federationJSONError(c, http.StatusBadRequest, "invalid_signature", "invalid public key")
	}
	if identity.NodeIDFromPublicKey(ed25519.PublicKey(publicKey)) != nodeID {
		return federationJSONError(c, http.StatusBadRequest, "invalid_signature", "node_id does not match public key")
	}
	signature, err := canonical.DecodeBase64URL(signatureValue)
	if err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_signature", "invalid signature")
	}
	canonicalRequest, err := canonical.Marshal(req)
	if err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_schema", "invalid canonical handshake")
	}
	if !identity.Verify(ed25519.PublicKey(publicKey), canonicalRequest, signature) {
		return federationJSONError(c, http.StatusUnauthorized, "invalid_signature", "handshake signature verification failed")
	}

	if store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore); ok {
		_ = store.UpsertFederationNode(c.Request().Context(), pgindex.FederationNodeRecord{
			NodeID:    nodeID,
			PublicKey: ed25519.PublicKey(publicKey),
			Status:    "handshaken",
		})
	}

	localNodeID, _ := id.NodeID(c.Request().Context())
	body := map[string]any{
		"schema_version":    "1.0",
		"type":              "HandshakeResponse",
		"node_id":           localNodeID,
		"nonce":             nonce,
		"accepted_versions": []string{profile.SpecVersion},
		"status":            "accepted",
		"created_at":        time.Now().UTC().Format(time.RFC3339),
	}
	canonicalResponse, err := canonical.Marshal(body)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	responseSig, err := id.Sign(c.Request().Context(), canonicalResponse)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	body["signature"] = canonical.Base64URL(responseSig)
	return c.JSON(http.StatusOK, body)
}

func (ctrl *GoNZBNetController) Inbox(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		code := federationBodyReadErrorCode(err)
		status := http.StatusBadRequest
		if code == "payload_too_large" {
			status = http.StatusRequestEntityTooLarge
		}
		return federationJSONError(c, status, code, "read inbox body")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	if _, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		body,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	); err != nil {
		return federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
	}

	eventsIn, err := decodeInboxEvents(body)
	if err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_json", err.Error())
	}
	maxBatch := cfg.MaxBatchEvents
	if maxBatch <= 0 {
		maxBatch = 100
	}
	if len(eventsIn) > maxBatch {
		return federationJSONError(c, http.StatusBadRequest, "invalid_schema", "event batch exceeds max_batch_events")
	}

	resp := inboxResponse{
		SchemaVersion: "1.0",
		Type:          "InboxResponse",
		Accepted:      []inboxEventResult{},
		Duplicate:     []inboxEventResult{},
		Rejected:      []inboxEventResult{},
	}
	for i := range eventsIn {
		event := eventsIn[i]
		item := ctrl.acceptInboxEvent(c.Request().Context(), store, &event)
		switch item.Status {
		case "accepted":
			resp.Accepted = append(resp.Accepted, item)
			resp.Cursor = item.EventID
		case "duplicate":
			resp.Duplicate = append(resp.Duplicate, item)
			resp.Cursor = item.EventID
		default:
			resp.Rejected = append(resp.Rejected, item)
		}
	}
	return c.JSON(http.StatusOK, resp)
}

func (ctrl *GoNZBNetController) RequestManifest(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		code := federationBodyReadErrorCode(err)
		status := http.StatusBadRequest
		if code == "payload_too_large" {
			status = http.StatusRequestEntityTooLarge
		}
		return federationJSONError(c, status, code, "read manifest request body")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	verified, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		body,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	)
	if err != nil {
		return federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
	}
	var req manifest.Request
	if err := decodeFederationJSON(body, &req); err != nil {
		return federationJSONError(c, http.StatusBadRequest, "invalid_json", "invalid manifest request json")
	}
	manifestID := pathParamTrimmed(c, "manifest_id")
	if req.ManifestID != manifestID {
		return c.JSON(http.StatusBadRequest, manifest.Response{
			SchemaVersion: "1.0",
			Type:          "ManifestResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "manifest_id_mismatch",
			Message:       "manifest_id does not match path",
		})
	}
	if req.RequestingNodeID != verified.NodeID {
		return c.JSON(http.StatusForbidden, manifest.Response{
			SchemaVersion: "1.0",
			Type:          "ManifestResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "requesting_node_mismatch",
			Message:       "requesting node does not match request signature",
		})
	}
	allowed, err := store.CanFetchResolutionManifest(c.Request().Context(), manifestID, verified.NodeID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if !allowed {
		return c.JSON(http.StatusForbidden, manifest.Response{
			SchemaVersion: "1.0",
			Type:          "ManifestResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "not_pool_member",
			Message:       "Requesting node is not authorized for this manifest",
		})
	}
	event, err := store.GetResolutionManifestEvent(c.Request().Context(), manifestID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if event == nil {
		return c.JSON(http.StatusNotFound, manifest.Response{
			SchemaVersion: "1.0",
			Type:          "ManifestResponse",
			RequestID:     req.RequestID,
			Status:        "error",
			Code:          "manifest_not_found",
			Message:       "manifest not found",
		})
	}
	return c.JSON(http.StatusOK, manifest.Response{
		SchemaVersion: "1.0",
		Type:          "ManifestResponse",
		RequestID:     req.RequestID,
		Status:        "ok",
		ManifestEvent: event,
	})
}

func (ctrl *GoNZBNetController) GetManifest(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	verified, err := requestauth.Verify(
		c.Request().Context(),
		store,
		requestauth.HeaderFromRequest(c.Request()),
		c.Request().Method,
		c.Request().URL.Path,
		c.Request().URL.RawQuery,
		nil,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.NonceTTLSeconds)*time.Second,
	)
	if err != nil {
		return federationJSONError(c, http.StatusUnauthorized, federationAuthErrorCode(err), err.Error())
	}
	manifestID := pathParamTrimmed(c, "manifest_id")
	allowed, err := store.CanFetchResolutionManifest(c.Request().Context(), manifestID, verified.NodeID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if !allowed {
		return federationJSONError(c, http.StatusForbidden, "not_pool_member", "requesting node is not authorized for this manifest")
	}
	item, err := store.GetResolutionManifest(c.Request().Context(), manifestID)
	if err != nil {
		return federationJSONError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
	if item == nil {
		return federationJSONError(c, http.StatusNotFound, "manifest_not_found", "manifest not found")
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *GoNZBNetController) GossipWS(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return federationJSONError(c, http.StatusServiceUnavailable, "internal_error", "gonzbnet store is unavailable")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	if !cfg.WebSocketGossipEnabled {
		return federationJSONError(c, http.StatusNotFound, "invalid_schema", "websocket gossip is disabled")
	}
	handler := websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		req := ws.Request()
		if _, err := requestauth.Verify(
			req.Context(),
			store,
			requestauth.HeaderFromRequest(req),
			req.Method,
			req.URL.Path,
			req.URL.RawQuery,
			nil,
			time.Now(),
			time.Duration(cfg.TimeToleranceSeconds)*time.Second,
			time.Duration(cfg.NonceTTLSeconds)*time.Second,
		); err != nil {
			_ = websocket.JSON.Send(ws, gossip.Response{
				SchemaVersion: "1.0",
				Type:          gossip.ResponseType,
				Rejected:      []gossip.EventResult{{Status: "rejected", Code: federationAuthErrorCode(err), Message: err.Error()}},
			})
			return
		}
		for {
			var payload []byte
			if err := websocket.Message.Receive(ws, &payload); err != nil {
				if err != io.EOF {
					_ = websocket.JSON.Send(ws, gossip.Response{
						SchemaVersion: "1.0",
						Type:          gossip.ResponseType,
						Rejected:      []gossip.EventResult{{Status: "rejected", Code: "invalid_json", Message: err.Error()}},
					})
				}
				return
			}
			var batch gossip.Batch
			if err := decodeFederationJSON(payload, &batch); err != nil {
				_ = websocket.JSON.Send(ws, gossip.Response{
					SchemaVersion: "1.0",
					Type:          gossip.ResponseType,
					Rejected:      []gossip.EventResult{{Status: "rejected", Code: "invalid_json", Message: err.Error()}},
				})
				return
			}
			resp := ctrl.processGossipBatch(req.Context(), store, batch)
			_ = websocket.JSON.Send(ws, resp)
		}
	})
	handler.ServeHTTP(c.Response(), c.Request())
	return nil
}

func (ctrl *GoNZBNetController) processGossipBatch(ctx context.Context, store gonzbnetStore, batch gossip.Batch) gossip.Response {
	cfg := ctrl.appCtx.Config.GoNZBNet
	resp := gossip.Response{
		SchemaVersion: "1.0",
		Type:          gossip.ResponseType,
		TTL:           gossip.ForwardTTL(gossip.NormalizeTTL(batch.TTL, cfg.GossipTTL)),
		Accepted:      []gossip.EventResult{},
		Duplicate:     []gossip.EventResult{},
		Rejected:      []gossip.EventResult{},
	}
	if strings.TrimSpace(batch.Type) != gossip.Type {
		resp.Rejected = append(resp.Rejected, gossip.EventResult{Status: "rejected", Code: "invalid_schema", Message: "expected GossipBatch"})
		return resp
	}
	if strings.TrimSpace(batch.NetworkID) != "" && strings.TrimSpace(batch.NetworkID) != strings.TrimSpace(cfg.NetworkID) {
		resp.Rejected = append(resp.Rejected, gossip.EventResult{Status: "rejected", Code: "invalid_schema", Message: "network_id mismatch"})
		return resp
	}
	maxBatch := cfg.GossipBatchSize
	if maxBatch <= 0 {
		maxBatch = cfg.MaxBatchEvents
	}
	if maxBatch <= 0 {
		maxBatch = 100
	}
	if len(batch.Events) > maxBatch {
		resp.Rejected = append(resp.Rejected, gossip.EventResult{Status: "rejected", Code: "invalid_schema", Message: "gossip batch exceeds limit"})
		return resp
	}
	if cfg.PeerExchangeEnabled {
		for _, peer := range gossip.FilterPeers(batch.Peers, true, cfg.GossipFanout) {
			_, _ = store.UpsertFederationPeerURL(ctx, peer)
		}
		peers, err := store.ListEnabledFederationPeers(ctx)
		if err == nil {
			urls := make([]string, 0, len(peers))
			for _, peer := range peers {
				urls = append(urls, peer.PeerURL)
			}
			resp.Peers = gossip.FilterPeers(urls, true, cfg.GossipFanout)
		}
	}
	for _, eventValue := range batch.Events {
		event := eventValue
		result := ctrl.acceptInboxEvent(ctx, store, &event)
		gossipResult := gossip.EventResult{
			EventID: result.EventID,
			Status:  result.Status,
			Code:    result.Code,
			Message: result.Message,
		}
		switch result.Status {
		case "accepted":
			resp.Accepted = append(resp.Accepted, gossipResult)
		case "duplicate":
			resp.Duplicate = append(resp.Duplicate, gossipResult)
		default:
			resp.Rejected = append(resp.Rejected, gossipResult)
		}
	}
	return resp
}

func (ctrl *GoNZBNetController) acceptInboxEvent(ctx context.Context, store gonzbnetStore, event *events.SignedEvent) inboxEventResult {
	eventID := ""
	if event != nil {
		eventID = event.EventID
	}
	raw, _ := json.Marshal(event)
	if event == nil {
		return inboxEventResult{Status: "rejected", Code: "invalid_schema", Message: "event is required"}
	}
	if exists, err := store.FederationEventExists(ctx, event.EventID); err != nil {
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
	} else if exists {
		return inboxEventResult{EventID: event.EventID, Status: "duplicate"}
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	validation, err := events.VerifyWithin(
		event,
		time.Now(),
		time.Duration(cfg.TimeToleranceSeconds)*time.Second,
		time.Duration(cfg.MaxEventAgeHours)*time.Hour,
	)
	if err != nil || validation == nil || !validation.OK {
		reason := "verification failed"
		if validation != nil && validation.Reason != "" {
			reason = validation.Reason
		}
		if err != nil {
			reason = err.Error()
		}
		_ = store.AppendRejectedFederationEvent(ctx, eventID, event.AuthorNodeID, event.EventType, raw, reason)
		return inboxEventResult{EventID: eventID, Status: "rejected", Code: federationVerificationCode(reason), Message: reason}
	}
	if !pools.EventTypeSupported(event.EventType) {
		reason := "unsupported event_type"
		_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, reason)
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: federationVerificationCode(reason), Message: reason}
	}
	if err := eventbody.Validate(event, time.Now().UTC(), time.Duration(cfg.TimeToleranceSeconds)*time.Second); err != nil {
		reason := err.Error()
		_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, reason)
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: reason}
	}
	if pools.EventIsPoolControl(event.EventType) {
		if err := store.ValidateFederationPoolControlEvent(ctx, event); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, err.Error())
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: federationPoolErrorCode(err.Error()), Message: err.Error()}
		}
	} else {
		authorization, err := store.CanAcceptFederationEventForPools(ctx, event.AuthorNodeID, event.PoolIDs, event.EventType)
		if err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
		if !authorization.Allowed {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, authorization.Reason)
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: federationPoolErrorCode(authorization.Reason), Message: authorization.Reason}
		}
	}
	var releaseProjection *releasecard.Projection
	if event.EventType == pools.EventTypeReleaseCard {
		var card releasecard.ReleaseCard
		if err := json.Unmarshal(event.Body, &card); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid release card body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid release card body"}
		}
		poolID := ""
		if len(event.PoolIDs) > 0 {
			poolID = event.PoolIDs[0]
		}
		releaseProjection = &releasecard.Projection{
			Card:         card,
			EventID:      event.EventID,
			SourceNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		}
	}
	var healthProjection *pgindex.HealthAttestationProjection
	if event.EventType == pools.EventTypeHealthAttestation {
		var attestation health.Attestation
		if err := json.Unmarshal(event.Body, &attestation); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid health attestation body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid health attestation body"}
		}
		poolID := ""
		if len(event.PoolIDs) > 0 {
			poolID = event.PoolIDs[0]
		}
		healthProjection = &pgindex.HealthAttestationProjection{
			Attestation:  attestation,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		}
	}
	var trustProjection *pgindex.TrustAttestationProjection
	if event.EventType == pools.EventTypeTrustAttestation {
		var attestation trust.Attestation
		if err := json.Unmarshal(event.Body, &attestation); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid trust attestation body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid trust attestation body"}
		}
		if err := trust.Validate(attestation, time.Now().UTC()); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, err.Error())
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: err.Error()}
		}
		poolID := ""
		if len(event.PoolIDs) > 0 {
			poolID = event.PoolIDs[0]
		}
		trustProjection = &pgindex.TrustAttestationProjection{
			Attestation:  attestation,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		}
	}
	var validatorCapacityProjection *pgindex.ValidatorCapacityProjection
	if event.EventType == pools.EventTypeValidatorCapacity {
		var capacity gonzbnetvalidation.ValidatorCapacity
		if err := json.Unmarshal(event.Body, &capacity); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid validator capacity body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid validator capacity body"}
		}
		validatorCapacityProjection = &pgindex.ValidatorCapacityProjection{
			Capacity:     capacity,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
		}
	}
	var articleAvailabilityProjection *pgindex.ArticleAvailabilityProjection
	if event.EventType == pools.EventTypeArticleAvailabilityAttestation {
		var attestation gonzbnetvalidation.ArticleAvailabilityAttestation
		if err := json.Unmarshal(event.Body, &attestation); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid article availability body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid article availability body"}
		}
		poolID := ""
		if len(event.PoolIDs) > 0 {
			poolID = event.PoolIDs[0]
		}
		articleAvailabilityProjection = &pgindex.ArticleAvailabilityProjection{
			Attestation:  attestation,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		}
	}
	var checksumProjection *pgindex.ChecksumAttestationProjection
	if event.EventType == pools.EventTypeChecksumAttestation {
		var attestation gonzbnetvalidation.ChecksumAttestation
		if err := json.Unmarshal(event.Body, &attestation); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid checksum attestation body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid checksum attestation body"}
		}
		poolID := ""
		if len(event.PoolIDs) > 0 {
			poolID = event.PoolIDs[0]
		}
		checksumProjection = &pgindex.ChecksumAttestationProjection{
			Attestation:  attestation,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		}
	}
	var manifestAvailabilityProjection *pgindex.ManifestAvailabilityProjection
	if event.EventType == pools.EventTypeManifestAvailability {
		var attestation manifestavailability.Attestation
		if err := json.Unmarshal(event.Body, &attestation); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid manifest availability body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid manifest availability body"}
		}
		poolID := ""
		if len(event.PoolIDs) > 0 {
			poolID = event.PoolIDs[0]
		}
		manifestAvailabilityProjection = &pgindex.ManifestAvailabilityProjection{
			Attestation:  attestation,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		}
	}
	var tombstoneProjection *pgindex.TombstoneProjection
	if event.EventType == pools.EventTypeTombstone {
		var tombstone moderation.Tombstone
		if err := json.Unmarshal(event.Body, &tombstone); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid tombstone body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_schema", Message: "invalid tombstone body"}
		}
		tombstoneProjection = &pgindex.TombstoneProjection{
			Tombstone:    tombstone,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
		}
	}
	if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		if errors.Is(err, pgindex.ErrFederationSequenceConflict) {
			reason := "sequence_conflict"
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, reason)
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: federationVerificationCode(reason), Message: reason}
		}
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
	}
	if pools.EventIsPoolControl(event.EventType) {
		if err := store.ProjectFederationPoolEvent(ctx, event); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if releaseProjection != nil {
		if err := store.UpsertFederatedReleaseCardProjection(ctx, *releaseProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if healthProjection != nil {
		if err := store.ProjectHealthAttestation(ctx, *healthProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if trustProjection != nil {
		if err := store.ProjectTrustAttestation(ctx, *trustProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if validatorCapacityProjection != nil {
		if err := store.ProjectValidatorCapacity(ctx, *validatorCapacityProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if articleAvailabilityProjection != nil {
		if err := store.ProjectArticleAvailabilityAttestation(ctx, *articleAvailabilityProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if checksumProjection != nil {
		if err := store.ProjectChecksumAttestation(ctx, *checksumProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if manifestAvailabilityProjection != nil {
		if err := store.ProjectManifestAvailability(ctx, *manifestAvailabilityProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if isCoverageEvent(event.EventType) {
		if err := store.ProjectCoverageEvent(ctx, event); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	if tombstoneProjection != nil {
		if err := store.ProjectTombstone(ctx, *tombstoneProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "internal_error", Message: err.Error()}
		}
	}
	return inboxEventResult{EventID: event.EventID, Status: "accepted"}
}

func decodeInboxEvents(body []byte) ([]events.SignedEvent, error) {
	if err := canonical.ValidateJSON(body); err != nil {
		return nil, fmt.Errorf("invalid inbox json: %w", err)
	}
	var batch inboxEventBatch
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, fmt.Errorf("invalid inbox json")
	}
	if strings.TrimSpace(batch.Type) == "EventBatch" {
		if len(batch.Events) == 0 {
			return nil, fmt.Errorf("event batch is empty")
		}
		return batch.Events, nil
	}
	var single events.SignedEvent
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, fmt.Errorf("invalid inbox event")
	}
	if strings.TrimSpace(single.EventID) == "" {
		return nil, fmt.Errorf("missing event batch")
	}
	return []events.SignedEvent{single}, nil
}

func decodeFederationJSON(body []byte, out any) error {
	if err := canonical.ValidateJSON(body); err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func isCoverageEvent(eventType string) bool {
	for _, candidate := range coverage.EventTypes() {
		if eventType == candidate {
			return true
		}
	}
	return false
}

func (ctrl *GoNZBNetController) localIdentity() (*identity.Identity, error) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.Config == nil {
		return nil, fmt.Errorf("gonzbnet controller is not initialized")
	}
	if ctrl.identity != nil {
		return ctrl.identity, nil
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	id, err := identity.LoadOrCreateWithPassword(cfg.KeysDir, cfg.KeyPassword)
	if err != nil {
		return nil, err
	}
	ctrl.identity = id
	return id, nil
}

func (ctrl *GoNZBNetController) profileConfig(c *echo.Context) profile.Config {
	cfg := ctrl.appCtx.Config.GoNZBNet
	return profile.Config{
		Alias:                         cfg.NodeAlias,
		AdvertiseURL:                  ctrl.baseURL(c),
		HTTPBasePath:                  cfg.HTTPBasePath,
		PrivateNetwork:                cfg.PrivateNetwork,
		LiveQueryEnabled:              cfg.LiveQueryEnabled,
		WebSocketGossip:               cfg.WebSocketGossipEnabled,
		PeerExchange:                  cfg.PeerExchangeEnabled,
		RelayMode:                     cfg.RelayEnabled,
		Consumer:                      cfg.ConsumerEnabled,
		Scanner:                       cfg.ScannerEnabled,
		Indexer:                       ctrl.appCtx.Config.Modules.UsenetIndexer.Enabled,
		IndexProjection:               cfg.IndexProjectionEnabled,
		ManifestBuilder:               cfg.ManifestBuilderEnabled,
		ManifestCache:                 cfg.ManifestCacheEnabled,
		Validator:                     cfg.ValidatorEnabled,
		HealthChecker:                 cfg.HealthCheckerEnabled,
		Coverage:                      cfg.CoverageEnabled,
		Scheduler:                     cfg.SchedulerEnabled,
		ScannerMaxGroups:              cfg.ScannerMaxGroups,
		ScannerMaxArticlesPerHour:     cfg.ScannerMaxArticlesPerHour,
		ValidationMaxManifestsPerHour: cfg.ValidationMaxManifestsPerHour,
		ValidationTiers:               cfg.ValidationTiers,
		ValidationAllowSamplePayload:  cfg.ValidationAllowSamplePayload,
		ValidationAllowPAR2:           cfg.ValidationAllowPAR2,
		ProviderDisclosure:            cfg.CoverageProviderScopeMode,
		ProviderBackboneHash:          providerBackboneHashForAppContext(ctrl.appCtx),
		MaxEventBytes:                 cfg.MaxEventBytes,
		MaxManifestBytes:              cfg.MaxManifestBytes,
		MaxBatchEvents:                cfg.MaxBatchEvents,
		RateLimitEventsPerMin:         cfg.RateLimitEventsPerMinute,
	}
}

func (ctrl *GoNZBNetController) baseURL(c *echo.Context) string {
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

func providerBackboneHashForAppContext(appCtx *app.Context) string {
	if appCtx == nil || appCtx.Config == nil || !appCtx.Config.GoNZBNet.ShareProviderBackbone {
		return ""
	}
	parts := make([]string, 0, len(appCtx.Config.Servers))
	for _, server := range appCtx.Config.Servers {
		host := strings.TrimSpace(server.Host)
		if host == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%t", host, server.Port, server.TLS))
	}
	return profile.ProviderBackboneHash(parts)
}
