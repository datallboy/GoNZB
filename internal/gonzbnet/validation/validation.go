package validation

import (
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	TypeValidatorCapacity              = "ValidatorCapacity"
	TypeArticleAvailabilityAttestation = "ArticleAvailabilityAttestation"
	TypeChecksumAttestation            = "ChecksumAttestation"
	TypeValidationRequest              = "ValidationRequest"

	ValidatorCapacityBodySchema              = "gonzbnet.ValidatorCapacity/1.0"
	ArticleAvailabilityAttestationBodySchema = "gonzbnet.ArticleAvailabilityAttestation/1.0"
	ChecksumAttestationBodySchema            = "gonzbnet.ChecksumAttestation/1.0"

	StatusAvailable  = "available"
	StatusPartial    = "partial"
	StatusMissing    = "missing"
	StatusUnverified = "unverified"
	StatusSkipped    = "skipped"
	StatusFailed     = "failed"
)

type ValidatorCapacity struct {
	SchemaVersion           string        `json:"schema_version"`
	Type                    string        `json:"type"`
	NodeID                  string        `json:"node_id"`
	PublishedAt             string        `json:"published_at"`
	MaxTasksPerHour         int           `json:"max_tasks_per_hour"`
	ArticleAvailability     bool          `json:"article_availability"`
	ChecksumValidation      bool          `json:"checksum_validation"`
	ProviderScope           ProviderScope `json:"provider_scope"`
	AcceptedManifestSchemas []string      `json:"accepted_manifest_schemas"`
}

type ArticleAvailabilityAttestation struct {
	SchemaVersion     string        `json:"schema_version"`
	Type              string        `json:"type"`
	ReleaseID         string        `json:"release_id"`
	ManifestID        string        `json:"manifest_id"`
	CheckedAt         string        `json:"checked_at"`
	Status            string        `json:"status"`
	ArticlesTotal     int           `json:"articles_total"`
	ArticlesAvailable int           `json:"articles_available"`
	MissingArticles   int           `json:"missing_articles"`
	ProviderScope     ProviderScope `json:"provider_scope"`
	Confidence        float64       `json:"confidence"`
	Method            string        `json:"method"`
}

type ChecksumAttestation struct {
	SchemaVersion     string  `json:"schema_version"`
	Type              string  `json:"type"`
	ReleaseID         string  `json:"release_id"`
	ManifestID        string  `json:"manifest_id"`
	CheckedAt         string  `json:"checked_at"`
	Status            string  `json:"status"`
	ChecksumsTotal    int     `json:"checksums_total"`
	ChecksumsVerified int     `json:"checksums_verified"`
	ChecksumsFailed   int     `json:"checksums_failed"`
	Confidence        float64 `json:"confidence"`
	Method            string  `json:"method"`
}

type Request struct {
	SchemaVersion    string `json:"schema_version"`
	Type             string `json:"type"`
	RequestID        string `json:"request_id"`
	ReleaseID        string `json:"release_id"`
	ManifestID       string `json:"manifest_id"`
	PoolID           string `json:"pool_id"`
	RequestingNodeID string `json:"requesting_node_id"`
	TargetNodeID     string `json:"target_node_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Priority         int    `json:"priority,omitempty"`
	DueAt            string `json:"due_at,omitempty"`
	CreatedAt        string `json:"created_at"`
}

type Response struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	RequestID     string `json:"request_id"`
	Status        string `json:"status"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
	Queued        bool   `json:"queued"`
}

type ProviderScope struct {
	ProviderBackboneHash  *string `json:"provider_backbone_hash,omitempty"`
	RetentionDaysObserved int     `json:"retention_days_observed"`
}

func ValidateCapacity(in ValidatorCapacity, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported validator capacity schema_version")
	}
	if strings.TrimSpace(in.Type) != TypeValidatorCapacity {
		return fmt.Errorf("unsupported validator capacity type")
	}
	if strings.TrimSpace(in.NodeID) == "" {
		return fmt.Errorf("node_id is required")
	}
	if err := validateTime("published_at", in.PublishedAt, now, futureTolerance); err != nil {
		return err
	}
	if in.MaxTasksPerHour < 0 {
		return fmt.Errorf("max_tasks_per_hour must not be negative")
	}
	return nil
}

func ValidateArticleAvailability(in ArticleAvailabilityAttestation, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported article availability schema_version")
	}
	if strings.TrimSpace(in.Type) != TypeArticleAvailabilityAttestation {
		return fmt.Errorf("unsupported article availability type")
	}
	if strings.TrimSpace(in.ReleaseID) == "" || strings.TrimSpace(in.ManifestID) == "" {
		return fmt.Errorf("release_id and manifest_id are required")
	}
	if err := validateTime("checked_at", in.CheckedAt, now, futureTolerance); err != nil {
		return err
	}
	if !statusAllowed(in.Status) {
		return fmt.Errorf("unsupported article availability status")
	}
	if in.ArticlesTotal < 0 || in.ArticlesAvailable < 0 || in.MissingArticles < 0 {
		return fmt.Errorf("article counts must not be negative")
	}
	if in.ArticlesTotal > 0 && in.ArticlesAvailable > in.ArticlesTotal {
		return fmt.Errorf("articles_available exceeds articles_total")
	}
	if in.Confidence < 0 || in.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

