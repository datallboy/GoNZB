package publicationstate

import (
	"fmt"
	"strings"
	"time"
)

const (
	Type           = "ReleasePublicationState"
	BodySchema     = "gonzbnet.ReleasePublicationState/1.0"
	StateActive    = "active"
	StateWithdrawn = "withdrawn"
)

// State is an author-scoped lifecycle assertion. It only controls the
// publishing node's own release source and cannot override pool moderation.
type State struct {
	SchemaVersion     string `json:"schema_version"`
	Type              string `json:"type"`
	PoolID            string `json:"pool_id"`
	ReleaseID         string `json:"release_id"`
	ManifestID        string `json:"manifest_id,omitempty"`
	State             string `json:"state"`
	Reason            string `json:"reason,omitempty"`
	ChangedAt         string `json:"changed_at"`
	SupersedesEventID string `json:"supersedes_event_id,omitempty"`
}

type Projection struct {
	Publication  State
	EventID      string
	AuthorNodeID string
	Sequence     int64
}

func Validate(item State, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(item.SchemaVersion) != "1.0" || strings.TrimSpace(item.Type) != Type {
		return fmt.Errorf("unsupported release publication state schema")
	}
	if strings.TrimSpace(item.PoolID) == "" || strings.TrimSpace(item.ReleaseID) == "" {
		return fmt.Errorf("pool_id and release_id are required")
	}
	if item.State != StateActive && item.State != StateWithdrawn {
		return fmt.Errorf("state must be active or withdrawn")
	}
	changedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.ChangedAt))
	if err != nil {
		return fmt.Errorf("changed_at must be RFC3339")
	}
	if changedAt.After(now.UTC().Add(futureTolerance)) {
		return fmt.Errorf("changed_at is too far in the future")
	}
	if len(item.Reason) > 4096 {
		return fmt.Errorf("reason exceeds field limit")
	}
	return nil
}
