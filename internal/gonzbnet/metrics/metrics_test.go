package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestRegistrySnapshotAndPrometheus(t *testing.T) {
	registry := NewRegistry()
	registry.Add(EventsReceivedTotal, 2)
	registry.Add(EventsAcceptedTotal, 1)
	registry.ObservePeerSync(250 * time.Millisecond)
	registry.SetActiveTombstones(3)

	snapshot := registry.Snapshot()
	if snapshot.Counters[EventsReceivedTotal] != 2 || snapshot.Counters[EventsAcceptedTotal] != 1 {
		t.Fatalf("unexpected counters: %#v", snapshot.Counters)
	}
	if snapshot.Durations[PeerSyncDurationSeconds].Count != 1 || snapshot.Gauges[TombstonesActiveTotal] != 3 {
		t.Fatalf("unexpected metric snapshot: %#v", snapshot)
	}
	text := registry.Prometheus()
	for _, expected := range []string{"gonzbnet_events_received_total 2", "gonzbnet_peer_sync_duration_seconds_count 1", "gonzbnet_tombstones_active_total 3"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in metrics:\n%s", expected, text)
		}
	}
}
