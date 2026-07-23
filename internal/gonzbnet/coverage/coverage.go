package coverage

import (
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	TypeScannerCapacity    = "ScannerCapacity"
	TypeScannerHeartbeat   = "ScannerHeartbeat"
	TypeGroupObservation   = "GroupObservation"
	TypeCoveragePlan       = "CoveragePlan"
	TypeCoverageAssignment = "CoverageAssignment"
	TypeRangeClaim         = "RangeClaim"
	TypeTimeWindowClaim    = "TimeWindowClaim"
	TypeCoverageCheckpoint = "CoverageCheckpoint"
	TypeRangeComplete      = "RangeComplete"
	TypeRangeFailed        = "RangeFailed"

	ScannerCapacityBodySchema    = "gonzbnet.ScannerCapacity/1.0"
	ScannerHeartbeatBodySchema   = "gonzbnet.ScannerHeartbeat/1.0"
	GroupObservationBodySchema   = "gonzbnet.GroupObservation/1.0"
	CoveragePlanBodySchema       = "gonzbnet.CoveragePlan/1.0"
	CoverageAssignmentBodySchema = "gonzbnet.CoverageAssignment/1.0"
	RangeClaimBodySchema         = "gonzbnet.RangeClaim/1.0"
	TimeWindowClaimBodySchema    = "gonzbnet.TimeWindowClaim/1.0"
	CoverageCheckpointBodySchema = "gonzbnet.CoverageCheckpoint/1.0"
	RangeCompleteBodySchema      = "gonzbnet.RangeComplete/1.0"
	RangeFailedBodySchema        = "gonzbnet.RangeFailed/1.0"
)

type ScannerCapacity struct {
	SchemaVersion            string   `json:"schema_version"`
	Type                     string   `json:"type"`
	NodeID                   string   `json:"node_id"`
	PoolID                   string   `json:"pool_id"`
	CreatedAt                string   `json:"created_at"`
	MaxGroups                int      `json:"max_groups"`
	MaxArticlesPerHour       int64    `json:"max_articles_per_hour"`
	MaxHeaderBytesPerHour    int64    `json:"max_header_bytes_per_hour,omitempty"`
	PreferredGroupPatterns   []string `json:"preferred_group_patterns,omitempty"`
	ExcludedGroupPatterns    []string `json:"excluded_group_patterns,omitempty"`
	SupportsArticleRangeScan bool     `json:"supports_article_range_scan"`
	SupportsTimeWindowScan   bool     `json:"supports_time_window_scan"`
	RetentionDaysObserved    int      `json:"retention_days_observed"`
	ProviderScope            string   `json:"provider_scope_hash,omitempty"`
	PublishedAt              string   `json:"-"`
	Groups                   []string `json:"-"`
	MaxRangesPerHour         int      `json:"-"`
	MaxBytesPerHour          int64    `json:"-"`
}

type ScannerHeartbeat struct {
	SchemaVersion            string   `json:"schema_version"`
	Type                     string   `json:"type"`
	NodeID                   string   `json:"node_id"`
	PoolID                   string   `json:"pool_id"`
	CreatedAt                string   `json:"created_at"`
	ActiveClaims             []string `json:"active_claims"`
	QueueDepth               int      `json:"queue_depth"`
	CurrentArticlesPerMinute int64    `json:"current_articles_per_minute"`
	Status                   string   `json:"status"`
	PublishedAt              string   `json:"-"`
	Groups                   []string `json:"-"`
}

type GroupObservation struct {
	SchemaVersion        string  `json:"schema_version"`
	Type                 string  `json:"type"`
	ObservationID        string  `json:"observation_id"`
	NodeID               string  `json:"node_id"`
	PoolID               string  `json:"pool_id"`
	Group                string  `json:"group"`
	ProviderScope        string  `json:"provider_scope_hash,omitempty"`
	ObservedAt           string  `json:"observed_at"`
	LowWatermark         int64   `json:"low_water"`
	HighWatermark        int64   `json:"high_water"`
	EstimatedCount       int64   `json:"estimated_count"`
	PostsPerHourEstimate float64 `json:"posts_per_hour_estimate"`
	ScanSupported        bool    `json:"scan_supported"`
	RetentionDays        int     `json:"retention_days_observed"`
	Confidence           float64 `json:"confidence,omitempty"`
}

