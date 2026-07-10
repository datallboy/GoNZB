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

	FetchPolicyTrustedPeers = "trusted_peers_only"
	FetchPolicyLocalOnly    = "local_only"
)

type Attestation struct {
	SchemaVersion       string `json:"schema_version"`
	Type                string `json:"type"`
	ManifestID          string `json:"manifest_id"`
	ReleaseID           string `json:"release_id"`
	SourceNodeID        string `json:"source_node_id"`
	PoolID              string `json:"pool_id"`
	Available           bool   `json:"available"`
	FetchPolicy         string `json:"fetch_policy"`
	CompressedSizeBytes int64  `json:"compressed_size_bytes"`
	UpdatedAt           string `json:"updated_at"`
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
	if strings.TrimSpace(in.SourceNodeID) == "" || strings.TrimSpace(in.PoolID) == "" {
		return fmt.Errorf("source_node_id and pool_id are required")
	}
	switch strings.TrimSpace(in.FetchPolicy) {
	case FetchPolicyTrustedPeers, FetchPolicyLocalOnly:
	default:
		return fmt.Errorf("unsupported manifest fetch_policy")
	}
	if in.CompressedSizeBytes < 0 {
		return fmt.Errorf("compressed_size_bytes must not be negative")
	}
	updatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(in.UpdatedAt))
	if err != nil {
		return fmt.Errorf("updated_at must be RFC3339")
	}
	if futureTolerance <= 0 {
		futureTolerance = 2 * time.Minute
	}
	if !now.IsZero() && updatedAt.After(now.UTC().Add(futureTolerance)) {
		return fmt.Errorf("updated_at is too far in the future")
	}
	return nil
}

func HashBody(attestation Attestation) (string, error) {
	hash, _, err := canonical.BodyHash(attestation)
	return hash, err
}
