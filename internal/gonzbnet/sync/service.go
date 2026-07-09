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
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/store/pgindex"
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
	MarkFederationPeerSyncSuccess(ctx context.Context, peerID int64, nodeID, cursor, lastEventID string) error
	MarkFederationPeerSyncFailure(ctx context.Context, peerID int64, errText string) error
	ListUndeliveredFederationEvents(ctx context.Context, peerID int64, limit int) ([]*events.SignedEvent, error)
	RecordFederationPeerDelivery(ctx context.Context, result pgindex.FederationDeliveryResult) error
}

type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

type Service struct {
	identity Identity
	store    Store
	client   *http.Client
	logger   Logger
}

type Result struct {
	Peers     int
	Accepted  int
	Duplicate int
	Rejected  int
	Projected int
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

func New(identity Identity, store Store, logger Logger) *Service {
	return &Service{
		identity: identity,
		store:    store,
		logger:   logger,
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
			validation, err := events.Verify(&event)
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
			if err := s.store.AppendVerifiedFederationEvent(ctx, &event, validation); err != nil {
				return result, err
			}
			result.Accepted++
			lastEventID = event.EventID
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/handshake", bytes.NewReader(payload))
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
