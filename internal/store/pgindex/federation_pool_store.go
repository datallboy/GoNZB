package pgindex

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
)

type TrustPoolRecord struct {
	PoolID                     string          `json:"pool_id"`
	DisplayName                string          `json:"display_name"`
	Description                string          `json:"description"`
	GenesisEventID             string          `json:"genesis_event_id"`
	PolicyJSON                 json.RawMessage `json:"policy_json"`
	MembershipThreshold        int             `json:"membership_threshold"`
	ModerationThreshold        int             `json:"moderation_threshold"`
	CheckpointWitnessThreshold int             `json:"checkpoint_witness_threshold"`
	AcceptMode                 string          `json:"accept_mode"`
	MinNodeTrustScore          float64         `json:"min_node_trust_score"`
	AcceptedEventTypes         []string        `json:"accepted_event_types"`
	Enabled                    bool            `json:"enabled"`
	Visibility                 string          `json:"visibility"`
	JoinMode                   string          `json:"join_mode"`
	AdmissionEnabled           bool            `json:"admission_enabled"`
	CreatedAt                  time.Time       `json:"created_at"`
	UpdatedAt                  time.Time       `json:"updated_at"`
}

type PoolMemberRecord struct {
	PoolID              string          `json:"pool_id"`
	NodeID              string          `json:"node_id"`
	Role                string          `json:"role"`
	Status              string          `json:"status"`
	ApprovedEventID     string          `json:"approved_event_id"`
	RevokedEventID      string          `json:"revoked_event_id"`
	AllowedCapabilities []string        `json:"allowed_capabilities"`
	LimitsJSON          json.RawMessage `json:"limits_json"`
	JoinedAt            *time.Time      `json:"joined_at,omitempty"`
	RevokedAt           *time.Time      `json:"revoked_at,omitempty"`
}

type PoolControlEventRecord struct {
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	AuthorNodeID string          `json:"author_node_id"`
	PoolIDs      json.RawMessage `json:"pool_ids"`
	BodyJSON     json.RawMessage `json:"body_json"`
	CreatedAt    time.Time       `json:"created_at"`
	ReceivedAt   time.Time       `json:"received_at"`
}

type PoolAuthorizationResult struct {
	Allowed bool
	Reason  string
}

func (s *Store) ValidateFederationPoolControlEvent(ctx context.Context, event *events.SignedEvent) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	switch event.EventType {
	case pools.EventTypePoolGenesis:
		var body pools.Genesis
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return fmt.Errorf("invalid pool genesis body: %w", err)
		}
		if strings.TrimSpace(body.PoolID) == "" || strings.TrimSpace(body.DisplayName) == "" {
			return fmt.Errorf("pool genesis requires pool_id and display_name")
		}
		if len(body.Admins) == 0 {
			return fmt.Errorf("pool genesis requires at least one admin")
		}
		if !containsString(body.Admins, event.AuthorNodeID) {
			return fmt.Errorf("pool genesis author must be an admin")
		}
		return nil
	case pools.EventTypePoolJoinRequest:
		var body pools.JoinRequest
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return fmt.Errorf("invalid pool join request body: %w", err)
		}
		if strings.TrimSpace(body.PoolID) == "" || strings.TrimSpace(body.CandidateNodeID) == "" {
			return fmt.Errorf("pool join request requires pool_id and candidate_node_id")
		}
		return nil
	case pools.EventTypePoolMemberApproved:
		var body pools.MemberApproved
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return fmt.Errorf("invalid pool member approval body: %w", err)
		}
		policy, err := s.GetTrustPoolPolicy(ctx, body.PoolID)
		if err != nil {
			return err
		}
		if body.ApprovalsRequired <= 0 {
			body.ApprovalsRequired = policy.MembershipThreshold
		}
		adminKeys, err := s.ActivePoolAdminPublicKeys(ctx, body.PoolID)
		if err != nil {
			return err
		}
		return pools.ValidateMemberApproval(body, adminKeys)
	case pools.EventTypePoolMemberRevoked:
		var body pools.MemberRevoked
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return fmt.Errorf("invalid pool member revocation body: %w", err)
		}
		policy, err := s.GetTrustPoolPolicy(ctx, body.PoolID)
		if err != nil {
			return err
		}
		if body.ApprovalsRequired <= 0 {
			body.ApprovalsRequired = policy.ModerationThreshold
		}
		adminKeys, err := s.ActivePoolAdminPublicKeys(ctx, body.PoolID)
		if err != nil {
			return err
		}
		return pools.ValidateMemberRevocation(body, adminKeys)
	case pools.EventTypePoolCheckpoint:
		var body pools.Checkpoint
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return fmt.Errorf("invalid pool checkpoint body: %w", err)
		}
		policy, err := s.GetTrustPoolPolicy(ctx, body.PoolID)
		if err != nil {
			return err
		}
		witnessKeys, err := s.ActivePoolWitnessPublicKeys(ctx, body.PoolID)
		if err != nil {
			return err
		}
		leaves, err := s.ListPoolCheckpointLeaves(ctx, body.PoolID, int(body.EventCount))
		if err != nil {
			return err
		}
		return pools.ValidateCheckpoint(body, witnessKeys, policy.CheckpointWitnessThreshold, leaves)
	default:
		return nil
	}
}

