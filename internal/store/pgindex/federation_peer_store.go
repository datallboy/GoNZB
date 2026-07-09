package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

type FederationPeerRecord struct {
	ID           int64
	NodeID       string
	PeerURL      string
	Source       string
	Enabled      bool
	Status       string
	Cursor       string
	LastEventID  string
	FailureCount int
	LastError    string
}

type FederationOutboxParams struct {
	Since     string
	PoolID    string
	EventType string
	Limit     int
}

type FederationOutboxPage struct {
	Events     []*events.SignedEvent
	NextCursor string
	HasMore    bool
}

type FederationDeliveryResult struct {
	PeerID  int64
	EventID string
	Status  string
	Error   string
}

func (s *Store) UpsertFederationPeerURL(ctx context.Context, peerURL string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}
	peerURL = strings.TrimRight(strings.TrimSpace(peerURL), "/")
	if peerURL == "" {
		return 0, fmt.Errorf("peer url is required")
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO federation_peers (peer_url, source, enabled, status, updated_at)
		VALUES ($1, 'manual', TRUE, 'pending', NOW())
		ON CONFLICT (peer_url) DO UPDATE SET
			enabled = TRUE,
			updated_at = NOW()
		RETURNING id`, peerURL).Scan(&id); err != nil {
		return 0, fmt.Errorf("upsert federation peer: %w", err)
	}
	return id, nil
}

func (s *Store) ListEnabledFederationPeers(ctx context.Context) ([]FederationPeerRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id,
			COALESCE(p.node_id, ''),
			p.peer_url,
			p.source,
			p.enabled,
			p.status,
			COALESCE(c.cursor, ''),
			COALESCE(c.last_event_id, ''),
			p.failure_count,
			COALESCE(p.last_error, '')
		FROM federation_peers p
		LEFT JOIN federation_peer_cursors c
		  ON c.peer_id = p.id
		 AND c.pool_id = ''
		 AND c.event_type = 'ReleaseCard'
		WHERE p.enabled = TRUE
		ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("list federation peers: %w", err)
	}
	defer rows.Close()

	out := make([]FederationPeerRecord, 0, 8)
	for rows.Next() {
		var item FederationPeerRecord
		if err := rows.Scan(
			&item.ID,
			&item.NodeID,
			&item.PeerURL,
			&item.Source,
			&item.Enabled,
			&item.Status,
			&item.Cursor,
			&item.LastEventID,
			&item.FailureCount,
			&item.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan federation peer: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate federation peers: %w", err)
	}
	return out, nil
}

func (s *Store) MarkFederationPeerSyncSuccess(ctx context.Context, peerID int64, nodeID, cursor, lastEventID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE federation_peers
		SET node_id = NULLIF($2, ''),
		    status = 'connected',
		    failure_count = 0,
		    last_error = NULL,
		    last_connected_at = NOW(),
		    last_sync_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`, peerID, strings.TrimSpace(nodeID)); err != nil {
		return fmt.Errorf("update federation peer success: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO federation_peer_cursors (peer_id, pool_id, event_type, cursor, last_event_id, updated_at)
		VALUES ($1, '', 'ReleaseCard', NULLIF($2, ''), NULLIF($3, ''), NOW())
		ON CONFLICT (peer_id, pool_id, event_type) DO UPDATE SET
			cursor = EXCLUDED.cursor,
			last_event_id = EXCLUDED.last_event_id,
			updated_at = NOW()`, peerID, strings.TrimSpace(cursor), strings.TrimSpace(lastEventID)); err != nil {
		return fmt.Errorf("update federation peer cursor: %w", err)
	}
	return tx.Commit()
}

func (s *Store) MarkFederationPeerSyncFailure(ctx context.Context, peerID int64, errText string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE federation_peers
		SET status = 'error',
		    failure_count = failure_count + 1,
		    last_error = $2,
		    updated_at = NOW()
		WHERE id = $1`, peerID, trimError(errText))
	if err != nil {
		return fmt.Errorf("update federation peer failure: %w", err)
	}
	return nil
}

func (s *Store) ListUndeliveredFederationEvents(ctx context.Context, peerID int64, limit int) ([]*events.SignedEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			e.event_id, e.spec_version, e.event_type, e.author_node_id, e.author_public_key,
			e.sequence, e.previous_event_id, e.body_schema, e.body_hash, e.signature_alg,
			e.signature, e.body_json, e.pool_ids, e.visibility, e.created_at, e.not_before,
			e.expires_at
		FROM federation_events e
		LEFT JOIN federation_peer_deliveries d
		  ON d.peer_id = $1
		 AND d.event_id = e.event_id
		WHERE e.validation_status = 'accepted'
		  AND e.event_type = 'ReleaseCard'
		  AND (
		    d.event_id IS NULL
		    OR (
		      d.status NOT IN ('accepted', 'duplicate')
		      AND d.last_attempt_at < NOW() - (LEAST(d.attempts, 10) * INTERVAL '1 minute')
		    )
		  )
		ORDER BY e.created_at ASC, e.event_id ASC
		LIMIT $2`, peerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list undelivered federation events: %w", err)
	}
	defer rows.Close()

	out := make([]*events.SignedEvent, 0, limit)
	for rows.Next() {
		event, err := scanFederationEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan undelivered federation event: %w", err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate undelivered federation events: %w", err)
	}
	return out, nil
}

func (s *Store) RecordFederationPeerDelivery(ctx context.Context, result FederationDeliveryResult) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "error"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_peer_deliveries (
			peer_id, event_id, status, attempts, last_attempt_at, delivered_at,
			last_error, updated_at
		)
		VALUES (
			$1, $2, $3, 1, NOW(),
			CASE WHEN $3 IN ('accepted', 'duplicate') THEN NOW() ELSE NULL END,
			NULLIF($4, ''), NOW()
		)
		ON CONFLICT (peer_id, event_id) DO UPDATE SET
			status = EXCLUDED.status,
			attempts = federation_peer_deliveries.attempts + 1,
			last_attempt_at = NOW(),
			delivered_at = CASE
				WHEN EXCLUDED.status IN ('accepted', 'duplicate') THEN NOW()
				ELSE federation_peer_deliveries.delivered_at
			END,
			last_error = EXCLUDED.last_error,
			updated_at = NOW()`,
		result.PeerID,
		result.EventID,
		status,
		trimError(result.Error),
	)
	if err != nil {
		return fmt.Errorf("record federation peer delivery: %w", err)
	}
	return nil
}

