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