type CoveragePlan struct {
	SchemaVersion        string                   `json:"schema_version"`
	Type                 string                   `json:"type"`
	PlanID               string                   `json:"plan_id"`
	PoolID               string                   `json:"pool_id"`
	Version              int                      `json:"version"`
	CreatedAt            string                   `json:"created_at"`
	CreatedByNodeID      string                   `json:"created_by_node_id"`
	RequiresPoolApproval bool                     `json:"requires_pool_approval"`
	Policy               CoveragePlanPolicy       `json:"policy"`
	Assignments          []CoveragePlanAssignment `json:"assignments"`
	Group                string                   `json:"-"`
	RangeStart           int64                    `json:"-"`
	RangeEnd             int64                    `json:"-"`
	WindowStart          string                   `json:"-"`
	WindowEnd            string                   `json:"-"`
	Priority             int                      `json:"-"`
}

type CoveragePlanPolicy struct {
	DefaultClaimTTLMinutes  int     `json:"default_claim_ttl_minutes"`
	MinValidatorOverlapPct  int     `json:"min_validator_overlap_percent"`
	TrustedClaimMinScore    float64 `json:"trusted_claim_min_score"`
	AllowUnassignedScanning bool    `json:"allow_unassigned_scanning"`
}

type CoveragePlanAssignment struct {
	AssignmentID         string   `json:"assignment_id"`
	Group                string   `json:"group"`
	Mode                 string   `json:"mode"`
	PrimaryNodes         []string `json:"primary_nodes"`
	ValidatorNodes       []string `json:"validator_nodes"`
	ManifestBuilderNodes []string `json:"manifest_builder_nodes"`
	Priority             int      `json:"priority"`
	MinRedundancy        int      `json:"min_redundancy"`
}

type CoverageAssignment struct {
	SchemaVersion  string `json:"schema_version"`
	Type           string `json:"type"`
	AssignmentID   string `json:"assignment_id"`
	PlanID         string `json:"-"`
	PoolID         string `json:"pool_id"`
	Group          string `json:"group"`
	Mode           string `json:"mode"`
	Role           string `json:"role"`
	AssignedNodeID string `json:"assigned_node_id"`
	ProviderScope  string `json:"provider_scope_hash,omitempty"`
	RangeStart     int64  `json:"range_start,omitempty"`
	RangeEnd       int64  `json:"range_end,omitempty"`
	WindowStart    string `json:"window_start,omitempty"`
	WindowEnd      string `json:"window_end,omitempty"`
	Priority       int    `json:"priority"`
	DueAt          string `json:"-"`
	ExpiresAt      string `json:"expires_at"`
	CreatedAt      string `json:"created_at"`
}

type RangeClaim struct {
	SchemaVersion                     string `json:"schema_version"`
	Type                              string `json:"type"`
	ClaimID                           string `json:"claim_id"`
	AssignmentID                      string `json:"assignment_id"`
	PoolID                            string `json:"pool_id"`
	Group                             string `json:"group"`
	NodeID                            string `json:"claimant_node_id"`
	ProviderScope                     string `json:"provider_scope_hash,omitempty"`
	RangeStart                        int64  `json:"range_start"`
	RangeEnd                          int64  `json:"range_end"`
	ClaimedAt                         string `json:"claimed_at"`
	ExpiresAt                         string `json:"expires_at"`
	ClaimMode                         string `json:"claim_mode"`
	ExpectedCheckpointIntervalSeconds int    `json:"expected_checkpoint_interval_seconds"`
}

