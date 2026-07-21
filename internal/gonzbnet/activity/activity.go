package activity

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type ExecutionMode string

const (
	Scheduled   ExecutionMode = "scheduled"
	EventDriven ExecutionMode = "event_driven"
	OnDemand    ExecutionMode = "on_demand"
)

type Status string

const (
	StatusOff      Status = "off"
	StatusStarting Status = "starting"
	StatusReady    Status = "ready"
	StatusWorking  Status = "working"
	StatusDegraded Status = "degraded"
	StatusBlocked  Status = "blocked"
)

const (
	JobConsume    = "consume"
	JobContribute = "contribute"
	JobVerify     = "verify"
	JobCoordinate = "coordinate"
	JobConnection = "connection"

	ComponentAdmissionPoller   = "admission_poller"
	ComponentReleasePublisher  = "release_publisher"
	ComponentHealthPublisher   = "health_publisher"
	ComponentValidator         = "validator"
	ComponentPullSync          = "pull_sync"
	ComponentPushSync          = "push_sync"
	ComponentGossip            = "gossip"
	ComponentCoverageScheduler = "coverage_scheduler"
	ComponentScanner           = "scanner_coordinator"
	ComponentIndexProjection   = "index_projection"
	ComponentManifestResolver  = "manifest_resolver"
	ComponentManifestCache     = "manifest_cache"
	ComponentRelay             = "relay"
	ComponentPeerExchange      = "peer_exchange"
)

const RollupBucket = 5 * time.Minute

type Definition struct {
	Key           string        `json:"key"`
	Job           string        `json:"job"`
	Label         string        `json:"label"`
	Description   string        `json:"description"`
	ExecutionMode ExecutionMode `json:"execution_mode"`
	Interval      time.Duration `json:"-"`
	Configured    bool          `json:"configured"`
	Eligible      bool          `json:"eligible"`
	Reason        string        `json:"reason,omitempty"`
}

type Result struct {
	ItemsIn  int64
	ItemsOut int64
	BytesIn  int64
	BytesOut int64
	Backlog  int64
	Err      error
}

type Snapshot struct {
	Definition
	Status              Status     `json:"status"`
	Pools               []string   `json:"pools"`
	Running             int        `json:"running"`
	Attempts            uint64     `json:"attempts"`
	Successes           uint64     `json:"successes"`
	Failures            uint64     `json:"failures"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	ItemsIn             int64      `json:"items_in"`
	ItemsOut            int64      `json:"items_out"`
	BytesIn             int64      `json:"bytes_in"`
	BytesOut            int64      `json:"bytes_out"`
	Backlog             int64      `json:"backlog"`
	LastAttemptAt       *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}

type Rollup struct {
	BucketStart   time.Time  `json:"bucket_start"`
	BucketSeconds int        `json:"bucket_seconds"`
	NodeID        string     `json:"node_id"`
	PoolID        string     `json:"pool_id"`
	Component     string     `json:"component"`
	Job           string     `json:"job"`
	Attempts      int64      `json:"attempts"`
	Successes     int64      `json:"successes"`
	Failures      int64      `json:"failures"`
	ItemsIn       int64      `json:"items_in"`
	ItemsOut      int64      `json:"items_out"`
	BytesIn       int64      `json:"bytes_in"`
	BytesOut      int64      `json:"bytes_out"`
	DurationMS    int64      `json:"duration_ms"`
	LastError     string     `json:"last_error,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
}

type stateKey struct {
	component string
	poolID    string
}

type rollupKey struct {
	bucket    time.Time
	component string
	poolID    string
}

type componentState struct {
	running             int
	attempts            uint64
	successes           uint64
	failures            uint64
	consecutiveFailures int
	itemsIn             int64
	itemsOut            int64
	bytesIn             int64
	bytesOut            int64
	backlog             int64
	lastAttemptAt       *time.Time
	lastSuccessAt       *time.Time
	lastFailureAt       *time.Time
	lastError           string
}

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]Definition
	states      map[stateKey]*componentState
	rollups     map[rollupKey]*Rollup
	now         func() time.Time
}

var Default = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{
		definitions: map[string]Definition{},
		states:      map[stateKey]*componentState{},
		rollups:     map[rollupKey]*Rollup{},
		now:         time.Now,
	}
}

