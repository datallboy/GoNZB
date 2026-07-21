package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/admission"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

type FederationAdmissionRecord struct {
	ProposalEventID       string     `json:"proposal_event_id"`
	PoolID                string     `json:"pool_id"`
	GenesisEventID        string     `json:"genesis_event_id"`
	CandidateNodeID       string     `json:"candidate_node_id"`
	CandidateURL          string     `json:"candidate_url"`
	RelayNodeID           string     `json:"relay_node_id"`
	RelayURL              string     `json:"relay_url"`
	RequestedRole         string     `json:"requested_role"`
	RequestedCapabilities []string   `json:"requested_capabilities"`
	Status                string     `json:"status"`
	FinalEventID          string     `json:"final_event_id"`
	RejectionReason       string     `json:"rejection_reason"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (s *Store) UpsertFederationAdmission(ctx context.Context, record FederationAdmissionRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	capabilities, _ := json.Marshal(normalizeStrings(record.RequestedCapabilities))
	_, err := s.federationExecutor(ctx).ExecContext(ctx, `
		INSERT INTO federation_pool_admissions (
			proposal_event_id, pool_id, genesis_event_id, candidate_node_id,
			candidate_url, relay_node_id, relay_url, requested_role,
			requested_capabilities, status, expires_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), $7,
		        $8, $9::jsonb, $10, $11, NOW())
		ON CONFLICT (proposal_event_id) DO UPDATE SET
			candidate_url = COALESCE(EXCLUDED.candidate_url, federation_pool_admissions.candidate_url),
			relay_node_id = COALESCE(EXCLUDED.relay_node_id, federation_pool_admissions.relay_node_id),
			relay_url = EXCLUDED.relay_url,
			requested_role = EXCLUDED.requested_role,
			requested_capabilities = EXCLUDED.requested_capabilities,
			expires_at = COALESCE(EXCLUDED.expires_at, federation_pool_admissions.expires_at),
			updated_at = NOW()`,
		strings.TrimSpace(record.ProposalEventID), strings.TrimSpace(record.PoolID),
		strings.TrimSpace(record.GenesisEventID), strings.TrimSpace(record.CandidateNodeID),
		strings.TrimSpace(record.CandidateURL), strings.TrimSpace(record.RelayNodeID),
		strings.TrimRight(strings.TrimSpace(record.RelayURL), "/"), firstNonBlank(record.RequestedRole, "member"),
		string(capabilities), firstNonBlank(record.Status, "pending"), record.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upsert federation admission: %w", err)
	}
	return nil
}

func (s *Store) GetFederationAdmission(ctx context.Context, proposalEventID string) (FederationAdmissionRecord, error) {
	return s.scanFederationAdmission(s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT proposal_event_id, pool_id, COALESCE(genesis_event_id, ''), candidate_node_id,
		       COALESCE(candidate_url, ''), COALESCE(relay_node_id, ''), relay_url,
		       requested_role, requested_capabilities, status, COALESCE(final_event_id, ''),
		       COALESCE(rejection_reason, ''), expires_at, created_at, updated_at
		FROM federation_pool_admissions WHERE proposal_event_id = $1`, strings.TrimSpace(proposalEventID)))
}

