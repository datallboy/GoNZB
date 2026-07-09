package pools

import (
	"crypto/ed25519"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
)

const (
	EventTypeReleaseCard                    = "ReleaseCard"
	EventTypeHealthAttestation              = "HealthAttestation"
	EventTypeTombstone                      = "Tombstone"
	EventTypeValidatorCapacity              = "ValidatorCapacity"
	EventTypeArticleAvailabilityAttestation = "ArticleAvailabilityAttestation"
	EventTypeChecksumAttestation            = "ChecksumAttestation"
	EventTypeManifestAvailability           = "ManifestAvailability"
	EventTypeScannerCapacity                = "ScannerCapacity"
	EventTypeScannerHeartbeat               = "ScannerHeartbeat"
	EventTypeGroupObservation               = "GroupObservation"
	EventTypeCoveragePlan                   = "CoveragePlan"
	EventTypeCoverageAssignment             = "CoverageAssignment"
	EventTypeRangeClaim                     = "RangeClaim"
	EventTypeTimeWindowClaim                = "TimeWindowClaim"
	EventTypeCoverageCheckpoint             = "CoverageCheckpoint"
	EventTypeRangeComplete                  = "RangeComplete"
	EventTypeRangeFailed                    = "RangeFailed"

	EventTypePoolGenesis        = "PoolGenesis"
	EventTypePoolJoinRequest    = "PoolJoinRequest"
	EventTypePoolMemberApproved = "PoolMemberApproved"
	EventTypePoolMemberRevoked  = "PoolMemberRevoked"

	RoleAdmin   = "admin"
	RoleWitness = "witness"
	RoleMember  = "member"

	StatusActive  = "active"
	StatusRevoked = "revoked"

	bodySchemaPrefix = "gonzbnet."
	bodySchemaSuffix = "/1.0"
)

type Policy struct {
	MembershipThreshold         int      `json:"membership_threshold"`
	ModerationThreshold         int      `json:"moderation_threshold"`
	CheckpointWitnessThreshold  int      `json:"checkpoint_witness_threshold"`
	ManifestQuorum              int      `json:"manifest_quorum"`
	HealthQuorum                int      `json:"health_quorum"`
	AcceptMode                  string   `json:"accept_mode"`
	MinNodeTrustScore           float64  `json:"min_node_trust_score"`
	MinResultScore              float64  `json:"min_result_score"`
	MaxReleaseCardAgeDays       int      `json:"max_release_card_age_days"`
	AllowManifestFetch          bool     `json:"allow_manifest_fetch"`
	ManifestFetchRequiresMember bool     `json:"manifest_fetch_requires_membership"`
	AllowLiveQuery              bool     `json:"allow_live_query"`
	ShareResolutionManifests    string   `json:"share_resolution_manifests"`
	AllowEncryptedManifests     bool     `json:"allow_encrypted_manifests"`
	AcceptedEventTypes          []string `json:"accepted_event_types,omitempty"`
}

type Genesis struct {
	SchemaVersion string   `json:"schema_version"`
	Type          string   `json:"type"`
	PoolID        string   `json:"pool_id"`
	DisplayName   string   `json:"display_name"`
	Description   string   `json:"description,omitempty"`
	CreatedAt     string   `json:"created_at"`
	Admins        []string `json:"admins"`
	Witnesses     []string `json:"witnesses"`
	Policy        Policy   `json:"policy"`
}

type JoinRequest struct {
	SchemaVersion           string   `json:"schema_version"`
	Type                    string   `json:"type"`
	PoolID                  string   `json:"pool_id"`
	CandidateNodeID         string   `json:"candidate_node_id"`
	CandidateProfileEventID string   `json:"candidate_profile_event_id,omitempty"`
	RequestedRoles          []string `json:"requested_roles"`
	Message                 string   `json:"message,omitempty"`
	CreatedAt               string   `json:"created_at"`
}

type Approval struct {
	NodeID     string `json:"node_id"`
	ApprovedAt string `json:"approved_at,omitempty"`
	Signature  string `json:"signature"`
}

type MemberApproved struct {
	SchemaVersion     string     `json:"schema_version"`
	Type              string     `json:"type"`
	PoolID            string     `json:"pool_id"`
	SubjectNodeID     string     `json:"subject_node_id"`
	Role              string     `json:"role"`
	ProposalEventID   string     `json:"proposal_event_id"`
	ApprovalsRequired int        `json:"approvals_required"`
	Approvals         []Approval `json:"approvals"`
}

type MemberRevoked struct {
	SchemaVersion     string     `json:"schema_version"`
	Type              string     `json:"type"`
	PoolID            string     `json:"pool_id"`
	SubjectNodeID     string     `json:"subject_node_id"`
	Reason            string     `json:"reason"`
	EffectiveAt       string     `json:"effective_at"`
	ApprovalsRequired int        `json:"approvals_required"`
	Approvals         []Approval `json:"approvals"`
}

type PoolPolicy struct {
	PoolID              string
	MembershipThreshold int
	ModerationThreshold int
	AcceptMode          string
	MinNodeTrustScore   float64
	AcceptedEventTypes  []string
}

type Member struct {
	PoolID string
	NodeID string
	Role   string
	Status string
}

