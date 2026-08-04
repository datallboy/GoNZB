package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

type federationChainLink struct {
	EventID         string
	AuthorNodeID    string
	Sequence        int64
	PreviousEventID string
}

type federationChainState struct {
	SameSequence      *federationChainLink
	Predecessor       *federationChainLink
	Successor         *federationChainLink
	PreviousReference *federationChainLink
}

type federationChainDecision struct {
	Gap              bool
	ExpectedSequence int64
	Conflict         *federationChainLink
	Reason           string
}

func validateFederationChain(event *events.SignedEvent, state federationChainState) (federationChainDecision, error) {
	var decision federationChainDecision
	if event == nil {
		return decision, fmt.Errorf("event is required")
	}
	if event.Sequence <= 0 {
		return decision, fmt.Errorf("sequence must be positive")
	}
	if state.SameSequence != nil && state.SameSequence.EventID != event.EventID {
		decision.Conflict = state.SameSequence
		decision.Reason = "different event already exists at author sequence"
		return decision, fmt.Errorf("%w: %s", ErrFederationSequenceConflict, decision.Reason)
	}

	previousEventID := optionalEventID(event.PreviousEventID)
	if event.Sequence == 1 {
		if previousEventID != "" {
			decision.Reason = "first author event must not name a predecessor"
			return decision, fmt.Errorf("%w: %s", ErrFederationForkDetected, decision.Reason)
		}
	} else if state.Predecessor != nil {
		if previousEventID != state.Predecessor.EventID {
			decision.Conflict = state.Predecessor
			decision.Reason = "previous_event_id does not match known predecessor"
			return decision, fmt.Errorf("%w: %s", ErrFederationForkDetected, decision.Reason)
		}
	} else {
		decision.Gap = true
		decision.ExpectedSequence = event.Sequence - 1
		decision.Reason = "immediate predecessor is not available"
		if state.PreviousReference != nil && (state.PreviousReference.AuthorNodeID != event.AuthorNodeID || state.PreviousReference.Sequence != event.Sequence-1) {
			decision.Conflict = state.PreviousReference
			decision.Gap = false
			decision.Reason = "previous_event_id references a non-predecessor event"
			return decision, fmt.Errorf("%w: %s", ErrFederationForkDetected, decision.Reason)
		}
	}

	if state.Successor != nil && state.Successor.PreviousEventID != event.EventID {
		decision.Conflict = state.Successor
		decision.Gap = false
		decision.Reason = "known successor does not reference this event"
		return decision, fmt.Errorf("%w: %s", ErrFederationForkDetected, decision.Reason)
	}
	return decision, nil
}

func optionalEventID(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func loadFederationChainState(ctx context.Context, tx *sql.Tx, event *events.SignedEvent) (federationChainState, error) {
	var state federationChainState
	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, author_node_id, sequence, COALESCE(previous_event_id, '')
		FROM federation_events
		WHERE author_node_id = $1
		  AND sequence BETWEEN $2 - 1 AND $2 + 1
		  AND validation_status = 'accepted'`, event.AuthorNodeID, event.Sequence)
	if err != nil {
		return state, fmt.Errorf("read federation event chain neighbors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		link := &federationChainLink{}
		if err := rows.Scan(&link.EventID, &link.AuthorNodeID, &link.Sequence, &link.PreviousEventID); err != nil {
			return state, fmt.Errorf("scan federation event chain neighbor: %w", err)
		}
		switch link.Sequence {
		case event.Sequence - 1:
			state.Predecessor = link
		case event.Sequence:
			state.SameSequence = link
		case event.Sequence + 1:
			state.Successor = link
		}
	}
	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("iterate federation event chain neighbors: %w", err)
	}

	previousEventID := optionalEventID(event.PreviousEventID)
	if state.Predecessor == nil && previousEventID != "" {
		link := &federationChainLink{}
		err := tx.QueryRowContext(ctx, `
			SELECT event_id, author_node_id, sequence, COALESCE(previous_event_id, '')
			FROM federation_events
			WHERE event_id = $1
			  AND validation_status = 'accepted'`, previousEventID).Scan(
			&link.EventID,
			&link.AuthorNodeID,
			&link.Sequence,
			&link.PreviousEventID,
		)
		if err != nil && err != sql.ErrNoRows {
			return state, fmt.Errorf("read previous federation event reference: %w", err)
		}
		if err == nil {
			state.PreviousReference = link
		}
	}
	return state, nil
}

func recordFederationChainIssue(ctx context.Context, tx *sql.Tx, event *events.SignedEvent, decision federationChainDecision, issueType string) error {
	rawEventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal federation chain issue event: %w", err)
	}
	var conflictingEventID any
	if decision.Conflict != nil {
		conflictingEventID = decision.Conflict.EventID
	}
	var expectedSequence any
	if decision.ExpectedSequence > 0 {
		expectedSequence = decision.ExpectedSequence
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO federation_event_chain_issues (
			author_node_id, event_id, issue_type, conflicting_event_id,
			expected_sequence, observed_sequence, observed_previous_event_id, details,
			raw_event_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
		ON CONFLICT (author_node_id, event_id, issue_type) DO UPDATE SET
			conflicting_event_id = EXCLUDED.conflicting_event_id,
			expected_sequence = EXCLUDED.expected_sequence,
			observed_sequence = EXCLUDED.observed_sequence,
			observed_previous_event_id = EXCLUDED.observed_previous_event_id,
			details = EXCLUDED.details,
			raw_event_json = EXCLUDED.raw_event_json,
			detected_at = NOW(),
			resolved_at = NULL`,
		event.AuthorNodeID,
		event.EventID,
		issueType,
		conflictingEventID,
		expectedSequence,
		event.Sequence,
		optionalEventID(event.PreviousEventID),
		decision.Reason,
		string(rawEventJSON),
	)
	if err != nil {
		return fmt.Errorf("record federation event chain issue: %w", err)
	}
	return nil
}
