package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/moderation"
)

type TombstoneProjection struct {
	Tombstone    moderation.Tombstone
	EventID      string
	AuthorNodeID string
}

type TombstoneRecord struct {
	ID                int64      `json:"id"`
	TargetType        string     `json:"target_type"`
	TargetID          string     `json:"target_id"`
	PoolID            string     `json:"pool_id,omitempty"`
	Reason            string     `json:"reason"`
	Severity          string     `json:"severity"`
	SourceEventID     string     `json:"source_event_id"`
	Active            bool       `json:"active"`
	ApprovalCount     int        `json:"approval_count"`
	ApprovalsRequired int        `json:"approvals_required"`
	EffectiveAt       time.Time  `json:"effective_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (s *Store) ProjectTombstone(ctx context.Context, projection TombstoneProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	item := projection.Tombstone
	if err := moderation.Validate(item, time.Now().UTC(), 2*time.Minute); err != nil {
		return err
	}
	eventID := strings.TrimSpace(projection.EventID)
	if eventID == "" {
		return fmt.Errorf("source event_id is required")
	}
	authorNodeID := strings.TrimSpace(projection.AuthorNodeID)
	if authorNodeID == "" {
		return fmt.Errorf("author_node_id is required")
	}
	effectiveAt, _ := time.Parse(time.RFC3339, strings.TrimSpace(item.EffectiveAt))
	var expiresAt *time.Time
	if item.ExpiresAt != nil && strings.TrimSpace(*item.ExpiresAt) != "" {
		parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(*item.ExpiresAt))
		utc := parsed.UTC()
		expiresAt = &utc
	}
	evidenceJSON, _ := json.Marshal(item.EvidenceEventIDs)
	poolID := strings.TrimSpace(item.PoolID)

	tx, commit, rollback, err := s.beginFederationProjection(ctx)
	if err != nil {
		return err
	}
	defer rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tombstone_votes (
			target_type, target_id, pool_id, reason, severity, author_node_id,
			source_event_id, evidence_event_ids, effective_at, expires_at
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8::jsonb, $9, $10)
		ON CONFLICT (source_event_id) DO NOTHING`,
		item.TargetType,
		item.TargetID,
		poolID,
		item.Reason,
		item.Severity,
		authorNodeID,
		eventID,
		string(evidenceJSON),
		effectiveAt.UTC(),
		expiresAt,
	); err != nil {
		return fmt.Errorf("insert tombstone vote: %w", err)
	}

	approvalsRequired, approvalCount, err := tombstoneApprovalState(ctx, tx, item)
	if err != nil {
		return err
	}
	active := approvalCount >= approvalsRequired && moderation.IsActive(item, time.Now().UTC())

	if err := upsertTombstoneProjection(ctx, tx, item, eventID, active, approvalCount, approvalsRequired, effectiveAt.UTC(), expiresAt); err != nil {
		return err
	}
	if active {
		if err := applyActiveTombstone(ctx, tx, item); err != nil {
			return err
		}
	}
	return commit()
}

