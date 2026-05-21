package nntp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
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
	CapacityPolicy            CapacityPolicy
	ModuleReservationsEnabled bool
	IdleBorrowEnabled         bool
	IndexerMaxPercent         int
	DownloaderReservePercent  int
	DownloaderDemandWindow    time.Duration
}

type ManagerStats struct {
	Capacity        int
	Active          int
	Idle            int
	Waiting         int64
	BusyReturns     int64
	WaitCount       int64
	WaitDurationMS  int64
	WaitMaxMS       int64
	Fetches         int64
	FetchBodyPrefix int64
	GroupStats      int64
	XOver           int64
	ArticleNotFound int64
	OperationErrors int64
	Modules         ManagerModuleStats
	Providers       []ManagerProviderStats
	Scopes          []ManagerScopeStats
}

type ManagerModuleStats struct {
	ReservationsEnabled      bool
	IdleBorrowEnabled        bool
	IndexerMaxPercent        int
	DownloaderReservePercent int
	DownloaderDemandWindowMS int64
	IndexerActive            int64
	DownloaderActive         int64
	IndexerLimit             int
	DownloaderLimit          int
	DownloaderDemandActive   bool
}

type ManagerScopeStats struct {
	Scope           string
	Active          int64
	Waiting         int64
	WaitCount       int64
	WaitDurationMS  int64
	WaitMaxMS       int64
	Fetches         int64
	FetchBodyPrefix int64
	GroupStats      int64
	XOver           int64
	ArticleNotFound int64
	OperationErrors int64
}

type ManagerProviderStats struct {
	ID                string
	Label             string
	Priority          int
	Capacity          int
	Active            int
	Idle              int
	Dials             int64
	DialFailures      int64
	PoolReuses        int64
	PoolReturns       int64
	PoolDiscardIdle   int64
	PoolDiscardAge    int64
	PoolDiscardError  int64
	FetchRetries      int64
	GroupStatsRetries int64
	XOverRetries      int64
	RecoverableErrors int64
}

type Manager struct {
	ctx       *app.Context
	providers []*managedProvider
	opts      ManagerOptions
	stats     managerStats
}

type managerStats struct {
	mu               sync.Mutex
	scopes           map[string]*managerScopeStats
	waiting          atomic.Int64
	busyReturns      atomic.Int64
	waitCount        atomic.Int64
	waitDurationNS   atomic.Int64
	waitMaxNS        atomic.Int64
	fetches          atomic.Int64
	fetchBodyPrefix  atomic.Int64
	groupStats       atomic.Int64
	xover            atomic.Int64
	articleNotFound  atomic.Int64
	operationErrors  atomic.Int64
	indexerActive    atomic.Int64
	downloaderActive atomic.Int64
	downloaderDemand atomic.Int64
}

