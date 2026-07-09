package trust

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EventType = "TrustAttestation"
)

type Attestation struct {
	SchemaVersion string          `json:"schema_version"`
	Type          string          `json:"type"`
	SubjectNodeID string          `json:"subject_node_id"`
	PoolID        string          `json:"pool_id"`
	ScoreDelta    float64         `json:"score_delta"`
	Reason        string          `json:"reason"`
	Evidence      json.RawMessage `json:"evidence,omitempty"`
	ExpiresAt     string          `json:"expires_at,omitempty"`
}

func Validate(item Attestation, now time.Time) error {
	if strings.TrimSpace(item.SubjectNodeID) == "" {
		return fmt.Errorf("subject_node_id is required")
	}
	if strings.TrimSpace(item.PoolID) == "" {
		return fmt.Errorf("pool_id is required")
	}
	if item.ScoreDelta < -100 || item.ScoreDelta > 100 {
		return fmt.Errorf("score_delta must be between -100 and 100")
	}
	if item.ScoreDelta == 0 {
		return fmt.Errorf("score_delta must not be zero")
	}
	if strings.TrimSpace(item.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	if len(item.Evidence) > 0 && !json.Valid(item.Evidence) {
		return fmt.Errorf("evidence must be valid json")
	}
	if strings.TrimSpace(item.ExpiresAt) != "" {
		expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.ExpiresAt))
		if err != nil {
			return fmt.Errorf("expires_at must be RFC3339")
		}
		if !now.IsZero() && !expiresAt.After(now.UTC()) {
			return fmt.Errorf("trust attestation expired")
		}
	}
	return nil
}

func NormalizedDelta(scoreDelta float64) float64 {
	return scoreDelta / 100
}
