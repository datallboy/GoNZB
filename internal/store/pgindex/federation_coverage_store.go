package pgindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

type CoverageDashboard struct {
	Assignments []CoverageAssignmentRecord `json:"assignments"`
	Claims      []CoverageClaimRecord      `json:"claims"`
	StaleClaims []CoverageClaimRecord      `json:"stale_claims"`
	Outcomes    []CoverageOutcomeRecord    `json:"outcomes"`
}

type CoverageAssignmentRecord struct {
	AssignmentID   string     `json:"assignment_id"`
	PoolID         string     `json:"pool_id"`
	Group          string     `json:"group"`
	AssignedNodeID string     `json:"assigned_node_id"`
	RangeStart     int64      `json:"range_start,omitempty"`
	RangeEnd       int64      `json:"range_end,omitempty"`
	WindowStart    *time.Time `json:"window_start,omitempty"`
	WindowEnd      *time.Time `json:"window_end,omitempty"`
	Priority       int        `json:"priority"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
}

type CoverageClaimRecord struct {
	ClaimID      string    `json:"claim_id"`
	ClaimType    string    `json:"claim_type"`
	AssignmentID string    `json:"assignment_id,omitempty"`
	PoolID       string    `json:"pool_id"`
	Group        string    `json:"group"`
	NodeID       string    `json:"node_id"`
	RangeStart   int64     `json:"range_start,omitempty"`
	RangeEnd     int64     `json:"range_end,omitempty"`
	ClaimedAt    time.Time `json:"claimed_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Status       string    `json:"status"`
}

