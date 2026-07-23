package pgindex

import "testing"

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
