package pgindex

import (
	"strings"
	"testing"
)

func TestInspectCandidateFilterPasswordRequiresEncryptedRelease(t *testing.T) {
	filter, err := inspectCandidateFilter("inspect_password")
	if err != nil {
		t.Fatalf("inspectCandidateFilter() error = %v", err)
	}

	if !strings.Contains(filter, "r.encrypted = TRUE") {
		t.Fatalf("expected inspect_password filter to require encrypted releases, got %q", filter)
	}
}
