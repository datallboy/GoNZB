package pgindex

import (
	"testing"
	"time"
)

func TestSubjectCohortRunLimit(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		want     int
	}{
		{name: "empty", capacity: 0, want: 0},
		{name: "remaining capacity", capacity: 800, want: 800},
		{name: "bounded chunk", capacity: 12000, want: articleCohortSubjectRunLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subjectCohortRunLimit(tt.capacity); got != tt.want {
				t.Fatalf("subjectCohortRunLimit(%d) = %d, want %d", tt.capacity, got, tt.want)
			}
		})
	}
}

func TestArticleCohortSchedulerRequestTargetWindow(t *testing.T) {
	start := time.Date(2026, 7, 24, 14, 5, 0, 0, time.FixedZone("offset", -4*60*60))
	end := start.Add(20 * time.Minute)

	req := ArticleCohortSchedulerRequest{
		TargetWindowStart: &start,
		TargetWindowEnd:   &end,
	}
	if !req.HasTargetWindow() {
		t.Fatal("expected valid target window")
	}
	gotStart, gotEnd := articleCohortTargetWindowArgs(req)
	if gotStart.Location() != time.UTC || gotEnd.Location() != time.UTC {
		t.Fatalf("target window was not normalized to UTC: %s..%s", gotStart, gotEnd)
	}

	req.TargetWindowEnd = nil
	if req.HasTargetWindow() {
		t.Fatal("expected partial target window to be disabled")
	}
}