type TimeWindowClaim struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	ClaimID       string `json:"claim_id"`
	AssignmentID  string `json:"assignment_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	NodeID        string `json:"claimant_node_id"`
	ProviderScope string `json:"provider_scope_hash,omitempty"`
	WindowStart   string `json:"window_start"`
	WindowEnd     string `json:"window_end"`
	ClaimedAt     string `json:"claimed_at"`
	ExpiresAt     string `json:"expires_at"`
	ClaimMode     string `json:"claim_mode"`
}

type CoverageCheckpoint struct {
	SchemaVersion       string `json:"schema_version"`
	Type                string `json:"type"`
	CheckpointID        string `json:"checkpoint_id"`
	PoolID              string `json:"pool_id"`
	NodeID              string `json:"node_id"`
	Group               string `json:"group"`
	ProviderScope       string `json:"provider_scope_hash,omitempty"`
	ClaimID             string `json:"claim_id"`
	RangeStart          int64  `json:"range_start"`
	RangeCurrent        int64  `json:"range_current"`
	RangeEnd            int64  `json:"range_end"`
	WindowStart         string `json:"window_start,omitempty"`
	WindowEnd           string `json:"window_end,omitempty"`
	ReleaseCardsEmitted int    `json:"release_cards_emitted"`
	ManifestsEmitted    int    `json:"manifests_emitted"`
	Errors              int    `json:"errors"`
	CheckedAt           string `json:"checked_at"`
	LowWatermark        int64  `json:"-"`
	HighWatermark       int64  `json:"-"`
	CreatedAt           string `json:"-"`
}

type RangeComplete struct {
	SchemaVersion          string `json:"schema_version"`
	Type                   string `json:"type"`
	OutcomeID              string `json:"completion_id"`
	ClaimID                string `json:"claim_id"`
	AssignmentID           string `json:"-"`
	PoolID                 string `json:"pool_id"`
	Group                  string `json:"group"`
	NodeID                 string `json:"node_id"`
	ProviderScope          string `json:"provider_scope_hash,omitempty"`
	RangeStart             int64  `json:"range_start"`
	RangeEnd               int64  `json:"range_end"`
	ArticlesSeen           int64  `json:"articles_seen"`
	HeadersProcessed       int64  `json:"headers_processed"`
	ReleaseCount           int    `json:"release_cards_emitted"`
	ManifestsEmitted       int    `json:"manifests_emitted"`
	DedupCandidatesSkipped int    `json:"dedup_candidates_skipped"`
	ErrorCount             int    `json:"error_count"`
	RangeFingerprint       string `json:"range_fingerprint,omitempty"`
	CompletedAt            string `json:"completed_at"`
}

type RangeFailed struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	OutcomeID     string `json:"failure_id"`
	ClaimID       string `json:"claim_id"`
	AssignmentID  string `json:"-"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	NodeID        string `json:"node_id"`
	ProviderScope string `json:"provider_scope_hash,omitempty"`
	RangeStart    int64  `json:"range_start"`
	RangeEnd      int64  `json:"range_end"`
	Reason        string `json:"reason_code"`
	Retryable     bool   `json:"retryable"`
	FailedAt      string `json:"failed_at"`
}

