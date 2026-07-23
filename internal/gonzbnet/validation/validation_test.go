package validation

import (
	"testing"
	"time"
)

func TestArticleAvailabilityValidationAndHash(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := ArticleAvailabilityAttestation{
		SchemaVersion:     "1.0",
		Type:              TypeArticleAvailabilityAttestation,
		ReleaseID:         "rel_1",
		ManifestID:        "man_1",
		CheckedAt:         now.Format(time.RFC3339),
		Status:            StatusUnverified,
		ArticlesTotal:     10,
		ArticlesAvailable: 0,
		MissingArticles:   0,
		Confidence:        0.2,
		Method:            "manifest_structure_validation",
	}
	if err := ValidateArticleAvailability(item, now, 2*time.Minute); err != nil {
		t.Fatalf("validate availability: %v", err)
	}
	first, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash availability: %v", err)
	}
	second, err := HashBody(item)
	if err != nil {
		t.Fatalf("hash availability again: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("hash should be deterministic: %q %q", first, second)
	}

	item.ArticlesAvailable = 11
	if err := ValidateArticleAvailability(item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid article counts to fail")
	}
}

func TestChecksumValidationRejectsBadCounts(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := ChecksumAttestation{
		SchemaVersion:     "1.0",
		Type:              TypeChecksumAttestation,
		ReleaseID:         "rel_1",
		ManifestID:        "man_1",
		CheckedAt:         now.Format(time.RFC3339),
		Status:            StatusSkipped,
		ChecksumsTotal:    1,
		ChecksumsVerified: 1,
		ChecksumsFailed:   1,
		Confidence:        0.1,
		Method:            "checksum_validation_disabled",
	}
	if err := ValidateChecksum(item, now, 2*time.Minute); err == nil {
		t.Fatalf("expected invalid checksum counts to fail")
	}
}

func TestValidationRequestAllowsFutureDueAt(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Request{
		SchemaVersion:    "1.0",
		Type:             TypeValidationRequest,
		RequestID:        "valreq_1",
		ReleaseID:        "rel_1",
		ManifestID:       "man_1",
		PoolID:           "pool.local",
		RequestingNodeID: "node_requester",
		CreatedAt:        now.Format(time.RFC3339),
		DueAt:            now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	if err := ValidateRequest(item, now, 2*time.Minute); err != nil {
		t.Fatalf("expected validation request to validate: %v", err)
	}
}

func TestValidationRequestRejectsMismatchedType(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	item := Request{
		SchemaVersion:    "1.0",
		Type:             TypeArticleAvailabilityAttestation,
		RequestID:        "valreq_1",
		ReleaseID:        "rel_1",
		ManifestID:       "man_1",
		PoolID:           "pool.local",
		RequestingNodeID: "node_requester",
		CreatedAt:        now.Format(time.RFC3339),
	}
	if err := ValidateRequest(item, now, 2*time.Minute); err == nil {
		t.Fatal("expected validation request type mismatch to fail")
	}
}
