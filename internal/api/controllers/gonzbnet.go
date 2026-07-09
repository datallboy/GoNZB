package controllers

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
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
	UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error
}

type outboxResponse struct {
	SchemaVersion string                `json:"schema_version"`
	Type          string                `json:"type"`
	Events        []*events.SignedEvent `json:"events"`
	NextCursor    string                `json:"next_cursor"`
	HasMore       bool                  `json:"has_more"`
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

func NewGoNZBNetController(appCtx *app.Context) *GoNZBNetController {
	return &GoNZBNetController{appCtx: appCtx}
}

func (ctrl *GoNZBNetController) WellKnown(c *echo.Context) error {
	id, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	resp, err := profile.WellKnownFor(c.Request().Context(), id, ctrl.baseURL(c))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, resp)
}

func (ctrl *GoNZBNetController) Node(c *echo.Context) error {
	id, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	resp, err := profile.NodeProfileFor(c.Request().Context(), id, ctrl.profileConfig(c), time.Now())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
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
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	page, err := store.ListFederationOutboxEvents(c.Request().Context(), pgindex.FederationOutboxParams{
		Since:     queryParamTrimmed(c, "since"),
		PoolID:    queryParamTrimmed(c, "pool"),
		EventType: queryParamTrimmed(c, "type"),
		Limit:     parseIntDefault(queryParamTrimmed(c, "limit"), 100),
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
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
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	event, err := store.GetFederationEvent(c.Request().Context(), pathParamTrimmed(c, "event_id"))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if event == nil {
		return jsonError(c, http.StatusNotFound, "event not found")
	}
	return c.JSON(http.StatusOK, event)
}

func (ctrl *GoNZBNetController) Handshake(c *echo.Context) error {
	id, err := ctrl.localIdentity()
	if err != nil {
		return jsonError(c, http.StatusServiceUnavailable, err.Error())
	}
	var req map[string]any
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return jsonError(c, http.StatusBadRequest, "invalid handshake json")
	}
	signatureValue, _ := req["signature"].(string)
	delete(req, "signature")
	if strings.TrimSpace(signatureValue) == "" {
		return jsonError(c, http.StatusBadRequest, "missing signature")
	}
	nodeID, _ := req["node_id"].(string)
	publicKeyValue, _ := req["public_key"].(string)
	nonce, _ := req["nonce"].(string)
	publicKey, err := canonical.DecodeBase64URL(publicKeyValue)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return jsonError(c, http.StatusBadRequest, "invalid public key")
	}
	if identity.NodeIDFromPublicKey(ed25519.PublicKey(publicKey)) != nodeID {
		return jsonError(c, http.StatusBadRequest, "node_id does not match public key")
	}
	signature, err := canonical.DecodeBase64URL(signatureValue)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, "invalid signature")
	}
	canonicalRequest, err := canonical.Marshal(req)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, "invalid canonical handshake")
	}
	if !identity.Verify(ed25519.PublicKey(publicKey), canonicalRequest, signature) {
		return jsonError(c, http.StatusUnauthorized, "handshake signature verification failed")
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
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	responseSig, err := id.Sign(c.Request().Context(), canonicalResponse)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	body["signature"] = canonical.Base64URL(responseSig)
	return c.JSON(http.StatusOK, body)
}

func (ctrl *GoNZBNetController) Inbox(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, "read inbox body")
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
		return jsonError(c, http.StatusUnauthorized, err.Error())
	}

	eventsIn, err := decodeInboxEvents(body)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	maxBatch := cfg.MaxBatchEvents
	if maxBatch <= 0 {
		maxBatch = 100
	}
	if len(eventsIn) > maxBatch {
		return jsonError(c, http.StatusBadRequest, "event batch exceeds max_batch_events")
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

func (ctrl *GoNZBNetController) acceptInboxEvent(ctx context.Context, store gonzbnetStore, event *events.SignedEvent) inboxEventResult {
	eventID := ""
	if event != nil {
		eventID = event.EventID
	}
	raw, _ := json.Marshal(event)
	if event == nil {
		return inboxEventResult{Status: "rejected", Code: "invalid_event", Message: "event is required"}
	}
	if exists, err := store.FederationEventExists(ctx, event.EventID); err != nil {
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "store_error", Message: err.Error()}
	} else if exists {
		return inboxEventResult{EventID: event.EventID, Status: "duplicate"}
	}
	validation, err := events.Verify(event)
	if err != nil || validation == nil || !validation.OK {
		reason := "verification failed"
		if validation != nil && validation.Reason != "" {
			reason = validation.Reason
		}
		if err != nil {
			reason = err.Error()
		}
		_ = store.AppendRejectedFederationEvent(ctx, eventID, event.AuthorNodeID, event.EventType, raw, reason)
		return inboxEventResult{EventID: eventID, Status: "rejected", Code: "verification_failed", Message: reason}
	}
	var releaseProjection *releasecard.Projection
	if event.EventType == "ReleaseCard" {
		var card releasecard.ReleaseCard
		if err := json.Unmarshal(event.Body, &card); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid release card body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid release card body"}
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
	if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "store_error", Message: err.Error()}
	}
	if releaseProjection != nil {
		if err := store.UpsertFederatedReleaseCardProjection(ctx, *releaseProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	return inboxEventResult{EventID: event.EventID, Status: "accepted"}
}

func decodeInboxEvents(body []byte) ([]events.SignedEvent, error) {
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

func (ctrl *GoNZBNetController) localIdentity() (*identity.Identity, error) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.Config == nil {
		return nil, fmt.Errorf("gonzbnet controller is not initialized")
	}
	if ctrl.identity != nil {
		return ctrl.identity, nil
	}
	id, err := identity.LoadOrCreate(ctrl.appCtx.Config.GoNZBNet.KeysDir)
	if err != nil {
		return nil, err
	}
	ctrl.identity = id
	return id, nil
}

func (ctrl *GoNZBNetController) profileConfig(c *echo.Context) profile.Config {
	cfg := ctrl.appCtx.Config.GoNZBNet
	return profile.Config{
		Alias:            cfg.NodeAlias,
		AdvertiseURL:     ctrl.baseURL(c),
		HTTPBasePath:     cfg.HTTPBasePath,
		PrivateNetwork:   cfg.PrivateNetwork,
		LiveQueryEnabled: cfg.LiveQueryEnabled,
		MaxEventBytes:    cfg.MaxEventBytes,
		MaxManifestBytes: cfg.MaxManifestBytes,
		MaxBatchEvents:   cfg.MaxBatchEvents,
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
