package health

import (
	"testing"
	"time"
)

func TestValidateAcceptsCompleteAndRejectsTamperedCounts(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Attestation{
		SchemaVersion:     "1.0",
		Type:              Type,
		ReleaseID:         "rel_1",
		ManifestID:        "man_1",
		CheckedAt:         now.Format(time.RFC3339),
		Status:            StatusComplete,
		ArticlesTotal:     10,
		ArticlesAvailable: 10,
		Confidence:        0.95,
		Method:            "article_stat_sampled",
	}
	if err := Validate(item, now, time.Minute); err != nil {
		t.Fatalf("expected valid attestation: %v", err)
	}
	item.ArticlesAvailable = 11
	if err := Validate(item, now, time.Minute); err == nil {
		t.Fatalf("expected available > total to fail")
	}
}

func TestValidateRejectsFutureAndUnknownStatus(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Attestation{
		SchemaVersion: "1.0",
		Type:          Type,
		ReleaseID:     "rel_1",
		CheckedAt:     now.Add(10 * time.Minute).Format(time.RFC3339),
		Status:        StatusComplete,
		Confidence:    0.8,
	}
	if err := Validate(item, now, time.Minute); err == nil {
		t.Fatalf("expected future checked_at to fail")
	}
	item.CheckedAt = now.Format(time.RFC3339)
	item.Status = "excellent"
	if err := Validate(item, now, time.Minute); err == nil {
		t.Fatalf("expected unknown status to fail")
	}
}

func TestScoresAndTrustDeltasAreDeterministic(t *testing.T) {
	complete := Attestation{
		Status:            StatusComplete,
		ArticlesTotal:     100,
		ArticlesAvailable: 100,
		Confidence:        0.9,
	}
	incomplete := complete
	incomplete.Status = StatusIncomplete
	incomplete.ArticlesAvailable = 60
	incomplete.MissingArticles = 40
	if AvailabilityScore(complete) <= AvailabilityScore(incomplete) {
		t.Fatalf("expected complete score to beat incomplete score")
	}
	delta, reason := TrustDelta(complete)
	if delta <= 0 || reason != "health_complete_verified" {
		t.Fatalf("expected positive complete trust delta, got %.2f %q", delta, reason)
	}
	falsePositive := complete
	falsePositive.ArticlesAvailable = 80
	falsePositive.MissingArticles = 20
	delta, reason = TrustDelta(falsePositive)
	if delta >= 0 || reason != "health_false_positive" {
		t.Fatalf("expected false positive trust penalty, got %.2f %q", delta, reason)
	}
}

func TestRankingScoreUsesAvailability(t *testing.T) {
	low := RankingScore(0.8, 1, 0.1, 0, 0.5)
	high := RankingScore(0.8, 1, 0.9, 0, 0.5)
	if high <= low {
		t.Fatalf("expected higher availability to improve ranking, low=%.3f high=%.3f", low, high)
	}
}
