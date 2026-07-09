package coverage

import (
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
		AssignedNodeID: "node_1",
		RangeStart:     100,
		RangeEnd:       200,
		Priority:       10,
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
	}
	if err := Validate(TypeTimeWindowClaim, item, now, 2*time.Minute); err != nil {
		t.Fatalf("validate time window claim: %v", err)
	}
	item.WindowEnd = item.WindowStart
	if err := Validate(TypeTimeWindowClaim, item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid time window to fail")
	}
}