func (r *Registry) Configure(definitions []Definition) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		definition.Key = strings.TrimSpace(definition.Key)
		if definition.Key == "" {
			continue
		}
		if definition.ExecutionMode == "" {
			definition.ExecutionMode = EventDriven
		}
		next[definition.Key] = definition
	}
	r.definitions = next
}

func (r *Registry) Begin(component, poolID string) func(Result) {
	if r == nil {
		return func(Result) {}
	}
	component = strings.TrimSpace(component)
	poolID = strings.TrimSpace(poolID)
	started := r.now().UTC()
	r.mu.Lock()
	state := r.stateLocked(stateKey{component: component, poolID: poolID})
	state.running++
	state.lastAttemptAt = timePtr(started)
	r.mu.Unlock()

	var once sync.Once
	return func(result Result) {
		once.Do(func() {
			r.finish(component, poolID, started, result)
		})
	}
}

func (r *Registry) Record(component, poolID string, result Result) {
	finish := r.Begin(component, poolID)
	finish(result)
}

func (r *Registry) finish(component, poolID string, started time.Time, result Result) {
	now := r.now().UTC()
	duration := now.Sub(started)
	if duration < 0 {
		duration = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := stateKey{component: component, poolID: poolID}
	state := r.stateLocked(key)
	if state.running > 0 {
		state.running--
	}
	state.attempts++
	state.itemsIn += maxZero(result.ItemsIn)
	state.itemsOut += maxZero(result.ItemsOut)
	state.bytesIn += maxZero(result.BytesIn)
	state.bytesOut += maxZero(result.BytesOut)
	state.backlog = maxZero(result.Backlog)
	rollup := r.rollupLocked(component, poolID, now)
	rollup.Attempts++
	rollup.ItemsIn += maxZero(result.ItemsIn)
	rollup.ItemsOut += maxZero(result.ItemsOut)
	rollup.BytesIn += maxZero(result.BytesIn)
	rollup.BytesOut += maxZero(result.BytesOut)
	rollup.DurationMS += duration.Milliseconds()
	rollup.LastAttemptAt = timePtr(now)
	if result.Err != nil {
		message := sanitizeError(result.Err.Error())
		state.failures++
		state.consecutiveFailures++
		state.lastFailureAt = timePtr(now)
		state.lastError = message
		rollup.Failures++
		rollup.LastFailureAt = timePtr(now)
		rollup.LastError = message
		return
	}
	state.successes++
	state.consecutiveFailures = 0
	state.lastSuccessAt = timePtr(now)
	state.lastError = ""
	rollup.Successes++
	rollup.LastSuccessAt = timePtr(now)
}

func (r *Registry) Snapshot() []Snapshot {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now().UTC()
	out := make([]Snapshot, 0, len(r.definitions))
	for _, definition := range r.definitions {
		snapshot := Snapshot{Definition: definition, Pools: []string{}}
		for key, state := range r.states {
			if key.component != definition.Key {
				continue
			}
			if key.poolID != "" {
				snapshot.Pools = append(snapshot.Pools, key.poolID)
			}
			mergeState(&snapshot, state)
		}
		sort.Strings(snapshot.Pools)
		snapshot.Pools = unique(snapshot.Pools)
		snapshot.Status = DeriveStatus(snapshot, now)
		if definition.ExecutionMode == Scheduled && definition.Interval > 0 && snapshot.LastAttemptAt != nil {
			next := snapshot.LastAttemptAt.Add(definition.Interval)
			snapshot.NextRunAt = &next
		}
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Job == out[j].Job {
			return out[i].Key < out[j].Key
		}
		return out[i].Job < out[j].Job
	})
	return out
}

func (r *Registry) DrainRollups(nodeID string) []Rollup {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Rollup, 0, len(r.rollups))
	for _, item := range r.rollups {
		copy := *item
		copy.NodeID = strings.TrimSpace(nodeID)
		out = append(out, copy)
	}
	r.rollups = map[rollupKey]*Rollup{}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BucketStart.Equal(out[j].BucketStart) {
			if out[i].Component == out[j].Component {
				return out[i].PoolID < out[j].PoolID
			}
			return out[i].Component < out[j].Component
		}
		return out[i].BucketStart.Before(out[j].BucketStart)
	})
	return out
}

func (r *Registry) CurrentRollups(nodeID string) []Rollup {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Rollup, 0, len(r.rollups))
	for _, item := range r.rollups {
		copy := *item
		copy.NodeID = strings.TrimSpace(nodeID)
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BucketStart.Equal(out[j].BucketStart) {
			return out[i].Component < out[j].Component
		}
		return out[i].BucketStart.Before(out[j].BucketStart)
	})
	return out
}

