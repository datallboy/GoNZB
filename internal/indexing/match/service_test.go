package match

import (
	"testing"
	"time"
)

func TestMatchHighConfidenceQuotedFilename(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 2, 14, 0, 0, 0, time.UTC)

	got := svc.Match(Candidate{
		ArticleNumber: 123456,
		MessageID:     "<part1@upload.example>",
		Subject:       `Cool.Movie.2025 "cool.movie.2025.r00" yEnc (3/20)`,
		Poster:        `Uploader <poster@example.com>`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.movies:12345 alt.binaries.hdtv:12346`,
	})

	if got.ReleaseName != "Cool.Movie.2025" {
		t.Fatalf("expected release name %q, got %q", "Cool.Movie.2025", got.ReleaseName)
	}
	if got.FileName != "cool.movie.2025.r00" {
		t.Fatalf("expected file name %q, got %q", "cool.movie.2025.r00", got.FileName)
	}
	if got.PartNumber != 3 || got.TotalParts != 20 {
		t.Fatalf("expected part 3/20, got %d/%d", got.PartNumber, got.TotalParts)
	}
	if got.MatchStatus != "matched" {
		t.Fatalf("expected matched status, got %q", got.MatchStatus)
	}
	if got.MatchConfidence < 0.85 {
		t.Fatalf("expected high confidence, got %f", got.MatchConfidence)
	}

	summary, ok := got.GroupingEvidence["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary evidence map, got %#v", got.GroupingEvidence["summary"])
	}
	if summary["short_circuited_after"] == "" {
		t.Fatalf("expected short-circuit marker in summary, got %#v", summary)
	}
}

func TestMatchLowConfidenceContextFallbackStaysDeterministic(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 2, 16, 20, 0, 0, time.UTC)

	first := svc.Match(Candidate{
		ArticleNumber: 200101,
		MessageID:     "<alpha@host.example>",
		Subject:       `[]`,
		Poster:        `weak.poster@example.com`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.misc:200101`,
	})
	second := svc.Match(Candidate{
		ArticleNumber: 200199,
		MessageID:     "<beta@host.example>",
		Subject:       `[]`,
		Poster:        `weak.poster@example.com`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.misc:200199`,
	})

	if first.MatchStatus != "low_confidence" {
		t.Fatalf("expected low confidence status, got %q", first.MatchStatus)
	}
	if first.BinaryKey != second.BinaryKey {
		t.Fatalf("expected deterministic contextual grouping, got %q vs %q", first.BinaryKey, second.BinaryKey)
	}

	fallback, ok := first.GroupingEvidence["fallback"].(map[string]any)
	if !ok {
		t.Fatalf("expected fallback evidence map, got %#v", first.GroupingEvidence["fallback"])
	}
	if fallback["used"] != true {
		t.Fatalf("expected fallback used marker, got %#v", fallback)
	}
}

func TestMatchUsesStructuredOverviewEvidence(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<structured@host.example>",
		Subject:   `obfuscated post`,
		RawOverview: map[string]any{
			"name":  "episode.part01.rar",
			"part":  1,
			"total": 12,
			"size":  7340032,
		},
	})

	if got.FileName != "episode.part01.rar" {
		t.Fatalf("expected file name from structured data, got %q", got.FileName)
	}
	if got.TotalParts != 12 {
		t.Fatalf("expected total parts 12, got %d", got.TotalParts)
	}
	if got.MatchConfidence <= 0 {
		t.Fatalf("expected structured evidence to contribute confidence, got %f", got.MatchConfidence)
	}
	if _, ok := got.GroupingEvidence["structured_markers"]; !ok {
		t.Fatalf("expected structured markers evidence, got %#v", got.GroupingEvidence)
	}
}
