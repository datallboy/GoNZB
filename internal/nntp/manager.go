package nntp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync/atomic"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

var FETCH_RETRY_COUNT = 3

type managedProvider struct {
	Provider
	semaphore chan struct{}
}

type CapacityPolicy string

const (
	CapacityReturnBusy CapacityPolicy = "return_busy"
	CapacityWaitQueue  CapacityPolicy = "wait_queue"
)

type ManagerOptions struct {
	CapacityPolicy CapacityPolicy
}

type ManagerStats struct {
	Capacity        int
	Active          int
	Waiting         int64
	BusyReturns     int64
	WaitCount       int64
	WaitDurationMS  int64
	WaitMaxMS       int64
	Fetches         int64
	FetchBodyPrefix int64
	GroupStats      int64
	XOver           int64
}

type Manager struct {
	ctx       *app.Context
	providers []*managedProvider
	opts      ManagerOptions
	stats     managerStats
}

type managerStats struct {
	waiting         atomic.Int64
	busyReturns     atomic.Int64
	waitCount       atomic.Int64
	waitDurationNS  atomic.Int64
	waitMaxNS       atomic.Int64
	fetches         atomic.Int64
	fetchBodyPrefix atomic.Int64
	groupStats      atomic.Int64
	xover           atomic.Int64
}

func NewManager(ctx *app.Context) (*Manager, error) {
	return NewManagerWithOptions(ctx, ManagerOptions{CapacityPolicy: CapacityReturnBusy})
}

func NewManagerWithOptions(ctx *app.Context, opts ManagerOptions) (*Manager, error) {
	var managed []*managedProvider

	for _, cfg := range ctx.Config.Servers {
		p := NewNNTPProviderWithLogger(cfg, ctx.Logger)

		if ctx.Logger != nil {
			ctx.Logger.Info("Validating provider: %s", p.Label())
		}
		if err := p.TestConnection(); err != nil {
			return nil, fmt.Errorf("connection test failed for %s: %w", p.Label(), err)
		}

		managed = append(managed, &managedProvider{
			Provider:  p,
			semaphore: make(chan struct{}, p.MaxConnection()),
		})
	}

	// Sort providers by priority (0 = highest)
	sort.Slice(managed, func(i, j int) bool {
		return managed[i].Priority() < managed[j].Priority()
	})
	return newManagerWithProviders(ctx, managed, opts), nil
}

func newManagerWithProviders(ctx *app.Context, providers []*managedProvider, opts ManagerOptions) *Manager {
	if opts.CapacityPolicy == "" {
		opts.CapacityPolicy = CapacityReturnBusy
	}
	return &Manager{ctx: ctx, providers: providers, opts: opts}
}

func newManagedProvider(p Provider) *managedProvider {
	capacity := p.MaxConnection()
	if capacity <= 0 {
		capacity = 1
	}
	return &managedProvider{
		Provider:  p,
		semaphore: make(chan struct{}, capacity),
	}
}

func (m *Manager) Client() *ManagerClient {
	return &ManagerClient{manager: m}
}

type ManagerClient struct {
	manager *Manager
}

func (c *ManagerClient) ID() string {
	if c == nil || c.manager == nil {
		return ""
	}
	return c.manager.ID()
}