func (s *Store) ProjectFederationPoolEvent(ctx context.Context, event *events.SignedEvent) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	switch event.EventType {
	case pools.EventTypePoolGenesis:
		var body pools.Genesis
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		policy := pools.NormalizePolicy(body.Policy, len(body.Admins))
		policyJSON, _ := json.Marshal(policy)
		acceptedTypes := policy.AcceptedEventTypes
		if len(acceptedTypes) == 0 {
			acceptedTypes = []string{"ReleaseCard"}
		}
		if err := s.UpsertTrustPool(ctx, TrustPoolRecord{
			PoolID:                     body.PoolID,
			DisplayName:                body.DisplayName,
			Description:                body.Description,
			GenesisEventID:             event.EventID,
			PolicyJSON:                 policyJSON,
			MembershipThreshold:        policy.MembershipThreshold,
			ModerationThreshold:        policy.ModerationThreshold,
			CheckpointWitnessThreshold: policy.CheckpointWitnessThreshold,
			AcceptMode:                 policy.AcceptMode,
			MinNodeTrustScore:          policy.MinNodeTrustScore,
			AcceptedEventTypes:         acceptedTypes,
			Enabled:                    true,
			Visibility:                 firstNonBlank(body.Visibility, "unlisted"),
			JoinMode:                   firstNonBlank(body.JoinMode, "approval"),
			AdmissionEnabled:           body.AdmissionEnabled || body.JoinMode == "",
		}); err != nil {
			return err
		}
		for _, nodeID := range body.Admins {
			_ = s.UpsertPoolMember(ctx, PoolMemberRecord{
				PoolID:              body.PoolID,
				NodeID:              nodeID,
				Role:                pools.RoleAdmin,
				Status:              pools.StatusActive,
				ApprovedEventID:     event.EventID,
				AllowedCapabilities: defaultAllowedCapabilities(pools.RoleAdmin, nil),
			})
		}
		for _, nodeID := range body.Witnesses {
			_ = s.UpsertPoolMember(ctx, PoolMemberRecord{
				PoolID:          body.PoolID,
				NodeID:          nodeID,
				Role:            pools.RoleWitness,
				Status:          pools.StatusActive,
				ApprovedEventID: event.EventID,
			})
		}
		return nil
	case pools.EventTypePoolJoinRequest:
		var body pools.JoinRequest
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		if _, err := s.federationExecutor(ctx).ExecContext(ctx, `
			UPDATE federation_nodes
			SET base_url = COALESCE(NULLIF($2, ''), base_url),
			    status = CASE
			        WHEN status IN ('local', 'blocked', 'forked', 'connected') THEN status
			        ELSE 'admission_pending'
			    END,
			    updated_at = NOW()
			WHERE node_id = $1`, body.CandidateNodeID, strings.TrimSpace(body.CandidateURL)); err != nil {
			return fmt.Errorf("update pool admission candidate: %w", err)
		}
		return s.UpsertFederationAdmission(ctx, FederationAdmissionRecord{
			ProposalEventID: event.EventID, PoolID: body.PoolID,
			GenesisEventID: body.GenesisEventID, CandidateNodeID: body.CandidateNodeID,
			CandidateURL: body.CandidateURL, RelayNodeID: body.RelayNodeID,
			RelayURL: body.RelayURL, RequestedRole: firstString(body.RequestedRoles, pools.RoleMember),
			RequestedCapabilities: body.RequestedCapabilities, Status: "pending",
		})
	case pools.EventTypePoolMemberApproved:
		var body pools.MemberApproved
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		role := strings.TrimSpace(body.Role)
		if role == "" {
			role = pools.RoleMember
		}
		if err := s.UpsertPoolMember(ctx, PoolMemberRecord{
			PoolID:              body.PoolID,
			NodeID:              body.SubjectNodeID,
			Role:                role,
			Status:              pools.StatusActive,
			ApprovedEventID:     event.EventID,
			AllowedCapabilities: body.AllowedCapabilities,
			LimitsJSON:          body.Limits,
		}); err != nil {
			return err
		}
		return s.FinalizeFederationAdmission(ctx, body.ProposalEventID, event.EventID, "approved", "")
	case pools.EventTypePoolMemberRevoked:
		var body pools.MemberRevoked
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		return s.RevokePoolMember(ctx, body.PoolID, body.SubjectNodeID, event.EventID, parsePoolTime(body.EffectiveAt))
	case pools.EventTypePoolCheckpoint:
		var body pools.Checkpoint
		if err := json.Unmarshal(event.Body, &body); err != nil {
			return err
		}
		return s.UpdateTrustPoolCheckpoint(ctx, body.PoolID, event.EventID, body.MerkleRoot)
	default:
		return nil
	}
}

