package manifestavailability

import (
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	Type       = "ManifestAvailability"
	BodySchema = "gonzbnet.ManifestAvailability/1.0"

	StatusAvailable   = "available"
	StatusUnavailable = "unavailable"
	StatusUnknown     = "unknown"
)

type Attestation struct {
	SchemaVersion string  `json:"schema_version"`
	Type          string  `json:"type"`
	ReleaseID     string  `json:"release_id"`
	ManifestID    string  `json:"manifest_id"`
	CheckedAt     string  `json:"checked_at"`
	Status        string  `json:"status"`
	Confidence    float64 `json:"confidence"`
	Method        string  `json:"method"`
}

func Validate(in Attestation, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported manifest availability schema_version")
	}
	if strings.TrimSpace(in.Type) != Type {
		return fmt.Errorf("unsupported manifest availability type")
	}
	if strings.TrimSpace(in.ReleaseID) == "" || strings.TrimSpace(in.ManifestID) == "" {
		return fmt.Errorf("release_id and manifest_id are required")
	}
	if !statusAllowed(in.Status) {
		return fmt.Errorf("unsupported manifest availability status")
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
	if in.Confidence < 0 || in.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

func HashBody(attestation Attestation) (string, error) {
	hash, _, err := canonical.BodyHash(attestation)
	return hash, err
}

func statusAllowed(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusAvailable, StatusUnavailable, StatusUnknown:
		return true
	default:
		return false
	}
}
