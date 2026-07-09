package sync

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/gossip"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifestavailability"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"golang.org/x/net/websocket"
)

type Identity interface {
	events.Identity
	PublicKey(context.Context) (ed25519.PublicKey, error)
}

type Store interface {
	UpsertFederationPeerURL(ctx context.Context, peerURL string) (int64, error)
	ListEnabledFederationPeers(ctx context.Context) ([]pgindex.FederationPeerRecord, error)
	UpsertFederationNode(ctx context.Context, node pgindex.FederationNodeRecord) error
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	AppendRejectedFederationEvent(ctx context.Context, eventID, authorNodeID, eventType string, rawEventJSON []byte, reason string) error
	UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error
	ProjectValidatorCapacity(ctx context.Context, projection pgindex.ValidatorCapacityProjection) error
	ProjectArticleAvailabilityAttestation(ctx context.Context, projection pgindex.ArticleAvailabilityProjection) error
	ProjectChecksumAttestation(ctx context.Context, projection pgindex.ChecksumAttestationProjection) error
	ProjectManifestAvailability(ctx context.Context, projection pgindex.ManifestAvailabilityProjection) error
	ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error
	MarkFederationPeerSyncSuccess(ctx context.Context, peerID int64, nodeID, cursor, lastEventID string) error
	MarkFederationPeerSyncFailure(ctx context.Context, peerID int64, errText string) error
	ListUndeliveredFederationEvents(ctx context.Context, peerID int64, limit int) ([]*events.SignedEvent, error)
	RecordFederationPeerDelivery(ctx context.Context, result pgindex.FederationDeliveryResult) error
	ValidateFederationPoolControlEvent(ctx context.Context, event *events.SignedEvent) error
	ProjectFederationPoolEvent(ctx context.Context, event *events.SignedEvent) error
	CanAcceptFederationEventForPools(ctx context.Context, authorNodeID string, poolIDs []string, eventType string) (pgindex.PoolAuthorizationResult, error)
}

type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

type Service struct {
	identity              Identity
	store                 Store
	client                *http.Client
	logger                Logger
	allowInsecurePeerHTTP bool
	eventTimeTolerance    time.Duration
	maxEventAge           time.Duration
}

type Result struct {
	Peers     int `json:"peers"`
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
	Rejected  int `json:"rejected"`
	Projected int `json:"projected"`
}

type OutboxPage struct {
	SchemaVersion string               `json:"schema_version"`
	Type          string               `json:"type"`
	Events        []events.SignedEvent `json:"events"`
	NextCursor    string               `json:"next_cursor"`
	HasMore       bool                 `json:"has_more"`
}

type EventBatch struct {
	SchemaVersion string                `json:"schema_version"`
	Type          string                `json:"type"`
	Events        []*events.SignedEvent `json:"events"`
}

type InboxEventResult struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type InboxResponse struct {
	SchemaVersion string             `json:"schema_version"`
	Type          string             `json:"type"`
	Accepted      []InboxEventResult `json:"accepted"`
	Duplicate     []InboxEventResult `json:"duplicate"`
	Rejected      []InboxEventResult `json:"rejected"`
	Cursor        string             `json:"cursor,omitempty"`
}

type GossipOptions struct {
	NetworkID           string
	TTL                 int
	BatchSize           int
	Fanout              int
	PeerExchangeEnabled bool
}

type Options struct {
	AllowInsecurePeerHTTP bool
	EventTimeTolerance    time.Duration
	MaxEventAge           time.Duration
}

func New(identity Identity, store Store, logger Logger) *Service {
	return NewWithOptions(identity, store, logger, Options{})
}

