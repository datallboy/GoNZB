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

type FederationRejectedEventSummary struct {
	AuthorNodeID    string    `json:"author_node_id"`
	RejectionReason string    `json:"rejection_reason"`
	Total           int       `json:"total"`
	LastHour        int       `json:"last_hour"`
	LastDay         int       `json:"last_day"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
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

type FederatedReleaseSourceDiagnostic struct {
	ReleaseID               string     `json:"release_id"`
	ManifestID              string     `json:"manifest_id"`
	Title                   string     `json:"title"`
	SourceNodeID            string     `json:"source_node_id"`
	SourceEventID           string     `json:"source_event_id"`
	PoolID                  string     `json:"pool_id"`
	TrustScore              float64    `json:"trust_score"`
	AvailabilityScore       float64    `json:"availability_score"`
	ManifestConfidenceScore float64    `json:"manifest_confidence_score"`
	Resolvable              bool       `json:"resolvable"`
	PostedAt                *time.Time `json:"posted_at,omitempty"`
	FirstSeenAt             time.Time  `json:"first_seen_at"`
	LastSeenAt              time.Time  `json:"last_seen_at"`
}

type FederatedManifestSourceDiagnostic struct {
	ManifestID    string     `json:"manifest_id"`
	ReleaseID     string     `json:"release_id"`
	SourceNodeID  string     `json:"source_node_id"`
	PoolID        string     `json:"pool_id"`
	Advertised    bool       `json:"advertised"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	FailureCount  int        `json:"failure_count"`
	AvgLatencyMS  int        `json:"avg_latency_ms"`
	TrustScore    float64    `json:"trust_score"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type HealthAttestationDiagnostic struct {
	AttestationID         string    `json:"attestation_id"`
	ManifestID            string    `json:"manifest_id"`
	ReleaseID             string    `json:"release_id"`
	AuthorNodeID          string    `json:"author_node_id"`
	PoolID                string    `json:"pool_id"`
	CheckedAt             time.Time `json:"checked_at"`
	Status                string    `json:"status"`
	ArticlesTotal         int       `json:"articles_total"`
	ArticlesAvailable     int       `json:"articles_available"`
	MissingArticles       int       `json:"missing_articles"`
	RepairAvailable       bool      `json:"repair_available"`
	RepairConfidence      float64   `json:"repair_confidence"`
	RetentionDaysObserved int       `json:"retention_days_observed"`
	Confidence            float64   `json:"confidence"`
	AvailabilityScore     float64   `json:"availability_score"`
	Method                string    `json:"method"`
	SourceEventID         string    `json:"source_event_id"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ArticleAvailabilityDiagnostic struct {
	AttestationID         string    `json:"attestation_id"`
	ManifestID            string    `json:"manifest_id"`
	ReleaseID             string    `json:"release_id"`
	AuthorNodeID          string    `json:"author_node_id"`
	PoolID                string    `json:"pool_id"`
	CheckedAt             time.Time `json:"checked_at"`
	Status                string    `json:"status"`
	ArticlesTotal         int       `json:"articles_total"`
	ArticlesAvailable     int       `json:"articles_available"`
	MissingArticles       int       `json:"missing_articles"`
	RetentionDaysObserved int       `json:"retention_days_observed"`
	Confidence            float64   `json:"confidence"`
	ValidationScore       float64   `json:"validation_score"`
	Method                string    `json:"method"`
	SourceEventID         string    `json:"source_event_id"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ReputationDiagnostic struct {
	ID              int64     `json:"id"`
	NodeID          string    `json:"node_id"`
	PoolID          string    `json:"pool_id"`
	EventID         string    `json:"event_id"`
	Delta           float64   `json:"delta"`
	Reason          string    `json:"reason"`
	LocalTrustScore float64   `json:"local_trust_score"`
	CreatedAt       time.Time `json:"created_at"`
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

func (s *Store) ListFederationRejectedEventSummary(ctx context.Context, limit int) ([]FederationRejectedEventSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(author_node_id, ''), rejection_reason,
		       COUNT(*)::integer,
		       COUNT(*) FILTER (WHERE received_at >= now() - interval '1 hour')::integer,
		       COUNT(*) FILTER (WHERE received_at >= now() - interval '1 day')::integer,
		       MIN(received_at), MAX(received_at)
		FROM federation_rejected_events
		GROUP BY COALESCE(author_node_id, ''), rejection_reason
		ORDER BY COUNT(*) FILTER (WHERE received_at >= now() - interval '1 day') DESC,
		         COUNT(*) DESC,
		         MAX(received_at) DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list rejected federation event summary: %w", err)
	}
	defer rows.Close()
	out := []FederationRejectedEventSummary{}
	for rows.Next() {
		var item FederationRejectedEventSummary
		if err := rows.Scan(&item.AuthorNodeID, &item.RejectionReason, &item.Total, &item.LastHour, &item.LastDay, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
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

func (s *Store) ListFederatedReleaseSourceDiagnostics(ctx context.Context, poolID string, limit int) ([]FederatedReleaseSourceDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	poolID = strings.TrimSpace(poolID)
	clauses := []string{"1=1"}
	args := []any{}
	arg := 1
	if poolID != "" {
		clauses = append(clauses, fmt.Sprintf("s.pool_id = $%d", arg))
		args = append(args, poolID)
		arg++
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT s.release_id, COALESCE(s.manifest_id, ''), c.title,
		       s.source_node_id, s.source_event_id, s.pool_id, s.trust_score,
		       s.availability_score, s.manifest_confidence_score, s.resolvable,
		       c.posted_at, s.first_seen_at, s.last_seen_at
		FROM federated_release_sources s
		JOIN federated_release_cards c ON c.release_id = s.release_id
		WHERE %s
		ORDER BY s.last_seen_at DESC, s.release_id
		LIMIT $%d`, strings.Join(clauses, " AND "), arg), args...)
	if err != nil {
		return nil, fmt.Errorf("list federated release source diagnostics: %w", err)
	}
	defer rows.Close()
	out := []FederatedReleaseSourceDiagnostic{}
	for rows.Next() {
		var item FederatedReleaseSourceDiagnostic
		var postedAt nullableTime
		if err := rows.Scan(&item.ReleaseID, &item.ManifestID, &item.Title, &item.SourceNodeID, &item.SourceEventID, &item.PoolID, &item.TrustScore, &item.AvailabilityScore, &item.ManifestConfidenceScore, &item.Resolvable, &postedAt, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
			return nil, err
		}
		item.PostedAt = postedAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListFederatedManifestSourceDiagnostics(ctx context.Context, poolID string, limit int) ([]FederatedManifestSourceDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	poolID = strings.TrimSpace(poolID)
	clauses := []string{"1=1"}
	args := []any{}
	arg := 1
	if poolID != "" {
		clauses = append(clauses, fmt.Sprintf("pool_id = $%d", arg))
		args = append(args, poolID)
		arg++
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT manifest_id, COALESCE(release_id, ''), source_node_id, pool_id,
		       advertised, last_success_at, last_failure_at, failure_count,
		       COALESCE(avg_latency_ms, 0), trust_score, updated_at
		FROM federated_manifest_sources
		WHERE %s
		ORDER BY updated_at DESC, manifest_id
		LIMIT $%d`, strings.Join(clauses, " AND "), arg), args...)
	if err != nil {
		return nil, fmt.Errorf("list federated manifest source diagnostics: %w", err)
	}
	defer rows.Close()
	out := []FederatedManifestSourceDiagnostic{}
	for rows.Next() {
		var item FederatedManifestSourceDiagnostic
		var lastSuccessAt, lastFailureAt nullableTime
		if err := rows.Scan(&item.ManifestID, &item.ReleaseID, &item.SourceNodeID, &item.PoolID, &item.Advertised, &lastSuccessAt, &lastFailureAt, &item.FailureCount, &item.AvgLatencyMS, &item.TrustScore, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.LastSuccessAt = lastSuccessAt.ptr()
		item.LastFailureAt = lastFailureAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListHealthAttestationDiagnostics(ctx context.Context, poolID string, limit int) ([]HealthAttestationDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	poolID = strings.TrimSpace(poolID)
	clauses := []string{"1=1"}
	args := []any{}
	arg := 1
	if poolID != "" {
		clauses = append(clauses, fmt.Sprintf("pool_id = $%d", arg))
		args = append(args, poolID)
		arg++
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT attestation_id, COALESCE(manifest_id, ''), release_id,
		       author_node_id, COALESCE(pool_id, ''), checked_at, status,
		       COALESCE(articles_total, 0), COALESCE(articles_available, 0),
		       COALESCE(missing_articles, 0), COALESCE(repair_available, FALSE),
		       repair_confidence, COALESCE(retention_days_observed, 0),
		       confidence, availability_score, COALESCE(method, ''),
		       source_event_id, updated_at
		FROM health_attestations
		WHERE %s
		ORDER BY checked_at DESC, attestation_id
		LIMIT $%d`, strings.Join(clauses, " AND "), arg), args...)
	if err != nil {
		return nil, fmt.Errorf("list health attestation diagnostics: %w", err)
	}
	defer rows.Close()
	out := []HealthAttestationDiagnostic{}
	for rows.Next() {
		var item HealthAttestationDiagnostic
		if err := rows.Scan(&item.AttestationID, &item.ManifestID, &item.ReleaseID, &item.AuthorNodeID, &item.PoolID, &item.CheckedAt, &item.Status, &item.ArticlesTotal, &item.ArticlesAvailable, &item.MissingArticles, &item.RepairAvailable, &item.RepairConfidence, &item.RetentionDaysObserved, &item.Confidence, &item.AvailabilityScore, &item.Method, &item.SourceEventID, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListArticleAvailabilityDiagnostics(ctx context.Context, poolID string, limit int) ([]ArticleAvailabilityDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	args := []any{limit}
	filter := ""
	if poolID = strings.TrimSpace(poolID); poolID != "" {
		filter = "WHERE pool_id = $2"
		args = append(args, poolID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT attestation_id, manifest_id, release_id, author_node_id, pool_id,
		       checked_at, status, articles_total, articles_available,
		       missing_articles, retention_days_observed, confidence,
		       validation_score, COALESCE(method, ''), source_event_id, updated_at
		FROM article_availability_attestations
		`+filter+`
		ORDER BY checked_at DESC, attestation_id
		LIMIT $1`, args...)
	if err != nil {
		return nil, fmt.Errorf("list article availability diagnostics: %w", err)
	}
	defer rows.Close()
	out := make([]ArticleAvailabilityDiagnostic, 0)
	for rows.Next() {
		var item ArticleAvailabilityDiagnostic
		if err := rows.Scan(
			&item.AttestationID, &item.ManifestID, &item.ReleaseID, &item.AuthorNodeID,
			&item.PoolID, &item.CheckedAt, &item.Status, &item.ArticlesTotal,
			&item.ArticlesAvailable, &item.MissingArticles, &item.RetentionDaysObserved,
			&item.Confidence, &item.ValidationScore, &item.Method, &item.SourceEventID,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListReputationDiagnostics(ctx context.Context, limit int) ([]ReputationDiagnostic, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	limit = clampDiagnosticsLimit(limit, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.node_id, COALESCE(r.pool_id, ''), COALESCE(r.event_id, ''),
		       r.delta, r.reason, COALESCE(n.local_trust_score, 0), r.created_at
		FROM reputation_events r
		JOIN federation_nodes n ON n.node_id = r.node_id
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list reputation diagnostics: %w", err)
	}
	defer rows.Close()
	out := []ReputationDiagnostic{}
	for rows.Next() {
		var item ReputationDiagnostic
		if err := rows.Scan(&item.ID, &item.NodeID, &item.PoolID, &item.EventID, &item.Delta, &item.Reason, &item.LocalTrustScore, &item.CreatedAt); err != nil {
			return nil, err
		}
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
