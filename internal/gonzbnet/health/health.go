package health

import (
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	Type       = "HealthAttestation"
	BodySchema = "gonzbnet.HealthAttestation/1.0"

	StatusUnknown         = "unknown"
	StatusComplete        = "complete"
	StatusIncomplete      = "incomplete"
	StatusMissing         = "missing"
	StatusRepairable      = "repairable"
	StatusUnverified      = "unverified"
	StatusProviderLimited = "provider_limited"
)

type Attestation struct {
	SchemaVersion     string        `json:"schema_version"`
	Type              string        `json:"type"`
	ReleaseID         string        `json:"release_id"`
	ManifestID        string        `json:"manifest_id,omitempty"`
	CheckedAt         string        `json:"checked_at"`
	Status            string        `json:"status"`
	ArticlesTotal     int           `json:"articles_total"`
	ArticlesAvailable int           `json:"articles_available"`
	MissingArticles   int           `json:"missing_articles"`
	RepairAvailable   bool          `json:"repair_available"`
	RepairConfidence  float64       `json:"repair_confidence"`
	ProviderScope     ProviderScope `json:"provider_scope"`
	Confidence        float64       `json:"confidence"`
	Method            string        `json:"method"`
}

type ProviderScope struct {
	ProviderBackboneHash  *string `json:"provider_backbone_hash"`
	RetentionDaysObserved int     `json:"retention_days_observed"`
}

func Validate(in Attestation, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported health attestation schema_version")
	}
	if strings.TrimSpace(in.Type) != Type {
		return fmt.Errorf("unsupported health attestation type")
	}
	if strings.TrimSpace(in.ReleaseID) == "" {
		return fmt.Errorf("release_id is required")
	}
	if !statusAllowed(in.Status) {
		return fmt.Errorf("unsupported health attestation status")
	}
	checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(in.CheckedAt))
	if err != nil {
		return fmt.Errorf("checked_at must be RFC3339")
	}
	if futureTolerance <= 0 {
		futureTolerance = 2 * time.Minute
	}
	if !now.IsZero() && checkedAt.After(now.UTC().Add(futureTolerance)) {
		return fmt.Errorf("checked_at is too far in the future")
	}
	if in.ArticlesTotal < 0 || in.ArticlesAvailable < 0 {
		return fmt.Errorf("article counts must not be negative")
	}
	if in.ArticlesTotal > 0 && in.ArticlesAvailable > in.ArticlesTotal {
		return fmt.Errorf("articles_available exceeds articles_total")
	}
	if in.MissingArticles < 0 {
		return fmt.Errorf("missing_articles must not be negative")
	}
	if in.Confidence < 0 || in.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if in.RepairConfidence < 0 || in.RepairConfidence > 1 {
		return fmt.Errorf("repair_confidence must be between 0 and 1")
	}
	return nil
}

func AvailabilityScore(in Attestation) float64 {
	ratio := 0.0
	if in.ArticlesTotal > 0 {
		ratio = float64(in.ArticlesAvailable) / float64(in.ArticlesTotal)
	}
	confidence := clamp01(in.Confidence)
	if confidence == 0 && in.ArticlesTotal > 0 {
		confidence = 0.5
	}
	switch strings.TrimSpace(in.Status) {
	case StatusComplete:
		if in.ArticlesTotal == 0 {
			return confidence
		}
		return clamp01((ratio * 0.7) + (confidence * 0.3))
	case StatusRepairable:
		return clamp01((ratio * 0.5) + (clamp01(in.RepairConfidence) * 0.3) + (confidence * 0.2))
	case StatusIncomplete:
		return clamp01(ratio * confidence)
	case StatusMissing:
		return 0
	case StatusProviderLimited:
		return clamp01(ratio * 0.5)
	case StatusUnknown, StatusUnverified:
		return clamp01(confidence * 0.25)
	default:
		return 0
	}
}

func TrustDelta(in Attestation) (float64, string) {
	status := strings.TrimSpace(in.Status)
	if status == StatusComplete && (in.MissingArticles > 0 || (in.ArticlesTotal > 0 && in.ArticlesAvailable < in.ArticlesTotal)) {
		return -0.20, "health_false_positive"
	}
	if status == StatusMissing && in.ArticlesTotal > 0 && in.ArticlesAvailable > 0 {
		return -0.10, "health_false_missing"
	}
	if status == StatusComplete && in.ArticlesTotal > 0 && in.ArticlesAvailable == in.ArticlesTotal && in.Confidence >= 0.8 {
		return 0.03, "health_complete_verified"
	}
	return 0, ""
}

func RankingScore(nodeTrust, manifestConfidence, availability, quorum, freshness float64) float64 {
	return clamp01(
		0.35*clamp01(nodeTrust) +
			0.25*clamp01(manifestConfidence) +
			0.25*clamp01(availability) +
			0.10*clamp01(quorum) +
			0.05*clamp01(freshness),
	)
}

func HashBody(attestation Attestation) (string, error) {
	hash, _, err := canonical.BodyHash(attestation)
	return hash, err
}

func statusAllowed(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusUnknown, StatusComplete, StatusIncomplete, StatusMissing, StatusRepairable, StatusUnverified, StatusProviderLimited:
		return true
	default:
		return false
	}
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
