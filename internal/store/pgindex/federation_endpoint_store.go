package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
)

type FederationNodeEndpoint struct {
	ID             int64      `json:"id"`
	NodeID         string     `json:"node_id"`
	TransportType  string     `json:"transport_type"`
	Locator        string     `json:"locator"`
	Priority       int        `json:"priority"`
	Enabled        bool       `json:"enabled"`
	PathType       string     `json:"path_type,omitempty"`
	ICEState       string     `json:"ice_state,omitempty"`
	RTTMS          int64      `json:"rtt_ms"`
	ReconnectCount int64      `json:"reconnect_count"`
	BytesSent      int64      `json:"bytes_sent"`
	BytesReceived  int64      `json:"bytes_received"`
	FailureCount   int        `json:"failure_count"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func endpointTransport(locator string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(locator))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("endpoint locator must be an absolute URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		return "https", nil
	case "gonzb+ice":
		return "ice", nil
	default:
		return "", fmt.Errorf("unsupported endpoint transport %q", parsed.Scheme)
	}
}

func (s *Store) UpsertFederationNodeEndpoint(ctx context.Context, endpoint FederationNodeEndpoint) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	return upsertFederationNodeEndpoint(ctx, s.federationExecutor(ctx), endpoint)
}

func upsertFederationNodeEndpoint(ctx context.Context, executor federationDBTX, endpoint FederationNodeEndpoint) error {
	endpoint.NodeID = strings.TrimSpace(endpoint.NodeID)
	endpoint.Locator = strings.TrimRight(strings.TrimSpace(endpoint.Locator), "/")
	if endpoint.NodeID == "" || endpoint.Locator == "" {
		return fmt.Errorf("endpoint node_id and locator are required")
	}
	transport, err := endpointTransport(endpoint.Locator)
	if err != nil {
		return err
	}
	if endpoint.TransportType != "" && endpoint.TransportType != transport {
		return fmt.Errorf("endpoint transport does not match locator")
	}
	if transport == "ice" {
		parsed, err := transportpolicy.ParseLocator(endpoint.Locator, false, true)
		if err != nil {
			return err
		}
		if parsed.NodeID != strings.ToLower(endpoint.NodeID) {
			return fmt.Errorf("traversal endpoint node ID does not match endpoint owner")
		}
	}
	if endpoint.Priority < 0 {
		return fmt.Errorf("endpoint priority must be non-negative")
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO federation_node_endpoints (
			node_id, transport_type, locator, priority, enabled, updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (node_id, locator) DO UPDATE SET
			transport_type = EXCLUDED.transport_type,
			priority = EXCLUDED.priority,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()`,
		endpoint.NodeID, transport, endpoint.Locator, endpoint.Priority, endpoint.Enabled)
	if err != nil {
		return fmt.Errorf("upsert federation node endpoint: %w", err)
	}
	return nil
}