func (s *Store) CanAcceptFederationEventForPools(ctx context.Context, authorNodeID string, poolIDs []string, eventType string) (PoolAuthorizationResult, error) {
	normalizedPools := normalizeStrings(poolIDs)
	if len(normalizedPools) == 0 {
		return PoolAuthorizationResult{Allowed: false, Reason: "missing_pool"}, nil
	}
	if len(normalizedPools) != 1 {
		return PoolAuthorizationResult{Allowed: false, Reason: "multiple_pools_not_supported"}, nil
	}
	for _, poolID := range normalizedPools {
		policy, err := s.GetTrustPoolPolicy(ctx, poolID)
		if err == sql.ErrNoRows {
			return PoolAuthorizationResult{Allowed: false, Reason: "unknown_pool"}, nil
		}
		if err != nil {
			return PoolAuthorizationResult{}, err
		}
		activeMember := true
		if strings.TrimSpace(policy.AcceptMode) == "pool_member" {
			ok, err := s.IsActivePoolMember(ctx, poolID, authorNodeID)
			if err != nil {
				return PoolAuthorizationResult{}, err
			}
			activeMember = ok
		}
		trustScore := 0.0
		if policy.MinNodeTrustScore > 0 {
			score, err := s.FederationNodeTrustScore(ctx, authorNodeID)
			if err != nil {
				return PoolAuthorizationResult{}, err
			}
			trustScore = score
		}
		if ok, reason := pools.AuthorizeEvent(policy, activeMember, trustScore, eventType); !ok {
			return PoolAuthorizationResult{Allowed: false, Reason: reason}, nil
		}
		requiredCapabilities := capability.RequiredForEvent(eventType)
		if len(requiredCapabilities) > 0 {
			ok, err := s.PoolMemberHasCapability(ctx, poolID, authorNodeID, requiredCapabilities)
			if err != nil {
				return PoolAuthorizationResult{}, err
			}
			if !ok {
				return PoolAuthorizationResult{Allowed: false, Reason: "node_capability_not_allowed"}, nil
			}
		}
	}
	return PoolAuthorizationResult{Allowed: true}, nil
}