func Validate(eventType string, body any, now time.Time, futureTolerance time.Duration) error {
	switch eventType {
	case TypeScannerCapacity:
		item, ok := body.(ScannerCapacity)
		if !ok {
			return fmt.Errorf("invalid scanner capacity body")
		}
		return validateScannerCapacity(item, now, futureTolerance)
	case TypeScannerHeartbeat:
		item, ok := body.(ScannerHeartbeat)
		if !ok {
			return fmt.Errorf("invalid scanner heartbeat body")
		}
		return validateScannerHeartbeat(item, now, futureTolerance)
	case TypeGroupObservation:
		item, ok := body.(GroupObservation)
		if !ok {
			return fmt.Errorf("invalid group observation body")
		}
		return validateGroupObservation(item, now, futureTolerance)
	case TypeCoveragePlan:
		item, ok := body.(CoveragePlan)
		if !ok {
			return fmt.Errorf("invalid coverage plan body")
		}
		return validatePlan(item, now, futureTolerance)
	case TypeCoverageAssignment:
		item, ok := body.(CoverageAssignment)
		if !ok {
			return fmt.Errorf("invalid coverage assignment body")
		}
		return validateAssignment(item, now, futureTolerance)
	case TypeRangeClaim:
		item, ok := body.(RangeClaim)
		if !ok {
			return fmt.Errorf("invalid range claim body")
		}
		return validateRangeClaim(item, now, futureTolerance)
	case TypeTimeWindowClaim:
		item, ok := body.(TimeWindowClaim)
		if !ok {
			return fmt.Errorf("invalid time window claim body")
		}
		return validateTimeWindowClaim(item, now, futureTolerance)
	case TypeCoverageCheckpoint:
		item, ok := body.(CoverageCheckpoint)
		if !ok {
			return fmt.Errorf("invalid coverage checkpoint body")
		}
		return validateCheckpoint(item, now, futureTolerance)
	case TypeRangeComplete:
		item, ok := body.(RangeComplete)
		if !ok {
			return fmt.Errorf("invalid range complete body")
		}
		return validateRangeComplete(item, now, futureTolerance)
	case TypeRangeFailed:
		item, ok := body.(RangeFailed)
		if !ok {
			return fmt.Errorf("invalid range failed body")
		}
		return validateRangeFailed(item, now, futureTolerance)
	default:
		return fmt.Errorf("unsupported coverage event type")
	}
}

func HashBody(body any) (string, error) {
	hash, _, err := canonical.BodyHash(body)
	return hash, err
}

func BodySchema(eventType string) string {
	switch eventType {
	case TypeScannerCapacity:
		return ScannerCapacityBodySchema
	case TypeScannerHeartbeat:
		return ScannerHeartbeatBodySchema
	case TypeGroupObservation:
		return GroupObservationBodySchema
	case TypeCoveragePlan:
		return CoveragePlanBodySchema
	case TypeCoverageAssignment:
		return CoverageAssignmentBodySchema
	case TypeRangeClaim:
		return RangeClaimBodySchema
	case TypeTimeWindowClaim:
		return TimeWindowClaimBodySchema
	case TypeCoverageCheckpoint:
		return CoverageCheckpointBodySchema
	case TypeRangeComplete:
		return RangeCompleteBodySchema
	case TypeRangeFailed:
		return RangeFailedBodySchema
	default:
		return "gonzbnet.Coverage/1.0"
	}
}

func EventTypes() []string {
	return []string{
		TypeScannerCapacity,
		TypeScannerHeartbeat,
		TypeGroupObservation,
		TypeCoveragePlan,
		TypeCoverageAssignment,
		TypeRangeClaim,
		TypeTimeWindowClaim,
		TypeCoverageCheckpoint,
		TypeRangeComplete,
		TypeRangeFailed,
	}
}

func validateScannerCapacity(in ScannerCapacity, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeScannerCapacity); err != nil {
		return err
	}
	if strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.PoolID) == "" {
		return fmt.Errorf("node_id and pool_id are required")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
	}
	if in.MaxGroups < 0 || in.MaxArticlesPerHour < 0 || in.MaxHeaderBytesPerHour < 0 || in.RetentionDaysObserved < 0 {
		return fmt.Errorf("capacity values must not be negative")
	}
	return nil
}

func validateScannerHeartbeat(in ScannerHeartbeat, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeScannerHeartbeat); err != nil {
		return err
	}
	if strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.PoolID) == "" {
		return fmt.Errorf("node_id and pool_id are required")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		return fmt.Errorf("status is required")
	}
	switch status {
	case "healthy", "degraded", "paused", "offline":
		return nil
	default:
		return fmt.Errorf("unsupported scanner heartbeat status")
	}
}

func validateGroupObservation(in GroupObservation, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeGroupObservation); err != nil {
		return err
	}
	if strings.TrimSpace(in.ObservationID) == "" || strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("observation_id, node_id, pool_id, and group are required")
	}
	if err := validateTimestamp("observed_at", in.ObservedAt, now, tolerance); err != nil {
		return err
	}
	if in.EstimatedCount < 0 || in.PostsPerHourEstimate < 0 || in.RetentionDays < 0 || in.Confidence < 0 || in.Confidence > 1 {
		return fmt.Errorf("observation values are invalid")
	}
	return validateRange(in.LowWatermark, in.HighWatermark)
}