type managerScopeStats struct {
	active          atomic.Int64
	waiting         atomic.Int64
	waitCount       atomic.Int64
	waitDurationNS  atomic.Int64
	waitMaxNS       atomic.Int64
	fetches         atomic.Int64
	fetchBodyPrefix atomic.Int64
	groupStats      atomic.Int64
	xover           atomic.Int64
	articleNotFound atomic.Int64
	operationErrors atomic.Int64
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
	if opts.IndexerMaxPercent <= 0 {
		opts.IndexerMaxPercent = 80
	}
	if opts.IndexerMaxPercent > 100 {
		opts.IndexerMaxPercent = 100
	}
	if opts.DownloaderReservePercent <= 0 {
		opts.DownloaderReservePercent = 20
	}
	if opts.DownloaderReservePercent > 100 {
		opts.DownloaderReservePercent = 100
	}
	if opts.DownloaderDemandWindow <= 0 {
		opts.DownloaderDemandWindow = 30 * time.Second
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
	return m.ClientForScope("unscoped")
}

func (m *Manager) ClientForScope(scope string) *ManagerClient {
	return &ManagerClient{manager: m, scope: normalizeScope(scope)}
}

type ManagerClient struct {
	manager *Manager
	scope   string
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
	return c.manager.FetchMessageForScope(ctx, c.scope, msgID, groups)
}

func (c *ManagerClient) FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error) {
	if c == nil || c.manager == nil {
		return nil, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.FetchBodyPrefixForScope(ctx, c.scope, msgID, groups, maxBytes)
}

func (c *ManagerClient) GroupStats(ctx context.Context, group string) (GroupStats, error) {
	if c == nil || c.manager == nil {
		return GroupStats{}, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.GroupStatsForScope(ctx, c.scope, group)
}

func (c *ManagerClient) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	if c == nil || c.manager == nil {
		return nil, fmt.Errorf("nntp manager client is nil")
	}
	return c.manager.XOverForScope(ctx, c.scope, group, from, to)
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
	scopeStats := m.scopeStats("downloader")
	scopeStats.fetches.Add(1)
	return m.fetch(ctx, "downloader", seg, groups)
}

func (m *Manager) FetchMessage(ctx context.Context, msgID string, groups []string) (io.Reader, error) {
	return m.FetchMessageForScope(ctx, "unscoped", msgID, groups)
}

func (m *Manager) FetchMessageForScope(ctx context.Context, scope, msgID string, groups []string) (io.Reader, error) {
	m.stats.fetches.Add(1)
	scopeStats := m.scopeStats(scope)
	scopeStats.fetches.Add(1)
	return m.fetch(ctx, scope, &domain.Segment{MessageID: msgID}, groups)
}

func (m *Manager) fetch(ctx context.Context, scope string, seg *domain.Segment, groups []string) (io.Reader, error) {
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

		acquired, err := m.acquire(ctx, scope, mp)
		if err != nil {
			return nil, err
		}
		if acquired {
			if m.ctx != nil && m.ctx.Logger != nil {
				m.ctx.Logger.Debug("Segment %s: Attempting fetch from %s", seg.MessageID, mp.Label())
			}
			reader, err := m.fetchFromAcquiredProvider(ctx, scope, mp, seg, groups)
			if err == nil {
				return reader, nil
			}
			if errors.Is(err, ErrArticleNotFound) {
				continue
			}
			lastErr = err
			continue
		}
	}

	// If all providers are confirmed missing
	if len(seg.MissingFrom) == len(m.providers) {
		return nil, ErrArticleNotFound
	}

	// If we have a real error (not 430), return it to trigger a retry with backoff
	if lastErr != nil {
		m.recordOperationError(scope, lastErr)
		return nil, lastErr
	}

	if m.opts.CapacityPolicy == CapacityWaitQueue {
		mp := m.firstFetchProvider(seg)
		if mp == nil {
			return nil, ErrArticleNotFound
		}
		if err := m.waitAcquire(ctx, scope, mp); err != nil {
			return nil, err
		}
		reader, err := m.fetchFromAcquiredProvider(ctx, scope, mp, seg, groups)
		if err != nil {
			if errors.Is(err, ErrArticleNotFound) {
				return m.fetch(ctx, scope, seg, groups)
			}
			m.recordOperationError(scope, err)
			return nil, err
		}
		return reader, nil
	}

	// Some providers were busy, so we tell the worker to wait and try again
	m.stats.busyReturns.Add(1)
	return nil, ErrProviderBusy
}

func (m *Manager) fetchFromAcquiredProvider(ctx context.Context, scope string, mp *managedProvider, seg *domain.Segment, groups []string) (io.Reader, error) {
	reader, err := m.tryFetch(ctx, mp, seg.MessageID, groups)
	if err != nil {
		m.releaseForScope(scope, mp)

		if errors.Is(err, ErrArticleNotFound) {
			m.recordArticleNotFound(scope)
			if m.ctx != nil && m.ctx.Logger != nil {
				m.ctx.Logger.Debug("Provider %s: 430 Missing, marking as missing for segment %s...", mp.Label(), seg.MessageID)
			}
			seg.MissingFrom[mp.ID()] = true
			time.Sleep(100 * time.Millisecond)
			return nil, ErrArticleNotFound
		}

		if m.ctx != nil && m.ctx.Logger != nil {
			m.ctx.Logger.Debug("Failover: %s error: %v", mp.Label(), err)
		}
		return nil, err
	}

	return &releaseReader{
		Reader: reader,
		onClose: func() {
			m.releaseForScope(scope, mp)
		},
	}, nil
}

func (m *Manager) FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error) {
	return m.FetchBodyPrefixForScope(ctx, "unscoped", msgID, groups, maxBytes)
}

