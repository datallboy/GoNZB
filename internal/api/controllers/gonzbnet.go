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
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
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
	UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error
	ValidateFederationPoolControlEvent(ctx context.Context, event *events.SignedEvent) error
	ProjectFederationPoolEvent(ctx context.Context, event *events.SignedEvent) error
	CanAcceptFederationEventForPools(ctx context.Context, authorNodeID string, poolIDs []string, eventType string) (pgindex.PoolAuthorizationResult, error)
	GetResolutionManifest(ctx context.Context, manifestID string) (*manifest.ResolutionManifest, error)
	GetResolutionManifestEvent(ctx context.Context, manifestID string) (*events.SignedEvent, error)
	CanFetchResolutionManifest(ctx context.Context, manifestID, nodeID string) (bool, error)
	ProjectHealthAttestation(ctx context.Context, projection pgindex.HealthAttestationProjection) error
	ProjectValidatorCapacity(ctx context.Context, projection pgindex.ValidatorCapacityProjection) error
	ProjectArticleAvailabilityAttestation(ctx context.Context, projection pgindex.ArticleAvailabilityProjection) error
	ProjectChecksumAttestation(ctx context.Context, projection pgindex.ChecksumAttestationProjection) error
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

func (ctrl *GoNZBNetController) RequestManifest(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, "read manifest request body")
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
		return jsonError(c, http.StatusUnauthorized, err.Error())
	}
	var req manifest.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, "invalid manifest request json")
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
		return jsonError(c, http.StatusInternalServerError, err.Error())
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
		return jsonError(c, http.StatusInternalServerError, err.Error())
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
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
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
		return jsonError(c, http.StatusUnauthorized, err.Error())
	}
	manifestID := pathParamTrimmed(c, "manifest_id")
	allowed, err := store.CanFetchResolutionManifest(c.Request().Context(), manifestID, verified.NodeID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if !allowed {
		return jsonError(c, http.StatusForbidden, "not_pool_member")
	}
	item, err := store.GetResolutionManifest(c.Request().Context(), manifestID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if item == nil {
		return jsonError(c, http.StatusNotFound, "manifest not found")
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *GoNZBNetController) GossipWS(c *echo.Context) error {
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetStore)
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet store is unavailable")
	}
	cfg := ctrl.appCtx.Config.GoNZBNet
	if !cfg.WebSocketGossipEnabled {
		return jsonError(c, http.StatusNotFound, "websocket gossip is disabled")
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
				Rejected:      []gossip.EventResult{{Status: "rejected", Code: "unauthorized", Message: err.Error()}},
			})
			return
		}
		for {
			var batch gossip.Batch
			if err := websocket.JSON.Receive(ws, &batch); err != nil {
				if err != io.EOF {
					_ = websocket.JSON.Send(ws, gossip.Response{
						SchemaVersion: "1.0",
						Type:          gossip.ResponseType,
						Rejected:      []gossip.EventResult{{Status: "rejected", Code: "invalid_batch", Message: err.Error()}},
					})
				}
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
		resp.Rejected = append(resp.Rejected, gossip.EventResult{Status: "rejected", Code: "invalid_type", Message: "expected GossipBatch"})
		return resp
	}
	if strings.TrimSpace(batch.NetworkID) != "" && strings.TrimSpace(batch.NetworkID) != strings.TrimSpace(cfg.NetworkID) {
		resp.Rejected = append(resp.Rejected, gossip.EventResult{Status: "rejected", Code: "wrong_network", Message: "network_id mismatch"})
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
		resp.Rejected = append(resp.Rejected, gossip.EventResult{Status: "rejected", Code: "batch_too_large", Message: "gossip batch exceeds limit"})
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
	if pools.EventIsPoolControl(event.EventType) {
		if err := store.ValidateFederationPoolControlEvent(ctx, event); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, err.Error())
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "pool_validation_failed", Message: err.Error()}
		}
	} else {
		authorization, err := store.CanAcceptFederationEventForPools(ctx, event.AuthorNodeID, event.PoolIDs, event.EventType)
		if err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "pool_authorization_error", Message: err.Error()}
		}
		if !authorization.Allowed {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, authorization.Reason)
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: authorization.Reason, Message: authorization.Reason}
		}
	}
	var releaseProjection *releasecard.Projection
	if event.EventType == pools.EventTypeReleaseCard {
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
	var healthProjection *pgindex.HealthAttestationProjection
	if event.EventType == pools.EventTypeHealthAttestation {
		var attestation health.Attestation
		if err := json.Unmarshal(event.Body, &attestation); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid health attestation body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid health attestation body"}
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
	var validatorCapacityProjection *pgindex.ValidatorCapacityProjection
	if event.EventType == pools.EventTypeValidatorCapacity {
		var capacity gonzbnetvalidation.ValidatorCapacity
		if err := json.Unmarshal(event.Body, &capacity); err != nil {
			_ = store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid validator capacity body")
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid validator capacity body"}
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
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid article availability body"}
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
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid checksum attestation body"}
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
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid manifest availability body"}
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
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "invalid_body", Message: "invalid tombstone body"}
		}
		tombstoneProjection = &pgindex.TombstoneProjection{
			Tombstone:    tombstone,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
		}
	}
	if err := store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
		return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "store_error", Message: err.Error()}
	}
	if pools.EventIsPoolControl(event.EventType) {
		if err := store.ProjectFederationPoolEvent(ctx, event); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if releaseProjection != nil {
		if err := store.UpsertFederatedReleaseCardProjection(ctx, *releaseProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if healthProjection != nil {
		if err := store.ProjectHealthAttestation(ctx, *healthProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if validatorCapacityProjection != nil {
		if err := store.ProjectValidatorCapacity(ctx, *validatorCapacityProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if articleAvailabilityProjection != nil {
		if err := store.ProjectArticleAvailabilityAttestation(ctx, *articleAvailabilityProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if checksumProjection != nil {
		if err := store.ProjectChecksumAttestation(ctx, *checksumProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if manifestAvailabilityProjection != nil {
		if err := store.ProjectManifestAvailability(ctx, *manifestAvailabilityProjection); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if isCoverageEvent(event.EventType) {
		if err := store.ProjectCoverageEvent(ctx, event); err != nil {
			return inboxEventResult{EventID: event.EventID, Status: "rejected", Code: "projection_failed", Message: err.Error()}
		}
	}
	if tombstoneProjection != nil {
		if err := store.ProjectTombstone(ctx, *tombstoneProjection); err != nil {
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
