package events

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

const (
	SpecVersion  = "gonzbnet/1.0"
	SignatureAlg = "Ed25519"
)

type Identity interface {
	NodeID(context.Context) (string, error)
	PublicKey(context.Context) (ed25519.PublicKey, error)
	Sign(context.Context, []byte) ([]byte, error)
}

type SignedEvent struct {
	SpecVersion     string          `json:"spec_version"`
	EventID         string          `json:"event_id"`
	EventType       string          `json:"event_type"`
	AuthorNodeID    string          `json:"author_node_id"`
	AuthorPublicKey string          `json:"author_public_key"`
	Sequence        int64           `json:"sequence"`
	PreviousEventID *string         `json:"previous_event_id"`
	CreatedAt       time.Time       `json:"created_at"`
	NotBefore       *time.Time      `json:"not_before"`
	ExpiresAt       *time.Time      `json:"expires_at"`
	PoolIDs         []string        `json:"pool_ids"`
	Visibility      string          `json:"visibility"`
	BodySchema      string          `json:"body_schema"`
	BodyHash        string          `json:"body_hash"`
	Body            json.RawMessage `json:"body"`
	SignatureAlg    string          `json:"signature_alg"`
	Signature       string          `json:"signature"`
}

type CreateOptions struct {
	EventType       string
	Sequence        int64
	PreviousEventID *string
	CreatedAt       time.Time
	NotBefore       *time.Time
	ExpiresAt       *time.Time
	PoolIDs         []string
	Visibility      string
	BodySchema      string
	Body            any
}

type ValidationResult struct {
	OK                 bool
	Reason             string
	CanonicalEventJSON []byte
	CanonicalBodyJSON  []byte
}

func Create(ctx context.Context, signer Identity, opts CreateOptions) (*SignedEvent, *ValidationResult, error) {
	if signer == nil {
		return nil, nil, fmt.Errorf("identity is required")
	}
	nodeID, err := signer.NodeID(ctx)
	if err != nil {
		return nil, nil, err
	}
	publicKey, err := signer.PublicKey(ctx)
	if err != nil {
		return nil, nil, err
	}

	bodyHash, bodyCanonical, err := canonical.BodyHash(opts.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize body: %w", err)
	}
	createdAt := opts.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	visibility := strings.TrimSpace(opts.Visibility)
	if visibility == "" {
		visibility = "pool"
	}

	event := &SignedEvent{
		SpecVersion:     SpecVersion,
		EventType:       strings.TrimSpace(opts.EventType),
		AuthorNodeID:    nodeID,
		AuthorPublicKey: canonical.Base64URL(publicKey),
		Sequence:        opts.Sequence,
		PreviousEventID: opts.PreviousEventID,
		CreatedAt:       createdAt.UTC(),
		NotBefore:       utcTimePtr(opts.NotBefore),
		ExpiresAt:       utcTimePtr(opts.ExpiresAt),
		PoolIDs:         append([]string(nil), opts.PoolIDs...),
		Visibility:      visibility,
		BodySchema:      strings.TrimSpace(opts.BodySchema),
		BodyHash:        bodyHash,
		Body:            append(json.RawMessage(nil), bodyCanonical...),
		SignatureAlg:    SignatureAlg,
	}
	if err := event.validateRequiredForSigning(); err != nil {
		return nil, nil, err
	}

	unsigned, err := event.CanonicalUnsigned()
	if err != nil {
		return nil, nil, err
	}
	event.EventID = canonical.HashID("evt", unsigned)

	signature, err := signer.Sign(ctx, unsigned)
	if err != nil {
		return nil, nil, err
	}
	event.Signature = canonical.Base64URL(signature)

	result, err := Verify(event)
	if err != nil {
		return nil, nil, err
	}
	return event, result, nil
}

func Verify(event *SignedEvent) (*ValidationResult, error) {
	result := &ValidationResult{}
	if event == nil {
		result.Reason = "event is required"
		return result, nil
	}
	if err := event.validateRequiredForVerification(); err != nil {
		result.Reason = err.Error()
		return result, nil
	}

	bodyHash, bodyCanonical, err := canonical.BodyHash(event.Body)
	if err != nil {
		result.Reason = fmt.Sprintf("canonicalize body: %v", err)
		return result, nil
	}
	result.CanonicalBodyJSON = bodyCanonical
	if bodyHash != event.BodyHash {
		result.Reason = "body_hash mismatch"
		return result, nil
	}

	publicKey, err := canonical.DecodeBase64URL(event.AuthorPublicKey)
	if err != nil {
		result.Reason = "invalid author_public_key"
		return result, nil
	}
	if len(publicKey) != ed25519.PublicKeySize {
		result.Reason = "invalid author_public_key size"
		return result, nil
	}
	if identity.NodeIDFromPublicKey(ed25519.PublicKey(publicKey)) != event.AuthorNodeID {
		result.Reason = "author_node_id does not match author_public_key"
		return result, nil
	}

	unsigned, err := event.CanonicalUnsigned()
	if err != nil {
		result.Reason = fmt.Sprintf("canonicalize unsigned event: %v", err)
		return result, nil
	}
	result.CanonicalEventJSON = unsigned
	if canonical.HashID("evt", unsigned) != event.EventID {
		result.Reason = "event_id mismatch"
		return result, nil
	}

	signature, err := canonical.DecodeBase64URL(event.Signature)
	if err != nil {
		result.Reason = "invalid signature"
		return result, nil
	}
	if !identity.Verify(ed25519.PublicKey(publicKey), unsigned, signature) {
		result.Reason = "signature verification failed"
		return result, nil
	}

	result.OK = true
	return result, nil
}