func (m *Manager) FetchBodyPrefixForScope(ctx context.Context, scope, msgID string, groups []string, maxBytes int64) ([]byte, error) {
	m.stats.fetchBodyPrefix.Add(1)
	scopeStats := m.scopeStats(scope)
	scopeStats.fetchBodyPrefix.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var lastErr error
	for _, mp := range m.providers {
		acquired, err := m.acquire(ctx, scope, mp)
		if err != nil {
			return nil, err
		}
		if !acquired {
			continue
		}

		result, err := mp.Provider.FetchBodyPrefix(ctx, msgID, groups, maxBytes)
		m.releaseForScope(scope, mp)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		m.recordOperationError(scope, lastErr)
		return nil, lastErr
	}
	if m.opts.CapacityPolicy == CapacityWaitQueue {
		mp, err := m.waitForProvider(ctx, scope)
		if err != nil {
			return nil, err
		}
		result, err := mp.Provider.FetchBodyPrefix(ctx, msgID, groups, maxBytes)
		m.releaseForScope(scope, mp)
		if err != nil {
			m.recordOperationError(scope, err)
		}
		return result, err
	}
	m.stats.busyReturns.Add(1)
	return nil, ErrProviderBusy
}

func (m *Manager) GroupStats(ctx context.Context, group string) (GroupStats, error) {
	return m.GroupStatsForScope(ctx, "unscoped", group)
}

func (m *Manager) GroupStatsForScope(ctx context.Context, scope, group string) (GroupStats, error) {
	m.stats.groupStats.Add(1)
	scopeStats := m.scopeStats(scope)
	scopeStats.groupStats.Add(1)
	if err := ctx.Err(); err != nil {
		return GroupStats{}, err
	}

	var lastErr error
	for _, mp := range m.providers {
		acquired, err := m.acquire(ctx, scope, mp)
		if err != nil {
			return GroupStats{}, err
		}
		if !acquired {
			continue
		}

		result, err := mp.Provider.GroupStats(ctx, group)
		m.releaseForScope(scope, mp)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		m.recordOperationError(scope, lastErr)
		return GroupStats{}, lastErr
	}
	if m.opts.CapacityPolicy == CapacityWaitQueue {
		mp, err := m.waitForProvider(ctx, scope)
		if err != nil {
			return GroupStats{}, err
		}
		result, err := mp.Provider.GroupStats(ctx, group)
		m.releaseForScope(scope, mp)
		if err != nil {
			m.recordOperationError(scope, err)
		}
		return result, err
	}
	m.stats.busyReturns.Add(1)
	return GroupStats{}, ErrProviderBusy
}

func (m *Manager) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	return m.XOverForScope(ctx, "unscoped", group, from, to)
}

func (m *Manager) XOverForScope(ctx context.Context, scope, group string, from, to int64) ([]OverviewHeader, error) {
	m.stats.xover.Add(1)
	scopeStats := m.scopeStats(scope)
	scopeStats.xover.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var lastErr error
	for _, mp := range m.providers {
		acquired, err := m.acquire(ctx, scope, mp)
		if err != nil {
			return nil, err
		}
		if !acquired {
			continue
		}

		result, err := mp.Provider.XOver(ctx, group, from, to)
		m.releaseForScope(scope, mp)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		m.recordOperationError(scope, lastErr)
		return nil, lastErr
	}
	if m.opts.CapacityPolicy == CapacityWaitQueue {
		mp, err := m.waitForProvider(ctx, scope)
		if err != nil {
			return nil, err
		}
		result, err := mp.Provider.XOver(ctx, group, from, to)
		m.releaseForScope(scope, mp)
		if err != nil {
			m.recordOperationError(scope, err)
		}
		return result, err
	}
	m.stats.busyReturns.Add(1)
	return nil, ErrProviderBusy
}

func (m *Manager) acquire(ctx context.Context, scope string, mp *managedProvider) (bool, error) {
	if mp == nil {
		return false, nil
	}
	module := moduleForScope(scope)
	if module == "downloader" {
		m.recordDownloaderDemand()
	}
	if !m.moduleCanAcquire(module) {
		return false, nil
	}
	select {
	case mp.semaphore <- struct{}{}:
		m.recordActive(scope, module, 1)
		return true, nil
	default:
	}
	if m.opts.CapacityPolicy != CapacityWaitQueue {
		return false, nil
	}

	return false, nil
}

