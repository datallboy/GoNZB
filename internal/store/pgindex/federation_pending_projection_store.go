package pgindex

import (
	"context"
	"fmt"
	"strings"
)

type PendingFederationProjection struct {
	EventID        string
	EventType      string
	ProjectionKind string
	Status         string
	Attempts       int
	LastError      string
}

func (s *Store) RecordFederationProjectionFailure(ctx context.Context, eventID, eventType, projectionKind string, cause error) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	reason := "projection failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_pending_projections
			(event_id, event_type, projection_kind, status, attempts, last_error, first_seen_at, last_attempt_at)
		VALUES ($1, $2, $3, 'pending', 1, $4, NOW(), NOW())
		ON CONFLICT (event_id) DO UPDATE SET
			status = 'pending', attempts = federation_pending_projections.attempts + 1,
			last_error = EXCLUDED.last_error, last_attempt_at = NOW(), resolved_at = NULL`,
		strings.TrimSpace(eventID), strings.TrimSpace(eventType), strings.TrimSpace(projectionKind), reason)
	if err != nil {
		return fmt.Errorf("record pending federation projection: %w", err)
	}
	return nil
}

func (s *Store) ResolveFederationProjection(ctx context.Context, eventID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE federation_pending_projections
		SET status = 'resolved', resolved_at = NOW(), last_attempt_at = NOW()
		WHERE event_id = $1`, strings.TrimSpace(eventID))
	return err
}

func (s *Store) ListPendingFederationProjections(ctx context.Context, limit int) ([]PendingFederationProjection, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, event_type, projection_kind, status, attempts, COALESCE(last_error, '')
		FROM federation_pending_projections
		WHERE status = 'pending'
		ORDER BY last_attempt_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending federation projections: %w", err)
	}
	defer rows.Close()
	var out []PendingFederationProjection
	for rows.Next() {
		var item PendingFederationProjection
		if err := rows.Scan(&item.EventID, &item.EventType, &item.ProjectionKind, &item.Status, &item.Attempts, &item.LastError); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
