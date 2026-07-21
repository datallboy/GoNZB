package admission

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
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
)

type Client struct {
	identity      events.Identity
	httpClient    *http.Client
	allowInsecure bool
}

func NewClient(nodeIdentity events.Identity, allowInsecure bool) *Client {
	return &Client{
		identity:      nodeIdentity,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		allowInsecure: allowInsecure,
	}
}

func NormalizeLocator(raw string, allowInsecure bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("node address is required")
	}
	if strings.HasPrefix(raw, "gonzbnet://") {
		invite, err := ParseInvitation(raw)
		if err != nil {
			return "", err
		}
		raw = invite.RelayURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("node address must contain a hostname")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	base := strings.TrimRight(parsed.String(), "/")
	if err := transportpolicy.ValidateHTTPURL(base, allowInsecure); err != nil {
		return "", err
	}
	return base, nil
}

func (c *Client) Discover(ctx context.Context, locator, expectedNodeID string) (Remote, error) {
	var remote Remote
	var invitation *Invitation
	if strings.HasPrefix(strings.TrimSpace(locator), "gonzbnet://") {
		parsed, err := ParseInvitation(locator)
		if err != nil {
			return remote, err
		}
		if err := parsed.Verify(time.Now().UTC()); err != nil {
			return remote, err
		}
		invitation = &parsed
	}
	base, err := NormalizeLocator(locator, c.allowInsecure)
	if err != nil {
		return remote, err
	}
	wellKnownURL, err := discoveryURL(base)
	if err != nil {
		return remote, err
	}
	if err := c.getJSON(ctx, wellKnownURL, &remote.WellKnown); err != nil {
		return remote, err
	}
	if err := transportpolicy.ValidateHTTPURL(remote.WellKnown.BaseURL, c.allowInsecure); err != nil {
		return remote, fmt.Errorf("invalid advertised base URL: %w", err)
	}
	baseURL := strings.TrimRight(remote.WellKnown.BaseURL, "/")
	if err := c.getJSON(ctx, baseURL+"/node", &remote.Profile); err != nil {
		return remote, err
	}
	if err := c.getJSON(ctx, baseURL+"/caps", &remote.Caps); err != nil {
		return remote, err
	}
	if err := validateRemoteIdentity(remote.WellKnown, remote.Profile, expectedNodeID); err != nil {
		return remote, err
	}
	var pools PoolList
	poolsURL := baseURL + "/pools"
	if invitation != nil {
		poolsURL += "?invitation=" + url.QueryEscape(strings.TrimSpace(locator))
	}
	if err := c.getJSON(ctx, poolsURL, &pools); err != nil {
		return remote, err
	}
	remote.Pools = pools.Items
	for i := range remote.Pools {
		if err := VerifyPoolDescriptor(remote.Pools[i], time.Now().UTC()); err != nil {
			return remote, fmt.Errorf("invalid pool descriptor %q: %w", remote.Pools[i].PoolID, err)
		}
	}
	if err := c.handshake(ctx, baseURL); err != nil {
		return remote, err
	}
	return remote, nil
}

func VerifyPoolDescriptor(descriptor PoolDescriptor, now time.Time) error {
	event := descriptor.GenesisEvent
	if event == nil || strings.TrimSpace(descriptor.PoolID) == "" || strings.TrimSpace(descriptor.GenesisEventID) == "" {
		return fmt.Errorf("signed pool genesis is required")
	}
	if event.EventID != descriptor.GenesisEventID || event.EventType != pools.EventTypePoolGenesis || len(event.PoolIDs) != 1 || event.PoolIDs[0] != descriptor.PoolID {
		return fmt.Errorf("pool genesis fingerprint or scope mismatch")
	}
	validation, err := events.VerifyWithin(event, now, 2*time.Minute, 0)
	if err != nil || validation == nil || !validation.OK {
		return fmt.Errorf("pool genesis signature verification failed")
	}
	var body pools.Genesis
	if err := json.Unmarshal(event.Body, &body); err != nil {
		return fmt.Errorf("decode pool genesis: %w", err)
	}
	if body.Type != pools.EventTypePoolGenesis || body.PoolID != descriptor.PoolID || !containsString(body.Admins, event.AuthorNodeID) {
		return fmt.Errorf("pool genesis body mismatch")
	}
	return nil
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(value) {
			return true
		}
	}
	return false
}