func (s *Store) ListFederationNodeEndpoints(ctx context.Context, nodeID string, enabledOnly bool) ([]FederationNodeEndpoint, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_id, transport_type, locator, priority, enabled,
		       path_type, ice_state, rtt_ms, reconnect_count, bytes_sent,
		       bytes_received, failure_count, last_success_at, last_failure_at,
		       last_error, updated_at
		FROM federation_node_endpoints
		WHERE node_id = $1 AND (NOT $2 OR enabled = TRUE)
		ORDER BY priority, transport_type, locator`, strings.TrimSpace(nodeID), enabledOnly)
	if err != nil {
		return nil, fmt.Errorf("list federation node endpoints: %w", err)
	}
	defer rows.Close()
	items := make([]FederationNodeEndpoint, 0)
	for rows.Next() {
		var item FederationNodeEndpoint
		if err := rows.Scan(
			&item.ID, &item.NodeID, &item.TransportType, &item.Locator,
			&item.Priority, &item.Enabled, &item.PathType, &item.ICEState,
			&item.RTTMS, &item.ReconnectCount, &item.BytesSent,
			&item.BytesReceived, &item.FailureCount, &item.LastSuccessAt,
			&item.LastFailureAt, &item.LastError, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ResolveFederationNodeEndpoint(ctx context.Context, nodeID string) (*FederationNodeEndpoint, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	preferDirect, traversalEnabled, coordinatorHosts := s.endpointPolicy()
	var item FederationNodeEndpoint
	err := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, transport_type, locator, priority, enabled,
		       path_type, ice_state, rtt_ms, reconnect_count, bytes_sent,
		       bytes_received, failure_count, last_success_at, last_failure_at,
		       last_error, updated_at
		FROM federation_node_endpoints
		WHERE node_id = $1
		  AND enabled = TRUE
		  AND ($2 OR transport_type <> 'ice')
		  AND (transport_type <> 'ice' OR lower(split_part(split_part(locator, '@', 2), '/', 1)) = ANY(string_to_array($4, ',')))
		ORDER BY
		  CASE
		    WHEN last_failure_at IS NOT NULL
		     AND (last_success_at IS NULL OR last_failure_at > last_success_at)
		      THEN CASE WHEN ($3 AND transport_type = 'https') OR (NOT $3 AND transport_type = 'ice') THEN 2 ELSE 3 END
		    WHEN ($3 AND transport_type = 'https') OR (NOT $3 AND transport_type = 'ice') THEN 0
		    ELSE 1
		  END,
		  priority,
		  last_success_at DESC NULLS LAST,
		  locator
		LIMIT 1`, strings.TrimSpace(nodeID), traversalEnabled, preferDirect, coordinatorHosts).Scan(
		&item.ID, &item.NodeID, &item.TransportType, &item.Locator,
		&item.Priority, &item.Enabled, &item.PathType, &item.ICEState,
		&item.RTTMS, &item.ReconnectCount, &item.BytesSent,
		&item.BytesReceived, &item.FailureCount, &item.LastSuccessAt,
		&item.LastFailureAt, &item.LastError, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve federation node endpoint: %w", err)
	}
	return &item, nil
}

func (s *Store) RecordFederationNodeEndpointResult(ctx context.Context, nodeID, locator string, success bool, pathType, iceState string, rttMS, reconnects, bytesSent, bytesReceived int64, errText string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if success {
		_, err := s.db.ExecContext(ctx, `
			UPDATE federation_node_endpoints
			SET last_success_at = NOW(), path_type = $3, ice_state = $4,
			    rtt_ms = GREATEST($5, 0), reconnect_count = GREATEST(reconnect_count, $6),
			    bytes_sent = bytes_sent + GREATEST($7, 0),
			    bytes_received = bytes_received + GREATEST($8, 0), last_error = '',
			    updated_at = NOW()
			WHERE node_id = $1 AND locator = $2`,
			strings.TrimSpace(nodeID), strings.TrimRight(strings.TrimSpace(locator), "/"),
			strings.TrimSpace(pathType), strings.TrimSpace(iceState), rttMS, reconnects, bytesSent, bytesReceived)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE federation_node_endpoints
		SET last_failure_at = NOW(), failure_count = failure_count + 1,
		    ice_state = $3, reconnect_count = GREATEST(reconnect_count, $4),
		    last_error = left($5, 2048), updated_at = NOW()
		WHERE node_id = $1 AND locator = $2`,
		strings.TrimSpace(nodeID), strings.TrimRight(strings.TrimSpace(locator), "/"),
		strings.TrimSpace(iceState), reconnects, strings.TrimSpace(errText))
	return err
}

func (s *Store) RecordFederationEndpointResultByLocator(ctx context.Context, locator string, success bool, rttMS, bytesSent, bytesReceived int64, errText string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	locator = strings.TrimRight(strings.TrimSpace(locator), "/")
	if success {
		_, err := s.db.ExecContext(ctx, `
			UPDATE federation_node_endpoints
			SET last_success_at = NOW(), path_type = 'direct', ice_state = '',
			    rtt_ms = GREATEST($2, 0), bytes_sent = bytes_sent + GREATEST($3, 0),
			    bytes_received = bytes_received + GREATEST($4, 0), last_error = '',
			    updated_at = NOW()
			WHERE locator = $1 AND transport_type = 'https'`, locator, rttMS, bytesSent, bytesReceived)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE federation_node_endpoints
		SET last_failure_at = NOW(), failure_count = failure_count + 1,
		    last_error = left($2, 2048), updated_at = NOW()
		WHERE locator = $1 AND transport_type = 'https'`, locator, strings.TrimSpace(errText))
	return err
}
