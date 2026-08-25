package pgindex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/publicationstate"
)

// ProjectReleasePublicationState applies only to the event author's source.
// Sequence ordering makes replay and out-of-order sync deterministic.
func (s *Store) ProjectReleasePublicationState(ctx context.Context, projection publicationstate.Projection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	item := projection.Publication
	changedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.ChangedAt))
	if err != nil {
		return fmt.Errorf("parse publication changed_at: %w", err)
	}
	tx, commit, rollback, err := s.beginFederationProjection(ctx)
	if err != nil {
		return err
	}
	defer rollback()
	var sourceExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM federated_release_sources
			WHERE release_id = $1 AND source_node_id = $2 AND pool_id = $3
			  AND COALESCE(manifest_id, '') = COALESCE(NULLIF($4, ''), '')
		)`, item.ReleaseID, projection.AuthorNodeID, item.PoolID, item.ManifestID).Scan(&sourceExists); err != nil {
		return fmt.Errorf("verify publication source ownership: %w", err)
	}
	if !sourceExists {
		return fmt.Errorf("release publication state author does not own the projected release source")
	}
	var currentEventID string
	var currentSequence int64
	err = tx.QueryRowContext(ctx, `
		SELECT source_event_id, source_sequence FROM federated_release_publication_states
		WHERE release_id = $1 AND source_node_id = $2 AND pool_id = $3`,
		item.ReleaseID, projection.AuthorNodeID, item.PoolID).Scan(&currentEventID, &currentSequence)
	if errors.Is(err, sql.ErrNoRows) {
		if strings.TrimSpace(item.SupersedesEventID) != "" {
			return fmt.Errorf("release publication state supersedes an unknown event")
		}
	} else if err != nil {
		return fmt.Errorf("read current release publication state: %w", err)
	} else if projection.Sequence <= currentSequence {
		// Replays and out-of-order deliveries are successful no-ops. Requiring
		// their supersession link to match the current (newer) event would leave
		// a valid older event stuck in the projection retry queue forever.
		return commit()
	} else if strings.TrimSpace(item.SupersedesEventID) != currentEventID {
		return fmt.Errorf("release publication state does not supersede the current event")
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO federated_release_publication_states (
			release_id, source_node_id, pool_id, manifest_id, state, reason,
			effective_at, source_event_id, source_sequence, supersedes_event_id, updated_at
		) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, NULLIF($10, ''), NOW())
		ON CONFLICT (release_id, source_node_id, pool_id) DO UPDATE SET
			manifest_id = EXCLUDED.manifest_id,
			state = EXCLUDED.state,
			reason = EXCLUDED.reason,
			effective_at = EXCLUDED.effective_at,
			source_event_id = EXCLUDED.source_event_id,
			source_sequence = EXCLUDED.source_sequence,
			supersedes_event_id = EXCLUDED.supersedes_event_id,
			updated_at = NOW()
		WHERE EXCLUDED.source_sequence > federated_release_publication_states.source_sequence`,
		item.ReleaseID, projection.AuthorNodeID, item.PoolID, item.ManifestID, item.State,
		item.Reason, changedAt.UTC(), projection.EventID, projection.Sequence, item.SupersedesEventID,
	)
	if err != nil {
		return fmt.Errorf("project release publication state: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return commit()
	}
	advertised := item.State == publicationstate.StateActive
	if strings.TrimSpace(item.ManifestID) != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE federated_manifest_sources
			SET advertised = $1, updated_at = NOW()
			WHERE manifest_id = $2 AND release_id = $3 AND source_node_id = $4 AND pool_id = $5`,
			advertised, item.ManifestID, item.ReleaseID, projection.AuthorNodeID, item.PoolID,
		); err != nil {
			return fmt.Errorf("apply publication state to manifest source: %w", err)
		}
	}
	return commit()
}