func (c *ManagerClient) Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error) {
	if c == nil || c.manager == nil {
		return nil, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.FetchMessage(ctx, msgID, groups)
}

func (c *ManagerClient) FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error) {
	if c == nil || c.manager == nil {
		return nil, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.FetchBodyPrefix(ctx, msgID, groups, maxBytes)
}

func (c *ManagerClient) GroupStats(ctx context.Context, group string) (GroupStats, error) {
	if c == nil || c.manager == nil {
		return GroupStats{}, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.GroupStats(ctx, group)
}

func (c *ManagerClient) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	if c == nil || c.manager == nil {
		return nil, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.XOver(ctx, group, from, to)
}

func (m *Manager) ID() string {
	if m == nil || len(m.providers) == 0 {
		return ""
	}
	return m.providers[0].ID()
}

func (m *Manager) Fetch(ctx context.Context, seg *domain.Segment, groups []string) (io.Reader, error) {
	if seg == nil {
		return nil, fmt.Errorf("segment is required")
	}
	m.stats.fetches.Add(1)
	return m.fetch(ctx, seg, groups)
}

func (m *Manager) FetchMessage(ctx context.Context, msgID string, groups []string) (io.Reader, error) {
	m.stats.fetches.Add(1)
	return m.fetch(ctx, &domain.Segment{MessageID: msgID}, groups)
}

func (m *Manager) fetch(ctx context.Context, seg *domain.Segment, groups []string) (io.Reader, error) {
	// Fast fail if user already cancelled
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if seg.MissingFrom == nil {
		seg.MissingFrom = make(map[string]bool)
	}

	var lastErr error

	for _, mp := range m.providers {
		// Skip if this provider already reported 430 for this article
		if seg.MissingFrom[mp.ID()] {
			continue
		}

		// If we already have some 430s for this segment, log that we are trying a failover
		if len(seg.MissingFrom) > 0 {
			m.ctx.Logger.Debug("[Failover] Segment %s missing on %d providers, trying %s (Priority %d)",
				seg.MessageID, len(seg.MissingFrom), mp.Label(), mp.Priority())
		}

		acquired, err := m.acquire(ctx, mp)
		if err != nil {
			return nil, err
		}
		if acquired {
			if m.ctx != nil && m.ctx.Logger != nil {
				m.ctx.Logger.Debug("Segment %s: Attempting fetch from %s", seg.MessageID, mp.Label())
			}
			reader, err := m.tryFetch(ctx, mp, seg.MessageID, groups)
			if err != nil {
				// Release the slot if the fetch fails
				m.release(mp)

				if errors.Is(err, ErrArticleNotFound) {
					if m.ctx != nil && m.ctx.Logger != nil {
						m.ctx.Logger.Debug("Provider %s: 430 Missing, marking as missing for segment %s...", mp.Label(), seg.MessageID)
					}
					seg.MissingFrom[mp.ID()] = true

					// Small sleep before trying next provider in failover
					time.Sleep(100 * time.Millisecond)
					continue
				}

				// If it's a network/auth error, keep looking but save error
				if m.ctx != nil && m.ctx.Logger != nil {
					m.ctx.Logger.Debug("Failover: %s error: %v", mp.Label(), err)
				}
				lastErr = err
				continue
			}

			// Return a reader that releases the semaphore ONLY when the
			// worker is finished reading the body.
			return &releaseReader{
				Reader: reader,
				onClose: func() {
					m.release(mp)
				},
			}, nil
		}
	}

	// If all providers are confirmed missing
	if len(seg.MissingFrom) == len(m.providers) {
		return nil, ErrArticleNotFound
	}

	// If we have a real error (not 430), return it to trigger a retry with backoff
	if lastErr != nil {
		return nil, lastErr
	}

	// Some providers were busy, so we tell the worker to wait and try again
	m.stats.busyReturns.Add(1)
	return nil, ErrProviderBusy
}

func (m *Manager) FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error) {
	m.stats.fetchBodyPrefix.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var lastErr error
	for _, mp := range m.providers {
		acquired, err := m.acquire(ctx, mp)
		if err != nil {
			return nil, err
		}
		if !acquired {
			continue
		}

		result, err := mp.Provider.FetchBodyPrefix(ctx, msgID, groups, maxBytes)
		m.release(mp)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	m.stats.busyReturns.Add(1)
	return nil, ErrProviderBusy
}

func (m *Manager) GroupStats(ctx context.Context, group string) (GroupStats, error) {
	m.stats.groupStats.Add(1)
	if err := ctx.Err(); err != nil {
		return GroupStats{}, err
	}

	var lastErr error
	for _, mp := range m.providers {
		acquired, err := m.acquire(ctx, mp)
		if err != nil {
			return GroupStats{}, err
		}
		if !acquired {
			continue
		}

		result, err := mp.Provider.GroupStats(ctx, group)
		m.release(mp)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return GroupStats{}, lastErr
	}
	m.stats.busyReturns.Add(1)
	return GroupStats{}, ErrProviderBusy
}

func (m *Manager) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	m.stats.xover.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var lastErr error
	for _, mp := range m.providers {
		acquired, err := m.acquire(ctx, mp)
		if err != nil {
			return nil, err
		}
		if !acquired {
			continue
		}

		result, err := mp.Provider.XOver(ctx, group, from, to)
		m.release(mp)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	m.stats.busyReturns.Add(1)
	return nil, ErrProviderBusy
}

func (m *Manager) acquire(ctx context.Context, mp *managedProvider) (bool, error) {
	if mp == nil {
		return false, nil
	}
	select {
	case mp.semaphore <- struct{}{}:
		return true, nil
	default:
	}
	if m.opts.CapacityPolicy != CapacityWaitQueue {
		return false, nil
	}

	start := time.Now()
	m.stats.waiting.Add(1)
	defer m.stats.waiting.Add(-1)
	select {
	case mp.semaphore <- struct{}{}:
		m.recordWait(time.Since(start))
		return true, nil
	case <-ctx.Done():
		m.recordWait(time.Since(start))
		return false, ctx.Err()
	}
}

func (m *Manager) release(mp *managedProvider) {
	if mp == nil {
		return
	}
	<-mp.semaphore
}

func (m *Manager) recordWait(d time.Duration) {
	if d <= 0 {
		return
	}
	ns := d.Nanoseconds()
	m.stats.waitCount.Add(1)
	m.stats.waitDurationNS.Add(ns)
	for {
		current := m.stats.waitMaxNS.Load()
		if ns <= current || m.stats.waitMaxNS.CompareAndSwap(current, ns) {
			return
		}
	}
}

// try fetch will attempt to fetch an article from a provider
func (m *Manager) tryFetch(ctx context.Context, p *managedProvider, msgID string, groups []string) (io.Reader, error) {
	reader, err := p.Fetch(ctx, msgID, groups)
	if err != nil {
		return nil, err // Let the caller decide how to retry
	}

	if reader == nil {
		return nil, fmt.Errorf("provider returned no error but reader is nil")
	}
	return reader, nil
}

type releaseReader struct {
	io.Reader
	onClose func()
}

func (r *releaseReader) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

func (r *releaseReader) Close() error {
	defer func() {
		if r.onClose != nil {
			r.onClose()
			r.onClose = nil
		}
	}()

	if r.Reader != nil {
		if c, ok := r.Reader.(io.ReadCloser); ok {
			return c.Close()
		}
	}
	return nil
}

// TotalCapacity returns the maximum number of concurrent connections
// allowed across all configured providers.
func (m *Manager) TotalCapacity() int {
	total := 0
	for _, mp := range m.providers {
		// cap() tells us the size of the semaphore buffer
		// which equals the MaxConnections for that provider.
		total += cap(mp.semaphore)
	}
	return total
}

func (m *Manager) Stats() ManagerStats {
	if m == nil {
		return ManagerStats{}
	}
	active := 0
	for _, mp := range m.providers {
		active += len(mp.semaphore)
	}
	return ManagerStats{
		Capacity:        m.TotalCapacity(),
		Active:          active,
		Waiting:         m.stats.waiting.Load(),
		BusyReturns:     m.stats.busyReturns.Load(),
		WaitCount:       m.stats.waitCount.Load(),
		WaitDurationMS:  m.stats.waitDurationNS.Load() / int64(time.Millisecond),
		WaitMaxMS:       m.stats.waitMaxNS.Load() / int64(time.Millisecond),
		Fetches:         m.stats.fetches.Load(),
		FetchBodyPrefix: m.stats.fetchBodyPrefix.Load(),
		GroupStats:      m.stats.groupStats.Load(),
		XOver:           m.stats.xover.Load(),
	}
}

// allows safe teardown when reloading downloader runtime while idle
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}

	var firstErr error
	for _, mp := range m.providers {
		if mp == nil || mp.Provider == nil {
			continue
		}

		if err := mp.Provider.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
