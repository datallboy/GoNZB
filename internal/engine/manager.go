package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/segmentio/ksuid"
)

type QueueManager struct {
	mu         sync.RWMutex
	downloader app.Downloader
	processor  app.Processor
	queue      []*domain.QueueItem
	activeItem *domain.QueueItem
	store      app.Store

	newJobChan chan struct{}
}

// Initializes a QueueManager
// Takes app.Context and loadExisting bool as parameters
// if loadExisting is true, will load pending items from the database
// if loadExisting is false, will skip the database lookup (for CLI mode)
func NewQueueManager(app *app.Context, loadExisting bool) *QueueManager {
	var active []*domain.QueueItem
	var err error

	if loadExisting {
		// Only get "active" queue items (not completed / failed)
		active, err = app.Store.GetActiveQueueItems()
		if err != nil {
			active = make([]*domain.QueueItem, 0)
		}
	}

	return &QueueManager{
		downloader: app.Downloader,
		processor:  app.Processor,
		queue:      active,
		store:      app.Store,
		newJobChan: make(chan struct{}, 1),
	}
}

// Add creates a new domain.QueueItem and notifies the processor loop
func (m *QueueManager) Add(nzbModel *nzb.Model, filename string) (*domain.QueueItem, error) {

	// PREPARE: Sanitize names and pre-allocate .part files
	tasks, err := m.processor.Prepare(nzbModel, filename)
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("all files in this NZB already exist in the output directory")
	}

	// Calculate total size of all tasks and the password for extraction
	var totalSize uint64
	var password string
	for _, t := range tasks {
		totalSize += uint64(t.Size)

		if password == "" && t.Password != "" {
			password = t.Password
		}
	}

	item := &domain.QueueItem{
		ID:         ksuid.New().String(),
		Name:       filename,
		Password:   password,
		Tasks:      tasks,
		Status:     domain.StatusPending,
		TotalBytes: totalSize,
	}

	// Save to database
	if err := m.store.SaveQueueItem(item); err != nil {
		return nil, fmt.Errorf("failed to save job to database: %w", err)
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
			if itm.Status == domain.StatusDownloading {
				itm.Status = domain.StatusPending
				next = itm
				break
			}

			if itm.Status == domain.StatusPending || itm.Status == domain.StatusProcessing {
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
		jobCtx, cancel := context.WithCancel(ctx)
		next.CancelFunc = cancel
		m.mu.Unlock()

		var jobErr error

		if next.Status == domain.StatusPending {
			m.updateStatus(next, domain.StatusDownloading)
			jobErr = m.downloader.Download(jobCtx, next)
		}

		if jobErr == nil && !isCancelled(jobCtx) {
			m.updateStatus(next, domain.StatusProcessing)
			jobErr = m.processor.PostProcess(jobCtx, next.Tasks)
		}

		m.finalizeJob(next, jobErr)
		cancel()
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

	// Get from live cache
	for _, item := range m.queue {
		if item.ID == id {
			return item, true
		}
	}

	// Get from DB as a fallback
	item, err := m.store.GetQueueItem(id)
	if err == nil && item != nil {
		return item, true
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

			return true
		}
	}
	return false
}

// updateStatus changes the status and saves to DB immediately
func (m *QueueManager) updateStatus(item *domain.QueueItem, status domain.JobStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.Status = status
	_ = m.store.SaveQueueItem(item)
}

func (m *QueueManager) finalizeJob(item *domain.QueueItem, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		item.Status = domain.StatusFailed
		if errors.Is(err, context.Canceled) {
			item.Error = "Cancelled by user"
		} else {
			item.Error = err.Error()
		}
	} else {
		item.Status = domain.StatusCompleted
		item.BytesWritten.Store(item.TotalBytes)
	}

	// Persist the final outcome
	_ = m.store.SaveQueueItem(item)

	m.activeItem = nil
	m.removeFromLiveQueue(item.ID)
}

// removeFromLiveQueue keeps the active slice small by removing finished items
func (m *QueueManager) removeFromLiveQueue(id string) {
	for i, itm := range m.queue {
		if itm.ID == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			break
		}
	}
}

// isCancelled is a small utility to check context state
func isCancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
