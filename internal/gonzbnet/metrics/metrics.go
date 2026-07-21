package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	EventsReceivedTotal          = "gonzbnet_events_received_total"
	EventsAcceptedTotal          = "gonzbnet_events_accepted_total"
	EventsRejectedTotal          = "gonzbnet_events_rejected_total"
	PeerFailuresTotal            = "gonzbnet_peer_failures_total"
	ManifestRequestsTotal        = "gonzbnet_manifest_requests_total"
	ManifestRequestFailuresTotal = "gonzbnet_manifest_request_failures_total"
	ReleaseCardsProjectedTotal   = "gonzbnet_release_cards_projected_total"
	HealthAttestationsTotal      = "gonzbnet_health_attestations_total"
	TombstonesActiveTotal        = "gonzbnet_tombstones_active_total"
	PeerSyncDurationSeconds      = "gonzbnet_peer_sync_duration_seconds"
	ManifestResolutionSeconds    = "gonzbnet_manifest_resolution_duration_seconds"
)

var counterNames = []string{
	EventsReceivedTotal,
	EventsAcceptedTotal,
	EventsRejectedTotal,
	PeerFailuresTotal,
	ManifestRequestsTotal,
	ManifestRequestFailuresTotal,
	ReleaseCardsProjectedTotal,
	HealthAttestationsTotal,
}

type durationMetric struct {
	count atomic.Uint64
	sumNS atomic.Uint64
}

type Registry struct {
	counters         sync.Map
	peerSync         durationMetric
	manifest         durationMetric
	activeTombstones atomic.Int64
}

type DurationSnapshot struct {
	Count uint64  `json:"count"`
	Sum   float64 `json:"sum_seconds"`
}

type Snapshot struct {
	Counters  map[string]uint64           `json:"counters"`
	Durations map[string]DurationSnapshot `json:"durations"`
	Gauges    map[string]int64            `json:"gauges"`
}

var Default = NewRegistry()

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Add(name string, delta uint64) {
	if r == nil || delta == 0 {
		return
	}
	value, _ := r.counters.LoadOrStore(name, &atomic.Uint64{})
	value.(*atomic.Uint64).Add(delta)
}

func (r *Registry) ObservePeerSync(duration time.Duration) {
	r.observe(&r.peerSync, duration)
}

func (r *Registry) ObserveManifestResolution(duration time.Duration) {
	r.observe(&r.manifest, duration)
}

func (r *Registry) observe(metric *durationMetric, duration time.Duration) {
	if r == nil || metric == nil {
		return
	}
	metric.count.Add(1)
	if duration > 0 {
		metric.sumNS.Add(uint64(duration))
	}
}

func (r *Registry) SetActiveTombstones(value int64) {
	if r == nil {
		return
	}
	if value < 0 {
		value = 0
	}
	r.activeTombstones.Store(value)
}

func (r *Registry) Snapshot() Snapshot {
	counters := make(map[string]uint64, len(counterNames))
	for _, name := range counterNames {
		counters[name] = r.counter(name)
	}
	return Snapshot{
		Counters: counters,
		Durations: map[string]DurationSnapshot{
			PeerSyncDurationSeconds:   durationSnapshot(&r.peerSync),
			ManifestResolutionSeconds: durationSnapshot(&r.manifest),
		},
		Gauges: map[string]int64{TombstonesActiveTotal: r.activeTombstones.Load()},
	}
}

func (r *Registry) Prometheus() string {
	snapshot := r.Snapshot()
	names := make([]string, 0, len(snapshot.Counters))
	for name := range snapshot.Counters {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, name := range names {
		fmt.Fprintf(&out, "# TYPE %s counter\n%s %d\n", name, name, snapshot.Counters[name])
	}
	for _, name := range []string{PeerSyncDurationSeconds, ManifestResolutionSeconds} {
		value := snapshot.Durations[name]
		fmt.Fprintf(&out, "# TYPE %s summary\n%s_count %d\n%s_sum %.9f\n", name, name, value.Count, name, value.Sum)
	}
	fmt.Fprintf(&out, "# TYPE %s gauge\n%s %d\n", TombstonesActiveTotal, TombstonesActiveTotal, snapshot.Gauges[TombstonesActiveTotal])
	return out.String()
}

func (r *Registry) counter(name string) uint64 {
	value, ok := r.counters.Load(name)
	if !ok {
		return 0
	}
	return value.(*atomic.Uint64).Load()
}

func durationSnapshot(metric *durationMetric) DurationSnapshot {
	return DurationSnapshot{Count: metric.count.Load(), Sum: time.Duration(metric.sumNS.Load()).Seconds()}
}