func (m *Manager) waitAcquire(ctx context.Context, scope string, mp *managedProvider) error {
	if mp == nil {
		return ErrProviderBusy
	}
	start := time.Now()
	scopeStats := m.scopeStats(scope)
	module := moduleForScope(scope)
	m.stats.waiting.Add(1)
	scopeStats.waiting.Add(1)
	defer m.stats.waiting.Add(-1)
	defer scopeStats.waiting.Add(-1)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if module == "downloader" {
			m.recordDownloaderDemand()
		}
		if m.moduleCanAcquire(module) {
			select {
			case mp.semaphore <- struct{}{}:
				m.recordActive(scope, module, 1)
				m.recordWait(scopeStats, time.Since(start))
				return nil
			default:
			}
		}
		select {
		case <-ctx.Done():
			m.recordWait(scopeStats, time.Since(start))
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) waitForProvider(ctx context.Context, scope string) (*managedProvider, error) {
	if len(m.providers) == 0 {
		return nil, ErrProviderBusy
	}
	mp := m.providers[0]
	if err := m.waitAcquire(ctx, scope, mp); err != nil {
		return nil, err
	}
	return mp, nil
}

func (m *Manager) firstFetchProvider(seg *domain.Segment) *managedProvider {
	for _, mp := range m.providers {
		if seg != nil && seg.MissingFrom != nil && seg.MissingFrom[mp.ID()] {
			continue
		}
		return mp
	}
	return nil
}

func (m *Manager) release(mp *managedProvider) {
	m.releaseForScope("unscoped", mp)
}

func (m *Manager) releaseForScope(scope string, mp *managedProvider) {
	if mp == nil {
		return
	}
	<-mp.semaphore
	m.recordActive(scope, moduleForScope(scope), -1)
}

func (m *Manager) recordWait(scopeStats *managerScopeStats, d time.Duration) {
	if d <= 0 {
		return
	}
	ns := d.Nanoseconds()
	m.stats.waitCount.Add(1)
	m.stats.waitDurationNS.Add(ns)
	if scopeStats != nil {
		scopeStats.waitCount.Add(1)
		scopeStats.waitDurationNS.Add(ns)
		for {
			current := scopeStats.waitMaxNS.Load()
			if ns <= current || scopeStats.waitMaxNS.CompareAndSwap(current, ns) {
				break
			}
		}
	}
	for {
		current := m.stats.waitMaxNS.Load()
		if ns <= current || m.stats.waitMaxNS.CompareAndSwap(current, ns) {
			return
		}
	}
}

func (m *Manager) recordArticleNotFound(scope string) {
	m.stats.articleNotFound.Add(1)
	m.scopeStats(scope).articleNotFound.Add(1)
}

func (m *Manager) recordOperationError(scope string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ErrArticleNotFound) {
		m.recordArticleNotFound(scope)
		return
	}
	m.stats.operationErrors.Add(1)
	m.scopeStats(scope).operationErrors.Add(1)
}

func (m *Manager) scopeStats(scope string) *managerScopeStats {
	scope = normalizeScope(scope)
	m.stats.mu.Lock()
	defer m.stats.mu.Unlock()
	if m.stats.scopes == nil {
		m.stats.scopes = make(map[string]*managerScopeStats)
	}
	stats, ok := m.stats.scopes[scope]
	if !ok {
		stats = &managerScopeStats{}
		m.stats.scopes[scope] = stats
	}
	return stats
}

func normalizeScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "unscoped"
	}
	return scope
}

func moduleForScope(scope string) string {
	if normalizeScope(scope) == "downloader" {
		return "downloader"
	}
	return "indexer"
}

func (m *Manager) recordDownloaderDemand() {
	if m == nil {
		return
	}
	m.stats.downloaderDemand.Store(time.Now().UnixNano())
}

func (m *Manager) downloaderDemandActive(now time.Time) bool {
	if m == nil {
		return false
	}
	last := m.stats.downloaderDemand.Load()
	if last == 0 {
		return m.stats.downloaderActive.Load() > 0
	}
	return m.stats.downloaderActive.Load() > 0 || now.Sub(time.Unix(0, last)) <= m.opts.DownloaderDemandWindow
}

func (m *Manager) moduleCanAcquire(module string) bool {
	if m == nil {
		return false
	}
	if !m.opts.ModuleReservationsEnabled {
		return true
	}
	limits := m.moduleLimits(time.Now())
	switch module {
	case "downloader":
		return m.stats.downloaderActive.Load() < int64(limits.downloader)
	default:
		return m.stats.indexerActive.Load() < int64(limits.indexer)
	}
}

type moduleLimits struct {
	indexer              int
	downloader           int
	downloaderDemandLive bool
}