func (s *Store) UpsertTrustPool(ctx context.Context, pool TrustPoolRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	acceptedTypesJSON, _ := json.Marshal(normalizeStrings(pool.AcceptedEventTypes))
	poolID := strings.TrimSpace(pool.PoolID)
	genesisEventID := strings.TrimSpace(pool.GenesisEventID)
	if poolID == "" {
		return fmt.Errorf("pool_id is required")
	}
	if genesisEventID != "" {
		var existing string
		err := s.federationExecutor(ctx).QueryRowContext(ctx, `SELECT COALESCE(genesis_event_id, '') FROM trust_pools WHERE pool_id = $1`, poolID).Scan(&existing)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("read trust pool genesis binding: %w", err)
		}
		if existing != "" && existing != genesisEventID {
			return fmt.Errorf("pool_id %q is already bound to genesis event %s", poolID, existing)
		}
	}
	_, err := s.federationExecutor(ctx).ExecContext(ctx, `
		INSERT INTO trust_pools (
			pool_id, display_name, description, genesis_event_id, policy_json,
			membership_threshold, moderation_threshold, checkpoint_witness_threshold,
			accept_mode, min_node_trust_score, accepted_event_types, enabled,
			visibility, join_mode, admission_enabled, updated_at
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5::jsonb, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, NOW())
		ON CONFLICT (pool_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			description = EXCLUDED.description,
			genesis_event_id = COALESCE(trust_pools.genesis_event_id, EXCLUDED.genesis_event_id),
			policy_json = EXCLUDED.policy_json,
			membership_threshold = EXCLUDED.membership_threshold,
			moderation_threshold = EXCLUDED.moderation_threshold,
			checkpoint_witness_threshold = EXCLUDED.checkpoint_witness_threshold,
			accept_mode = EXCLUDED.accept_mode,
			min_node_trust_score = EXCLUDED.min_node_trust_score,
			accepted_event_types = EXCLUDED.accepted_event_types,
			enabled = EXCLUDED.enabled,
			visibility = EXCLUDED.visibility,
			join_mode = EXCLUDED.join_mode,
			admission_enabled = EXCLUDED.admission_enabled,
			updated_at = NOW()`,
		poolID,
		pool.DisplayName,
		pool.Description,
		genesisEventID,
		string(defaultJSON(pool.PolicyJSON, `{}`)),
		positivePoolInt(pool.MembershipThreshold, 1),
		positivePoolInt(pool.ModerationThreshold, 1),
		positivePoolInt(pool.CheckpointWitnessThreshold, 1),
		firstNonBlank(pool.AcceptMode, "pool_member"),
		pool.MinNodeTrustScore,
		string(acceptedTypesJSON),
		pool.Enabled,
		firstNonBlank(pool.Visibility, "unlisted"),
		firstNonBlank(pool.JoinMode, "approval"),
		pool.AdmissionEnabled,
	)
	if err != nil {
		return fmt.Errorf("upsert trust pool: %w", err)
	}
	return nil
}