func NewWithOptions(identity Identity, store Store, logger Logger, opts Options) *Service {
	eventTimeTolerance := opts.EventTimeTolerance
	if eventTimeTolerance <= 0 {
		eventTimeTolerance = 2 * time.Minute
	}
	return &Service{
		identity:              identity,
		store:                 store,
		logger:                logger,
		allowInsecurePeerHTTP: opts.AllowInsecurePeerHTTP,
		eventTimeTolerance:    eventTimeTolerance,
		maxEventAge:           opts.MaxEventAge,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Service) UpsertManualPeers(ctx context.Context, peerURLs []string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("sync store is required")
	}
	for _, peerURL := range peerURLs {
		peerURL = strings.TrimSpace(peerURL)
		if peerURL == "" {
			continue
		}
		if err := transportpolicy.ValidateHTTPURL(peerURL, s.allowInsecurePeerHTTP); err != nil {
			return err
		}
		if _, err := s.store.UpsertFederationPeerURL(ctx, peerURL); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SyncOnce(ctx context.Context) (Result, error) {
	var result Result
	if s == nil || s.identity == nil || s.store == nil {
		return result, fmt.Errorf("sync dependencies are required")
	}
	peers, err := s.store.ListEnabledFederationPeers(ctx)
	if err != nil {
		return result, err
	}
	result.Peers = len(peers)
	for _, peer := range peers {
		peerResult, err := s.syncPeer(ctx, peer)
		if err != nil {
			_ = s.store.MarkFederationPeerSyncFailure(ctx, peer.ID, err.Error())
			if s.logger != nil {
				s.logger.Warn("gonzbnet peer sync failed peer=%s: %v", peer.PeerURL, err)
			}
			continue
		}
		result.Accepted += peerResult.Accepted
		result.Duplicate += peerResult.Duplicate
		result.Rejected += peerResult.Rejected
		result.Projected += peerResult.Projected
	}
	return result, nil
}

func (s *Service) PushOnce(ctx context.Context, limit int) (Result, error) {
	var result Result
	if s == nil || s.identity == nil || s.store == nil {
		return result, fmt.Errorf("sync dependencies are required")
	}
	peers, err := s.store.ListEnabledFederationPeers(ctx)
	if err != nil {
		return result, err
	}
	result.Peers = len(peers)
	for _, peer := range peers {
		peerResult, err := s.pushPeer(ctx, peer, limit)
		if err != nil {
			_ = s.store.MarkFederationPeerSyncFailure(ctx, peer.ID, err.Error())
			if s.logger != nil {
				s.logger.Warn("gonzbnet peer push failed peer=%s: %v", peer.PeerURL, err)
			}
			continue
		}
		result.Accepted += peerResult.Accepted
		result.Duplicate += peerResult.Duplicate
		result.Rejected += peerResult.Rejected
	}
	return result, nil
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		_, err := s.SyncOnce(ctx)
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.SyncOnce(ctx); err != nil && s.logger != nil {
			s.logger.Warn("gonzbnet pull sync pass failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunPush(ctx context.Context, interval time.Duration, limit int) error {
	if interval <= 0 {
		_, err := s.PushOnce(ctx, limit)
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.PushOnce(ctx, limit); err != nil && s.logger != nil {
			s.logger.Warn("gonzbnet push sync pass failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunGossip(ctx context.Context, interval time.Duration, opts GossipOptions) error {
	if interval <= 0 {
		_, err := s.GossipOnce(ctx, opts)
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.GossipOnce(ctx, opts); err != nil && s.logger != nil {
			s.logger.Warn("gonzbnet websocket gossip pass failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) GossipOnce(ctx context.Context, opts GossipOptions) (Result, error) {
	var result Result
	if s == nil || s.identity == nil || s.store == nil {
		return result, fmt.Errorf("sync dependencies are required")
	}
	peers, err := s.store.ListEnabledFederationPeers(ctx)
	if err != nil {
		return result, err
	}
	if opts.Fanout <= 0 || opts.Fanout > len(peers) {
		opts.Fanout = len(peers)
	}
	attempted := 0
	for i := 0; i < len(peers) && attempted < opts.Fanout; i++ {
		if !peerBackoffReady(peers[i], time.Now()) {
			continue
		}
		attempted++
		peerResult, err := s.gossipPeer(ctx, peers[i], opts)
		if err != nil {
			_ = s.store.MarkFederationPeerSyncFailure(ctx, peers[i].ID, err.Error())
			if s.logger != nil {
				s.logger.Warn("gonzbnet websocket gossip failed peer=%s: %v", peers[i].PeerURL, err)
			}
			continue
		}
		result.Accepted += peerResult.Accepted
		result.Duplicate += peerResult.Duplicate
		result.Rejected += peerResult.Rejected
	}
	result.Peers = attempted
	return result, nil
}

func peerBackoffReady(peer pgindex.FederationPeerRecord, now time.Time) bool {
	if strings.TrimSpace(peer.Status) != "error" || peer.FailureCount <= 0 || peer.UpdatedAt.IsZero() {
		return true
	}
	delay := time.Duration(peer.FailureCount) * time.Minute
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}
	return now.Sub(peer.UpdatedAt) >= delay
}

func (s *Service) syncPeer(ctx context.Context, peer pgindex.FederationPeerRecord) (Result, error) {
	var result Result
	wellKnown, err := s.fetchWellKnown(ctx, peer.PeerURL)
	if err != nil {
		return result, err
	}
	nodeProfile, err := s.fetchNodeProfile(ctx, wellKnown.BaseURL)
	if err != nil {
		return result, err
	}
	if err := s.validatePeerIdentity(wellKnown, nodeProfile); err != nil {
		return result, err
	}
	publicKey, err := canonical.DecodeBase64URL(nodeProfile.PublicKey)
	if err != nil {
		return result, fmt.Errorf("decode peer public key: %w", err)
	}
	profileJSON, _ := json.Marshal(nodeProfile)
	capabilitiesJSON, _ := json.Marshal(nodeProfile.Capabilities)
	if err := s.store.UpsertFederationNode(ctx, pgindex.FederationNodeRecord{
		NodeID:          nodeProfile.NodeID,
		PublicKey:       ed25519.PublicKey(publicKey),
		Alias:           nodeProfile.Alias,
		Software:        nodeProfile.Software,
		SoftwareVersion: nodeProfile.SoftwareVersion,
		BaseURL:         wellKnown.BaseURL,
		Capabilities:    capabilitiesJSON,
		ProfileJSON:     profileJSON,
		Status:          "connected",
	}); err != nil {
		return result, err
	}
	if _, err := s.fetchCaps(ctx, wellKnown.BaseURL); err != nil {
		return result, err
	}
	if err := s.handshake(ctx, wellKnown.BaseURL); err != nil && s.logger != nil {
		s.logger.Warn("gonzbnet peer handshake failed peer=%s: %v", peer.PeerURL, err)
	}

	cursor := peer.Cursor
	lastEventID := peer.LastEventID
	for {
		page, err := s.fetchOutbox(ctx, wellKnown.BaseURL, cursor)
		if err != nil {
			return result, err
		}
		for _, eventValue := range page.Events {
			event := eventValue
			raw, _ := json.Marshal(event)
			validation, err := events.VerifyWithin(&event, time.Now(), s.eventTimeTolerance, s.maxEventAge)
			if err != nil || validation == nil || !validation.OK {
				reason := "verification failed"
				if validation != nil && validation.Reason != "" {
					reason = validation.Reason
				}
				if err != nil {
					reason = err.Error()
				}
				_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, reason)
				result.Rejected++
				continue
			}
			if !pools.EventTypeSupported(event.EventType) {
				_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "unsupported event_type")
				result.Rejected++
				continue
			}
			if pools.EventIsPoolControl(event.EventType) {
				if err := s.store.ValidateFederationPoolControlEvent(ctx, &event); err != nil {
					_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, err.Error())
					result.Rejected++
					continue
				}
			} else {
				authorization, err := s.store.CanAcceptFederationEventForPools(ctx, event.AuthorNodeID, event.PoolIDs, event.EventType)
				if err != nil {
					return result, err
				}
				if !authorization.Allowed {
					_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, authorization.Reason)
					result.Rejected++
					continue
				}
			}
			if err := s.store.AppendVerifiedFederationEvent(ctx, &event, validation); err != nil {
				return result, err
			}
			result.Accepted++
			lastEventID = event.EventID
			if pools.EventIsPoolControl(event.EventType) {
				if err := s.store.ProjectFederationPoolEvent(ctx, &event); err != nil {
					return result, err
				}
			}
			if event.EventType == "ReleaseCard" {
				var card releasecard.ReleaseCard
				if err := json.Unmarshal(event.Body, &card); err != nil {
					_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid release card body")
					result.Rejected++
					continue
				}
				poolID := ""
				if len(event.PoolIDs) > 0 {
					poolID = event.PoolIDs[0]
				}
				if err := s.store.UpsertFederatedReleaseCardProjection(ctx, releasecard.Projection{
					Card:         card,
					EventID:      event.EventID,
					SourceNodeID: event.AuthorNodeID,
					PoolID:       poolID,
				}); err != nil {
					return result, err
				}
				result.Projected++
			}
			if err := s.projectValidationEvent(ctx, &event, raw); err != nil {
				return result, err
			}
			if isSyncCoverageEvent(event.EventType) {
				if err := s.store.ProjectCoverageEvent(ctx, &event); err != nil {
					return result, err
				}
				result.Projected++
			}
		}
		if page.NextCursor != "" {
			cursor = page.NextCursor
		}
		if !page.HasMore {
			break
		}
	}
	if err := s.store.MarkFederationPeerSyncSuccess(ctx, peer.ID, nodeProfile.NodeID, cursor, lastEventID); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) pushPeer(ctx context.Context, peer pgindex.FederationPeerRecord, limit int) (Result, error) {
	var result Result
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	wellKnown, err := s.fetchWellKnown(ctx, peer.PeerURL)
	if err != nil {
		return result, err
	}
	nodeProfile, err := s.fetchNodeProfile(ctx, wellKnown.BaseURL)
	if err != nil {
		return result, err
	}
	if err := s.validatePeerIdentity(wellKnown, nodeProfile); err != nil {
		return result, err
	}
	publicKey, err := canonical.DecodeBase64URL(nodeProfile.PublicKey)
	if err != nil {
		return result, fmt.Errorf("decode peer public key: %w", err)
	}
	profileJSON, _ := json.Marshal(nodeProfile)
	capabilitiesJSON, _ := json.Marshal(nodeProfile.Capabilities)
	if err := s.store.UpsertFederationNode(ctx, pgindex.FederationNodeRecord{
		NodeID:          nodeProfile.NodeID,
		PublicKey:       ed25519.PublicKey(publicKey),
		Alias:           nodeProfile.Alias,
		Software:        nodeProfile.Software,
		SoftwareVersion: nodeProfile.SoftwareVersion,
		BaseURL:         wellKnown.BaseURL,
		Capabilities:    capabilitiesJSON,
		ProfileJSON:     profileJSON,
		Status:          "connected",
	}); err != nil {
		return result, err
	}
	items, err := s.store.ListUndeliveredFederationEvents(ctx, peer.ID, limit)
	if err != nil {
		return result, err
	}
	if len(items) == 0 {
		return result, nil
	}

	response, err := s.pushEvents(ctx, wellKnown.BaseURL, items)
	if err != nil {
		for _, event := range items {
			_ = s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{
				PeerID:  peer.ID,
				EventID: event.EventID,
				Status:  "error",
				Error:   err.Error(),
			})
		}
		return result, err
	}
	for _, item := range response.Accepted {
		result.Accepted++
		if err := s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{
			PeerID:  peer.ID,
			EventID: item.EventID,
			Status:  "accepted",
		}); err != nil {
			return result, err
		}
	}
	for _, item := range response.Duplicate {
		result.Duplicate++
		if err := s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{
			PeerID:  peer.ID,
			EventID: item.EventID,
			Status:  "duplicate",
		}); err != nil {
			return result, err
		}
	}
	for _, item := range response.Rejected {
		result.Rejected++
		if err := s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{
			PeerID:  peer.ID,
			EventID: item.EventID,
			Status:  "rejected",
			Error:   firstNonBlank(item.Message, item.Code),
		}); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Service) gossipPeer(ctx context.Context, peer pgindex.FederationPeerRecord, opts GossipOptions) (Result, error) {
	var result Result
	limit := opts.BatchSize
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	wellKnown, err := s.fetchWellKnown(ctx, peer.PeerURL)
	if err != nil {
		return result, err
	}
	nodeProfile, err := s.fetchNodeProfile(ctx, wellKnown.BaseURL)
	if err != nil {
		return result, err
	}
	if err := s.validatePeerIdentity(wellKnown, nodeProfile); err != nil {
		return result, err
	}
	publicKey, err := canonical.DecodeBase64URL(nodeProfile.PublicKey)
	if err != nil {
		return result, fmt.Errorf("decode peer public key: %w", err)
	}
	profileJSON, _ := json.Marshal(nodeProfile)
	capabilitiesJSON, _ := json.Marshal(nodeProfile.Capabilities)
	if err := s.store.UpsertFederationNode(ctx, pgindex.FederationNodeRecord{
		NodeID:          nodeProfile.NodeID,
		PublicKey:       ed25519.PublicKey(publicKey),
		Alias:           nodeProfile.Alias,
		Software:        nodeProfile.Software,
		SoftwareVersion: nodeProfile.SoftwareVersion,
		BaseURL:         wellKnown.BaseURL,
		Capabilities:    capabilitiesJSON,
		ProfileJSON:     profileJSON,
		Status:          "connected",
	}); err != nil {
		return result, err
	}
	if !nodeProfile.Capabilities.WebSocketGossip {
		return result, fmt.Errorf("peer does not advertise websocket gossip")
	}
	items, err := s.store.ListUndeliveredFederationEvents(ctx, peer.ID, limit)
	if err != nil {
		return result, err
	}
	if len(items) == 0 {
		return result, nil
	}
	batchEvents := make([]events.SignedEvent, 0, len(items))
	for _, item := range items {
		if item != nil {
			batchEvents = append(batchEvents, *item)
		}
	}
	wsURL := websocketURL(nodeProfile, wellKnown.BaseURL)
	if err := transportpolicy.ValidateWebSocketURL(wsURL, s.allowInsecurePeerHTTP); err != nil {
		return result, err
	}
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return result, err
	}
	authorization, err := requestauth.Sign(ctx, s.identity, http.MethodGet, parsed.Path, parsed.RawQuery, nil, time.Now())
	if err != nil {
		return result, err
	}
	cfg, err := websocket.NewConfig(wsURL, wellKnown.BaseURL)
	if err != nil {
		return result, err
	}
	cfg.Header.Set("Authorization", authorization)
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		return result, err
	}
	defer ws.Close()
	peers := []string{}
	if opts.PeerExchangeEnabled {
		knownPeers, err := s.store.ListEnabledFederationPeers(ctx)
		if err == nil {
			for _, known := range knownPeers {
				peers = append(peers, known.PeerURL)
			}
		}
	}
	if err := websocket.JSON.Send(ws, gossip.Batch{
		SchemaVersion: "1.0",
		Type:          gossip.Type,
		NetworkID:     opts.NetworkID,
		TTL:           gossip.NormalizeTTL(opts.TTL, opts.TTL),
		Events:        batchEvents,
		WantMissing:   false,
		Peers:         gossip.FilterPeers(peers, opts.PeerExchangeEnabled, opts.Fanout),
	}); err != nil {
		return result, err
	}
	var response gossip.Response
	if err := websocket.JSON.Receive(ws, &response); err != nil {
		return result, err
	}
	for _, item := range response.Accepted {
		result.Accepted++
		_ = s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{PeerID: peer.ID, EventID: item.EventID, Status: "accepted"})
	}
	for _, item := range response.Duplicate {
		result.Duplicate++
		_ = s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{PeerID: peer.ID, EventID: item.EventID, Status: "duplicate"})
	}
	for _, item := range response.Rejected {
		result.Rejected++
		_ = s.store.RecordFederationPeerDelivery(ctx, pgindex.FederationDeliveryResult{PeerID: peer.ID, EventID: item.EventID, Status: "rejected", Error: firstNonBlank(item.Message, item.Code)})
	}
	if opts.PeerExchangeEnabled {
		for _, peerURL := range gossip.FilterPeers(response.Peers, true, opts.Fanout) {
			if err := transportpolicy.ValidateHTTPURL(peerURL, s.allowInsecurePeerHTTP); err != nil {
				continue
			}
			_, _ = s.store.UpsertFederationPeerURL(ctx, peerURL)
		}
	}
	return result, nil
}

