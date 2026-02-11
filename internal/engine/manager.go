package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/segmentio/ksuid"
)

type QueueManager struct {
	mu         sync.RWMutex
	downloader app.Downloader
	processor  app.Processor
	queue      []*domain.QueueItem
	parser     app.NZBParser
	activeItem *domain.QueueItem
	store      app.Store
	indexer    app.IndexerManager
	logger     *logger.Logger

	stopFunc   context.CancelFunc
	newJobChan chan struct{}
}

// Initializes a QueueManager
// Takes app.Context and loadExisting bool as parameters
// if loadExisting is true, will load pending items from the database
// if loadExisting is false, will skip the database lookup (for CLI mode)
func NewQueueManager(app *app.Context, loadExisting bool) *QueueManager {
	m := &QueueManager{
		downloader: app.Downloader,
		processor:  app.Processor,
		parser:     app.NZBParser,
		store:      app.Store,
		indexer:    app.Indexer,
		logger:     app.Logger,
		newJobChan: make(chan struct{}, 1),
		queue:      make([]*domain.QueueItem, 0),
	}

	if loadExisting {
		m.initFromDatabase()
	}

	return m
}

func (m *QueueManager) initFromDatabase() {

	ctx := context.Background()

	err := m.store.ResetStuckQueueItems(ctx,
		domain.StatusPending,
		domain.StatusDownloading,
	)

	if err != nil {
		m.logger.Error("Failed to reset stuck items in DB: %v", err)
	}

	activeItems, err := m.store.GetActiveQueueItems(ctx)
	if err != nil {
		m.logger.Error("Failed to load queue from database: %v", err)
		return
	}

	m.mu.Lock()
	m.queue = activeItems
	m.mu.Unlock()

	m.logger.Info("Queue initialized with %d items", len(m.queue))
}