func ValidateChecksum(in ChecksumAttestation, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported checksum attestation schema_version")
	}
	if strings.TrimSpace(in.Type) != TypeChecksumAttestation {
		return fmt.Errorf("unsupported checksum attestation type")
	}
	if strings.TrimSpace(in.ReleaseID) == "" || strings.TrimSpace(in.ManifestID) == "" {
		return fmt.Errorf("release_id and manifest_id are required")
	}
	if err := validateTime("checked_at", in.CheckedAt, now, futureTolerance); err != nil {
		return err
	}
	if !statusAllowed(in.Status) {
		return fmt.Errorf("unsupported checksum status")
	}
	if in.ChecksumsTotal < 0 || in.ChecksumsVerified < 0 || in.ChecksumsFailed < 0 {
		return fmt.Errorf("checksum counts must not be negative")
	}
	if in.ChecksumsTotal > 0 && in.ChecksumsVerified+in.ChecksumsFailed > in.ChecksumsTotal {
		return fmt.Errorf("checksum counts exceed checksum total")
	}
	if in.Confidence < 0 || in.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

func ValidateRequest(in Request, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported validation request schema_version")
	}
	if strings.TrimSpace(in.Type) != TypeValidationRequest {
		return fmt.Errorf("unsupported validation request type")
	}
	if strings.TrimSpace(in.RequestID) == "" {
		return fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(in.ReleaseID) == "" || strings.TrimSpace(in.ManifestID) == "" {
		return fmt.Errorf("release_id and manifest_id are required")
	}
	if strings.TrimSpace(in.PoolID) == "" {
		return fmt.Errorf("pool_id is required")
	}
	if strings.TrimSpace(in.RequestingNodeID) == "" {
		return fmt.Errorf("requesting_node_id is required")
	}
	if in.Priority < 0 {
		return fmt.Errorf("priority must not be negative")
	}
	if err := validateTime("created_at", in.CreatedAt, now, futureTolerance); err != nil {
		return err
	}
	if strings.TrimSpace(in.DueAt) != "" {
		if _, err := time.Parse(time.RFC3339, strings.TrimSpace(in.DueAt)); err != nil {
			return fmt.Errorf("due_at must be RFC3339")
		}
	}
	return nil
}

func ArticleAvailabilityScore(in ArticleAvailabilityAttestation) float64 {
	confidence := clamp01(in.Confidence)
	switch strings.TrimSpace(in.Status) {
	case StatusAvailable:
		if in.ArticlesTotal == 0 {
			return confidence
		}
		return clamp01((float64(in.ArticlesAvailable)/float64(in.ArticlesTotal))*0.75 + confidence*0.25)
	case StatusPartial:
		if in.ArticlesTotal == 0 {
			return 0
		}
		return clamp01((float64(in.ArticlesAvailable) / float64(in.ArticlesTotal)) * confidence)
	case StatusMissing, StatusFailed:
		return 0
	case StatusUnverified, StatusSkipped:
		return clamp01(confidence * 0.25)
	default:
		return 0
	}
}

func ChecksumScore(in ChecksumAttestation) float64 {
	confidence := clamp01(in.Confidence)
	switch strings.TrimSpace(in.Status) {
	case StatusAvailable:
		if in.ChecksumsTotal == 0 {
			return confidence
		}
		return clamp01((float64(in.ChecksumsVerified)/float64(in.ChecksumsTotal))*0.8 + confidence*0.2)
	case StatusPartial:
		if in.ChecksumsTotal == 0 {
			return 0
		}
		return clamp01((float64(in.ChecksumsVerified) / float64(in.ChecksumsTotal)) * confidence)
	case StatusMissing, StatusFailed:
		return 0
	case StatusUnverified, StatusSkipped:
		return clamp01(confidence * 0.25)
	default:
		return 0
	}
}

func HashBody(body any) (string, error) {
	hash, _, err := canonical.BodyHash(body)
	return hash, err
}

func validateTime(field, value string, now time.Time, futureTolerance time.Duration) error {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s must be RFC3339", field)
	}
	if futureTolerance <= 0 {
		futureTolerance = 2 * time.Minute
	}
	if !now.IsZero() && parsed.After(now.UTC().Add(futureTolerance)) {
		return fmt.Errorf("%s is too far in the future", field)
	}
	return nil
}

func statusAllowed(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusAvailable, StatusPartial, StatusMissing, StatusUnverified, StatusSkipped, StatusFailed:
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