func (c *Client) SubmitJoin(ctx context.Context, baseURL, poolID string, event *events.SignedEvent) error {
	if event == nil {
		return fmt.Errorf("join event is required")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/pools/" + url.PathEscape(poolID) + "/join-requests"
	return c.postJSON(ctx, endpoint, event, nil, false)
}

func (c *Client) SubmitApproval(ctx context.Context, baseURL string, fragment ApprovalFragment) (Status, error) {
	var status Status
	endpoint := strings.TrimRight(baseURL, "/") + "/pools/" + url.PathEscape(fragment.PoolID) + "/admissions/" + url.PathEscape(fragment.ProposalEventID) + "/approvals"
	err := c.postJSON(ctx, endpoint, fragment, &status, false)
	return status, err
}

func (c *Client) SubmitRejection(ctx context.Context, baseURL string, fragment RejectionFragment) (Status, error) {
	var status Status
	endpoint := strings.TrimRight(baseURL, "/") + "/pools/" + url.PathEscape(fragment.PoolID) + "/admissions/" + url.PathEscape(fragment.ProposalEventID) + "/rejections"
	err := c.postJSON(ctx, endpoint, fragment, &status, false)
	return status, err
}

func (c *Client) FetchStatus(ctx context.Context, baseURL, poolID, proposalEventID string) (Status, error) {
	var status Status
	endpoint := strings.TrimRight(baseURL, "/") + "/pools/" + url.PathEscape(poolID) + "/admissions/" + url.PathEscape(proposalEventID)
	err := c.getSignedJSON(ctx, endpoint, &status)
	return status, err
}

func (c *Client) handshake(ctx context.Context, baseURL string) error {
	if c.identity == nil {
		return fmt.Errorf("node identity is required")
	}
	nodeID, err := c.identity.NodeID(ctx)
	if err != nil {
		return err
	}
	publicKey, err := c.identity.PublicKey(ctx)
	if err != nil {
		return err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	body := map[string]any{
		"schema_version": "1.0", "type": "HandshakeRequest", "node_id": nodeID,
		"public_key": canonical.Base64URL(publicKey), "nonce": canonical.Base64URL(nonce),
		"supported_versions": []string{profile.SpecVersion}, "requested_pools": []string{},
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := canonical.Marshal(body)
	if err != nil {
		return err
	}
	signature, err := c.identity.Sign(ctx, payload)
	if err != nil {
		return err
	}
	body["signature"] = canonical.Base64URL(signature)
	return c.postJSON(ctx, strings.TrimRight(baseURL, "/")+"/handshake", body, nil, false)
}

func (c *Client) getSignedJSON(ctx context.Context, endpoint string, out any) error {
	if err := transportpolicy.ValidateHTTPURL(endpoint, c.allowInsecure); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	auth, err := requestauth.Sign(ctx, c.identity, req.Method, req.URL.Path, req.URL.RawQuery, nil, time.Now())
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	return c.doJSON(req, out)
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	if err := transportpolicy.ValidateHTTPURL(endpoint, c.allowInsecure); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, out)
}

func (c *Client) postJSON(ctx context.Context, endpoint string, body, out any, signed bool) error {
	if err := transportpolicy.ValidateHTTPURL(endpoint, c.allowInsecure); err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if signed {
		auth, err := requestauth.Sign(ctx, c.identity, req.Method, req.URL.Path, req.URL.RawQuery, payload, time.Now())
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", auth)
	}
	return c.doJSON(req, out)
}

func (c *Client) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s status=%d body=%s", req.Method, req.URL.String(), resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return err
		}
	}
	return nil
}

func discoveryURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed.Path = "/.well-known/gonzbnet"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func validateRemoteIdentity(wellKnown profile.WellKnown, node profile.NodeProfile, expectedNodeID string) error {
	if wellKnown.SpecVersion != profile.SpecVersion || wellKnown.NodeID == "" || wellKnown.NodeID != node.NodeID || wellKnown.PublicKey != node.PublicKey {
		return fmt.Errorf("remote node identity mismatch")
	}
	publicKey, err := canonical.DecodeBase64URL(node.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || identity.NodeIDFromPublicKey(ed25519.PublicKey(publicKey)) != node.NodeID {
		return fmt.Errorf("remote public key does not match node id")
	}
	if expectedNodeID != "" && strings.TrimSpace(expectedNodeID) != node.NodeID {
		return fmt.Errorf("remote node does not match expected node id")
	}
	return nil
}
