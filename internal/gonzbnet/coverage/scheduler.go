package coverage

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"strings"
)

type SchedulerNode struct {
	NodeID string
	Weight float64
}

type SchedulerWorkItem struct {
	WorkID   string
	Priority int
	SeenBy   []string
}

type SchedulerAssignment struct {
	WorkID string
	NodeID string
	Score  float64
}

func RendezvousAssignments(work []SchedulerWorkItem, nodes []SchedulerNode) []SchedulerAssignment {
	nodes = normalizeSchedulerNodes(nodes)
	assignments := make([]SchedulerAssignment, 0, len(work))
	if len(nodes) == 0 {
		return assignments
	}
	for _, item := range work {
		workID := strings.TrimSpace(item.WorkID)
		if workID == "" {
			continue
		}
		seen := stringSet(item.SeenBy)
		var best SchedulerAssignment
		for _, node := range nodes {
			if _, ok := seen[node.NodeID]; ok {
				continue
			}
			score := weightedRendezvousScore(workID, node)
			if best.NodeID == "" || score > best.Score || (score == best.Score && node.NodeID < best.NodeID) {
				best = SchedulerAssignment{WorkID: workID, NodeID: node.NodeID, Score: score}
			}
		}
		if best.NodeID != "" {
			assignments = append(assignments, best)
		}
	}
	sort.SliceStable(assignments, func(i, j int) bool {
		return assignments[i].WorkID < assignments[j].WorkID
	})
	return assignments
}

func weightedRendezvousScore(workID string, node SchedulerNode) float64 {
	sum := sha256.Sum256([]byte(strings.TrimSpace(workID) + "\x00" + strings.TrimSpace(node.NodeID)))
	raw := binary.BigEndian.Uint64(sum[:8])
	unit := (float64(raw) + 1) / (float64(math.MaxUint64) + 1)
	weight := node.Weight
	if weight <= 0 {
		weight = 1
	}
	return math.Pow(unit, 1/weight)
}

func normalizeSchedulerNodes(nodes []SchedulerNode) []SchedulerNode {
	out := make([]SchedulerNode, 0, len(nodes))
	seen := map[string]struct{}{}
	for _, node := range nodes {
		node.NodeID = strings.TrimSpace(node.NodeID)
		if node.NodeID == "" {
			continue
		}
		if _, ok := seen[node.NodeID]; ok {
			continue
		}
		seen[node.NodeID] = struct{}{}
		if node.Weight <= 0 {
			node.Weight = 1
		}
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

func stringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}
