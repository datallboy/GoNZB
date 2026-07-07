package nntp

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerReturnBusyPolicyDoesNotWaitForCapacity(t *testing.T) {
	provider := newBlockingProvider(1)
	manager := newManagerWithProviders(nil, []*managedProvider{newManagedProvider(provider)}, ManagerOptions{CapacityPolicy: CapacityReturnBusy})

	first, err := manager.FetchMessage(context.Background(), "first@example", nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	defer first.(io.Closer).Close()

	_, err = manager.FetchMessage(context.Background(), "second@example", nil)
	if err != ErrProviderBusy {
		t.Fatalf("expected ErrProviderBusy, got %v", err)
	}

	stats := manager.Stats()
	if stats.Capacity != 1 || stats.Active != 1 {
		t.Fatalf("expected capacity=1 active=1, got capacity=%d active=%d", stats.Capacity, stats.Active)
	}
	if stats.BusyReturns != 1 {
		t.Fatalf("expected one busy return, got %d", stats.BusyReturns)
	}
}

func TestManagerWaitQueuePolicyWaitsForCapacity(t *testing.T) {
	provider := newBlockingProvider(1)
	manager := newManagerWithProviders(nil, []*managedProvider{newManagedProvider(provider)}, ManagerOptions{CapacityPolicy: CapacityWaitQueue})

	first, err := manager.FetchMessage(context.Background(), "first@example", nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	gotSecond := make(chan error, 1)
	go func() {
		reader, err := manager.FetchMessage(context.Background(), "second@example", nil)
		if err == nil {
			_ = reader.(io.Closer).Close()
		}
		gotSecond <- err
	}()

	deadline := time.After(500 * time.Millisecond)
	for {
		if manager.Stats().Waiting == 1 {
			break
		}
		select {
		case err := <-gotSecond:
			t.Fatalf("second fetch returned before capacity was released: %v", err)
		case <-deadline:
			t.Fatal("second fetch did not enter wait queue")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if err := first.(io.Closer).Close(); err != nil {
		t.Fatalf("close first reader: %v", err)
	}

	select {
	case err := <-gotSecond:
		if err != nil {
			t.Fatalf("second fetch after capacity release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second fetch did not resume after capacity release")
	}

	stats := manager.Stats()
	if stats.Active != 0 {
		t.Fatalf("expected no active slots after readers close, got %d", stats.Active)
	}
	if stats.WaitCount == 0 || stats.WaitDurationMS == 0 {
		t.Fatalf("expected wait metrics, got count=%d duration_ms=%d", stats.WaitCount, stats.WaitDurationMS)
	}
	if provider.maxObserved() > 1 {
		t.Fatalf("provider concurrency exceeded capacity: %d", provider.maxObserved())
	}
}

func TestManagerWaitQueuePolicyHonorsContextCancellation(t *testing.T) {
	provider := newBlockingProvider(1)
	manager := newManagerWithProviders(nil, []*managedProvider{newManagedProvider(provider)}, ManagerOptions{CapacityPolicy: CapacityWaitQueue})

	first, err := manager.FetchMessage(context.Background(), "first@example", nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	defer first.(io.Closer).Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = manager.FetchMessage(ctx, "second@example", nil)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected context deadline, got %v", err)
	}
}

func TestManagerClientPolicyCanWaitOnReturnBusyManager(t *testing.T) {
	provider := newBlockingProvider(1)
	manager := newManagerWithProviders(nil, []*managedProvider{newManagedProvider(provider)}, ManagerOptions{CapacityPolicy: CapacityReturnBusy})
	client := manager.ClientForScopeWithPolicy("inspect_par2", CapacityWaitQueue)

	first, err := manager.FetchMessage(context.Background(), "first@example", nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	gotSecond := make(chan error, 1)
	go func() {
		reader, err := client.Fetch(context.Background(), "second@example", nil)
		if err == nil {
			_ = reader.(io.Closer).Close()
		}
		gotSecond <- err
	}()

	deadline := time.After(500 * time.Millisecond)
	for {
		if manager.Stats().Waiting == 1 {
			break
		}
		select {
		case err := <-gotSecond:
			t.Fatalf("client fetch returned before capacity was released: %v", err)
		case <-deadline:
			t.Fatal("client fetch did not enter wait queue")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if err := first.(io.Closer).Close(); err != nil {
		t.Fatalf("close first reader: %v", err)
	}

	select {
	case err := <-gotSecond:
		if err != nil {
			t.Fatalf("client fetch after capacity release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client fetch did not resume after capacity release")
	}

	if got := manager.RuntimeStats("indexer").Policy; got != string(CapacityWaitQueue) {
		t.Fatalf("expected indexer runtime policy wait_queue, got %q", got)
	}
}

func TestManagerWaitQueuePolicyTriesOtherProviderBeforeWaiting(t *testing.T) {
	busyProvider := newBlockingProvider(1)
	freeProvider := newBlockingProvider(1)
	manager := newManagerWithProviders(nil, []*managedProvider{
		newManagedProvider(busyProvider),
		newManagedProvider(freeProvider),
	}, ManagerOptions{CapacityPolicy: CapacityWaitQueue})

	first, err := manager.FetchMessage(context.Background(), "first@example", nil)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	defer first.(io.Closer).Close()

	second, err := manager.FetchMessage(context.Background(), "second@example", nil)
	if err != nil {
		t.Fatalf("second fetch should use free provider instead of waiting: %v", err)
	}
	defer second.(io.Closer).Close()

	stats := manager.Stats()
	if stats.WaitCount != 0 {
		t.Fatalf("expected no wait when another provider had capacity, got %d", stats.WaitCount)
	}
	if freeProvider.maxObserved() != 1 {
		t.Fatalf("expected second provider to be used, max observed %d", freeProvider.maxObserved())
	}
}

func TestManagerArticleNotFoundIncludesProviderAttempts(t *testing.T) {
	first := &missingProvider{id: "easynews"}
	second := &missingProvider{id: "newshosting"}
	manager := newManagerWithProviders(nil, []*managedProvider{
		newManagedProvider(first),
		newManagedProvider(second),
	}, ManagerOptions{CapacityPolicy: CapacityWaitQueue})

	_, err := manager.FetchMessage(context.Background(), "missing@example", []string{"alt.binaries.test"})
	if !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("expected article not found, got %v", err)
	}
	if !strings.Contains(err.Error(), "providers=easynews,newshosting") {
		t.Fatalf("expected provider attempts in error, got %q", err.Error())
	}
}

func TestManagerClientExposesIndexerStyleCalls(t *testing.T) {
	provider := newBlockingProvider(1)
	manager := newManagerWithProviders(nil, []*managedProvider{newManagedProvider(provider)}, ManagerOptions{CapacityPolicy: CapacityWaitQueue})
	client := manager.ClientForScope("inspect_par2")

	prefix, err := client.FetchBodyPrefix(context.Background(), "prefix@example", nil, 128)
	if err != nil {
		t.Fatalf("fetch body prefix: %v", err)
	}
	if string(prefix) != "body" {
		t.Fatalf("expected body prefix, got %q", string(prefix))
	}

	groupStats, err := client.GroupStats(context.Background(), "alt.binaries.test")
	if err != nil {
		t.Fatalf("group stats: %v", err)
	}
	if groupStats.Low != 1 || groupStats.High != 1 {
		t.Fatalf("unexpected group stats: %#v", groupStats)
	}

	rows, err := client.XOver(context.Background(), "alt.binaries.test", 1, 1)
	if err != nil {
		t.Fatalf("xover: %v", err)
	}
	if len(rows) != 1 || rows[0].ArticleNumber != 1 {
		t.Fatalf("unexpected xover rows: %#v", rows)
	}

	stats := manager.Stats()
	if len(stats.Scopes) != 1 {
		t.Fatalf("expected one scope stat, got %#v", stats.Scopes)
	}
	scope := stats.Scopes[0]
	if scope.Scope != "inspect_par2" || scope.FetchBodyPrefix != 1 || scope.GroupStats != 1 || scope.XOver != 1 {
		t.Fatalf("unexpected scope stats: %#v", scope)
	}
}

func TestManagerReservationsCapIndexerWhenDownloaderHasDemand(t *testing.T) {
	provider := newBlockingProvider(5)
	manager := newManagerWithProviders(nil, []*managedProvider{newManagedProvider(provider)}, ManagerOptions{
		CapacityPolicy:            CapacityReturnBusy,
		ModuleReservationsEnabled: true,
		IdleBorrowEnabled:         true,
		IndexerMaxPercent:         80,
		DownloaderReservePercent:  20,
		DownloaderDemandWindow:    time.Minute,
	})

	readers := make([]io.Closer, 0, 5)
	for i := 0; i < 5; i++ {
		reader, err := manager.FetchMessageForScope(context.Background(), "inspect_par2", "indexer@example", nil)
		if err != nil {
			t.Fatalf("indexer fetch %d: %v", i, err)
		}
		readers = append(readers, reader.(io.Closer))
	}

	if _, err := manager.FetchMessageForScope(context.Background(), "downloader", "download@example", nil); err != ErrProviderBusy {
		t.Fatalf("expected downloader to see busy pool and record demand, got %v", err)
	}
	if err := readers[0].Close(); err != nil {
		t.Fatalf("close indexer reader: %v", err)
	}

	if _, err := manager.FetchMessageForScope(context.Background(), "inspect_par2", "indexer-2@example", nil); err != ErrProviderBusy {
		t.Fatalf("expected indexer to be capped while downloader demand is active, got %v", err)
	}
	downloaderReader, err := manager.FetchMessageForScope(context.Background(), "downloader", "download-2@example", nil)
	if err != nil {
		t.Fatalf("expected downloader to borrow released slot: %v", err)
	}
	readers = append(readers, downloaderReader.(io.Closer))

	stats := manager.Stats()
	if !stats.Modules.DownloaderDemandActive || stats.Modules.IndexerLimit != 4 {
		t.Fatalf("unexpected module stats: %#v", stats.Modules)
	}

	for _, reader := range readers {
		_ = reader.Close()
	}
}

func TestManagerRoutesRequestsByProviderRole(t *testing.T) {
	scrapeProvider := &roleTestProvider{id: "scrape-provider", prefix: []byte("wrong")}
	yencProvider := &roleTestProvider{id: "yenc-provider", prefix: []byte("yenc")}
	manager := newManagerWithProviders(nil, []*managedProvider{
		{Provider: scrapeProvider, semaphore: make(chan struct{}, 1), roles: normalizeProviderRoles([]string{"scrape"})},
		{Provider: yencProvider, semaphore: make(chan struct{}, 1), roles: normalizeProviderRoles([]string{"yenc_recovery"})},
	}, ManagerOptions{CapacityPolicy: CapacityReturnBusy})

	stats, providerID, err := manager.groupStatsForScopeWithProvider(context.Background(), "scrape", CapacityReturnBusy, "alt.binaries.test")
	if err != nil {
		t.Fatalf("scrape group stats failed: %v", err)
	}
	if providerID != "scrape-provider" || stats.Group != "alt.binaries.test" {
		t.Fatalf("expected scrape provider, got id=%q stats=%+v", providerID, stats)
	}

	prefix, err := manager.FetchBodyPrefixForScope(context.Background(), "recover_yenc", "msg@example", nil, 128)
	if err != nil {
		t.Fatalf("recover_yenc prefix failed: %v", err)
	}
	if string(prefix) != "yenc" || scrapeProvider.prefixCalls != 0 || yencProvider.prefixCalls != 1 {
		t.Fatalf("expected yenc provider only, prefix=%q scrape_calls=%d yenc_calls=%d", string(prefix), scrapeProvider.prefixCalls, yencProvider.prefixCalls)
	}

	if _, err := manager.FetchBodyPrefixForScope(context.Background(), "inspect_par2", "msg@example", nil, 128); err != ErrProviderBusy {
		t.Fatalf("expected no inspection provider to be busy/config failure, got %v", err)
	}
}

func TestManagerKeepsProviderHeadroomBeforeUsingLowerPriorityProvider(t *testing.T) {
	primary := &roleTestProvider{id: "primary", priority: 1, prefix: []byte("primary")}
	secondary := &roleTestProvider{id: "secondary", priority: 2, prefix: []byte("secondary")}
	primaryManaged := &managedProvider{
		Provider:  primary,
		semaphore: make(chan struct{}, 100),
		roles:     normalizeProviderRoles([]string{"yenc_recovery"}),
	}
	for i := 0; i < 95; i++ {
		primaryManaged.semaphore <- struct{}{}
	}
	secondaryManaged := &managedProvider{
		Provider:  secondary,
		semaphore: make(chan struct{}, 30),
		roles:     normalizeProviderRoles([]string{"yenc_recovery"}),
	}
	manager := newManagerWithProviders(nil, []*managedProvider{primaryManaged, secondaryManaged}, ManagerOptions{CapacityPolicy: CapacityReturnBusy})

	prefix, err := manager.FetchBodyPrefixForScope(context.Background(), "recover_yenc", "msg@example", nil, 128)
	if err != nil {
		t.Fatalf("fetch prefix: %v", err)
	}
	if string(prefix) != "secondary" {
		t.Fatalf("expected lower-priority provider at primary headroom, got %q", string(prefix))
	}
	if primary.prefixCalls != 0 || secondary.prefixCalls != 1 {
		t.Fatalf("expected secondary only, primary_calls=%d secondary_calls=%d", primary.prefixCalls, secondary.prefixCalls)
	}
}

func TestManagerBalancesRecoverYEncByProviderUtilization(t *testing.T) {
	primary := &roleTestProvider{id: "primary", priority: 1, prefix: []byte("primary")}
	secondary := &roleTestProvider{id: "secondary", priority: 2, prefix: []byte("secondary")}
	primaryManaged := &managedProvider{
		Provider:  primary,
		semaphore: make(chan struct{}, 100),
		roles:     normalizeProviderRoles([]string{"yenc_recovery"}),
	}
	for i := 0; i < 50; i++ {
		primaryManaged.semaphore <- struct{}{}
	}
	secondaryManaged := &managedProvider{
		Provider:  secondary,
		semaphore: make(chan struct{}, 30),
		roles:     normalizeProviderRoles([]string{"yenc_recovery"}),
	}
	manager := newManagerWithProviders(nil, []*managedProvider{primaryManaged, secondaryManaged}, ManagerOptions{CapacityPolicy: CapacityReturnBusy})

	prefix, err := manager.FetchBodyPrefixForScope(context.Background(), "recover_yenc", "msg@example", nil, 128)
	if err != nil {
		t.Fatalf("fetch prefix: %v", err)
	}
	if string(prefix) != "secondary" {
		t.Fatalf("expected less-utilized provider, got %q", string(prefix))
	}
	if primary.prefixCalls != 0 || secondary.prefixCalls != 1 {
		t.Fatalf("expected secondary only, primary_calls=%d secondary_calls=%d", primary.prefixCalls, secondary.prefixCalls)
	}
}

type blockingProvider struct {
	capacity int
	mu       sync.Mutex
	active   int
	max      int
}

type missingProvider struct {
	id string
}

type roleTestProvider struct {
	id          string
	priority    int
	prefix      []byte
	prefixCalls int
}

func (p *roleTestProvider) ID() string               { return p.id }
func (p *roleTestProvider) Label() string            { return p.id }
func (p *roleTestProvider) Priority() int            { return p.priority }
func (p *roleTestProvider) MaxConnection() int       { return 1 }
func (p *roleTestProvider) IdleConnectionCount() int { return 0 }
func (p *roleTestProvider) TestConnection() error    { return nil }
func (p *roleTestProvider) Close() error             { return nil }
func (p *roleTestProvider) StatsSnapshot() ProviderStatsSnapshot {
	return ProviderStatsSnapshot{}
}
func (p *roleTestProvider) Fetch(context.Context, string, []string) (io.Reader, error) {
	return strings.NewReader("body"), nil
}
func (p *roleTestProvider) FetchBodyPrefix(context.Context, string, []string, int64) ([]byte, error) {
	p.prefixCalls++
	return append([]byte(nil), p.prefix...), nil
}
func (p *roleTestProvider) GroupStats(_ context.Context, group string) (GroupStats, error) {
	return GroupStats{Group: group, Low: 1, High: 10, Count: 10}, nil
}
func (p *roleTestProvider) ListGroups(context.Context, string) ([]GroupListing, error) {
	return nil, nil
}
func (p *roleTestProvider) XOver(context.Context, string, int64, int64) ([]OverviewHeader, error) {
	return nil, nil
}

func (p *missingProvider) ID() string               { return p.id }
func (p *missingProvider) Label() string            { return p.id }
func (p *missingProvider) Priority() int            { return 0 }
func (p *missingProvider) MaxConnection() int       { return 1 }
func (p *missingProvider) IdleConnectionCount() int { return 0 }
func (p *missingProvider) TestConnection() error    { return nil }
func (p *missingProvider) Close() error             { return nil }
func (p *missingProvider) StatsSnapshot() ProviderStatsSnapshot {
	return ProviderStatsSnapshot{}
}

func (p *missingProvider) Fetch(context.Context, string, []string) (io.Reader, error) {
	return nil, ErrArticleNotFound
}

func (p *missingProvider) FetchBodyPrefix(context.Context, string, []string, int64) ([]byte, error) {
	return nil, ErrArticleNotFound
}

func (p *missingProvider) GroupStats(context.Context, string) (GroupStats, error) {
	return GroupStats{Low: 1, High: 1}, nil
}

func (p *missingProvider) ListGroups(context.Context, string) ([]GroupListing, error) {
	return nil, nil
}

func (p *missingProvider) XOver(context.Context, string, int64, int64) ([]OverviewHeader, error) {
	return nil, nil
}

func newBlockingProvider(capacity int) *blockingProvider {
	return &blockingProvider{capacity: capacity}
}

func (p *blockingProvider) ID() string               { return "test-provider" }
func (p *blockingProvider) Label() string            { return "test-provider" }
func (p *blockingProvider) Priority() int            { return 0 }
func (p *blockingProvider) MaxConnection() int       { return p.capacity }
func (p *blockingProvider) IdleConnectionCount() int { return 0 }
func (p *blockingProvider) TestConnection() error    { return nil }
func (p *blockingProvider) Close() error             { return nil }
func (p *blockingProvider) StatsSnapshot() ProviderStatsSnapshot {
	return ProviderStatsSnapshot{}
}

func (p *blockingProvider) Fetch(context.Context, string, []string) (io.Reader, error) {
	p.enter()
	return &trackedReader{
		Reader: strings.NewReader("body"),
		close:  p.leave,
	}, nil
}

func (p *blockingProvider) FetchBodyPrefix(context.Context, string, []string, int64) ([]byte, error) {
	p.enter()
	defer p.leave()
	return []byte("body"), nil
}

func (p *blockingProvider) GroupStats(context.Context, string) (GroupStats, error) {
	p.enter()
	defer p.leave()
	return GroupStats{Low: 1, High: 1}, nil
}

func (p *blockingProvider) ListGroups(context.Context, string) ([]GroupListing, error) {
	p.enter()
	defer p.leave()
	return []GroupListing{{Group: "alt.binaries.test", High: 1, Low: 1, Status: "y"}}, nil
}

func (p *blockingProvider) XOver(context.Context, string, int64, int64) ([]OverviewHeader, error) {
	p.enter()
	defer p.leave()
	return []OverviewHeader{{ArticleNumber: 1}}, nil
}

func (p *blockingProvider) enter() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active++
	if p.active > p.max {
		p.max = p.active
	}
}

func (p *blockingProvider) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active--
}

func (p *blockingProvider) maxObserved() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max
}

type trackedReader struct {
	io.Reader
	once  sync.Once
	close func()
}

func (r *trackedReader) Close() error {
	r.once.Do(func() {
		if r.close != nil {
			r.close()
		}
	})
	return nil
}
