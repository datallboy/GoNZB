package pgindex

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

var ErrFederationSequenceConflict = errors.New("federation sequence conflict")

type FederationNodeRecord struct {
	NodeID            string
	PublicKey         ed25519.PublicKey
	Alias             string
	Software          string
	SoftwareVersion   string
	BaseURL           string
	Capabilities      json.RawMessage
	ModuleStatus      json.RawMessage
	ScannerCapacity   json.RawMessage
	ValidatorCapacity json.RawMessage
	ProviderScope     json.RawMessage
	ProfileJSON       json.RawMessage
	Status            string
	LastVerifiedAt    *time.Time
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
	moduleStatus := defaultJSON(node.ModuleStatus, `{}`)
	scannerCapacity := nullableJSON(node.ScannerCapacity)
	validatorCapacity := nullableJSON(node.ValidatorCapacity)
	providerScope := nullableJSON(node.ProviderScope)
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
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_node_capabilities (
			node_id, capabilities, module_status, scanner_capacity, validator_capacity,
			provider_scope, updated_at
		)
		VALUES ($1, $2::jsonb, $3::jsonb, $4::jsonb, $5::jsonb, $6::jsonb, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			capabilities = EXCLUDED.capabilities,
			module_status = EXCLUDED.module_status,
			scanner_capacity = COALESCE(EXCLUDED.scanner_capacity, federation_node_capabilities.scanner_capacity),
			validator_capacity = COALESCE(EXCLUDED.validator_capacity, federation_node_capabilities.validator_capacity),
			provider_scope = COALESCE(EXCLUDED.provider_scope, federation_node_capabilities.provider_scope),
			updated_at = NOW()`,
		node.NodeID,
		string(capabilities),
		string(moduleStatus),
		scannerCapacity,
		validatorCapacity,
		providerScope,
	); err != nil {
		return fmt.Errorf("upsert federation node capabilities: %w", err)
	}
	return nil
}

func (s *Store) UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error {
	return s.UpsertFederationNode(ctx, FederationNodeRecord{
		NodeID:    nodeID,
		PublicKey: publicKey,
		Software:  "GoNZB",
		Status:    "local",
	})
}

func (s *Store) SetFederationNodeStatus(ctx context.Context, nodeID, status string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	nodeID = strings.TrimSpace(nodeID)
	status = strings.TrimSpace(status)
	if nodeID == "" {
		return false, fmt.Errorf("node_id is required")
	}
	switch status {
	case "known", "blocked":
	default:
		return false, fmt.Errorf("unsupported node status %q", status)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE federation_nodes
		SET status = $2,
		    updated_at = NOW()
		WHERE node_id = $1
		  AND status <> 'local'`, nodeID, status)
	if err != nil {
		return false, fmt.Errorf("set federation node status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read federation node status count: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error) {
	if s == nil || s.db == nil {
		return 0, nil, fmt.Errorf("pgindex store is not initialized")
	}
	authorNodeID = strings.TrimSpace(authorNodeID)
	if authorNodeID == "" {
		return 0, nil, fmt.Errorf("author_node_id is required")
	}

	var (
		sequence int64
		eventID  string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT sequence, event_id
		FROM federation_events
		WHERE author_node_id = $1
		ORDER BY sequence DESC, received_at DESC
		LIMIT 1`, authorNodeID).Scan(&sequence, &eventID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 1, nil, nil
		}
		return 0, nil, fmt.Errorf("read latest federation event sequence: %w", err)
	}

	return sequence + 1, &eventID, nil
}

func (s *Store) FindFederationEventByBodyHash(ctx context.Context, authorNodeID, eventType, bodyHash string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("pgindex store is not initialized")
	}
	authorNodeID = strings.TrimSpace(authorNodeID)
	eventType = strings.TrimSpace(eventType)
	bodyHash = strings.TrimSpace(bodyHash)
	if authorNodeID == "" || eventType == "" || bodyHash == "" {
		return "", nil
	}

	var eventID string
	err := s.db.QueryRowContext(ctx, `
		SELECT event_id
		FROM federation_events
		WHERE author_node_id = $1
		  AND event_type = $2
		  AND body_hash = $3
		  AND validation_status = 'accepted'
		ORDER BY sequence DESC
		LIMIT 1`, authorNodeID, eventType, bodyHash).Scan(&eventID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find federation event by body hash: %w", err)
	}
	return eventID, nil
}

func (s *Store) FederationEventExists(ctx context.Context, eventID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, nil
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM federation_events
			WHERE event_id = $1
		)`, eventID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check federation event exists: %w", err)
	}
	return exists, nil
}

func (s *Store) GetFederationNodePublicKey(ctx context.Context, nodeID string) (ed25519.PublicKey, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	var publicKey []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT public_key
		FROM federation_nodes
		WHERE node_id = $1
		  AND status <> 'blocked'`, nodeID).Scan(&publicKey)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("unknown federation node")
	}
	if err != nil {
		return nil, fmt.Errorf("read federation node public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("stored federation public key has invalid size")
	}
	return ed25519.PublicKey(publicKey), nil
}

func (s *Store) StoreFederationNonce(ctx context.Context, nodeID, nonce string, expiresAt time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_nonce_replay_cache (node_id, nonce, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (node_id, nonce) DO NOTHING`,
		strings.TrimSpace(nodeID),
		strings.TrimSpace(nonce),
		expiresAt.UTC(),
	)
	if err != nil {
		return false, fmt.Errorf("store federation nonce: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error {
	return s.AppendFederationEvent(ctx, FederationEventRecord{
		Event:      event,
		Validation: validation,
	})
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
	existingEventID, err := s.findFederationEventByAuthorSequence(ctx, event.AuthorNodeID, event.Sequence)
	if err != nil {
		return err
	}
	if existingEventID != "" && existingEventID != event.EventID {
		return fmt.Errorf("%w: author_node_id=%s sequence=%d existing_event_id=%s",
			ErrFederationSequenceConflict,
			event.AuthorNodeID,
			event.Sequence,
			existingEventID,
		)
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

func (s *Store) findFederationEventByAuthorSequence(ctx context.Context, authorNodeID string, sequence int64) (string, error) {
	var eventID string
	err := s.db.QueryRowContext(ctx, `
		SELECT event_id
		FROM federation_events
		WHERE author_node_id = $1
		  AND sequence = $2
		LIMIT 1`, strings.TrimSpace(authorNodeID), sequence).Scan(&eventID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find federation event by author sequence: %w", err)
	}
	return eventID, nil
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

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	return string(raw)
}
