package engine

import (
	"context"
	"errors"
	"sync"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/segmentio/ksuid"
)

type QueueManager struct {
	mu         sync.RWMutex
	downloader app.Downloader
	queue      []*domain.QueueItem
	activeItem *domain.QueueItem

	newJobChan chan struct{}
}

func NewQueueManager(d app.Downloader) *QueueManager {
	return &QueueManager{
		downloader: d,
	}
}

// Add creates a new domain.QueueItem and notifies the processor loop
func (m *QueueManager) Add(nzbModel *nzb.Model, filename string) (*domain.QueueItem, error) {
	item := &domain.QueueItem{
		ID:       ksuid.New().String(), // Simple UUID or timestamp
		Name:     filename,
		NZBModel: nzbModel,
		Status:   domain.StatusPending,
	}

	m.mu.Lock()
	m.queue = append(m.queue, item)
	m.mu.Unlock()

	// Signal the Start() loop that there is work to do
	select {
	case m.newJobChan <- struct{}{}:
	default:
		// Signal already pending, no need to block
	}

	return item, nil
}

func (m *QueueManager) Start(ctx context.Context) {
	for {
		var next *domain.QueueItem

		m.mu.RLock()
		for _, itm := range m.queue {
			if itm.Status == domain.StatusPending {
				next = itm
				break
			}
		}
		m.mu.RUnlock()

		if next == nil {
			select {
			case <-m.newJobChan:
				continue
			case <-ctx.Done():
				return
			}
		}

		m.mu.Lock()
		m.activeItem = next
		next.Status = domain.StatusDownloading

		jobCtx, cancel := context.WithCancel(ctx)
		next.CancelFunc = cancel
		m.mu.Unlock()

		// Run the engine
		err := m.downloader.Download(jobCtx, next)

		m.mu.Lock()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				next.Status = domain.StatusFailed
				next.Error = "Cancelled by user"
			} else {
				next.Status = domain.StatusFailed
				next.Error = err.Error()
			}
		} else {
			next.Status = domain.StatusCompleted
		}
		m.activeItem = nil
		if next.CancelFunc != nil {
			next.CancelFunc()
		}
		m.mu.Unlock()
	}
}

// GetActiveItem allows the UI to see what's currently running
func (m *QueueManager) GetActiveItem() *domain.QueueItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeItem
}

// GetItem searches the queue for a specific ID.
// Returns the item and 'true' if found, nil and 'false' otherwise.
func (m *QueueManager) GetItem(id string) (*domain.QueueItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, item := range m.queue {
		if item.ID == id {
			return item, true
		}
	}

	return nil, false
}

// GetAllItems returns a copy of the current queue slice.
func (m *QueueManager) GetAllItems() []*domain.QueueItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent the caller from modifying the internal slice
	items := make([]*domain.QueueItem, len(m.queue))
	copy(items, m.queue)
	return items
}

func (m *QueueManager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, item := range m.queue {
		if item.ID == id {
			// 1. If it's already finished, don't bother
			if item.Status == domain.StatusCompleted || item.Status == domain.StatusFailed {
				return false
			}

			// 2. Call the context cancel function
			if item.CancelFunc != nil {
				item.CancelFunc()
			}

			// 3. Mark it as failed/cancelled
			item.Status = domain.StatusFailed
			item.Error = "Cancelled by user"
			return true
		}
	}
	return false
}
