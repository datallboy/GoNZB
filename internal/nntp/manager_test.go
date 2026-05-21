package nntp

import (
	"context"
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

type blockingProvider struct {
	capacity int
	mu       sync.Mutex
	active   int
	max      int
}

func newBlockingProvider(capacity int) *blockingProvider {
	return &blockingProvider{capacity: capacity}
}

func (p *blockingProvider) ID() string            { return "test-provider" }
func (p *blockingProvider) Label() string         { return "test-provider" }
func (p *blockingProvider) Priority() int         { return 0 }
func (p *blockingProvider) MaxConnection() int    { return p.capacity }
func (p *blockingProvider) TestConnection() error { return nil }
func (p *blockingProvider) Close() error          { return nil }

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