func (s *Store) ListFederationAdmissions(ctx context.Context, poolID, status string, limit int) ([]FederationAdmissionRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT proposal_event_id, pool_id, COALESCE(genesis_event_id, ''), candidate_node_id,
		       COALESCE(candidate_url, ''), COALESCE(relay_node_id, ''), relay_url,
		       requested_role, requested_capabilities, status, COALESCE(final_event_id, ''),
		       COALESCE(rejection_reason, ''), expires_at, created_at, updated_at
		FROM federation_pool_admissions
		WHERE ($1 = '' OR pool_id = $1) AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC LIMIT $3`, strings.TrimSpace(poolID), strings.TrimSpace(status), limit)
	if err != nil {
		return nil, fmt.Errorf("list federation admissions: %w", err)
	}
	defer rows.Close()
	items := make([]FederationAdmissionRecord, 0)
	for rows.Next() {
		item, err := s.scanFederationAdmission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func (s *Store) scanFederationAdmission(row rowScanner) (FederationAdmissionRecord, error) {
	var item FederationAdmissionRecord
	var capabilities []byte
	var expiresAt sql.NullTime
	err := row.Scan(&item.ProposalEventID, &item.PoolID, &item.GenesisEventID, &item.CandidateNodeID,
		&item.CandidateURL, &item.RelayNodeID, &item.RelayURL, &item.RequestedRole,
		&capabilities, &item.Status, &item.FinalEventID, &item.RejectionReason,
		&expiresAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	_ = json.Unmarshal(capabilities, &item.RequestedCapabilities)
	if expiresAt.Valid {
		value := expiresAt.Time.UTC()
		item.ExpiresAt = &value
	}
	return item, nil
}

func (s *Store) StoreFederationApprovalFragment(ctx context.Context, fragment admission.ApprovalFragment) error {
	payload, err := json.Marshal(fragment)
	if err != nil {
		return err
	}
	approvedAt, err := time.Parse(time.RFC3339, fragment.ApprovedAt)
	if err != nil {
		return fmt.Errorf("invalid approval timestamp")
	}
	_, err = s.federationExecutor(ctx).ExecContext(ctx, `
		INSERT INTO federation_pool_approval_fragments (proposal_event_id, admin_node_id, approved_at, fragment_json)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (proposal_event_id, admin_node_id) DO UPDATE SET
			approved_at = EXCLUDED.approved_at, fragment_json = EXCLUDED.fragment_json`,
		fragment.ProposalEventID, fragment.AdminNodeID, approvedAt, string(payload))
	if err != nil {
		return fmt.Errorf("store federation approval fragment: %w", err)
	}
	return nil
}

func (s *Store) ListFederationApprovalFragments(ctx context.Context, proposalEventID string) ([]admission.ApprovalFragment, error) {
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT fragment_json FROM federation_pool_approval_fragments
		WHERE proposal_event_id = $1 ORDER BY admin_node_id`, strings.TrimSpace(proposalEventID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []admission.ApprovalFragment{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var item admission.ApprovalFragment
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) FinalizeFederationAdmission(ctx context.Context, proposalEventID, finalEventID, status, reason string) error {
	_, err := s.federationExecutor(ctx).ExecContext(ctx, `
		UPDATE federation_pool_admissions
		SET final_event_id = COALESCE(final_event_id, NULLIF($2, '')),
		    status = CASE WHEN status IN ('approved', 'rejected', 'expired') THEN status ELSE $3 END,
		    rejection_reason = COALESCE(rejection_reason, NULLIF($4, '')),
		    updated_at = NOW()
		WHERE proposal_event_id = $1`, strings.TrimSpace(proposalEventID), strings.TrimSpace(finalEventID),
		firstNonBlank(status, "approved"), strings.TrimSpace(reason))
	return err
}

func (s *Store) GetTrustPool(ctx context.Context, poolID string) (TrustPoolRecord, error) {
	var item TrustPoolRecord
	var acceptedTypes []byte
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT pool_id, display_name, COALESCE(description, ''), COALESCE(genesis_event_id, ''),
		       policy_json, membership_threshold, moderation_threshold,
		       checkpoint_witness_threshold, accept_mode, min_node_trust_score,
		       accepted_event_types, enabled, visibility, join_mode, admission_enabled,
		       created_at, updated_at
		FROM trust_pools WHERE pool_id = $1`, strings.TrimSpace(poolID)).Scan(
		&item.PoolID, &item.DisplayName, &item.Description, &item.GenesisEventID,
		&item.PolicyJSON, &item.MembershipThreshold, &item.ModerationThreshold,
		&item.CheckpointWitnessThreshold, &item.AcceptMode, &item.MinNodeTrustScore,
		&acceptedTypes, &item.Enabled, &item.Visibility, &item.JoinMode,
		&item.AdmissionEnabled, &item.CreatedAt, &item.UpdatedAt)
	_ = json.Unmarshal(acceptedTypes, &item.AcceptedEventTypes)
	return item, err
}

func (s *Store) GetPoolGenesisEvent(ctx context.Context, poolID string) (*events.SignedEvent, error) {
	pool, err := s.GetTrustPool(ctx, poolID)
	if err != nil || pool.GenesisEventID == "" {
		return nil, err
	}
	return s.GetFederationEvent(ctx, pool.GenesisEventID)
}

func (s *Store) ListActivePoolIDsForNode(ctx context.Context, nodeID string) ([]string, error) {
	return s.ListActivePoolIDsForNodeCapabilities(ctx, nodeID, nil)
}

func (s *Store) ListActivePoolMemberEndpoints(ctx context.Context, poolID string) ([]admission.MemberEndpoint, error) {
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT DISTINCT node.node_id, node.base_url
		FROM pool_members member
		JOIN federation_nodes node ON node.node_id = member.node_id
		WHERE member.pool_id = $1
		  AND member.status = 'active'
		  AND node.status <> 'blocked'
		  AND node.base_url <> ''
		ORDER BY node.node_id`, strings.TrimSpace(poolID))
	if err != nil {
		return nil, fmt.Errorf("list active pool member endpoints: %w", err)
	}
	defer rows.Close()
	items := make([]admission.MemberEndpoint, 0)
	for rows.Next() {
		var item admission.MemberEndpoint
		if err := rows.Scan(&item.NodeID, &item.BaseURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListActivePoolIDsForNodeCapabilities(ctx context.Context, nodeID string, required []string) ([]string, error) {
	requiredJSON, _ := json.Marshal(normalizeStrings(required))
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT DISTINCT member.pool_id
		FROM pool_members member
		JOIN trust_pools pool ON pool.pool_id = member.pool_id
		WHERE member.node_id = $1
		  AND member.status = 'active'
		  AND pool.enabled = TRUE
		  AND (
		    jsonb_array_length($2::jsonb) = 0
		    OR member.role = 'admin'
		    OR EXISTS (
		      SELECT 1
		      FROM jsonb_array_elements_text(
		        CASE
		          WHEN jsonb_typeof(member.allowed_capabilities) = 'array' THEN member.allowed_capabilities
		          ELSE '[]'::jsonb
		        END
		      ) allowed(capability)
		      WHERE allowed.capability IN (SELECT jsonb_array_elements_text($2::jsonb))
		    )
		  )
		ORDER BY member.pool_id`, strings.TrimSpace(nodeID), string(requiredJSON))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var poolID string
		if err := rows.Scan(&poolID); err != nil {
			return nil, err
		}
		items = append(items, poolID)
	}
	return items, rows.Err()
}
