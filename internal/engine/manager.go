package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
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
	jobStore   app.JobStore
	resolver   app.ReleaseResolver
	logger     *logger.Logger
	config     *config.Config

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
		jobStore:   app.JobStore,
		resolver:   app.Resolver,
		logger:     app.Logger,
		config:     app.Config,
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

	err := m.jobStore.ResetStuckQueueItems(ctx,
		domain.StatusPending,
		domain.StatusDownloading,
		domain.StatusProcessing,
	)

	if err != nil {
		m.logger.Error("Failed to reset stuck items in DB: %v", err)
	}

	activeItems, err := m.jobStore.GetActiveQueueItems(ctx)
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
	release, err := m.resolver.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate release metadata for %s: %w", releaseID, err)
	}
	if release == nil {
		return nil, fmt.Errorf("release metadata missing for %s", releaseID)
	}

	item := &domain.QueueItem{
		ID:        ksuid.New().String(),
		ReleaseID: releaseID,
		Status:    domain.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Save to database
	if err := m.jobStore.SaveQueueItem(ctx, item); err != nil {
		return nil, fmt.Errorf("failed to save job to database: %w", err)
	}

	// Add to RAM queue
	m.mu.Lock()
	m.queue = append(m.queue, item)
	m.mu.Unlock()

	m.recordEvent(ctx, item.ID, "queue", string(domain.StatusPending), "Queued")

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
				m.recordEvent(jobCtx, next.ID, "hydrate", "start", "Hydrating queue item")
				m.logger.Debug("Hydrating job - id: %s name: %s", next.ID, releaseTitle(next))
				jobErr = m.HydrateItem(jobCtx, next)
			}

			if jobErr != nil {
				m.finalizeJob(jobCtx, next, jobErr)
				jobCancel()
				continue
			}
			m.recordEvent(jobCtx, next.ID, "hydrate", "ok", "Hydration complete")

			m.UpdateStatus(jobCtx, next, domain.StatusDownloading)
		}

		// DOWNLOAD STEP
		if jobErr == nil && !isCancelled(jobCtx) && next.Status == domain.StatusDownloading {

			if m.isDownloadAlreadyFinished(next) {
				m.logger.Info("All files present on disk for: %s. Skipping download.", releaseTitle(next))
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
	item, err := m.jobStore.GetQueueItem(ctx, id)
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
				m.recordEvent(context.Background(), item.ID, "queue", "cancel_requested", "Cancellation requested")
				return true
			}

			// Pending items may not have a cancel func yet. Mark terminal now.
			item.Status = domain.StatusFailed
			cancelErr := "Cancelled by user"
			item.Error = &cancelErr
			now := time.Now().UTC()
			if item.StartedAt.IsZero() {
				item.StartedAt = now
			}
			item.CompletedAt = now
			item.UpdatedAt = now
			item.DownloadedBytes = item.GetBytes()
			m.removeFromLiveQueue(item.ID)
			if err := m.jobStore.SaveQueueItem(context.Background(), item); err != nil {
				m.logger.Error("Failed to persist cancelled queue item %s: %v", item.ID, err)
			}
			m.recordEvent(context.Background(), item.ID, "queue", "cancelled", "Cancelled by user")

			return true
		}
	}
	return false
}

func (m *QueueManager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, item := range m.queue {
		if item.ID == id {
			// Do not allow deleting live items from in-memory queue.
			if item.Status != domain.StatusCompleted && item.Status != domain.StatusFailed {
				return false
			}
		}
	}

	rows, err := m.jobStore.DeleteQueueItems(context.Background(), []string{id})
	if err != nil {
		m.logger.Error("Failed to delete queue item %s: %v", id, err)
		return false
	}
	return rows > 0
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
		m.logger.Debug("QueueManager: Cancelling active job: %s", releaseTitle(m.activeItem))
		m.activeItem.CancelFunc()
	}
}

// updateStatus changes the status and saves to DB immediately
func (m *QueueManager) UpdateStatus(ctx context.Context, item *domain.QueueItem, status domain.JobStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()

	if status == domain.StatusDownloading {
		if item.StartedAt.IsZero() {
			item.StartedAt = now
		}
		if item.DownloadStartedAt.IsZero() {
			item.DownloadStartedAt = now
		}
	}

	if status == domain.StatusProcessing {
		if !item.DownloadStartedAt.IsZero() && item.DownloadSeconds == 0 {
			item.DownloadSeconds = int64(now.Sub(item.DownloadStartedAt).Seconds())
		}
		if item.ProcessingStartedAt.IsZero() {
			item.ProcessingStartedAt = now
		}
		item.DownloadedBytes = item.GetBytes()
		if item.DownloadSeconds > 0 {
			item.AvgBps = item.DownloadedBytes / item.DownloadSeconds
		}
	}

	item.Status = status
	item.UpdatedAt = now
	if err := m.jobStore.SaveQueueItem(ctx, item); err != nil {
		m.logger.Error("Failed to persist status %s for queue item %s: %v", status, item.ID, err)
	}
	m.recordEvent(ctx, item.ID, "queue", string(status), "Status updated")
}