func (r *Registry) RestoreRollups(items []Rollup) {
	if r == nil || len(items) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range items {
		item := items[i]
		key := rollupKey{bucket: item.BucketStart.UTC(), component: item.Component, poolID: item.PoolID}
		target, ok := r.rollups[key]
		if !ok {
			item.NodeID = ""
			r.rollups[key] = &item
			continue
		}
		mergeRollup(target, item)
	}
}

func (r *Registry) stateLocked(key stateKey) *componentState {
	state, ok := r.states[key]
	if !ok {
		state = &componentState{}
		r.states[key] = state
	}
	return state
}

func (r *Registry) rollupLocked(component, poolID string, now time.Time) *Rollup {
	bucket := now.Truncate(RollupBucket)
	key := rollupKey{bucket: bucket, component: component, poolID: poolID}
	item, ok := r.rollups[key]
	if ok {
		return item
	}
	definition := r.definitions[component]
	item = &Rollup{
		BucketStart:   bucket,
		BucketSeconds: int(RollupBucket.Seconds()),
		PoolID:        poolID,
		Component:     component,
		Job:           definition.Job,
	}
	r.rollups[key] = item
	return item
}

func mergeState(out *Snapshot, state *componentState) {
	out.Running += state.running
	out.Attempts += state.attempts
	out.Successes += state.successes
	out.Failures += state.failures
	if state.consecutiveFailures > out.ConsecutiveFailures {
		out.ConsecutiveFailures = state.consecutiveFailures
	}
	out.ItemsIn += state.itemsIn
	out.ItemsOut += state.itemsOut
	out.BytesIn += state.bytesIn
	out.BytesOut += state.bytesOut
	out.Backlog += state.backlog
	out.LastAttemptAt = latest(out.LastAttemptAt, state.lastAttemptAt)
	out.LastSuccessAt = latest(out.LastSuccessAt, state.lastSuccessAt)
	out.LastFailureAt = latest(out.LastFailureAt, state.lastFailureAt)
	if out.LastFailureAt != nil && state.lastFailureAt != nil && out.LastFailureAt.Equal(*state.lastFailureAt) {
		out.LastError = state.lastError
	}
}

func DeriveStatus(snapshot Snapshot, now time.Time) Status {
	if !snapshot.Configured {
		return StatusOff
	}
	if !snapshot.Eligible {
		return StatusBlocked
	}
	if snapshot.Running > 0 {
		return StatusWorking
	}
	if snapshot.ConsecutiveFailures >= 3 {
		return StatusDegraded
	}
	if snapshot.ExecutionMode != Scheduled {
		return StatusReady
	}
	if snapshot.LastAttemptAt == nil {
		return StatusStarting
	}
	if snapshot.Interval > 0 {
		grace := 2 * snapshot.Interval
		if grace < 5*time.Minute {
			grace = 5 * time.Minute
		}
		if snapshot.LastSuccessAt == nil && now.Sub(*snapshot.LastAttemptAt) > grace {
			return StatusDegraded
		}
		if snapshot.LastSuccessAt != nil && now.Sub(*snapshot.LastSuccessAt) > grace {
			return StatusDegraded
		}
	}
	return StatusReady
}

func mergeRollup(target *Rollup, item Rollup) {
	target.Attempts += item.Attempts
	target.Successes += item.Successes
	target.Failures += item.Failures
	target.ItemsIn += item.ItemsIn
	target.ItemsOut += item.ItemsOut
	target.BytesIn += item.BytesIn
	target.BytesOut += item.BytesOut
	target.DurationMS += item.DurationMS
	target.LastAttemptAt = latest(target.LastAttemptAt, item.LastAttemptAt)
	target.LastSuccessAt = latest(target.LastSuccessAt, item.LastSuccessAt)
	if next := latest(target.LastFailureAt, item.LastFailureAt); next != target.LastFailureAt {
		target.LastError = item.LastError
		target.LastFailureAt = next
	}
}

func latest(left, right *time.Time) *time.Time {
	if right == nil {
		return left
	}
	if left == nil || right.After(*left) {
		copy := right.UTC()
		return &copy
	}
	return left
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func maxZero(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func sanitizeError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}

func unique(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
