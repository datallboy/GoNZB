package coverage

import (
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	TypeScannerCapacity    = "ScannerCapacity"
	TypeGroupObservation   = "GroupObservation"
	TypeCoveragePlan       = "CoveragePlan"
	TypeCoverageAssignment = "CoverageAssignment"
	TypeRangeClaim         = "RangeClaim"
	TypeTimeWindowClaim    = "TimeWindowClaim"
	TypeCoverageCheckpoint = "CoverageCheckpoint"
	TypeRangeComplete      = "RangeComplete"
	TypeRangeFailed        = "RangeFailed"

	ScannerCapacityBodySchema    = "gonzbnet.ScannerCapacity/1.0"
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
	SchemaVersion    string   `json:"schema_version"`
	Type             string   `json:"type"`
	NodeID           string   `json:"node_id"`
	PublishedAt      string   `json:"published_at"`
	Groups           []string `json:"groups"`
	MaxRangesPerHour int      `json:"max_ranges_per_hour"`
	MaxBytesPerHour  int64    `json:"max_bytes_per_hour"`
}

type GroupObservation struct {
	SchemaVersion string  `json:"schema_version"`
	Type          string  `json:"type"`
	ObservationID string  `json:"observation_id"`
	PoolID        string  `json:"pool_id"`
	Group         string  `json:"group"`
	ObservedAt    string  `json:"observed_at"`
	LowWatermark  int64   `json:"low_watermark"`
	HighWatermark int64   `json:"high_watermark"`
	RetentionDays int     `json:"retention_days"`
	Confidence    float64 `json:"confidence"`
}

type CoveragePlan struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	PlanID        string `json:"plan_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	RangeStart    int64  `json:"range_start,omitempty"`
	RangeEnd      int64  `json:"range_end,omitempty"`
	WindowStart   string `json:"window_start,omitempty"`
	WindowEnd     string `json:"window_end,omitempty"`
	Priority      int    `json:"priority"`
	CreatedAt     string `json:"created_at"`
}

type CoverageAssignment struct {
	SchemaVersion  string `json:"schema_version"`
	Type           string `json:"type"`
	AssignmentID   string `json:"assignment_id"`
	PlanID         string `json:"plan_id,omitempty"`
	PoolID         string `json:"pool_id"`
	Group          string `json:"group"`
	AssignedNodeID string `json:"assigned_node_id"`
	RangeStart     int64  `json:"range_start,omitempty"`
	RangeEnd       int64  `json:"range_end,omitempty"`
	WindowStart    string `json:"window_start,omitempty"`
	WindowEnd      string `json:"window_end,omitempty"`
	Priority       int    `json:"priority"`
	DueAt          string `json:"due_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type RangeClaim struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	ClaimID       string `json:"claim_id"`
	AssignmentID  string `json:"assignment_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	NodeID        string `json:"node_id"`
	RangeStart    int64  `json:"range_start"`
	RangeEnd      int64  `json:"range_end"`
	ClaimedAt     string `json:"claimed_at"`
	ExpiresAt     string `json:"expires_at"`
}

type TimeWindowClaim struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	ClaimID       string `json:"claim_id"`
	AssignmentID  string `json:"assignment_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	NodeID        string `json:"node_id"`
	WindowStart   string `json:"window_start"`
	WindowEnd     string `json:"window_end"`
	ClaimedAt     string `json:"claimed_at"`
	ExpiresAt     string `json:"expires_at"`
}

type CoverageCheckpoint struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	CheckpointID  string `json:"checkpoint_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	LowWatermark  int64  `json:"low_watermark"`
	HighWatermark int64  `json:"high_watermark"`
	CreatedAt     string `json:"created_at"`
}

type RangeComplete struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	OutcomeID     string `json:"outcome_id"`
	ClaimID       string `json:"claim_id"`
	AssignmentID  string `json:"assignment_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	NodeID        string `json:"node_id"`
	RangeStart    int64  `json:"range_start"`
	RangeEnd      int64  `json:"range_end"`
	ReleaseCount  int    `json:"release_count"`
	CompletedAt   string `json:"completed_at"`
}

type RangeFailed struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	OutcomeID     string `json:"outcome_id"`
	ClaimID       string `json:"claim_id"`
	AssignmentID  string `json:"assignment_id"`
	PoolID        string `json:"pool_id"`
	Group         string `json:"group"`
	NodeID        string `json:"node_id"`
	RangeStart    int64  `json:"range_start"`
	RangeEnd      int64  `json:"range_end"`
	Reason        string `json:"reason"`
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
	if strings.TrimSpace(in.NodeID) == "" {
		return fmt.Errorf("node_id is required")
	}
	if err := validateTimestamp("published_at", in.PublishedAt, now, tolerance); err != nil {
		return err
	}
	if in.MaxRangesPerHour < 0 || in.MaxBytesPerHour < 0 {
		return fmt.Errorf("capacity values must not be negative")
	}
	return nil
}

func validateGroupObservation(in GroupObservation, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeGroupObservation); err != nil {
		return err
	}
	if strings.TrimSpace(in.ObservationID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("observation_id, pool_id, and group are required")
	}
	if err := validateTimestamp("observed_at", in.ObservedAt, now, tolerance); err != nil {
		return err
	}
	return validateRange(in.LowWatermark, in.HighWatermark)
}

func validatePlan(in CoveragePlan, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeCoveragePlan); err != nil {
		return err
	}
	if strings.TrimSpace(in.PlanID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("plan_id, pool_id, and group are required")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
	}
	return validateRangeOrWindow(in.RangeStart, in.RangeEnd, in.WindowStart, in.WindowEnd)
}

func validateAssignment(in CoverageAssignment, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeCoverageAssignment); err != nil {
		return err
	}
	if strings.TrimSpace(in.AssignmentID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" || strings.TrimSpace(in.AssignedNodeID) == "" {
		return fmt.Errorf("assignment_id, pool_id, group, and assigned_node_id are required")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
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
	return validateWindow(in.WindowStart, in.WindowEnd)
}

func validateCheckpoint(in CoverageCheckpoint, now time.Time, tolerance time.Duration) error {
	if err := base(in.SchemaVersion, in.Type, TypeCoverageCheckpoint); err != nil {
		return err
	}
	if strings.TrimSpace(in.CheckpointID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.Group) == "" {
		return fmt.Errorf("checkpoint_id, pool_id, and group are required")
	}
	if err := validateTimestamp("created_at", in.CreatedAt, now, tolerance); err != nil {
		return err
	}
	return validateRange(in.LowWatermark, in.HighWatermark)
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