func (s *Store) ListTombstones(ctx context.Context, activeOnly bool) ([]TombstoneRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	clauses := []string{"1=1"}
	if activeOnly {
		clauses = append(clauses, "active = TRUE")
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, fmt.Sprintf(`
		SELECT id, target_type, target_id, COALESCE(pool_id, ''), reason,
		       severity, source_event_id, active, approval_count,
		       approvals_required, effective_at, expires_at, created_at, updated_at
		FROM tombstones
		WHERE %s
		ORDER BY updated_at DESC, id DESC`, strings.Join(clauses, " AND ")))
	if err != nil {
		return nil, fmt.Errorf("list tombstones: %w", err)
	}
	defer rows.Close()
	out := []TombstoneRecord{}
	for rows.Next() {
		var item TombstoneRecord
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.TargetType,
			&item.TargetID,
			&item.PoolID,
			&item.Reason,
			&item.Severity,
			&item.SourceEventID,
			&item.Active,
			&item.ApprovalCount,
			&item.ApprovalsRequired,
			&item.EffectiveAt,
			&expiresAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			value := expiresAt.Time.UTC()
			item.ExpiresAt = &value
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type tombstoneTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func tombstoneApprovalState(ctx context.Context, tx tombstoneTx, item moderation.Tombstone) (int, int, error) {
	poolID := strings.TrimSpace(item.PoolID)
	if poolID == "" || strings.TrimSpace(item.Severity) == moderation.SeverityLocalOnly {
		return 1, 1, nil
	}
	required := 1
	if err := tx.QueryRowContext(ctx, `
		SELECT moderation_threshold
		FROM trust_pools
		WHERE pool_id = $1
		  AND enabled = TRUE`, poolID).Scan(&required); err != nil {
		if err != sql.ErrNoRows {
			return 0, 0, fmt.Errorf("read moderation threshold: %w", err)
		}
	}
	if required <= 0 {
		required = 1
	}
	approvals := 0
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT v.author_node_id)
		FROM tombstone_votes v
		JOIN pool_members pm ON pm.pool_id = v.pool_id
		 AND pm.node_id = v.author_node_id
		 AND pm.status = 'active'
		 AND pm.role = 'admin'
		WHERE v.target_type = $1
		  AND v.target_id = $2
		  AND v.pool_id = $3
		  AND v.severity = $4
		  AND v.effective_at <= NOW()
		  AND (v.expires_at IS NULL OR v.expires_at > NOW())`,
		item.TargetType,
		item.TargetID,
		poolID,
		item.Severity,
	).Scan(&approvals); err != nil {
		return 0, 0, fmt.Errorf("count tombstone approvals: %w", err)
	}
	return required, approvals, nil
}

func upsertTombstoneProjection(ctx context.Context, tx tombstoneTx, item moderation.Tombstone, eventID string, active bool, approvalCount, approvalsRequired int, effectiveAt time.Time, expiresAt *time.Time) error {
	poolID := strings.TrimSpace(item.PoolID)
	res, err := tx.ExecContext(ctx, `
		UPDATE tombstones
		SET reason = $4,
		    severity = $5,
		    source_event_id = $6,
		    active = $7,
		    approval_count = $8,
		    approvals_required = $9,
		    effective_at = $10,
		    expires_at = $11,
		    updated_at = NOW()
		WHERE target_type = $1
		  AND target_id = $2
		  AND COALESCE(pool_id, '') = $3`,
		item.TargetType,
		item.TargetID,
		poolID,
		item.Reason,
		item.Severity,
		eventID,
		active,
		approvalCount,
		approvalsRequired,
		effectiveAt,
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("update tombstone projection: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tombstones (
			target_type, target_id, pool_id, reason, severity, source_event_id,
			active, approval_count, approvals_required, effective_at, expires_at
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, $10, $11)`,
		item.TargetType,
		item.TargetID,
		poolID,
		item.Reason,
		item.Severity,
		eventID,
		active,
		approvalCount,
		approvalsRequired,
		effectiveAt,
		expiresAt,
	); err != nil {
		return fmt.Errorf("insert tombstone projection: %w", err)
	}
	return nil
}

func applyActiveTombstone(ctx context.Context, tx tombstoneTx, item moderation.Tombstone) error {
	severity := strings.TrimSpace(item.Severity)
	if severity == moderation.SeverityWarn {
		return nil
	}
	switch strings.TrimSpace(item.TargetType) {
	case moderation.TargetRelease:
		status := "hidden"
		if severity == moderation.SeverityReject || severity == moderation.SeverityLocalOnly {
			status = "rejected"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE federated_release_cards
			SET status = $2,
			    updated_at = NOW()
			WHERE release_id = $1`,
			item.TargetID,
			status,
		); err != nil {
			return fmt.Errorf("apply release tombstone: %w", err)
		}
		if severity == moderation.SeverityReject || severity == moderation.SeverityLocalOnly {
			if _, err := tx.ExecContext(ctx, `
				UPDATE resolution_manifests
				SET validation_status = 'invalidated',
				    rejection_reason = 'tombstoned_release',
				    generated_nzb = NULL,
				    updated_at = NOW()
				WHERE release_id = $1`,
				item.TargetID,
			); err != nil {
				return fmt.Errorf("invalidate release manifests: %w", err)
			}
		}
	case moderation.TargetManifest:
		if severity == moderation.SeverityReject || severity == moderation.SeverityLocalOnly {
			if _, err := tx.ExecContext(ctx, `
				UPDATE resolution_manifests
				SET validation_status = 'invalidated',
				    rejection_reason = 'tombstoned_manifest',
				    generated_nzb = NULL,
				    updated_at = NOW()
				WHERE manifest_id = $1`,
				item.TargetID,
			); err != nil {
				return fmt.Errorf("invalidate manifest tombstone: %w", err)
			}
		}
	}
	return nil
}