func validatePlan(in CoveragePlan, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeCoveragePlan); err != nil {
		return err
	}
	if strings.TrimSpace(in.PlanID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.CreatedByNodeID) == "" {
		return fmt.Errorf("plan_id, pool_id, and created_by_node_id are required")
	}
	if in.Version <= 0 || len(in.Assignments) == 0 {
		return fmt.Errorf("plan version and assignments are required")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
	}
	if in.Policy.DefaultClaimTTLMinutes < 0 || in.Policy.MinValidatorOverlapPct < 0 || in.Policy.MinValidatorOverlapPct > 100 || in.Policy.TrustedClaimMinScore < 0 || in.Policy.TrustedClaimMinScore > 1 {
		return fmt.Errorf("plan policy values are invalid")
	}
	for _, assignment := range in.Assignments {
		if strings.TrimSpace(assignment.AssignmentID) == "" || strings.TrimSpace(assignment.Group) == "" || strings.TrimSpace(assignment.Mode) == "" {
			return fmt.Errorf("plan assignment fields are required")
		}
		if len(assignment.PrimaryNodes) == 0 && len(assignment.ValidatorNodes) == 0 && len(assignment.ManifestBuilderNodes) == 0 {
			return fmt.Errorf("plan assignment must name at least one node")
		}
	}
	return nil
}

func validateAssignment(in CoverageAssignment, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeCoverageAssignment); err != nil {
		return err
	}
	if strings.TrimSpace(in.AssignmentID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" || strings.TrimSpace(in.AssignedNodeID) == "" {
		return fmt.Errorf("assignment_id, pool_id, group, and assigned_node_id are required")
	}
	if strings.TrimSpace(in.Mode) != "article_range" && strings.TrimSpace(in.Mode) != "time_window" {
		return fmt.Errorf("unsupported assignment mode")
	}
	switch strings.TrimSpace(in.Role) {
	case "primary_scanner", "validator", "manifest_builder":
	default:
		return fmt.Errorf("unsupported assignment role")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(in.ExpiresAt)); err != nil {
		return fmt.Errorf("expires_at must be RFC3339")
	}
	if in.Mode == "article_range" && (strings.TrimSpace(in.WindowStart) != "" || strings.TrimSpace(in.WindowEnd) != "") {
		return fmt.Errorf("article_range assignment cannot contain a time window")
	}
	if in.Mode == "time_window" && (in.RangeStart != 0 || in.RangeEnd != 0) {
		return fmt.Errorf("time_window assignment cannot contain an article range")
	}
	return validateRangeOrWindow(in.RangeStart, in.RangeEnd, in.WindowStart, in.WindowEnd)
}

func validateRangeClaim(in RangeClaim, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeRangeClaim); err != nil {
		return err
	}
	if strings.TrimSpace(in.ClaimID) == "" || strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("claim_id, node_id, and group are required")
	}
	if err := validateTimestamp("claimed_at", in.ClaimedAt, now, tolerance); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(in.ExpiresAt)); err != nil {
		return fmt.Errorf("expires_at must be RFC3339")
	}
	if strings.TrimSpace(in.ClaimMode) == "" {
		return fmt.Errorf("claim_mode is required")
	}
	if in.ExpectedCheckpointIntervalSeconds < 0 {
		return fmt.Errorf("expected_checkpoint_interval_seconds must not be negative")
	}
	return validateRange(in.RangeStart, in.RangeEnd)
}

func validateTimeWindowClaim(in TimeWindowClaim, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeTimeWindowClaim); err != nil {
		return err
	}
	if strings.TrimSpace(in.ClaimID) == "" || strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("claim_id, node_id, and group are required")
	}
	if err := validateTimestamp("claimed_at", in.ClaimedAt, now, tolerance); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(in.ExpiresAt)); err != nil {
		return fmt.Errorf("expires_at must be RFC3339")
	}
	if strings.TrimSpace(in.ClaimMode) == "" {
		return fmt.Errorf("claim_mode is required")
	}
	return validateWindow(in.WindowStart, in.WindowEnd)
}

