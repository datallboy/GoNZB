package coverage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCoverageAssignmentValidationAndHash(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := CoverageAssignment{
		SchemaVersion:  "1.0",
		Type:           TypeCoverageAssignment,
		AssignmentID:   "assign_1",
		PoolID:         "pool.local",
		Group:          "alt.binaries.example",
		Mode:           "article_range",
		Role:           "primary_scanner",
		AssignedNodeID: "node_1",
		RangeStart:     100,
		RangeEnd:       200,
		Priority:       10,
		ExpiresAt:      now.Add(time.Hour).Format(time.RFC3339),
		CreatedAt:      now.Format(time.RFC3339),
	}
	if err := Validate(TypeCoverageAssignment, item, now, 2*time.Minute); err != nil {
		t.Fatalf("validate assignment: %v", err)
	}
	first, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash assignment: %v", err)
	}
	second, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash assignment again: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("hash should be deterministic: %q %q", first, second)
	}

	item.RangeEnd = 99
	if err := Validate(TypeCoverageAssignment, item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid range to fail")
	}
}

func TestActiveCoverageBodiesUseAddendumWireNames(t *testing.T) {
	assignment := CoverageAssignment{
		AssignmentID: "assign_1", PlanID: "internal-plan", Mode: "article_range",
		Role: "primary_scanner", ExpiresAt: "2026-07-09T13:00:00Z", DueAt: "legacy",
	}
	assertCoverageJSONKeys(t, assignment,
		[]string{"assignment_id", "mode", "role", "expires_at"},
		[]string{"plan_id", "due_at"},
	)
	assertCoverageJSONKeys(t, RangeClaim{ClaimID: "claim_1", NodeID: "node_1", ClaimMode: "primary_scan"},
		[]string{"claim_id", "claimant_node_id", "claim_mode"},
		[]string{"node_id"},
	)
	assertCoverageJSONKeys(t, RangeComplete{OutcomeID: "complete_1", ReleaseCount: 2},
		[]string{"completion_id", "release_cards_emitted"},
		[]string{"outcome_id", "release_count"},
	)
	assertCoverageJSONKeys(t, RangeFailed{OutcomeID: "failed_1", Reason: "provider_timeout", Retryable: true},
		[]string{"failure_id", "reason_code", "retryable"},
		[]string{"outcome_id", "reason"},
	)
}

func TestRemainingCoverageBodiesValidateSpecFields(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := Validate(TypeScannerCapacity, ScannerCapacity{
		SchemaVersion: "1.0", Type: TypeScannerCapacity, NodeID: "node_1", PoolID: "pool.local",
		CreatedAt: now.Format(time.RFC3339), MaxGroups: 10, MaxArticlesPerHour: 100,
	}, now, 2*time.Minute); err != nil {
		t.Fatalf("capacity validation: %v", err)
	}
	if err := Validate(TypeScannerHeartbeat, ScannerHeartbeat{
		SchemaVersion: "1.0", Type: TypeScannerHeartbeat, NodeID: "node_1", PoolID: "pool.local",
		CreatedAt: now.Format(time.RFC3339), Status: "healthy",
	}, now, 2*time.Minute); err != nil {
		t.Fatalf("heartbeat validation: %v", err)
	}
	if err := Validate(TypeGroupObservation, GroupObservation{
		SchemaVersion: "1.0", Type: TypeGroupObservation, ObservationID: "obs_1", NodeID: "node_1",
		PoolID: "pool.local", Group: "alt.binaries.example", ObservedAt: now.Format(time.RFC3339),
		LowWatermark: 1, HighWatermark: 2, ScanSupported: true,
	}, now, 2*time.Minute); err != nil {
		t.Fatalf("observation validation: %v", err)
	}
	if err := Validate(TypeCoveragePlan, CoveragePlan{
		SchemaVersion: "1.0", Type: TypeCoveragePlan, PlanID: "plan_1", PoolID: "pool.local",
		Version: 1, CreatedAt: now.Format(time.RFC3339), CreatedByNodeID: "node_1",
		Assignments: []CoveragePlanAssignment{{AssignmentID: "assign_1", Group: "alt.binaries.example", Mode: "article_range", PrimaryNodes: []string{"node_1"}}},
	}, now, 2*time.Minute); err != nil {
		t.Fatalf("plan validation: %v", err)
	}
	if err := Validate(TypeCoverageCheckpoint, CoverageCheckpoint{
		SchemaVersion: "1.0", Type: TypeCoverageCheckpoint, CheckpointID: "chk_1", PoolID: "pool.local",
		NodeID: "node_1", Group: "alt.binaries.example", ClaimID: "claim_1", RangeStart: 1,
		RangeCurrent: 2, RangeEnd: 3, CheckedAt: now.Format(time.RFC3339),
	}, now, 2*time.Minute); err != nil {
		t.Fatalf("checkpoint validation: %v", err)
	}
}

func assertCoverageJSONKeys(t *testing.T, value any, required, forbidden []string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal coverage body: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode coverage body: %v", err)
	}
	for _, key := range required {
		if _, ok := object[key]; !ok {
			t.Fatalf("required key %q missing from %s", key, raw)
		}
	}
	for _, key := range forbidden {
		if _, ok := object[key]; ok {
			t.Fatalf("forbidden legacy key %q present in %s", key, raw)
		}
	}
}

func TestTimeWindowClaimRequiresValidWindowAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := TimeWindowClaim{
		SchemaVersion: "1.0",
		Type:          TypeTimeWindowClaim,
		ClaimID:       "claim_1",
		AssignmentID:  "assign_1",
		PoolID:        "pool.local",
		Group:         "alt.binaries.example",
		NodeID:        "node_1",
		WindowStart:   now.Add(-time.Hour).Format(time.RFC3339),
		WindowEnd:     now.Format(time.RFC3339),
		ClaimedAt:     now.Format(time.RFC3339),
		ExpiresAt:     now.Add(time.Hour).Format(time.RFC3339),
		ClaimMode:     "primary_scan",
	}
	if err := Validate(TypeTimeWindowClaim, item, now, 2*time.Minute); err != nil {
		t.Fatalf("validate time window claim: %v", err)
	}
	item.WindowEnd = item.WindowStart
	if err := Validate(TypeTimeWindowClaim, item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid time window to fail")
	}
}

func TestScannerHeartbeatValidationAndHash(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := ScannerHeartbeat{
		SchemaVersion: "1.0",
		Type:          TypeScannerHeartbeat,
		NodeID:        "node_1",
		PoolID:        "pool.local",
		CreatedAt:     now.Format(time.RFC3339),
		Groups:        []string{"alt.binaries.example"},
		ActiveClaims:  []string{"claim_1"},
		Status:        "healthy",
	}
	if err := Validate(TypeScannerHeartbeat, item, now, 2*time.Minute); err != nil {
		t.Fatalf("validate heartbeat: %v", err)
	}
	first, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash heartbeat: %v", err)
	}
	second, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash heartbeat again: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("hash should be deterministic: %q %q", first, second)
	}
	item.Status = "bad"
	if err := Validate(TypeScannerHeartbeat, item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid heartbeat status to fail")
	}
}