func VerifyAt(event *SignedEvent, now time.Time, futureTolerance time.Duration) (*ValidationResult, error) {
	result, err := Verify(event)
	if err != nil || result == nil || !result.OK {
		return result, err
	}
	if err := ValidateTimeWindow(event, now, futureTolerance); err != nil {
		result.OK = false
		result.Reason = err.Error()
	}
	return result, nil
}

func ValidateTimeWindow(event *SignedEvent, now time.Time, futureTolerance time.Duration) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if now.IsZero() {
		return nil
	}
	now = now.UTC()
	if futureTolerance <= 0 {
		futureTolerance = 2 * time.Minute
	}
	if event.CreatedAt.After(now.Add(futureTolerance)) {
		return fmt.Errorf("created_at is in the future")
	}
	if event.NotBefore != nil && event.NotBefore.After(now.Add(futureTolerance)) {
		return fmt.Errorf("not_before is in the future")
	}
	if event.ExpiresAt != nil && !event.ExpiresAt.After(now) {
		return fmt.Errorf("event expired")
	}
	return nil
}

func (e *SignedEvent) CanonicalUnsigned() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("event is required")
	}
	return canonical.Marshal(e.unsignedPayload())
}

func (e *SignedEvent) unsignedPayload() map[string]any {
	return map[string]any{
		"spec_version":      e.SpecVersion,
		"event_type":        e.EventType,
		"author_node_id":    e.AuthorNodeID,
		"author_public_key": e.AuthorPublicKey,
		"sequence":          e.Sequence,
		"previous_event_id": e.PreviousEventID,
		"created_at":        formatTime(e.CreatedAt),
		"not_before":        formatOptionalTime(e.NotBefore),
		"expires_at":        formatOptionalTime(e.ExpiresAt),
		"pool_ids":          nonNilStrings(e.PoolIDs),
		"visibility":        e.Visibility,
		"body_schema":       e.BodySchema,
		"body_hash":         e.BodyHash,
		"body":              e.Body,
		"signature_alg":     e.SignatureAlg,
	}
}

func (e *SignedEvent) validateRequiredForSigning() error {
	if strings.TrimSpace(e.EventType) == "" {
		return fmt.Errorf("event_type is required")
	}
	if strings.TrimSpace(e.BodySchema) == "" {
		return fmt.Errorf("body_schema is required")
	}
	return nil
}

func (e *SignedEvent) validateRequiredForVerification() error {
	if strings.TrimSpace(e.SpecVersion) != SpecVersion {
		return fmt.Errorf("unsupported spec_version")
	}
	if strings.TrimSpace(e.EventID) == "" {
		return fmt.Errorf("event_id is required")
	}
	if strings.TrimSpace(e.EventType) == "" {
		return fmt.Errorf("event_type is required")
	}
	if strings.TrimSpace(e.AuthorNodeID) == "" {
		return fmt.Errorf("author_node_id is required")
	}
	if strings.TrimSpace(e.AuthorPublicKey) == "" {
		return fmt.Errorf("author_public_key is required")
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if strings.TrimSpace(e.BodySchema) == "" {
		return fmt.Errorf("body_schema is required")
	}
	if strings.TrimSpace(e.BodyHash) == "" {
		return fmt.Errorf("body_hash is required")
	}
	if len(e.Body) == 0 {
		return fmt.Errorf("body is required")
	}
	if strings.TrimSpace(e.SignatureAlg) != SignatureAlg {
		return fmt.Errorf("unsupported signature_alg")
	}
	if strings.TrimSpace(e.Signature) == "" {
		return fmt.Errorf("signature is required")
	}
	return nil
}

func utcTimePtr(in *time.Time) *time.Time {
	if in == nil || in.IsZero() {
		return nil
	}
	out := in.UTC()
	return &out
}

func formatOptionalTime(in *time.Time) any {
	if in == nil || in.IsZero() {
		return nil
	}
	return formatTime(*in)
}

func formatTime(in time.Time) string {
	return in.UTC().Format(time.RFC3339Nano)
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return append([]string(nil), in...)
}