func (s *Store) GetFederationEvent(ctx context.Context, eventID string) (*events.SignedEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	event, err := scanFederationEvent(s.db.QueryRowContext(ctx, `
		SELECT
			event_id, spec_version, event_type, author_node_id, author_public_key,
			sequence, previous_event_id, body_schema, body_hash, signature_alg,
			signature, body_json, pool_ids, visibility, created_at, not_before,
			expires_at
		FROM federation_events
		WHERE event_id = $1
		  AND validation_status = 'accepted'`, eventID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get federation event: %w", err)
	}
	return event, nil
}

func (s *Store) ListFederationOutboxEvents(ctx context.Context, params FederationOutboxParams) (FederationOutboxPage, error) {
	var page FederationOutboxPage
	if s == nil || s.db == nil {
		return page, fmt.Errorf("pgindex store is not initialized")
	}
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	clauses := []string{"validation_status = 'accepted'"}
	args := make([]any, 0, 6)
	arg := 1
	if eventType := strings.TrimSpace(params.EventType); eventType != "" {
		clauses = append(clauses, fmt.Sprintf("event_type = $%d", arg))
		args = append(args, eventType)
		arg++
	}
	if poolID := strings.TrimSpace(params.PoolID); poolID != "" {
		poolJSON, _ := json.Marshal([]string{poolID})
		clauses = append(clauses, fmt.Sprintf("pool_ids @> $%d::jsonb", arg))
		args = append(args, string(poolJSON))
		arg++
	}
	if since := strings.TrimSpace(params.Since); since != "" {
		var cursorCreatedAt time.Time
		var cursorEventID string
		err := s.db.QueryRowContext(ctx, `
			SELECT created_at, event_id
			FROM federation_events
			WHERE event_id = $1`, since).Scan(&cursorCreatedAt, &cursorEventID)
		if err == nil {
			clauses = append(clauses, fmt.Sprintf("(created_at, event_id) > ($%d, $%d)", arg, arg+1))
			args = append(args, cursorCreatedAt, cursorEventID)
			arg += 2
		} else if err != sql.ErrNoRows {
			return page, fmt.Errorf("read federation outbox cursor: %w", err)
		}
	}

	args = append(args, limit+1)
	query := fmt.Sprintf(`
		SELECT
			event_id, spec_version, event_type, author_node_id, author_public_key,
			sequence, previous_event_id, body_schema, body_hash, signature_alg,
			signature, body_json, pool_ids, visibility, created_at, not_before,
			expires_at
		FROM federation_events
		WHERE %s
		ORDER BY created_at ASC, event_id ASC
		LIMIT $%d`, strings.Join(clauses, "\n  AND "), arg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list federation outbox events: %w", err)
	}
	defer rows.Close()

	eventsOut := make([]*events.SignedEvent, 0, limit)
	for rows.Next() {
		event, err := scanFederationEvent(rows)
		if err != nil {
			return page, fmt.Errorf("scan federation outbox event: %w", err)
		}
		if len(eventsOut) >= limit {
			page.HasMore = true
			break
		}
		eventsOut = append(eventsOut, event)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("iterate federation outbox events: %w", err)
	}
	page.Events = eventsOut
	if len(eventsOut) > 0 {
		page.NextCursor = eventsOut[len(eventsOut)-1].EventID
	}
	return page, nil
}

type federationEventScanner interface {
	Scan(dest ...any) error
}

func scanFederationEvent(scanner federationEventScanner) (*events.SignedEvent, error) {
	var (
		event       events.SignedEvent
		publicKey   []byte
		signature   []byte
		bodyJSON    []byte
		poolIDsJSON []byte
		previous    sql.NullString
		notBefore   sql.NullTime
		expiresAt   sql.NullTime
	)
	if err := scanner.Scan(
		&event.EventID,
		&event.SpecVersion,
		&event.EventType,
		&event.AuthorNodeID,
		&publicKey,
		&event.Sequence,
		&previous,
		&event.BodySchema,
		&event.BodyHash,
		&event.SignatureAlg,
		&signature,
		&bodyJSON,
		&poolIDsJSON,
		&event.Visibility,
		&event.CreatedAt,
		&notBefore,
		&expiresAt,
	); err != nil {
		return nil, err
	}
	if previous.Valid {
		value := previous.String
		event.PreviousEventID = &value
	}
	if notBefore.Valid {
		value := notBefore.Time.UTC()
		event.NotBefore = &value
	}
	if expiresAt.Valid {
		value := expiresAt.Time.UTC()
		event.ExpiresAt = &value
	}
	event.AuthorPublicKey = canonical.Base64URL(publicKey)
	event.Signature = canonical.Base64URL(signature)
	event.Body = append([]byte(nil), bodyJSON...)
	if len(poolIDsJSON) > 0 {
		_ = json.Unmarshal(poolIDsJSON, &event.PoolIDs)
	}
	event.CreatedAt = event.CreatedAt.UTC()
	return &event, nil
}

func trimError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