func (s *Store) ListTrustPools(ctx context.Context) ([]TrustPoolRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT pool_id, display_name, COALESCE(description, ''), COALESCE(genesis_event_id, ''),
		       policy_json, membership_threshold, moderation_threshold,
		       checkpoint_witness_threshold, accept_mode, min_node_trust_score,
		       accepted_event_types, enabled, visibility, join_mode,
		       admission_enabled, created_at, updated_at
		FROM trust_pools
		ORDER BY pool_id`)
	if err != nil {
		return nil, fmt.Errorf("list trust pools: %w", err)
	}
	defer rows.Close()
	out := []TrustPoolRecord{}
	for rows.Next() {
		var item TrustPoolRecord
		var acceptedTypesJSON []byte
		if err := rows.Scan(
			&item.PoolID,
			&item.DisplayName,
			&item.Description,
			&item.GenesisEventID,
			&item.PolicyJSON,
			&item.MembershipThreshold,
			&item.ModerationThreshold,
			&item.CheckpointWitnessThreshold,
			&item.AcceptMode,
			&item.MinNodeTrustScore,
			&acceptedTypesJSON,
			&item.Enabled,
			&item.Visibility,
			&item.JoinMode,
			&item.AdmissionEnabled,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(acceptedTypesJSON, &item.AcceptedEventTypes)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListPoolMembers(ctx context.Context, poolID string) ([]PoolMemberRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT pool_id, node_id, role, status, COALESCE(approved_event_id, ''),
		       COALESCE(revoked_event_id, ''), allowed_capabilities, limits_json,
		       joined_at, revoked_at
		FROM pool_members
		WHERE pool_id = $1
		ORDER BY role, node_id`, strings.TrimSpace(poolID))
	if err != nil {
		return nil, fmt.Errorf("list pool members: %w", err)
	}
	defer rows.Close()
	out := []PoolMemberRecord{}
	for rows.Next() {
		var item PoolMemberRecord
		var joinedAt sql.NullTime
		var revokedAt sql.NullTime
		var allowedCapabilitiesJSON []byte
		if err := rows.Scan(
			&item.PoolID,
			&item.NodeID,
			&item.Role,
			&item.Status,
			&item.ApprovedEventID,
			&item.RevokedEventID,
			&allowedCapabilitiesJSON,
			&item.LimitsJSON,
			&joinedAt,
			&revokedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(allowedCapabilitiesJSON, &item.AllowedCapabilities)
		if joinedAt.Valid {
			value := joinedAt.Time.UTC()
			item.JoinedAt = &value
		}
		if revokedAt.Valid {
			value := revokedAt.Time.UTC()
			item.RevokedAt = &value
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListPoolControlEvents(ctx context.Context, poolID string, limit int) ([]PoolControlEventRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, fmt.Errorf("pool_id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	poolFilter, _ := json.Marshal([]string{poolID})
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT event_id, event_type, author_node_id, pool_ids, body_json, created_at, received_at
		FROM federation_events
		WHERE validation_status = 'accepted'
		  AND event_type IN ('PoolJoinRequest', 'PoolMemberApproved', 'PoolMemberRevoked')
		  AND pool_ids @> $1::jsonb
		ORDER BY created_at DESC, event_id DESC
		LIMIT $2`, string(poolFilter), limit)
	if err != nil {
		return nil, fmt.Errorf("list pool control events: %w", err)
	}
	defer rows.Close()
	out := []PoolControlEventRecord{}
	for rows.Next() {
		var item PoolControlEventRecord
		var poolIDs []byte
		var bodyJSON []byte
		if err := rows.Scan(&item.EventID, &item.EventType, &item.AuthorNodeID, &poolIDs, &bodyJSON, &item.CreatedAt, &item.ReceivedAt); err != nil {
			return nil, err
		}
		item.PoolIDs = defaultRawJSON(poolIDs, `[]`)
		item.BodyJSON = defaultRawJSON(bodyJSON, `{}`)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertPoolMember(ctx context.Context, member PoolMemberRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	joinedAt := member.JoinedAt
	if joinedAt == nil {
		now := time.Now().UTC()
		joinedAt = &now
	}
	allowedCapabilities, _ := json.Marshal(defaultAllowedCapabilities(member.Role, member.AllowedCapabilities))
	limitsJSON := defaultJSON(member.LimitsJSON, `{}`)
	_, err := s.federationExecutor(ctx).ExecContext(ctx, `
		INSERT INTO pool_members (
			pool_id, node_id, role, status, approved_event_id, allowed_capabilities,
			limits_json, joined_at, updated_at
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, $7::jsonb, $8, NOW())
		ON CONFLICT (pool_id, node_id, role) DO UPDATE SET
			status = EXCLUDED.status,
			approved_event_id = COALESCE(EXCLUDED.approved_event_id, pool_members.approved_event_id),
			allowed_capabilities = EXCLUDED.allowed_capabilities,
			limits_json = EXCLUDED.limits_json,
			revoked_event_id = NULL,
			joined_at = COALESCE(pool_members.joined_at, EXCLUDED.joined_at),
			revoked_at = NULL,
			updated_at = NOW()`,
		member.PoolID,
		member.NodeID,
		firstNonBlank(member.Role, pools.RoleMember),
		firstNonBlank(member.Status, pools.StatusActive),
		member.ApprovedEventID,
		string(allowedCapabilities),
		string(limitsJSON),
		joinedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert pool member: %w", err)
	}
	return nil
}

func (s *Store) RevokePoolMember(ctx context.Context, poolID, nodeID, eventID string, effectiveAt *time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if effectiveAt == nil {
		now := time.Now().UTC()
		effectiveAt = &now
	}
	_, err := s.federationExecutor(ctx).ExecContext(ctx, `
		UPDATE pool_members
		SET status = 'revoked',
		    revoked_event_id = NULLIF($3, ''),
		    revoked_at = $4,
		    updated_at = NOW()
		WHERE pool_id = $1
		  AND node_id = $2`,
		poolID,
		nodeID,
		eventID,
		effectiveAt,
	)
	if err != nil {
		return fmt.Errorf("revoke pool member: %w", err)
	}
	return nil
}

func (s *Store) GetTrustPoolPolicy(ctx context.Context, poolID string) (pools.PoolPolicy, error) {
	var policy pools.PoolPolicy
	if s == nil || s.db == nil {
		return policy, fmt.Errorf("pgindex store is not initialized")
	}
	var acceptedTypesJSON []byte
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT pool_id, membership_threshold, moderation_threshold,
		       checkpoint_witness_threshold, accept_mode, min_node_trust_score,
		       accepted_event_types
		FROM trust_pools
		WHERE pool_id = $1
		  AND enabled = TRUE`, strings.TrimSpace(poolID)).Scan(
		&policy.PoolID,
		&policy.MembershipThreshold,
		&policy.ModerationThreshold,
		&policy.CheckpointWitnessThreshold,
		&policy.AcceptMode,
		&policy.MinNodeTrustScore,
		&acceptedTypesJSON,
	)
	if err != nil {
		return policy, err
	}
	_ = json.Unmarshal(acceptedTypesJSON, &policy.AcceptedEventTypes)
	return policy, nil
}

func (s *Store) ActivePoolAdminPublicKeys(ctx context.Context, poolID string) (map[string]ed25519.PublicKey, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT n.node_id, n.public_key
		FROM pool_members m
		JOIN federation_nodes n ON n.node_id = m.node_id
		WHERE m.pool_id = $1
		  AND m.role = 'admin'
		  AND m.status = 'active'`, strings.TrimSpace(poolID))
	if err != nil {
		return nil, fmt.Errorf("list active pool admins: %w", err)
	}
	defer rows.Close()
	out := map[string]ed25519.PublicKey{}
	for rows.Next() {
		var nodeID string
		var publicKey []byte
		if err := rows.Scan(&nodeID, &publicKey); err != nil {
			return nil, err
		}
		if len(publicKey) == ed25519.PublicKeySize {
			out[nodeID] = ed25519.PublicKey(publicKey)
		}
	}
	return out, rows.Err()
}

func (s *Store) ActivePoolWitnessPublicKeys(ctx context.Context, poolID string) (map[string]ed25519.PublicKey, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT n.node_id, n.public_key
		FROM pool_members m
		JOIN federation_nodes n ON n.node_id = m.node_id
		WHERE m.pool_id = $1
		  AND m.role IN ('admin', 'witness')
		  AND m.status = 'active'`, strings.TrimSpace(poolID))
	if err != nil {
		return nil, fmt.Errorf("list active pool witnesses: %w", err)
	}
	defer rows.Close()
	out := map[string]ed25519.PublicKey{}
	for rows.Next() {
		var nodeID string
		var publicKey []byte
		if err := rows.Scan(&nodeID, &publicKey); err != nil {
			return nil, err
		}
		if len(publicKey) == ed25519.PublicKeySize {
			out[nodeID] = ed25519.PublicKey(publicKey)
		}
	}
	return out, rows.Err()
}

func (s *Store) ListPoolCheckpointLeaves(ctx context.Context, poolID string, limit int) ([]pools.CheckpointLeaf, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, fmt.Errorf("pool_id is required")
	}
	if limit <= 0 || limit > 100000 {
		return nil, fmt.Errorf("checkpoint event_count is out of range")
	}
	poolFilter, _ := json.Marshal([]string{poolID})
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT event_id, author_node_id, sequence, body_hash, created_at
		FROM federation_events
		WHERE validation_status = 'accepted'
		  AND visibility <> 'local'
		  AND event_type <> $1
		  AND pool_ids @> $2::jsonb
		ORDER BY created_at ASC, event_id ASC
		LIMIT $3`, pools.EventTypePoolCheckpoint, string(poolFilter), limit)
	if err != nil {
		return nil, fmt.Errorf("list pool checkpoint leaves: %w", err)
	}
	defer rows.Close()
	out := []pools.CheckpointLeaf{}
	for rows.Next() {
		var leaf pools.CheckpointLeaf
		if err := rows.Scan(&leaf.EventID, &leaf.AuthorNodeID, &leaf.Sequence, &leaf.BodyHash, &leaf.CreatedAt); err != nil {
			return nil, err
		}
		leaf.CreatedAt = leaf.CreatedAt.UTC()
		out = append(out, leaf)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTrustPoolCheckpoint(ctx context.Context, poolID, eventID, merkleRoot string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	res, err := s.federationExecutor(ctx).ExecContext(ctx, `
		UPDATE trust_pools
		SET latest_checkpoint_event_id = $2,
		    latest_merkle_root = $3,
		    updated_at = NOW()
		WHERE pool_id = $1`, strings.TrimSpace(poolID), strings.TrimSpace(eventID), strings.TrimSpace(merkleRoot))
	if err != nil {
		return fmt.Errorf("update trust pool checkpoint: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) GetPoolCheckpointEvent(ctx context.Context, poolID string) (*events.SignedEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, fmt.Errorf("pool_id is required")
	}
	var eventID string
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT COALESCE(latest_checkpoint_event_id, '')
		FROM trust_pools
		WHERE pool_id = $1
		  AND enabled = TRUE`, poolID).Scan(&eventID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pool checkpoint: %w", err)
	}
	if strings.TrimSpace(eventID) == "" {
		return nil, nil
	}
	return s.GetFederationEvent(ctx, eventID)
}