func websocketURL(nodeProfile profile.NodeProfile, baseURL string) string {
	endpoint := strings.TrimSpace(nodeProfile.Endpoints.WS)
	if endpoint == "" {
		endpoint = strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/ws"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}
	return parsed.String()
}

func (s *Service) pushEvents(ctx context.Context, baseURL string, items []*events.SignedEvent) (InboxResponse, error) {
	var out InboxResponse
	payload, err := json.Marshal(EventBatch{
		SchemaVersion: "1.0",
		Type:          "EventBatch",
		Events:        items,
	})
	if err != nil {
		return out, err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/inbox"
	if err := transportpolicy.ValidateHTTPURL(endpoint, s.allowInsecurePeerHTTP); err != nil {
		return out, err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return out, err
	}
	authorization, err := requestauth.Sign(ctx, s.identity, http.MethodPost, u.Path, u.RawQuery, payload, time.Now())
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/gonzbnet+json")
	req.Header.Set("Authorization", authorization)
	resp, err := s.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return out, fmt.Errorf("inbox status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decode inbox response: %w", err)
	}
	return out, nil
}

func (s *Service) fetchWellKnown(ctx context.Context, peerURL string) (profile.WellKnown, error) {
	var out profile.WellKnown
	endpoint, err := wellKnownURL(peerURL)
	if err != nil {
		return out, err
	}
	if err := transportpolicy.ValidateHTTPURL(endpoint, s.allowInsecurePeerHTTP); err != nil {
		return out, err
	}
	if err := s.getJSON(ctx, endpoint, &out); err != nil {
		return out, err
	}
	out.BaseURL = strings.TrimRight(out.BaseURL, "/")
	return out, nil
}

func (s *Service) fetchNodeProfile(ctx context.Context, baseURL string) (profile.NodeProfile, error) {
	var out profile.NodeProfile
	if err := s.getJSON(ctx, strings.TrimRight(baseURL, "/")+"/node", &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) fetchCaps(ctx context.Context, baseURL string) (profile.Caps, error) {
	var out profile.Caps
	if err := s.getJSON(ctx, strings.TrimRight(baseURL, "/")+"/caps", &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) fetchOutbox(ctx context.Context, baseURL, cursor string) (OutboxPage, error) {
	var out OutboxPage
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/outbox")
	if err != nil {
		return out, err
	}
	q := u.Query()
	q.Set("type", "ReleaseCard")
	q.Set("limit", "100")
	if strings.TrimSpace(cursor) != "" {
		q.Set("since", strings.TrimSpace(cursor))
	}
	u.RawQuery = q.Encode()
	if err := s.getJSON(ctx, u.String(), &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) getJSON(ctx context.Context, endpoint string, out any) error {
	if err := transportpolicy.ValidateHTTPURL(endpoint, s.allowInsecurePeerHTTP); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("GET %s status=%d body=%s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

func (s *Service) handshake(ctx context.Context, baseURL string) error {
	nodeID, err := s.identity.NodeID(ctx)
	if err != nil {
		return err
	}
	publicKey, err := s.identity.PublicKey(ctx)
	if err != nil {
		return err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	body := map[string]any{
		"schema_version":     "1.0",
		"type":               "HandshakeRequest",
		"node_id":            nodeID,
		"public_key":         canonical.Base64URL(publicKey),
		"nonce":              canonical.Base64URL(nonce),
		"supported_versions": []string{profile.SpecVersion},
		"requested_pools":    []string{},
		"created_at":         time.Now().UTC().Format(time.RFC3339),
	}
	signBytes, err := canonical.Marshal(body)
	if err != nil {
		return err
	}
	signature, err := s.identity.Sign(ctx, signBytes)
	if err != nil {
		return err
	}
	body["signature"] = canonical.Base64URL(signature)
	payload, _ := json.Marshal(body)
	endpoint := strings.TrimRight(baseURL, "/") + "/handshake"
	if err := transportpolicy.ValidateHTTPURL(endpoint, s.allowInsecurePeerHTTP); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/gonzbnet+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("handshake status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	return nil
}

func (s *Service) validatePeerIdentity(wellKnown profile.WellKnown, nodeProfile profile.NodeProfile) error {
	if wellKnown.SpecVersion != profile.SpecVersion {
		return fmt.Errorf("unsupported peer spec version %q", wellKnown.SpecVersion)
	}
	if strings.TrimSpace(wellKnown.NodeID) == "" || wellKnown.NodeID != nodeProfile.NodeID {
		return fmt.Errorf("peer node id mismatch")
	}
	if strings.TrimSpace(wellKnown.PublicKey) == "" || wellKnown.PublicKey != nodeProfile.PublicKey {
		return fmt.Errorf("peer public key mismatch")
	}
	publicKey, err := canonical.DecodeBase64URL(nodeProfile.PublicKey)
	if err != nil {
		return err
	}
	if identity.NodeIDFromPublicKey(ed25519.PublicKey(publicKey)) != nodeProfile.NodeID {
		return fmt.Errorf("peer node id does not match public key")
	}
	return nil
}

func (s *Service) projectValidationEvent(ctx context.Context, event *events.SignedEvent, raw []byte) error {
	if event == nil {
		return nil
	}
	poolID := ""
	if len(event.PoolIDs) > 0 {
		poolID = event.PoolIDs[0]
	}
	switch event.EventType {
	case pools.EventTypeValidatorCapacity:
		var body validation.ValidatorCapacity
		if err := json.Unmarshal(event.Body, &body); err != nil {
			_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid validator capacity body")
			return nil
		}
		return s.store.ProjectValidatorCapacity(ctx, pgindex.ValidatorCapacityProjection{
			Capacity:     body,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
		})
	case pools.EventTypeArticleAvailabilityAttestation:
		var body validation.ArticleAvailabilityAttestation
		if err := json.Unmarshal(event.Body, &body); err != nil {
			_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid article availability body")
			return nil
		}
		return s.store.ProjectArticleAvailabilityAttestation(ctx, pgindex.ArticleAvailabilityProjection{
			Attestation:  body,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		})
	case pools.EventTypeChecksumAttestation:
		var body validation.ChecksumAttestation
		if err := json.Unmarshal(event.Body, &body); err != nil {
			_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid checksum attestation body")
			return nil
		}
		return s.store.ProjectChecksumAttestation(ctx, pgindex.ChecksumAttestationProjection{
			Attestation:  body,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		})
	case pools.EventTypeManifestAvailability:
		var body manifestavailability.Attestation
		if err := json.Unmarshal(event.Body, &body); err != nil {
			_ = s.store.AppendRejectedFederationEvent(ctx, event.EventID, event.AuthorNodeID, event.EventType, raw, "invalid manifest availability body")
			return nil
		}
		return s.store.ProjectManifestAvailability(ctx, pgindex.ManifestAvailabilityProjection{
			Attestation:  body,
			EventID:      event.EventID,
			AuthorNodeID: event.AuthorNodeID,
			PoolID:       poolID,
		})
	default:
		return nil
	}
}

func isSyncCoverageEvent(eventType string) bool {
	for _, candidate := range coverage.EventTypes() {
		if eventType == candidate {
			return true
		}
	}
	return false
}

func wellKnownURL(peerURL string) (string, error) {
	peerURL = strings.TrimRight(strings.TrimSpace(peerURL), "/")
	u, err := url.Parse(peerURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("peer url must be absolute")
	}
	root := *u
	root.Path = ""
	root.RawQuery = ""
	root.Fragment = ""
	return strings.TrimRight(root.String(), "/") + "/.well-known/gonzbnet", nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