func NormalizePolicy(policy Policy, adminCount int) Policy {
	out := policy
	if out.MembershipThreshold <= 0 {
		out.MembershipThreshold = defaultThreshold(adminCount)
	}
	if out.ModerationThreshold <= 0 {
		out.ModerationThreshold = defaultThreshold(adminCount)
	}
	if out.CheckpointWitnessThreshold <= 0 {
		out.CheckpointWitnessThreshold = defaultThreshold(adminCount)
	}
	if strings.TrimSpace(out.AcceptMode) == "" {
		out.AcceptMode = "pool_member"
	}
	if len(out.AcceptedEventTypes) == 0 {
		out.AcceptedEventTypes = []string{
			EventTypeReleaseCard,
			EventTypeHealthAttestation,
			EventTypeTombstone,
			EventTypeValidatorCapacity,
			EventTypeArticleAvailabilityAttestation,
			EventTypeChecksumAttestation,
			EventTypeManifestAvailability,
			EventTypeScannerCapacity,
			EventTypeScannerHeartbeat,
			EventTypeGroupObservation,
			EventTypeCoveragePlan,
			EventTypeCoverageAssignment,
			EventTypeRangeClaim,
			EventTypeTimeWindowClaim,
			EventTypeCoverageCheckpoint,
			EventTypeRangeComplete,
			EventTypeRangeFailed,
		}
	}
	return out
}

func EventIsPoolControl(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case EventTypePoolGenesis, EventTypePoolJoinRequest, EventTypePoolMemberApproved, EventTypePoolMemberRevoked:
		return true
	default:
		return false
	}
}

func EventTypeSupported(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case EventTypeReleaseCard,
		EventTypeHealthAttestation,
		EventTypeTombstone,
		EventTypeValidatorCapacity,
		EventTypeArticleAvailabilityAttestation,
		EventTypeChecksumAttestation,
		EventTypeManifestAvailability,
		EventTypeScannerCapacity,
		EventTypeScannerHeartbeat,
		EventTypeGroupObservation,
		EventTypeCoveragePlan,
		EventTypeCoverageAssignment,
		EventTypeRangeClaim,
		EventTypeTimeWindowClaim,
		EventTypeCoverageCheckpoint,
		EventTypeRangeComplete,
		EventTypeRangeFailed,
		EventTypePoolGenesis,
		EventTypePoolJoinRequest,
		EventTypePoolMemberApproved,
		EventTypePoolMemberRevoked:
		return true
	default:
		return false
	}
}

func BodySchema(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return bodySchemaPrefix + "PoolControl" + bodySchemaSuffix
	}
	return bodySchemaPrefix + eventType + bodySchemaSuffix
}

func AuthorizeEvent(policy PoolPolicy, activeMember bool, trustScore float64, eventType string) (bool, string) {
	if !eventTypeAllowed(policy.AcceptedEventTypes, eventType) {
		return false, "event_type_not_allowed"
	}
	if strings.TrimSpace(policy.AcceptMode) == "pool_member" && !activeMember {
		return false, "not_pool_member"
	}
	if policy.MinNodeTrustScore > 0 && trustScore < policy.MinNodeTrustScore {
		return false, "node_trust_below_pool_minimum"
	}
	return true, ""
}

func ValidateMemberApproval(body MemberApproved, adminKeys map[string]ed25519.PublicKey) error {
	required := body.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	return validateApprovals(required, body.Approvals, adminKeys, func(approval Approval) (map[string]any, error) {
		return map[string]any{
			"pool_id":           body.PoolID,
			"proposal_event_id": body.ProposalEventID,
			"subject_node_id":   body.SubjectNodeID,
			"role":              body.Role,
			"approved_at":       approval.ApprovedAt,
		}, nil
	})
}

func ValidateMemberRevocation(body MemberRevoked, adminKeys map[string]ed25519.PublicKey) error {
	required := body.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	return validateApprovals(required, body.Approvals, adminKeys, func(approval Approval) (map[string]any, error) {
		return map[string]any{
			"pool_id":         body.PoolID,
			"subject_node_id": body.SubjectNodeID,
			"reason":          body.Reason,
			"effective_at":    body.EffectiveAt,
		}, nil
	})
}

func validateApprovals(required int, approvals []Approval, adminKeys map[string]ed25519.PublicKey, payload func(Approval) (map[string]any, error)) error {
	if required <= 0 {
		required = 1
	}
	seen := map[string]struct{}{}
	valid := 0
	for _, approval := range approvals {
		nodeID := strings.TrimSpace(approval.NodeID)
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		publicKey, ok := adminKeys[nodeID]
		if !ok || identity.NodeIDFromPublicKey(publicKey) != nodeID {
			continue
		}
		signature, err := canonical.DecodeBase64URL(approval.Signature)
		if err != nil {
			continue
		}
		object, err := payload(approval)
		if err != nil {
			return err
		}
		canonicalPayload, err := canonical.Marshal(object)
		if err != nil {
			return err
		}
		if !identity.Verify(publicKey, canonicalPayload, signature) {
			continue
		}
		seen[nodeID] = struct{}{}
		valid++
	}
	if valid < required {
		return fmt.Errorf("approval threshold not met: have %d need %d", valid, required)
	}
	return nil
}

func defaultThreshold(adminCount int) int {
	if adminCount <= 1 {
		return 1
	}
	return 2
}

func eventTypeAllowed(allowed []string, eventType string) bool {
	if len(allowed) == 0 {
		return true
	}
	eventType = strings.TrimSpace(eventType)
	for _, value := range allowed {
		if strings.TrimSpace(value) == eventType {
			return true
		}
	}
	return false
}
