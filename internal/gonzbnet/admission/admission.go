package admission

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/profile"
)

const (
	InvitationType        = "PoolInvitation"
	ApprovalFragmentType  = "PoolApprovalFragment"
	RejectionFragmentType = "PoolRejectionFragment"
)

type Invitation struct {
	SchemaVersion  string `json:"schema_version"`
	Type           string `json:"type"`
	PoolID         string `json:"pool_id"`
	GenesisEventID string `json:"genesis_event_id"`
	RelayURL       string `json:"relay_url"`
	CreatedByNode  string `json:"created_by_node_id"`
	CreatedByKey   string `json:"created_by_public_key"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	Signature      string `json:"signature"`
}

type PoolDescriptor struct {
	PoolID              string              `json:"pool_id"`
	DisplayName         string              `json:"display_name"`
	Description         string              `json:"description,omitempty"`
	GenesisEventID      string              `json:"genesis_event_id"`
	MembershipThreshold int                 `json:"membership_threshold"`
	Visibility          string              `json:"visibility"`
	JoinMode            string              `json:"join_mode"`
	MemberCount         int                 `json:"member_count"`
	GenesisEvent        *events.SignedEvent `json:"genesis_event,omitempty"`
}

type PoolList struct {
	SchemaVersion string           `json:"schema_version"`
	Type          string           `json:"type"`
	Items         []PoolDescriptor `json:"items"`
}

type Remote struct {
	WellKnown profile.WellKnown   `json:"well_known"`
	Profile   profile.NodeProfile `json:"profile"`
	Caps      profile.Caps        `json:"caps"`
	Pools     []PoolDescriptor    `json:"pools"`
}

type ApprovalFragment struct {
	SchemaVersion       string          `json:"schema_version"`
	Type                string          `json:"type"`
	PoolID              string          `json:"pool_id"`
	ProposalEventID     string          `json:"proposal_event_id"`
	SubjectNodeID       string          `json:"subject_node_id"`
	Role                string          `json:"role"`
	AllowedCapabilities []string        `json:"allowed_capabilities,omitempty"`
	Limits              json.RawMessage `json:"limits,omitempty"`
	AdminNodeID         string          `json:"admin_node_id"`
	AdminPublicKey      string          `json:"admin_public_key"`
	ApprovedAt          string          `json:"approved_at"`
	Signature           string          `json:"signature"`
}

type RejectionFragment struct {
	SchemaVersion   string `json:"schema_version"`
	Type            string `json:"type"`
	PoolID          string `json:"pool_id"`
	ProposalEventID string `json:"proposal_event_id"`
	SubjectNodeID   string `json:"subject_node_id"`
	Reason          string `json:"reason"`
	AdminNodeID     string `json:"admin_node_id"`
	AdminPublicKey  string `json:"admin_public_key"`
	RejectedAt      string `json:"rejected_at"`
	Signature       string `json:"signature"`
}

type Status struct {
	SchemaVersion     string                `json:"schema_version"`
	Type              string                `json:"type"`
	PoolID            string                `json:"pool_id"`
	ProposalEventID   string                `json:"proposal_event_id"`
	Status            string                `json:"status"`
	Approvals         int                   `json:"approvals"`
	ApprovalsRequired int                   `json:"approvals_required"`
	GenesisEvent      *events.SignedEvent   `json:"genesis_event,omitempty"`
	ApprovalEvent     *events.SignedEvent   `json:"approval_event,omitempty"`
	TrustEvents       []*events.SignedEvent `json:"trust_events,omitempty"`
	MemberEndpoints   []MemberEndpoint      `json:"member_endpoints,omitempty"`
	RejectionReason   string                `json:"rejection_reason,omitempty"`
}

type MemberEndpoint struct {
	NodeID  string `json:"node_id"`
	BaseURL string `json:"base_url"`
}

func NewInvitation(ctx context.Context, signer events.Identity, poolID, genesisEventID, relayURL string, expiresAt *time.Time) (Invitation, error) {
	nodeID, err := signer.NodeID(ctx)
	if err != nil {
		return Invitation{}, err
	}
	publicKey, err := signer.PublicKey(ctx)
	if err != nil {
		return Invitation{}, err
	}
	item := Invitation{
		SchemaVersion:  "1.0",
		Type:           InvitationType,
		PoolID:         strings.TrimSpace(poolID),
		GenesisEventID: strings.TrimSpace(genesisEventID),
		RelayURL:       strings.TrimRight(strings.TrimSpace(relayURL), "/"),
		CreatedByNode:  nodeID,
		CreatedByKey:   canonical.Base64URL(publicKey),
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if expiresAt != nil {
		item.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	}
	if err := item.validate(time.Now().UTC()); err != nil {
		return Invitation{}, err
	}
	payload, err := item.canonicalUnsigned()
	if err != nil {
		return Invitation{}, err
	}
	signature, err := signer.Sign(ctx, payload)
	if err != nil {
		return Invitation{}, err
	}
	item.Signature = canonical.Base64URL(signature)
	return item, nil
}

func (i Invitation) Verify(now time.Time) error {
	if err := i.validate(now); err != nil {
		return err
	}
	publicKeyBytes, err := canonical.DecodeBase64URL(i.CreatedByKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid invitation public key")
	}
	publicKey := ed25519.PublicKey(publicKeyBytes)
	if identity.NodeIDFromPublicKey(publicKey) != i.CreatedByNode {
		return fmt.Errorf("invitation signer does not match public key")
	}
	payload, err := i.canonicalUnsigned()
	if err != nil {
		return err
	}
	signature, err := canonical.DecodeBase64URL(i.Signature)
	if err != nil || !identity.Verify(publicKey, payload, signature) {
		return fmt.Errorf("invalid invitation signature")
	}
	return nil
}

func (i Invitation) Encode() (string, error) {
	payload, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	return "gonzbnet://join?invite=" + url.QueryEscape(base64.RawURLEncoding.EncodeToString(payload)), nil
}

func ParseInvitation(raw string) (Invitation, error) {
	var item Invitation
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "gonzbnet" || parsed.Host != "join" {
		return item, fmt.Errorf("invalid gonzbnet invitation")
	}
	encoded := parsed.Query().Get("invite")
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return item, fmt.Errorf("decode invitation: %w", err)
	}
	if err := json.Unmarshal(payload, &item); err != nil {
		return item, fmt.Errorf("decode invitation json: %w", err)
	}
	return item, nil
}

func NewApprovalFragment(ctx context.Context, signer events.Identity, body pools.MemberApproved, approvedAt time.Time) (ApprovalFragment, error) {
	nodeID, err := signer.NodeID(ctx)
	if err != nil {
		return ApprovalFragment{}, err
	}
	publicKey, err := signer.PublicKey(ctx)
	if err != nil {
		return ApprovalFragment{}, err
	}
	item := ApprovalFragment{
		SchemaVersion:       "1.0",
		Type:                ApprovalFragmentType,
		PoolID:              body.PoolID,
		ProposalEventID:     body.ProposalEventID,
		SubjectNodeID:       body.SubjectNodeID,
		Role:                body.Role,
		AllowedCapabilities: append([]string(nil), body.AllowedCapabilities...),
		Limits:              append(json.RawMessage(nil), body.Limits...),
		AdminNodeID:         nodeID,
		AdminPublicKey:      canonical.Base64URL(publicKey),
		ApprovedAt:          approvedAt.UTC().Format(time.RFC3339),
	}
	payload := pools.MemberApprovalPayload(body, item.ApprovedAt)
	bytes, err := canonical.Marshal(payload)
	if err != nil {
		return ApprovalFragment{}, err
	}
	signature, err := signer.Sign(ctx, bytes)
	if err != nil {
		return ApprovalFragment{}, err
	}
	item.Signature = canonical.Base64URL(signature)
	return item, nil
}

func (f ApprovalFragment) Verify() (ed25519.PublicKey, error) {
	publicKeyBytes, err := canonical.DecodeBase64URL(f.AdminPublicKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid approval public key")
	}
	publicKey := ed25519.PublicKey(publicKeyBytes)
	if identity.NodeIDFromPublicKey(publicKey) != strings.TrimSpace(f.AdminNodeID) {
		return nil, fmt.Errorf("approval node id does not match public key")
	}
	body := f.MemberApproved(nil, 1)
	payload := pools.MemberApprovalPayload(body, f.ApprovedAt)
	bytes, err := canonical.Marshal(payload)
	if err != nil {
		return nil, err
	}
	signature, err := canonical.DecodeBase64URL(f.Signature)
	if err != nil || !identity.Verify(publicKey, bytes, signature) {
		return nil, fmt.Errorf("invalid approval signature")
	}
	return publicKey, nil
}

func (f ApprovalFragment) MemberApproved(fragments []ApprovalFragment, required int) pools.MemberApproved {
	approvals := make([]pools.Approval, 0, len(fragments))
	for _, fragment := range fragments {
		approvals = append(approvals, pools.Approval{
			NodeID: fragment.AdminNodeID, ApprovedAt: fragment.ApprovedAt, Signature: fragment.Signature,
		})
	}
	sort.Slice(approvals, func(i, j int) bool { return approvals[i].NodeID < approvals[j].NodeID })
	return pools.MemberApproved{
		SchemaVersion: "1.0", Type: pools.EventTypePoolMemberApproved,
		PoolID: f.PoolID, SubjectNodeID: f.SubjectNodeID, Role: f.Role,
		ProposalEventID: f.ProposalEventID, AllowedCapabilities: append([]string(nil), f.AllowedCapabilities...),
		Limits: append(json.RawMessage(nil), f.Limits...), ApprovalsRequired: required, Approvals: approvals,
	}
}

func NewRejectionFragment(ctx context.Context, signer events.Identity, poolID, proposalEventID, subjectNodeID, reason string, rejectedAt time.Time) (RejectionFragment, error) {
	nodeID, err := signer.NodeID(ctx)
	if err != nil {
		return RejectionFragment{}, err
	}
	publicKey, err := signer.PublicKey(ctx)
	if err != nil {
		return RejectionFragment{}, err
	}
	item := RejectionFragment{
		SchemaVersion: "1.0", Type: RejectionFragmentType,
		PoolID: strings.TrimSpace(poolID), ProposalEventID: strings.TrimSpace(proposalEventID),
		SubjectNodeID: strings.TrimSpace(subjectNodeID), Reason: strings.TrimSpace(reason),
		AdminNodeID: nodeID, AdminPublicKey: canonical.Base64URL(publicKey),
		RejectedAt: rejectedAt.UTC().Format(time.RFC3339),
	}
	payload, err := item.canonicalUnsigned()
	if err != nil {
		return RejectionFragment{}, err
	}
	signature, err := signer.Sign(ctx, payload)
	if err != nil {
		return RejectionFragment{}, err
	}
	item.Signature = canonical.Base64URL(signature)
	return item, nil
}

func (f RejectionFragment) Verify(now time.Time) (ed25519.PublicKey, error) {
	if f.Type != RejectionFragmentType || f.PoolID == "" || f.ProposalEventID == "" || f.SubjectNodeID == "" || f.Reason == "" {
		return nil, fmt.Errorf("incomplete rejection fragment")
	}
	rejectedAt, err := time.Parse(time.RFC3339, f.RejectedAt)
	if err != nil || rejectedAt.Before(now.Add(-10*time.Minute)) || rejectedAt.After(now.Add(2*time.Minute)) {
		return nil, fmt.Errorf("invalid rejection timestamp")
	}
	publicKeyBytes, err := canonical.DecodeBase64URL(f.AdminPublicKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid rejection public key")
	}
	publicKey := ed25519.PublicKey(publicKeyBytes)
	if identity.NodeIDFromPublicKey(publicKey) != strings.TrimSpace(f.AdminNodeID) {
		return nil, fmt.Errorf("rejection node id does not match public key")
	}
	payload, err := f.canonicalUnsigned()
	if err != nil {
		return nil, err
	}
	signature, err := canonical.DecodeBase64URL(f.Signature)
	if err != nil || !identity.Verify(publicKey, payload, signature) {
		return nil, fmt.Errorf("invalid rejection signature")
	}
	return publicKey, nil
}

func (f RejectionFragment) canonicalUnsigned() ([]byte, error) {
	return canonical.Marshal(map[string]any{
		"schema_version": f.SchemaVersion, "type": f.Type, "pool_id": f.PoolID,
		"proposal_event_id": f.ProposalEventID, "subject_node_id": f.SubjectNodeID,
		"reason": f.Reason, "admin_node_id": f.AdminNodeID,
		"admin_public_key": f.AdminPublicKey, "rejected_at": f.RejectedAt,
	})
}

func (i Invitation) canonicalUnsigned() ([]byte, error) {
	return canonical.Marshal(map[string]any{
		"schema_version": i.SchemaVersion, "type": i.Type, "pool_id": i.PoolID,
		"genesis_event_id": i.GenesisEventID, "relay_url": i.RelayURL,
		"created_by_node_id": i.CreatedByNode, "created_by_public_key": i.CreatedByKey, "created_at": i.CreatedAt,
		"expires_at": i.ExpiresAt,
	})
}

func (i Invitation) validate(now time.Time) error {
	if i.Type != InvitationType || strings.TrimSpace(i.PoolID) == "" || strings.TrimSpace(i.GenesisEventID) == "" || strings.TrimSpace(i.RelayURL) == "" || strings.TrimSpace(i.CreatedByNode) == "" || strings.TrimSpace(i.CreatedByKey) == "" {
		return fmt.Errorf("incomplete pool invitation")
	}
	if _, err := time.Parse(time.RFC3339, i.CreatedAt); err != nil {
		return fmt.Errorf("invalid invitation created_at")
	}
	if i.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339, i.ExpiresAt)
		if err != nil || !expires.After(now) {
			return fmt.Errorf("pool invitation expired")
		}
	}
	return nil
}