func (m *Manager) moduleLimits(now time.Time) moduleLimits {
	capacity := m.TotalCapacity()
	if capacity <= 0 {
		return moduleLimits{}
	}

	indexerLimit := capacity
	downloaderLimit := capacity
	downloaderDemand := m.downloaderDemandActive(now)
	if !m.opts.IdleBorrowEnabled || downloaderDemand {
		indexerLimit = percentOfCapacity(capacity, m.opts.IndexerMaxPercent)
	}
	if !m.opts.IdleBorrowEnabled {
		downloaderLimit = percentOfCapacity(capacity, 100-m.opts.IndexerMaxPercent)
		if downloaderLimit <= 0 {
			downloaderLimit = percentOfCapacity(capacity, m.opts.DownloaderReservePercent)
		}
	}
	if downloaderLimit <= 0 {
		downloaderLimit = 1
	}
	return moduleLimits{indexer: indexerLimit, downloader: downloaderLimit, downloaderDemandLive: downloaderDemand}
}

func percentOfCapacity(capacity, percent int) int {
	if capacity <= 0 {
		return 0
	}
	if percent <= 0 {
		return 1
	}
	if percent > 100 {
		percent = 100
	}
	limit := capacity * percent / 100
	if limit <= 0 {
		return 1
	}
	return limit
}

