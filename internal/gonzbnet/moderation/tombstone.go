package moderation

import (
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	Type       = "Tombstone"
	BodySchema = "gonzbnet.Tombstone/1.0"

	TargetRelease           = "release"
	TargetManifest          = "manifest"
	TargetEvent             = "event"
	TargetNode              = "node"
	TargetPoolMember        = "pool_member"
	TargetHealthAttestation = "health_attestation"
	TargetTrustAttestation  = "trust_attestation"

	SeverityHide      = "hide"
	SeverityReject    = "reject"
	SeverityWarn      = "warn"
	SeverityLocalOnly = "local_only"
)

type Tombstone struct {
	SchemaVersion    string   `json:"schema_version"`
	Type             string   `json:"type"`
	TargetType       string   `json:"target_type"`
	TargetID         string   `json:"target_id"`
	PoolID           string   `json:"pool_id,omitempty"`
	Reason           string   `json:"reason"`
	Severity         string   `json:"severity"`
	EvidenceEventIDs []string `json:"evidence_event_ids"`
	EffectiveAt      string   `json:"effective_at"`
	ExpiresAt        *string  `json:"expires_at"`
}

func Validate(in Tombstone, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported tombstone schema_version")
	}
	if strings.TrimSpace(in.Type) != Type {
		return fmt.Errorf("unsupported tombstone type")
	}
	if !targetTypeAllowed(in.TargetType) {
		return fmt.Errorf("unsupported tombstone target_type")
	}
	if strings.TrimSpace(in.TargetID) == "" {
		return fmt.Errorf("target_id is required")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	if !severityAllowed(in.Severity) {
		return fmt.Errorf("unsupported tombstone severity")
	}
	effectiveAt, err := time.Parse(time.RFC3339, strings.TrimSpace(in.EffectiveAt))
	if err != nil {
		return fmt.Errorf("effective_at must be RFC3339")
	}
	if futureTolerance <= 0 {
		futureTolerance = 2 * time.Minute
	}
	if !now.IsZero() && effectiveAt.After(now.UTC().Add(futureTolerance)) {
		return fmt.Errorf("effective_at is too far in the future")
	}
	if in.ExpiresAt != nil && strings.TrimSpace(*in.ExpiresAt) != "" {
		expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.ExpiresAt))
		if err != nil {
			return fmt.Errorf("expires_at must be RFC3339")
		}
		if expiresAt.Before(effectiveAt) {
			return fmt.Errorf("expires_at must be after effective_at")
		}
	}
	return nil
}

func HashBody(tombstone Tombstone) (string, error) {
	hash, _, err := canonical.BodyHash(tombstone)
	return hash, err
}

func IsActive(tombstone Tombstone, now time.Time) bool {
	effectiveAt, err := time.Parse(time.RFC3339, strings.TrimSpace(tombstone.EffectiveAt))
	if err != nil {
		return false
	}
	now = now.UTC()
	if effectiveAt.After(now) {
		return false
	}
	if tombstone.ExpiresAt == nil || strings.TrimSpace(*tombstone.ExpiresAt) == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*tombstone.ExpiresAt))
	if err != nil {
		return false
	}
	return expiresAt.After(now)
}

func targetTypeAllowed(targetType string) bool {
	switch strings.TrimSpace(targetType) {
	case TargetRelease, TargetManifest, TargetEvent, TargetNode, TargetPoolMember, TargetHealthAttestation, TargetTrustAttestation:
		return true
	default:
		return false
	}
}

func severityAllowed(severity string) bool {
	switch strings.TrimSpace(severity) {
	case SeverityHide, SeverityReject, SeverityWarn, SeverityLocalOnly:
		return true
	default:
		return false
	}
}
