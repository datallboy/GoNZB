package gossip

import (
	"strings"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

const (
	Type         = "GossipBatch"
	ResponseType = "GossipResponse"
)

type Batch struct {
	SchemaVersion string               `json:"schema_version"`
	Type          string               `json:"type"`
	NetworkID     string               `json:"network_id"`
	TTL           int                  `json:"ttl"`
	Events        []events.SignedEvent `json:"events"`
	WantMissing   bool                 `json:"want_missing"`
	Peers         []string             `json:"peers,omitempty"`
}

type EventResult struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type Response struct {
	SchemaVersion string        `json:"schema_version"`
	Type          string        `json:"type"`
	TTL           int           `json:"ttl"`
	Accepted      []EventResult `json:"accepted"`
	Duplicate     []EventResult `json:"duplicate"`
	Rejected      []EventResult `json:"rejected"`
	Peers         []string      `json:"peers,omitempty"`
}

func NormalizeTTL(ttl, maxTTL int) int {
	if maxTTL <= 0 {
		maxTTL = 4
	}
	if ttl <= 0 {
		return 0
	}
	if ttl > maxTTL {
		return maxTTL
	}
	return ttl
}

func ForwardTTL(ttl int) int {
	if ttl <= 1 {
		return 0
	}
	return ttl - 1
}

func FilterPeers(peers []string, enabled bool, limit int) []string {
	if !enabled {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	out := make([]string, 0, min(len(peers), limit))
	seen := map[string]struct{}{}
	for _, peer := range peers {
		peer = strings.TrimRight(strings.TrimSpace(peer), "/")
		if peer == "" {
			continue
		}
		if _, ok := seen[peer]; ok {
			continue
		}
		seen[peer] = struct{}{}
		out = append(out, peer)
		if len(out) >= limit {
			break
		}
	}
	return out
}