func (m *Manager) recordActive(scope, module string, delta int64) {
	m.scopeStats(scope).active.Add(delta)
	switch module {
	case "downloader":
		m.stats.downloaderActive.Add(delta)
	default:
		m.stats.indexerActive.Add(delta)
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
	idle := 0
	providers := make([]ManagerProviderStats, 0, len(m.providers))
	for _, mp := range m.providers {
		providerActive := len(mp.semaphore)
		providerIdle := mp.IdleConnectionCount()
		active += providerActive
		idle += providerIdle
		providerStats := mp.Provider.StatsSnapshot()
		providers = append(providers, ManagerProviderStats{
			ID:                mp.ID(),
			Label:             mp.Label(),
			Priority:          mp.Priority(),
			Capacity:          cap(mp.semaphore),
			Active:            providerActive,
			Idle:              providerIdle,
			Dials:             providerStats.Dials,
			DialFailures:      providerStats.DialFailures,
			PoolReuses:        providerStats.PoolReuses,
			PoolReturns:       providerStats.PoolReturns,
			PoolDiscardIdle:   providerStats.PoolDiscardIdle,
			PoolDiscardAge:    providerStats.PoolDiscardAge,
			PoolDiscardError:  providerStats.PoolDiscardError,
			FetchRetries:      providerStats.FetchRetries,
			GroupStatsRetries: providerStats.GroupStatsRetries,
			XOverRetries:      providerStats.XOverRetries,
			RecoverableErrors: providerStats.RecoverableErrors,
		})
	}
	return ManagerStats{
		Capacity:        m.TotalCapacity(),
		Active:          active,
		Idle:            idle,
		Waiting:         m.stats.waiting.Load(),
		BusyReturns:     m.stats.busyReturns.Load(),
		WaitCount:       m.stats.waitCount.Load(),
		WaitDurationMS:  m.stats.waitDurationNS.Load() / int64(time.Millisecond),
		WaitMaxMS:       m.stats.waitMaxNS.Load() / int64(time.Millisecond),
		Fetches:         m.stats.fetches.Load(),
		FetchBodyPrefix: m.stats.fetchBodyPrefix.Load(),
		GroupStats:      m.stats.groupStats.Load(),
		XOver:           m.stats.xover.Load(),
		ArticleNotFound: m.stats.articleNotFound.Load(),
		OperationErrors: m.stats.operationErrors.Load(),
		Modules:         m.moduleStats(),
		Providers:       providers,
		Scopes:          m.scopeStatsSnapshot(),
	}
}

func (m *Manager) moduleStats() ManagerModuleStats {
	if m == nil {
		return ManagerModuleStats{}
	}
	limits := m.moduleLimits(time.Now())
	return ManagerModuleStats{
		ReservationsEnabled:      m.opts.ModuleReservationsEnabled,
		IdleBorrowEnabled:        m.opts.IdleBorrowEnabled,
		IndexerMaxPercent:        m.opts.IndexerMaxPercent,
		DownloaderReservePercent: m.opts.DownloaderReservePercent,
		DownloaderDemandWindowMS: m.opts.DownloaderDemandWindow.Milliseconds(),
		IndexerActive:            m.stats.indexerActive.Load(),
		DownloaderActive:         m.stats.downloaderActive.Load(),
		IndexerLimit:             limits.indexer,
		DownloaderLimit:          limits.downloader,
		DownloaderDemandActive:   limits.downloaderDemandLive,
	}
}

func (m *Manager) scopeStatsSnapshot() []ManagerScopeStats {
	m.stats.mu.Lock()
	defer m.stats.mu.Unlock()
	out := make([]ManagerScopeStats, 0, len(m.stats.scopes))
	for scope, stats := range m.stats.scopes {
		out = append(out, ManagerScopeStats{
			Scope:           scope,
			Active:          stats.active.Load(),
			Waiting:         stats.waiting.Load(),
			WaitCount:       stats.waitCount.Load(),
			WaitDurationMS:  stats.waitDurationNS.Load() / int64(time.Millisecond),
			WaitMaxMS:       stats.waitMaxNS.Load() / int64(time.Millisecond),
			Fetches:         stats.fetches.Load(),
			FetchBodyPrefix: stats.fetchBodyPrefix.Load(),
			GroupStats:      stats.groupStats.Load(),
			XOver:           stats.xover.Load(),
			ArticleNotFound: stats.articleNotFound.Load(),
			OperationErrors: stats.operationErrors.Load(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Scope < out[j].Scope
	})
	return out
}

func (m *Manager) RuntimeStats(scope string) app.NNTPRuntimeStats {
	stats := m.Stats()
	out := app.NNTPRuntimeStats{
		Scope:           scope,
		Policy:          string(m.opts.CapacityPolicy),
		Capacity:        stats.Capacity,
		Active:          stats.Active,
		Idle:            stats.Idle,
		Waiting:         stats.Waiting,
		BusyReturns:     stats.BusyReturns,
		WaitCount:       stats.WaitCount,
		WaitDurationMS:  stats.WaitDurationMS,
		WaitMaxMS:       stats.WaitMaxMS,
		Fetches:         stats.Fetches,
		FetchBodyPrefix: stats.FetchBodyPrefix,
		GroupStats:      stats.GroupStats,
		XOver:           stats.XOver,
		ArticleNotFound: stats.ArticleNotFound,
		OperationErrors: stats.OperationErrors,
		Modules: app.NNTPModuleRuntimeStats{
			ReservationsEnabled:      stats.Modules.ReservationsEnabled,
			IdleBorrowEnabled:        stats.Modules.IdleBorrowEnabled,
			IndexerMaxPercent:        stats.Modules.IndexerMaxPercent,
			DownloaderReservePercent: stats.Modules.DownloaderReservePercent,
			DownloaderDemandWindowMS: stats.Modules.DownloaderDemandWindowMS,
			IndexerActive:            stats.Modules.IndexerActive,
			DownloaderActive:         stats.Modules.DownloaderActive,
			IndexerLimit:             stats.Modules.IndexerLimit,
			DownloaderLimit:          stats.Modules.DownloaderLimit,
			DownloaderDemandActive:   stats.Modules.DownloaderDemandActive,
		},
		Providers: make([]app.NNTPProviderRuntimeStats, 0, len(stats.Providers)),
		Scopes:    make([]app.NNTPScopeRuntimeStats, 0, len(stats.Scopes)),
	}
	for _, provider := range stats.Providers {
		out.Providers = append(out.Providers, app.NNTPProviderRuntimeStats{
			ID:                provider.ID,
			Label:             provider.Label,
			Priority:          provider.Priority,
			Capacity:          provider.Capacity,
			Active:            provider.Active,
			Idle:              provider.Idle,
			Dials:             provider.Dials,
			DialFailures:      provider.DialFailures,
			PoolReuses:        provider.PoolReuses,
			PoolReturns:       provider.PoolReturns,
			PoolDiscardIdle:   provider.PoolDiscardIdle,
			PoolDiscardAge:    provider.PoolDiscardAge,
			PoolDiscardError:  provider.PoolDiscardError,
			FetchRetries:      provider.FetchRetries,
			GroupStatsRetries: provider.GroupStatsRetries,
			XOverRetries:      provider.XOverRetries,
			RecoverableErrors: provider.RecoverableErrors,
		})
	}
	for _, scope := range stats.Scopes {
		out.Scopes = append(out.Scopes, app.NNTPScopeRuntimeStats{
			Scope:           scope.Scope,
			Active:          scope.Active,
			Waiting:         scope.Waiting,
			WaitCount:       scope.WaitCount,
			WaitDurationMS:  scope.WaitDurationMS,
			WaitMaxMS:       scope.WaitMaxMS,
			Fetches:         scope.Fetches,
			FetchBodyPrefix: scope.FetchBodyPrefix,
			GroupStats:      scope.GroupStats,
			XOver:           scope.XOver,
			ArticleNotFound: scope.ArticleNotFound,
			OperationErrors: scope.OperationErrors,
		})
	}
	return out
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