// Add creates a new domain.QueueItem and notifies the processor loop
func (m *QueueManager) Add(ctx context.Context, releaseID string, title string) (*domain.QueueItem, error) {

	item := &domain.QueueItem{
		ID:        ksuid.New().String(),
		ReleaseID: releaseID,
		Status:    domain.StatusPending,
	}

	// Save to database
	if err := m.store.SaveQueueItem(ctx, item); err != nil {
		return nil, fmt.Errorf("failed to save job to database: %w", err)
	}

	// Add to RAM queue
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

	loopCtx, loopCancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.stopFunc = loopCancel
	m.mu.Unlock()

	for {
		var next *domain.QueueItem

		m.mu.RLock()
		for _, itm := range m.queue {
			if itm.Status == domain.StatusPending || itm.Status == domain.StatusDownloading || itm.Status == domain.StatusProcessing {
				next = itm
				break
			}
		}
		m.mu.RUnlock()

		if next == nil {
			select {
			case <-m.newJobChan:
				continue
			case <-loopCtx.Done():
				return
			}
		}

		if isCancelled(loopCtx) {
			return
		}

		m.mu.Lock()
		m.activeItem = next
		jobCtx, jobCancel := context.WithCancel(loopCtx)
		next.CancelFunc = jobCancel
		m.mu.Unlock()

		var jobErr error

		// HYDRATION STEP
		if next.Status == domain.StatusPending {
			if len(next.Tasks) == 0 {
				m.logger.Debug("Hydrating job - id: %s name: %s", next.ID, next.Release.Title)
				jobErr = m.HydrateItem(jobCtx, next)
			}

			if jobErr != nil {
				m.finalizeJob(jobCtx, next, jobErr)
				jobCancel()
				continue
			}

			m.UpdateStatus(jobCtx, next, domain.StatusDownloading)
		}

		// DOWNLOAD STEP
		if jobErr == nil && !isCancelled(jobCtx) && next.Status == domain.StatusDownloading {

			if m.isDownloadAlreadyFinished(next) {
				m.logger.Info("All files present on disk for: %s. Skipping download.", next.Release.Title)
			} else {
				jobErr = m.downloader.Download(jobCtx, next)
			}

			if jobErr == nil && !isCancelled(jobCtx) {
				m.UpdateStatus(jobCtx, next, domain.StatusProcessing)
			}
		}

		// POST-PROCESSING STEP
		if jobErr == nil && !isCancelled(jobCtx) && next.Status == domain.StatusProcessing {
			jobErr = m.processor.PostProcess(jobCtx, next.Tasks)
		}

		// FINALIZE
		m.finalizeJob(jobCtx, next, jobErr)
		jobCancel()

		m.mu.Lock()
		m.activeItem = nil
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
func (m *QueueManager) GetItem(ctx context.Context, id string) (*domain.QueueItem, bool) {
	m.mu.RLock()

	// Get from live cache
	for _, item := range m.queue {
		if item.ID == id {
			m.mu.RUnlock()
			return item, true
		}
	}
	m.mu.RUnlock()

	// Get from DB as a fallback
	item, err := m.store.GetQueueItem(ctx, id)
	if err != nil {
		m.logger.Debug("DB Fallback for %s failed: %v", id, err)
		return nil, false
	}

	return item, item != nil
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

func (m *QueueManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Warn("QueueManager: Shutdown requested...")

	// 1. Stop the manager loop (prevents next = itm from running again)
	if m.stopFunc != nil {
		m.stopFunc()
	}

	// 2. Kill the currently active task (Hydrate, Download, or PostProcess)
	if m.activeItem != nil && m.activeItem.CancelFunc != nil {
		m.logger.Debug("QueueManager: Cancelling active job: %s", m.activeItem.Release.Title)
		m.activeItem.CancelFunc()
	}
}

// updateStatus changes the status and saves to DB immediately
func (m *QueueManager) UpdateStatus(ctx context.Context, item *domain.QueueItem, status domain.JobStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.Status = status
	_ = m.store.SaveQueueItem(ctx, item)
}

func (m *QueueManager) finalizeJob(ctx context.Context, item *domain.QueueItem, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		item.Status = domain.StatusFailed
		var errorMsg string
		if errors.Is(err, context.Canceled) {
			errorMsg = "Cancelled by user"
		} else {
			errorMsg = err.Error()
		}
		item.Error = &errorMsg
	} else {
		item.Status = domain.StatusCompleted
		// This doesn't matter a whole lot since we're not updating the db
		item.BytesWritten.Store(item.Release.Size)
	}

	// Persist the final outcome
	_ = m.store.SaveQueueItem(ctx, item)

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

func (m *QueueManager) HydrateItem(ctx context.Context, item *domain.QueueItem) error {
	// 1. Fetch File Metadata from DB
	if item.Release == nil {
		rel, err := m.store.GetRelease(ctx, item.ReleaseID)
		if err != nil {
			return fmt.Errorf("could not find file metadata: %w", err)
		}
		item.Release = rel
	}

	// 2. Fetch NZB from Blob Store
	reader, err := m.indexer.GetNZB(ctx, item.Release)
	if err != nil {
		return fmt.Errorf("nzb file missing from blob store: %w", err)
	}
	defer reader.Close()

	nzbModel, err := m.parser.Parse(reader)
	if err != nil {
		return fmt.Errorf("failed to parse cached nzb: %w", err)
	}

	tasks, err := m.processor.Prepare(ctx, nzbModel, item.Release.Title)
	if err != nil {
		return fmt.Errorf("failed to prepare download: %w", err)
	}

	// Update the Release record (now with real size and potential poster)
	if err := m.store.UpsertReleases(ctx, []*domain.Release{item.Release}); err != nil {
		m.logger.Warn("failed to update release size: %v", err)
	}

	// Save the File Roadmap
	if err := m.store.SaveReleaseFiles(ctx, item.ReleaseID, tasks); err != nil {
		m.logger.Warn("failed to save files to db: %v", err)
	}

	m.mu.Lock()
	item.Tasks = tasks
	m.mu.Unlock()
	return nil
}

func (m *QueueManager) isDownloadAlreadyFinished(item *domain.QueueItem) bool {
	if len(item.Tasks) == 0 {
		return false
	}

	for _, task := range item.Tasks {
		if !task.IsComplete {
			return false // At least one file is missing
		}
	}
	return true
}