type CoverageOutcomeRecord struct {
	OutcomeID    string    `json:"outcome_id"`
	OutcomeType  string    `json:"outcome_type"`
	ClaimID      string    `json:"claim_id,omitempty"`
	AssignmentID string    `json:"assignment_id,omitempty"`
	PoolID       string    `json:"pool_id"`
	Group        string    `json:"group"`
	NodeID       string    `json:"node_id"`
	RangeStart   int64     `json:"range_start"`
	RangeEnd     int64     `json:"range_end"`
	ReleaseCount int       `json:"release_count,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

func (s *Store) ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if event == nil {
		return fmt.Errorf("event is required")
	}
	switch event.EventType {
	case coverage.TypeScannerCapacity:
		var body coverage.ScannerCapacity
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		bodyJSON, _ := json.Marshal(body)
		groupsJSON, _ := json.Marshal(body.Groups)
		publishedAt, _ := time.Parse(time.RFC3339, body.PublishedAt)
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO scanner_capacities (
				node_id, published_at, groups_json, max_ranges_per_hour,
				max_bytes_per_hour, body_json, source_event_id, updated_at
			)
			VALUES ($1, $2, $3::jsonb, $4, $5, $6::jsonb, $7, NOW())
			ON CONFLICT (node_id) DO UPDATE SET
				published_at = EXCLUDED.published_at,
				groups_json = EXCLUDED.groups_json,
				max_ranges_per_hour = EXCLUDED.max_ranges_per_hour,
				max_bytes_per_hour = EXCLUDED.max_bytes_per_hour,
				body_json = EXCLUDED.body_json,
				source_event_id = EXCLUDED.source_event_id,
				updated_at = NOW()`,
			body.NodeID, publishedAt.UTC(), string(groupsJSON), body.MaxRangesPerHour,
			body.MaxBytesPerHour, string(bodyJSON), event.EventID)
		return err
	case coverage.TypeGroupObservation:
		var body coverage.GroupObservation
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		bodyJSON, _ := json.Marshal(body)
		observedAt, _ := time.Parse(time.RFC3339, body.ObservedAt)
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO coverage_group_observations (
				observation_id, pool_id, group_name, observed_at, low_watermark,
				high_watermark, retention_days, confidence, author_node_id,
				body_json, source_event_id, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, NOW())
			ON CONFLICT (observation_id) DO UPDATE SET
				observed_at = EXCLUDED.observed_at,
				low_watermark = EXCLUDED.low_watermark,
				high_watermark = EXCLUDED.high_watermark,
				retention_days = EXCLUDED.retention_days,
				confidence = EXCLUDED.confidence,
				body_json = EXCLUDED.body_json,
				source_event_id = EXCLUDED.source_event_id,
				updated_at = NOW()`,
			body.ObservationID, body.PoolID, body.Group, observedAt.UTC(), body.LowWatermark,
			body.HighWatermark, body.RetentionDays, body.Confidence, event.AuthorNodeID,
			string(bodyJSON), event.EventID)
		return err
	case coverage.TypeCoveragePlan:
		var body coverage.CoveragePlan
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectCoveragePlan(ctx, body, event)
	case coverage.TypeCoverageAssignment:
		var body coverage.CoverageAssignment
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectCoverageAssignment(ctx, body, event)
	case coverage.TypeRangeClaim:
		var body coverage.RangeClaim
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectRangeClaim(ctx, body, event)
	case coverage.TypeTimeWindowClaim:
		var body coverage.TimeWindowClaim
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectTimeWindowClaim(ctx, body, event)
	case coverage.TypeCoverageCheckpoint:
		var body coverage.CoverageCheckpoint
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectCoverageCheckpoint(ctx, body, event)
	case coverage.TypeRangeComplete:
		var body coverage.RangeComplete
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectRangeComplete(ctx, body, event)
	case coverage.TypeRangeFailed:
		var body coverage.RangeFailed
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if err := coverage.Validate(event.EventType, body, time.Now().UTC(), 2*time.Minute); err != nil {
			return err
		}
		return s.projectRangeFailed(ctx, body, event)
	default:
		return nil
	}
}

func (s *Store) ListCoverageDashboard(ctx context.Context, poolID string) (CoverageDashboard, error) {
	var out CoverageDashboard
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		poolID = "pool.local"
	}
	assignments, err := s.listCoverageAssignments(ctx, poolID)
	if err != nil {
		return out, err
	}
	claims, err := s.listCoverageClaims(ctx, poolID, false)
	if err != nil {
		return out, err
	}
	staleClaims, err := s.listCoverageClaims(ctx, poolID, true)
	if err != nil {
		return out, err
	}
	outcomes, err := s.listCoverageOutcomes(ctx, poolID)
	if err != nil {
		return out, err
	}
	out.Assignments = assignments
	out.Claims = claims
	out.StaleClaims = staleClaims
	out.Outcomes = outcomes
	return out, nil
}

func (s *Store) projectCoveragePlan(ctx context.Context, body coverage.CoveragePlan, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	createdAt, _ := time.Parse(time.RFC3339, body.CreatedAt)
	windowStart := parseNullableTime(body.WindowStart)
	windowEnd := parseNullableTime(body.WindowEnd)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO coverage_plans (
			plan_id, pool_id, group_name, range_start, range_end, window_start,
			window_end, priority, author_node_id, body_json, source_event_id,
			created_at, updated_at
		)
		VALUES ($1, $2, $3, NULLIF($4, 0), NULLIF($5, 0), $6, $7, $8, $9, $10::jsonb, $11, $12, NOW())
		ON CONFLICT (plan_id) DO UPDATE SET
			priority = EXCLUDED.priority,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		body.PlanID, body.PoolID, body.Group, body.RangeStart, body.RangeEnd,
		windowStart, windowEnd, body.Priority, event.AuthorNodeID,
		string(bodyJSON), event.EventID, createdAt.UTC())
	return err
}

func (s *Store) projectCoverageAssignment(ctx context.Context, body coverage.CoverageAssignment, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	createdAt, _ := time.Parse(time.RFC3339, body.CreatedAt)
	windowStart := parseNullableTime(body.WindowStart)
	windowEnd := parseNullableTime(body.WindowEnd)
	dueAt := parseNullableTime(body.DueAt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO coverage_assignments (
			assignment_id, plan_id, pool_id, group_name, assigned_node_id,
			range_start, range_end, window_start, window_end, priority, due_at,
			status, author_node_id, body_json, source_event_id, created_at,
			updated_at
		)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, NULLIF($6, 0), NULLIF($7, 0),
		        $8, $9, $10, $11, 'assigned', $12, $13::jsonb, $14, $15, NOW())
		ON CONFLICT (assignment_id) DO UPDATE SET
			assigned_node_id = EXCLUDED.assigned_node_id,
			priority = EXCLUDED.priority,
			due_at = EXCLUDED.due_at,
			status = EXCLUDED.status,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		body.AssignmentID, body.PlanID, body.PoolID, body.Group, body.AssignedNodeID,
		body.RangeStart, body.RangeEnd, windowStart, windowEnd, body.Priority, dueAt,
		event.AuthorNodeID, string(bodyJSON), event.EventID, createdAt.UTC())
	return err
}

func (s *Store) projectRangeClaim(ctx context.Context, body coverage.RangeClaim, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	claimedAt, _ := time.Parse(time.RFC3339, body.ClaimedAt)
	expiresAt, _ := time.Parse(time.RFC3339, body.ExpiresAt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO coverage_claims (
			claim_id, claim_type, assignment_id, pool_id, group_name, node_id,
			range_start, range_end, claimed_at, expires_at, status, author_node_id,
			body_json, source_event_id, updated_at
		)
		VALUES ($1, 'range', NULLIF($2, ''), $3, $4, $5, $6, $7, $8, $9, 'active', $10, $11::jsonb, $12, NOW())
		ON CONFLICT (claim_id) DO UPDATE SET
			expires_at = EXCLUDED.expires_at,
			status = EXCLUDED.status,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		body.ClaimID, body.AssignmentID, body.PoolID, body.Group, body.NodeID,
		body.RangeStart, body.RangeEnd, claimedAt.UTC(), expiresAt.UTC(),
		event.AuthorNodeID, string(bodyJSON), event.EventID)
	return err
}

func (s *Store) projectTimeWindowClaim(ctx context.Context, body coverage.TimeWindowClaim, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	claimedAt, _ := time.Parse(time.RFC3339, body.ClaimedAt)
	expiresAt, _ := time.Parse(time.RFC3339, body.ExpiresAt)
	windowStart, _ := time.Parse(time.RFC3339, body.WindowStart)
	windowEnd, _ := time.Parse(time.RFC3339, body.WindowEnd)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO coverage_claims (
			claim_id, claim_type, assignment_id, pool_id, group_name, node_id,
			window_start, window_end, claimed_at, expires_at, status, author_node_id,
			body_json, source_event_id, updated_at
		)
		VALUES ($1, 'time_window', NULLIF($2, ''), $3, $4, $5, $6, $7, $8, $9, 'active', $10, $11::jsonb, $12, NOW())
		ON CONFLICT (claim_id) DO UPDATE SET
			expires_at = EXCLUDED.expires_at,
			status = EXCLUDED.status,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		body.ClaimID, body.AssignmentID, body.PoolID, body.Group, body.NodeID,
		windowStart.UTC(), windowEnd.UTC(), claimedAt.UTC(), expiresAt.UTC(),
		event.AuthorNodeID, string(bodyJSON), event.EventID)
	return err
}

func (s *Store) projectCoverageCheckpoint(ctx context.Context, body coverage.CoverageCheckpoint, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	createdAt, _ := time.Parse(time.RFC3339, body.CreatedAt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO coverage_checkpoints (
			checkpoint_id, pool_id, group_name, low_watermark, high_watermark,
			author_node_id, body_json, source_event_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, NOW())
		ON CONFLICT (checkpoint_id) DO UPDATE SET
			low_watermark = EXCLUDED.low_watermark,
			high_watermark = EXCLUDED.high_watermark,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		body.CheckpointID, body.PoolID, body.Group, body.LowWatermark,
		body.HighWatermark, event.AuthorNodeID, string(bodyJSON), event.EventID,
		createdAt.UTC())
	return err
}

func (s *Store) projectRangeComplete(ctx context.Context, body coverage.RangeComplete, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	completedAt, _ := time.Parse(time.RFC3339, body.CompletedAt)
	return s.projectCoverageOutcome(ctx, "complete", body.OutcomeID, body.ClaimID, body.AssignmentID, body.PoolID, body.Group, body.NodeID, body.RangeStart, body.RangeEnd, body.ReleaseCount, "", completedAt, string(bodyJSON), event)
}

func (s *Store) projectRangeFailed(ctx context.Context, body coverage.RangeFailed, event *events.SignedEvent) error {
	bodyJSON, _ := json.Marshal(body)
	failedAt, _ := time.Parse(time.RFC3339, body.FailedAt)
	return s.projectCoverageOutcome(ctx, "failed", body.OutcomeID, body.ClaimID, body.AssignmentID, body.PoolID, body.Group, body.NodeID, body.RangeStart, body.RangeEnd, 0, body.Reason, failedAt, string(bodyJSON), event)
}

func (s *Store) projectCoverageOutcome(ctx context.Context, outcomeType, outcomeID, claimID, assignmentID, poolID, group, nodeID string, rangeStart, rangeEnd int64, releaseCount int, reason string, occurredAt time.Time, bodyJSON string, event *events.SignedEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO coverage_range_outcomes (
			outcome_id, outcome_type, claim_id, assignment_id, pool_id, group_name,
			node_id, range_start, range_end, release_count, reason, occurred_at,
			author_node_id, body_json, source_event_id, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7, $8, $9, $10, NULLIF($11, ''), $12, $13, $14::jsonb, $15, NOW())
		ON CONFLICT (outcome_id) DO UPDATE SET
			outcome_type = EXCLUDED.outcome_type,
			release_count = EXCLUDED.release_count,
			reason = EXCLUDED.reason,
			occurred_at = EXCLUDED.occurred_at,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		outcomeID, outcomeType, claimID, assignmentID, poolID, group, nodeID,
		rangeStart, rangeEnd, releaseCount, reason, occurredAt.UTC(),
		event.AuthorNodeID, bodyJSON, event.EventID); err != nil {
		return err
	}
	if strings.TrimSpace(claimID) != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE coverage_claims
			SET status = $2,
			    updated_at = NOW()
			WHERE claim_id = $1`, claimID, outcomeType); err != nil {
			return err
		}
	}
	if strings.TrimSpace(assignmentID) != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE coverage_assignments
			SET status = $2,
			    updated_at = NOW()
			WHERE assignment_id = $1`, assignmentID, outcomeType); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) listCoverageAssignments(ctx context.Context, poolID string) ([]CoverageAssignmentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT assignment_id, pool_id, group_name, assigned_node_id,
		       COALESCE(range_start, 0), COALESCE(range_end, 0),
		       window_start, window_end, priority, due_at, status, created_at
		FROM coverage_assignments
		WHERE pool_id = $1
		ORDER BY priority DESC, created_at DESC
		LIMIT 100`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoverageAssignmentRecord{}
	for rows.Next() {
		var item CoverageAssignmentRecord
		var windowStart, windowEnd, dueAt nullableTime
		if err := rows.Scan(&item.AssignmentID, &item.PoolID, &item.Group, &item.AssignedNodeID, &item.RangeStart, &item.RangeEnd, &windowStart, &windowEnd, &item.Priority, &dueAt, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.WindowStart = windowStart.ptr()
		item.WindowEnd = windowEnd.ptr()
		item.DueAt = dueAt.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) listCoverageClaims(ctx context.Context, poolID string, stale bool) ([]CoverageClaimRecord, error) {
	comparator := ">"
	if stale {
		comparator = "<="
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.claim_id, c.claim_type, COALESCE(c.assignment_id, ''), c.pool_id,
		       c.group_name, c.node_id, COALESCE(c.range_start, 0),
		       COALESCE(c.range_end, 0), c.claimed_at, c.expires_at, c.status
		FROM coverage_claims c
		WHERE c.pool_id = $1
		  AND c.status = 'active'
		  AND c.expires_at %s NOW()
		  AND NOT EXISTS (
		    SELECT 1 FROM coverage_range_outcomes o
		    WHERE o.claim_id = c.claim_id
		  )
		ORDER BY c.expires_at
		LIMIT 100`, comparator), poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoverageClaimRecord{}
	for rows.Next() {
		var item CoverageClaimRecord
		if err := rows.Scan(&item.ClaimID, &item.ClaimType, &item.AssignmentID, &item.PoolID, &item.Group, &item.NodeID, &item.RangeStart, &item.RangeEnd, &item.ClaimedAt, &item.ExpiresAt, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) listCoverageOutcomes(ctx context.Context, poolID string) ([]CoverageOutcomeRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT outcome_id, outcome_type, COALESCE(claim_id, ''),
		       COALESCE(assignment_id, ''), pool_id, group_name, node_id,
		       range_start, range_end, release_count, COALESCE(reason, ''),
		       occurred_at
		FROM coverage_range_outcomes
		WHERE pool_id = $1
		ORDER BY occurred_at DESC
		LIMIT 100`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoverageOutcomeRecord{}
	for rows.Next() {
		var item CoverageOutcomeRecord
		if err := rows.Scan(&item.OutcomeID, &item.OutcomeType, &item.ClaimID, &item.AssignmentID, &item.PoolID, &item.Group, &item.NodeID, &item.RangeStart, &item.RangeEnd, &item.ReleaseCount, &item.Reason, &item.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type nullableTime struct {
	Time  time.Time
	Valid bool
}

func (n *nullableTime) Scan(value any) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	t, ok := value.(time.Time)
	if !ok {
		return fmt.Errorf("expected time value")
	}
	n.Time = t.UTC()
	n.Valid = true
	return nil
}

func (n nullableTime) ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	value := n.Time.UTC()
	return &value
}

func parseNullableTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}