func (s *Store) IsActivePoolAdmin(ctx context.Context, poolID, nodeID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	var ok bool
	if err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pool_members
			WHERE pool_id = $1
			  AND node_id = $2
			  AND role = $3
			  AND status = $4
		)`, strings.TrimSpace(poolID), strings.TrimSpace(nodeID), pools.RoleAdmin, pools.StatusActive).Scan(&ok); err != nil {
		return false, fmt.Errorf("check active pool admin: %w", err)
	}
	return ok, nil
}

func (s *Store) IsActivePoolMember(ctx context.Context, poolID, nodeID string) (bool, error) {
	var ok bool
	if err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pool_members
			WHERE pool_id = $1
			  AND node_id = $2
			  AND status = 'active'
		)`, strings.TrimSpace(poolID), strings.TrimSpace(nodeID)).Scan(&ok); err != nil {
		return false, fmt.Errorf("check pool membership: %w", err)
	}
	return ok, nil
}

func (s *Store) IsFederationPoolMember(ctx context.Context, nodeID string) (bool, error) {
	var exists bool
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pool_members WHERE node_id = $1
		)`, strings.TrimSpace(nodeID)).Scan(&exists)
	return exists, err
}

func (s *Store) IsActiveFederationPoolMember(ctx context.Context, nodeID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	var active bool
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM pool_members
		  WHERE node_id = $1
		    AND status = 'active'
		)`, strings.TrimSpace(nodeID)).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("check active federation pool membership: %w", err)
	}
	return active, nil
}

