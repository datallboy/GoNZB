package coverage

import "testing"

func TestRendezvousAssignmentsAreDeterministic(t *testing.T) {
	work := []SchedulerWorkItem{
		{WorkID: "alt.binaries.example:100-200"},
		{WorkID: "alt.binaries.example:201-300"},
	}
	nodes := []SchedulerNode{
		{NodeID: "node_b", Weight: 2},
		{NodeID: "node_a", Weight: 1},
	}
	first := RendezvousAssignments(work, nodes)
	second := RendezvousAssignments(work, []SchedulerNode{{NodeID: "node_a", Weight: 1}, {NodeID: "node_b", Weight: 2}})
	if len(first) != len(second) {
		t.Fatalf("assignment length mismatch: %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i].WorkID != second[i].WorkID || first[i].NodeID != second[i].NodeID {
			t.Fatalf("assignments should be deterministic: %#v != %#v", first, second)
		}
	}
}

func TestRendezvousAssignmentsRespectSeenSet(t *testing.T) {
	work := []SchedulerWorkItem{{WorkID: "range-1", SeenBy: []string{"node_a"}}}
	nodes := []SchedulerNode{{NodeID: "node_a", Weight: 100}, {NodeID: "node_b", Weight: 1}}
	assignments := RendezvousAssignments(work, nodes)
	if len(assignments) != 1 {
		t.Fatalf("expected one assignment, got %d", len(assignments))
	}
	if assignments[0].NodeID == "node_a" {
		t.Fatalf("seen node should not receive work: %#v", assignments[0])
	}
}
