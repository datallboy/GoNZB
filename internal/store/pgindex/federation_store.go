package pgindex

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

type FederationNodeRecord struct {
	NodeID          string
	PublicKey       ed25519.PublicKey
	Alias           string
	Software        string
	SoftwareVersion string
	BaseURL         string
	Capabilities    json.RawMessage
	ProfileJSON     json.RawMessage
	Status          string
	LastVerifiedAt  *time.Time
}

type FederationEventRecord struct {
	Event              *events.SignedEvent
	Validation         *events.ValidationResult
	ValidationStatus   string
	RejectionReason    string
	CanonicalEventJSON []byte
}

func (s *Store) UpsertFederationNode(ctx context.Context, node FederationNodeRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if strings.TrimSpace(node.NodeID) == "" {
		return fmt.Errorf("node_id is required")
	}
	if len(node.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("public_key must be %d bytes", ed25519.PublicKeySize)
	}
	capabilities := defaultJSON(node.Capabilities, `{}`)
	profileJSON := defaultJSON(node.ProfileJSON, `{}`)
	status := strings.TrimSpace(node.Status)
	if status == "" {
		status = "unknown"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_nodes (
			node_id, public_key, alias, software, software_version, base_url,
			capabilities, profile_json, status, last_seen_at, last_verified_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, NOW(), $10, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			public_key = EXCLUDED.public_key,
			alias = EXCLUDED.alias,
			software = EXCLUDED.software,
			software_version = EXCLUDED.software_version,
			base_url = EXCLUDED.base_url,
			capabilities = EXCLUDED.capabilities,
			profile_json = EXCLUDED.profile_json,
			status = EXCLUDED.status,
			last_seen_at = NOW(),
			last_verified_at = EXCLUDED.last_verified_at,
			updated_at = NOW()`,
		node.NodeID,
		[]byte(node.PublicKey),
		node.Alias,
		node.Software,
		node.SoftwareVersion,
		node.BaseURL,
		string(capabilities),
		string(profileJSON),
		status,
		node.LastVerifiedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert federation node: %w", err)
	}
	return nil
}

func (s *Store) AppendFederationEvent(ctx context.Context, record FederationEventRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	event := record.Event
	if event == nil {
		return fmt.Errorf("event is required")
	}

	validation := record.Validation
	if validation == nil {
		var err error
		validation, err = events.Verify(event)
		if err != nil {
			return err
		}
	}
	status := strings.TrimSpace(record.ValidationStatus)
	rejectionReason := strings.TrimSpace(record.RejectionReason)
	if status == "" {
		if validation.OK {
			status = "accepted"
		} else {
			status = "rejected"
			rejectionReason = firstNonBlank(rejectionReason, validation.Reason)
		}
	}

	canonicalEventJSON := record.CanonicalEventJSON
	if len(canonicalEventJSON) == 0 && validation != nil {
		canonicalEventJSON = validation.CanonicalEventJSON
	}
	if len(canonicalEventJSON) == 0 {
		unsigned, err := event.CanonicalUnsigned()
		if err != nil {
			return fmt.Errorf("canonicalize federation event: %w", err)
		}
		canonicalEventJSON = unsigned
	}

	publicKey, err := canonical.DecodeBase64URL(event.AuthorPublicKey)
	if err != nil {
		return fmt.Errorf("decode author public key: %w", err)
	}
	signature, err := canonical.DecodeBase64URL(event.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	poolIDs, err := json.Marshal(event.PoolIDs)
	if err != nil {
		return fmt.Errorf("marshal pool ids: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO federation_events (
			event_id, spec_version, event_type, author_node_id, author_public_key,
			sequence, previous_event_id, body_schema, body_hash, signature_alg,
			signature, canonical_event_json, body_json, pool_ids, visibility,
			created_at, not_before, expires_at, validation_status, rejection_reason
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13::jsonb, $14::jsonb, $15,
			$16, $17, $18, $19, NULLIF($20, '')
		)
		ON CONFLICT (event_id) DO NOTHING`,
		event.EventID,
		event.SpecVersion,
		event.EventType,
		event.AuthorNodeID,
		publicKey,
		event.Sequence,
		event.PreviousEventID,
		event.BodySchema,
		event.BodyHash,
		event.SignatureAlg,
		signature,
		string(canonicalEventJSON),
		string(event.Body),
		string(poolIDs),
		event.Visibility,
		event.CreatedAt,
		event.NotBefore,
		event.ExpiresAt,
		status,
		rejectionReason,
	)
	if err != nil {
		return fmt.Errorf("append federation event: %w", err)
	}
	return nil
}

func (s *Store) AppendRejectedFederationEvent(ctx context.Context, eventID, authorNodeID, eventType string, rawEventJSON []byte, reason string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if len(rawEventJSON) == 0 {
		return fmt.Errorf("raw event json is required")
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("rejection reason is required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_rejected_events (
			event_id, author_node_id, event_type, raw_event_json, rejection_reason
		)
		VALUES ($1, $2, $3, $4, $5)`,
		eventID,
		authorNodeID,
		eventType,
		string(rawEventJSON),
		reason,
	)
	if err != nil {
		return fmt.Errorf("append rejected federation event: %w", err)
	}
	return nil
}

func defaultJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(fallback)
	}
	return raw
}