func (s *Store) PoolMemberHasCapability(ctx context.Context, poolID, nodeID string, required []string) (bool, error) {
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT role, allowed_capabilities
		FROM pool_members
		WHERE pool_id = $1
		  AND node_id = $2
		  AND status = 'active'`, strings.TrimSpace(poolID), strings.TrimSpace(nodeID))
	if err != nil {
		return false, fmt.Errorf("read pool member capabilities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		var allowedJSON []byte
		if err := rows.Scan(&role, &allowedJSON); err != nil {
			return false, err
		}
		var allowed []string
		_ = json.Unmarshal(allowedJSON, &allowed)
		if poolMemberCapabilityAllowed(role, allowed, required) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) FederationNodeTrustScore(ctx context.Context, nodeID string) (float64, error) {
	var score float64
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT local_trust_score
		FROM federation_nodes
		WHERE node_id = $1`, strings.TrimSpace(nodeID)).Scan(&score)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return score, err
}

func poolMemberCapabilityAllowed(role string, allowed, required []string) bool {
	if len(required) == 0 {
		return true
	}
	if strings.TrimSpace(role) == pools.RoleAdmin {
		return true
	}
	return capability.HasAny(allowed, required...)
}

func defaultAllowedCapabilities(role string, allowed []string) []string {
	allowed = capability.Normalize(allowed)
	if len(allowed) > 0 {
		return allowed
	}
	if strings.TrimSpace(role) != pools.RoleAdmin {
		return nil
	}
	return []string{
		capability.Admin,
		capability.Scanner,
		capability.Indexer,
		capability.ManifestBuilder,
		capability.ManifestCache,
		capability.Validator,
		capability.HealthChecker,
		capability.Relay,
		capability.CoverageCoordinator,
	}
}

func parsePoolTime(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func positivePoolInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func containsString(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func firstString(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}
