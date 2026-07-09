package pgindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type FederationPeerDiagnostic struct {
	ID              int64      `json:"id"`
	NodeID          string     `json:"node_id"`
	PeerURL         string     `json:"peer_url"`
	Source          string     `json:"source"`
	Enabled         bool       `json:"enabled"`
	Status          string     `json:"status"`
	Cursor          string     `json:"cursor"`
	LastEventID     string     `json:"last_event_id"`
	FailureCount    int        `json:"failure_count"`
	LastError       string     `json:"last_error"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type FederationEventDiagnostic struct {
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	AuthorNodeID     string          `json:"author_node_id"`
	Sequence         int64           `json:"sequence"`
	BodyHash         string          `json:"body_hash"`
	PoolIDs          json.RawMessage `json:"pool_ids"`
	Visibility       string          `json:"visibility"`
	CreatedAt        time.Time       `json:"created_at"`
	ReceivedAt       time.Time       `json:"received_at"`
	ValidationStatus string          `json:"validation_status"`
	RejectionReason  string          `json:"rejection_reason,omitempty"`
	Projected        bool            `json:"projected"`
	ProjectedAt      *time.Time      `json:"projected_at,omitempty"`
}

type FederationRejectedEventDiagnostic struct {
	ID              int64     `json:"id"`
	EventID         string    `json:"event_id"`
	AuthorNodeID    string    `json:"author_node_id"`
	EventType       string    `json:"event_type"`
	RejectionReason string    `json:"rejection_reason"`
	ReceivedAt      time.Time `json:"received_at"`
}

type FederationPeerDeliveryDiagnostic struct {
	PeerID        int64      `json:"peer_id"`
	PeerURL       string     `json:"peer_url"`
	EventID       string     `json:"event_id"`
	EventType     string     `json:"event_type"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	LastError     string     `json:"last_error"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ValidationTaskDiagnostic struct {
	TaskID          int64      `json:"task_id"`
	ManifestID      string     `json:"manifest_id"`
	ReleaseID       string     `json:"release_id"`
	SourceNodeID    string     `json:"source_node_id"`
	SourceEventID   string     `json:"source_event_id"`
	PoolID          string     `json:"pool_id"`
	Status          string     `json:"status"`
	Priority        int        `json:"priority"`
	Attempts        int        `json:"attempts"`
	LastError       string     `json:"last_error"`
	ClaimedByNodeID string     `json:"claimed_by_node_id"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	DueAt           time.Time  `json:"due_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (s *Store) ListFederationPeerDiagnostics(ctx context.Context, limit int) ([]FederationPeerDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, COALESCE(p.node_id, ''), p.peer_url, p.source, p.enabled,
		       p.status, COALESCE(c.cursor, ''), COALESCE(c.last_event_id, ''),
		       p.failure_count, COALESCE(p.last_error, ''),
		       p.last_connected_at, p.last_sync_at, p.updated_at
		FROM federation_peers p
		LEFT JOIN federation_peer_cursors c
		  ON c.peer_id = p.id
		 AND c.pool_id = ''
		 AND c.event_type = 'ReleaseCard'
		ORDER BY p.updated_at DESC, p.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list federation peer diagnostics: %w", err)
	}
	defer rows.Close()
	out := []FederationPeerDiagnostic{}
	for rows.Next() {
		var item FederationPeerDiagnostic
		var lastConnectedAt, lastSyncAt nullableTime
		if err := rows.Scan(&item.ID, &item.NodeID, &item.PeerURL, &item.Source, &item.Enabled, &item.Status, &item.Cursor, &item.LastEventID, &item.FailureCount, &item.LastError, &lastConnectedAt, &lastSyncAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.LastConnectedAt = lastConnectedAt.ptr()
		item.LastSyncAt = lastSyncAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListFederationEventDiagnostics(ctx context.Context, limit int) ([]FederationEventDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, event_type, author_node_id, sequence, body_hash,
		       pool_ids, visibility, created_at, received_at, validation_status,
		       COALESCE(rejection_reason, ''), projected, projected_at
		FROM federation_events
		ORDER BY received_at DESC, event_id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list federation event diagnostics: %w", err)
	}
	defer rows.Close()
	out := []FederationEventDiagnostic{}
	for rows.Next() {
		var item FederationEventDiagnostic
		var poolIDs []byte
		var projectedAt nullableTime
		if err := rows.Scan(&item.EventID, &item.EventType, &item.AuthorNodeID, &item.Sequence, &item.BodyHash, &poolIDs, &item.Visibility, &item.CreatedAt, &item.ReceivedAt, &item.ValidationStatus, &item.RejectionReason, &item.Projected, &projectedAt); err != nil {
			return nil, err
		}
		item.PoolIDs = defaultRawJSON(poolIDs, `[]`)
		item.ProjectedAt = projectedAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListFederationRejectedEventDiagnostics(ctx context.Context, limit int) ([]FederationRejectedEventDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(event_id, ''), COALESCE(author_node_id, ''),
		       COALESCE(event_type, ''), rejection_reason, received_at
		FROM federation_rejected_events
		ORDER BY received_at DESC, id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list rejected federation event diagnostics: %w", err)
	}
	defer rows.Close()
	out := []FederationRejectedEventDiagnostic{}
	for rows.Next() {
		var item FederationRejectedEventDiagnostic
		if err := rows.Scan(&item.ID, &item.EventID, &item.AuthorNodeID, &item.EventType, &item.RejectionReason, &item.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListFederationPeerDeliveryDiagnostics(ctx context.Context, limit int) ([]FederationPeerDeliveryDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.peer_id, p.peer_url, d.event_id, COALESCE(e.event_type, ''),
		       d.status, d.attempts, d.last_attempt_at, d.delivered_at,
		       COALESCE(d.last_error, ''), d.updated_at
		FROM federation_peer_deliveries d
		JOIN federation_peers p ON p.id = d.peer_id
		LEFT JOIN federation_events e ON e.event_id = d.event_id
		ORDER BY d.updated_at DESC, d.peer_id DESC, d.event_id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list federation peer delivery diagnostics: %w", err)
	}
	defer rows.Close()
	out := []FederationPeerDeliveryDiagnostic{}
	for rows.Next() {
		var item FederationPeerDeliveryDiagnostic
		var lastAttemptAt, deliveredAt nullableTime
		if err := rows.Scan(&item.PeerID, &item.PeerURL, &item.EventID, &item.EventType, &item.Status, &item.Attempts, &lastAttemptAt, &deliveredAt, &item.LastError, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.LastAttemptAt = lastAttemptAt.ptr()
		item.DeliveredAt = deliveredAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListValidationTaskDiagnostics(ctx context.Context, limit int) ([]ValidationTaskDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, manifest_id, release_id, COALESCE(source_node_id, ''),
		       COALESCE(source_event_id, ''), pool_id, status, priority,
		       attempts, COALESCE(last_error, ''), COALESCE(claimed_by_node_id, ''),
		       claimed_at, due_at, completed_at, created_at, updated_at
		FROM federation_validation_tasks
		ORDER BY updated_at DESC, task_id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list validation task diagnostics: %w", err)
	}
	defer rows.Close()
	out := []ValidationTaskDiagnostic{}
	for rows.Next() {
		var item ValidationTaskDiagnostic
		var claimedAt, completedAt nullableTime
		if err := rows.Scan(&item.TaskID, &item.ManifestID, &item.ReleaseID, &item.SourceNodeID, &item.SourceEventID, &item.PoolID, &item.Status, &item.Priority, &item.Attempts, &item.LastError, &item.ClaimedByNodeID, &claimedAt, &item.DueAt, &completedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ClaimedAt = claimedAt.ptr()
		item.CompletedAt = completedAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func clampDiagnosticsLimit(limit, fallback int) int {
	if fallback <= 0 {
		fallback = 100
	}
	if limit <= 0 {
		return fallback
	}
	return min(limit, 500)
}

func defaultRawJSON(raw []byte, fallback string) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(strings.TrimSpace(fallback))
	}
	return json.RawMessage(raw)
}