func validateCheckpoint(in CoverageCheckpoint, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeCoverageCheckpoint); err != nil {
		return err
	}
	if strings.TrimSpace(in.CheckpointID) == "" || strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" || strings.TrimSpace(in.ClaimID) == "" {
		return fmt.Errorf("checkpoint_id, node_id, pool_id, group, and claim_id are required")
	}
	if err := validateTimestamp("checked_at", in.CheckedAt, now, tolerance); err != nil {
		return err
	}
	if in.RangeStart <= 0 || in.RangeCurrent < in.RangeStart || in.RangeEnd < in.RangeCurrent || in.ReleaseCardsEmitted < 0 || in.ManifestsEmitted < 0 || in.Errors < 0 {
		return fmt.Errorf("checkpoint progress is invalid")
	}
	return nil
}

func validateRangeComplete(in RangeComplete, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeRangeComplete); err != nil {
		return err
	}
	if strings.TrimSpace(in.OutcomeID) == "" || strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("outcome_id, node_id, and group are required")
	}
	if err := validateTimestamp("completed_at", in.CompletedAt, now, tolerance); err != nil {
		return err
	}
	if in.ArticlesSeen < 0 || in.HeadersProcessed < 0 || in.ReleaseCount < 0 || in.ManifestsEmitted < 0 || in.DedupCandidatesSkipped < 0 || in.ErrorCount < 0 {
		return fmt.Errorf("completion counters must not be negative")
	}
	return validateRange(in.RangeStart, in.RangeEnd)
}

func validateRangeFailed(in RangeFailed, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeRangeFailed); err != nil {
		return err
	}
	if strings.TrimSpace(in.OutcomeID) == "" || strings.TrimSpace(in.NodeID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("outcome_id, node_id, and group are required")
	}
	if err := validateTimestamp("failed_at", in.FailedAt, now, tolerance); err != nil {
		return err
	}
	if strings.TrimSpace(in.Reason) == "" {
		return fmt.Errorf("reason_code is required")
	}
	return validateRange(in.RangeStart, in.RangeEnd)
}

func base(schemaVersion, eventType, expected string) error {
	if strings.TrimSpace(schemaVersion) != "1.0" {
		return fmt.Errorf("unsupported coverage schema_version")
	}
	if strings.TrimSpace(eventType) != expected {
		return fmt.Errorf("unsupported coverage event type")
	}
	return nil
}

func validateRangeOrWindow(start, end int64, windowStart, windowEnd string) error {
	hasRange := start > 0 || end > 0
	hasWindow := strings.TrimSpace(windowStart) != "" || strings.TrimSpace(windowEnd) != ""
	if hasRange == hasWindow {
		return fmt.Errorf("exactly one range or time window is required")
	}
	if hasRange {
		return validateRange(start, end)
	}
	return validateWindow(windowStart, windowEnd)
}

func validateRange(start, end int64) error {
	if start <= 0 || end <= 0 || end < start {
		return fmt.Errorf("invalid range")
	}
	return nil
}

func validateWindow(start, end string) error {
	parsedStart, err := time.Parse(time.RFC3339, strings.TrimSpace(start))
	if err != nil {
		return fmt.Errorf("window_start must be RFC3339")
	}
	parsedEnd, err := time.Parse(time.RFC3339, strings.TrimSpace(end))
	if err != nil {
		return fmt.Errorf("window_end must be RFC3339")
	}
	if !parsedEnd.After(parsedStart) {
		return fmt.Errorf("window_end must be after window_start")
	}
	return nil
}

func validateTimestamp(field, value string, now time.Time, tolerance time.Duration) error {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s must be RFC3339", field)
	}
	if tolerance <= 0 {
		tolerance = 2 * time.Minute
	}
	if !now.IsZero() && parsed.After(now.UTC().Add(tolerance)) {
		return fmt.Errorf("%s is too far in the future", field)
	}
	return nil
}