func (m *QueueManager) finalizeJob(ctx context.Context, item *domain.QueueItem, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()

	if item.StartedAt.IsZero() {
		item.StartedAt = now
	}
	if item.CompletedAt.IsZero() {
		item.CompletedAt = now
	}
	if !item.DownloadStartedAt.IsZero() && item.DownloadSeconds == 0 {
		item.DownloadSeconds = int64(now.Sub(item.DownloadStartedAt).Seconds())
	}
	if !item.ProcessingStartedAt.IsZero() && item.PostProcessSeconds == 0 {
		item.PostProcessSeconds = int64(now.Sub(item.ProcessingStartedAt).Seconds())
	}
	item.DownloadedBytes = item.GetBytes()
	if item.DownloadSeconds > 0 {
		item.AvgBps = item.DownloadedBytes / item.DownloadSeconds
	}
	item.UpdatedAt = now

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
	}

	// Persist the final outcome
	if err := m.jobStore.SaveQueueItem(ctx, item); err != nil {
		m.logger.Error("Failed to persist final state for queue item %s: %v", item.ID, err)
	}

	if item.Status == domain.StatusCompleted {
		m.recordEvent(ctx, item.ID, "finalize", "completed", "Queue item completed")
	} else {
		m.recordEvent(ctx, item.ID, "finalize", "failed", "Queue item failed")
	}

	m.activeItem = nil
	m.removeFromLiveQueue(item.ID)
}

func (m *QueueManager) recordEvent(ctx context.Context, queueID, stage, status, message string) {
	ev := &domain.QueueItemEvent{
		QueueID:  queueID,
		Stage:    stage,
		Status:   status,
		Message:  message,
		MetaJSON: "",
	}
	if err := m.jobStore.SaveQueueEvent(ctx, ev); err != nil {
		m.logger.Debug("Failed to persist queue event for %s: %v", queueID, err)
	}
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
		rel, err := m.resolver.GetRelease(ctx, item.ReleaseID)
		if err != nil {
			return fmt.Errorf("could not find file metadata: %w", err)
		}
		if rel == nil {
			return fmt.Errorf("release metadata not found for %s", item.ReleaseID)
		}
		item.Release = rel
	}

	// 2. Fetch NZB from Blob jobStore
	reader, err := m.resolver.GetNZB(ctx, item.Release)
	if err != nil {
		return fmt.Errorf("nzb file missing from blob jobStore: %w", err)
	}
	defer reader.Close()

	// 3. Parse NZB and Prepare
	nzbModel, err := m.parser.Parse(reader)
	if err != nil {
		return fmt.Errorf("failed to parse cached nzb: %w", err)
	}

	prepRes, err := m.processor.Prepare(ctx, nzbModel, item.Release.Title)
	if err != nil {
		return fmt.Errorf("failed to prepare download: %w", err)
	}

	// 4. THE ENRICHMENT: Map Prep results to the Release
	m.mu.Lock()
	item.Tasks = prepRes.Tasks

	// Update Release metadata
	item.Release.Size = prepRes.TotalSize
	item.Release.Password = prepRes.Password

	// Use the first task's date as a reasonable release-level default
	if len(prepRes.Tasks) > 0 {
		item.Release.PublishDate = time.Unix(prepRes.Tasks[0].Date, 0)
	}
	m.mu.Unlock()

	// Update the Release record
	if err := m.resolver.UpsertReleases(ctx, []*domain.Release{item.Release}); err != nil {
		m.logger.Warn("failed to update release size: %v", err)
	}

	// Save the ReleaseFiles
	if err := m.jobStore.SaveReleaseFiles(ctx, item.ReleaseID, prepRes.Tasks); err != nil {
		m.logger.Warn("failed to save files to db: %v", err)
	}

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

func releaseTitle(item *domain.QueueItem) string {
	if item == nil {
		return "unknown"
	}
	if item.Release != nil && item.Release.Title != "" {
		return item.Release.Title
	}
	if item.ReleaseID != "" {
		return item.ReleaseID
	}
	return item.ID
}
