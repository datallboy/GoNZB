package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/segmentio/ksuid"
)

type QueueManager struct {
	mu             sync.RWMutex
	downloader     app.Downloader
	processor      app.Processor
	queue          []*domain.QueueItem
	parser         app.NZBParser
	activeItem     *domain.QueueItem
	jobStore       app.JobStore
	queueFiles     app.QueueFileStore
	resolver       app.ReleaseResolver
	payloadFetcher app.PayloadFetcher
	arrNotifier    app.ArrNotifier
	logger         *logger.Logger
	config         *config.Config

	// global runtime pause state for SAB-compatible downloader API.
	paused           bool
	pauseRequestedID string

	stopFunc   context.CancelFunc
	newJobChan chan struct{}
}

// Initializes a QueueManager
// Takes app.Context and loadExisting bool as parameters
// if loadExisting is true, will load pending items from the database
// if loadExisting is false, will skip the database lookup (for CLI mode)
func NewQueueManager(app *app.Context, loadExisting bool) *QueueManager {
	m := &QueueManager{
		downloader:     app.Downloader,
		processor:      app.Processor,
		parser:         app.NZBParser,
		jobStore:       app.JobStore,
		queueFiles:     app.QueueFileStore,
		resolver:       app.Resolver,
		payloadFetcher: app.PayloadFetcher,
		arrNotifier:    app.ArrNotifier,
		logger:         app.Logger,
		config:         app.Config,
		newJobChan:     make(chan struct{}, 1),
		queue:          make([]*domain.QueueItem, 0),
	}

	if loadExisting {
		m.initFromDatabase()
	}

	return m
}

// refresh future-job runtime dependencies after settings reload
// Active downloads are not interrupted; this only affects subsequent hydation/download steps
func (m *QueueManager) ReloadRuntime(appCtx *app.Context) {
	if m == nil || appCtx == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.downloader = appCtx.Downloader
	m.processor = appCtx.Processor
	m.parser = appCtx.NZBParser
	m.resolver = appCtx.Resolver
	m.payloadFetcher = appCtx.PayloadFetcher
	m.arrNotifier = appCtx.ArrNotifier
	m.config = appCtx.Config
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

	if m.config != nil && !m.config.Store.PayloadCacheEnabled {
		survivors := make([]*domain.QueueItem, 0, len(activeItems))
		for _, item := range activeItems {
			if item.Resumable {
				survivors = append(survivors, item)
				continue
			}

			reason := "payload_not_persisted"
			now := time.Now().UTC()
			item.Status = domain.StatusFailed
			item.Error = &reason
			if item.StartedAt.IsZero() {
				item.StartedAt = now
			}
			item.CompletedAt = now
			item.UpdatedAt = now
			item.DownloadedBytes = item.GetBytes()

			if err := m.jobStore.SaveQueueItem(ctx, item); err != nil {
				m.logger.Error("Failed to persist non-resumable failure for %s: %v", item.ID, err)
			}
			m.recordEvent(ctx, item.ID, "hydrate", "failed", reason)
		}
		activeItems = survivors
	}

	m.mu.Lock()
	m.queue = activeItems
	m.mu.Unlock()

	m.logger.Info("Queue initialized with %d items", len(m.queue))
}

// Add creates a new queue item and persists explicit source provenance.
// resolver lookup is no longer done here; caller must provide source kind + snapshot.
func (m *QueueManager) Add(ctx context.Context, req app.QueueAddRequest) (*domain.QueueItem, error) {
	sourceKind := req.SourceKind
	sourceReleaseID := req.SourceReleaseID

	if sourceKind == "" {
		return nil, fmt.Errorf("source kind is required")
	}
	if sourceReleaseID == "" {
		return nil, fmt.Errorf("source release id is required")
	}

	release := req.Release
	if release == nil {
		release = &domain.Release{
			ID:       sourceReleaseID,
			GUID:     sourceReleaseID,
			Title:    req.Title,
			Source:   sourceKind,
			Category: "Uncategorized",
		}
	}

	if release.ID == "" {
		release.ID = sourceReleaseID
	}
	if release.GUID == "" {
		release.GUID = sourceReleaseID
	}
	if release.Title == "" {
		release.Title = req.Title
	}
	if release.Title == "" {
		release.Title = sourceReleaseID
	}
	if release.Source == "" {
		release.Source = sourceKind
	}
	if release.Category == "" {
		release.Category = "Uncategorized"
	}

	payloadMode := domain.PayloadModeCached
	resumable := true
	if m.config != nil && !m.config.Store.PayloadCacheEnabled {
		payloadMode = domain.PayloadModeEphemeral
		resumable = false
	}

	now := time.Now().UTC()
	itemID := ksuid.New().String()

	outDir := ""
	if m.config != nil && m.config.Download.OutDir != "" {
		outDir = buildQueueItemOutDir(m.config.Download.OutDir, release.Title, itemID)
	}

	item := &domain.QueueItem{
		ID:                  itemID,
		ReleaseID:           sourceReleaseID,
		Release:             release,
		Status:              domain.StatusPending,
		OutDir:              outDir, //  each job gets its own working dir
		SourceKind:          sourceKind,
		SourceReleaseID:     sourceReleaseID,
		ReleaseTitle:        release.Title,
		ReleaseSize:         release.Size,
		ReleaseSnapshotJSON: buildReleaseSnapshotJSON(release),
		PayloadMode:         payloadMode,
		Resumable:           resumable,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := m.jobStore.SaveQueueItem(ctx, item); err != nil {
		return nil, fmt.Errorf("failed to save job to database: %w", err)
	}

	m.mu.Lock()
	m.queue = append(m.queue, item)
	m.mu.Unlock()

	m.recordEvent(ctx, item.ID, "queue", string(domain.StatusPending), "Queued")

	select {
	case m.newJobChan <- struct{}{}:
	default:
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
		var paused bool

		m.mu.RLock()
		paused = m.paused
		if !paused {
			for _, itm := range m.queue {
				if itm.Status == domain.StatusPending || itm.Status == domain.StatusDownloading || itm.Status == domain.StatusProcessing {
					next = itm
					break
				}
			}
		}
		m.mu.RUnlock()

		if paused || next == nil {
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
			jobErr = m.processor.PostProcess(jobCtx, next, next.Tasks)

			// PostProcess may update next.OutDir to the final completed/import path.
			// Persist that before finalization so SAB history points Arr at the right folder.
			if jobErr == nil {
				next.UpdatedAt = time.Now().UTC()
				if persistErr := m.jobStore.SaveQueueItem(jobCtx, next); persistErr != nil {
					m.logger.Warn("Failed to persist queue item completed path for %s: %v", next.ID, persistErr)
				}
			}
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

// global pause stops new jobs from starting.
// If a job is active, we cooperatively cancel it and requeue it as pending.
func (m *QueueManager) Pause() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.paused {
		return true
	}

	m.paused = true

	if m.activeItem != nil && m.activeItem.CancelFunc != nil {
		m.pauseRequestedID = m.activeItem.ID
		m.activeItem.CancelFunc()
		m.recordEvent(context.Background(), m.activeItem.ID, "queue", "pause_requested", "Pause requested")
	}

	return true
}

// resume allows the queue loop to continue processing pending jobs.
func (m *QueueManager) Resume() bool {
	m.mu.Lock()
	wasPaused := m.paused
	m.paused = false
	m.mu.Unlock()

	if wasPaused {
		select {
		case m.newJobChan <- struct{}{}:
		default:
		}
	}

	return true
}

func (m *QueueManager) IsPaused() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused
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

	now := time.Now().UTC()

	// a paused active job is cooperatively requeued instead of failed.
	if errors.Is(err, context.Canceled) && m.pauseRequestedID == item.ID {
		item.Status = domain.StatusPending
		item.Error = nil
		item.UpdatedAt = now
		item.CompletedAt = time.Time{}
		item.CancelFunc = nil

		// Reset transient runtime timing markers; this item is returning to queue state.
		item.DownloadStartedAt = time.Time{}
		item.ProcessingStartedAt = time.Time{}
		item.DownloadSeconds = 0
		item.PostProcessSeconds = 0
		item.AvgBps = 0

		if persistErr := m.jobStore.SaveQueueItem(ctx, item); persistErr != nil {
			m.logger.Error("Failed to persist paused queue item %s: %v", item.ID, persistErr)
		}

		m.recordEvent(ctx, item.ID, "queue", "paused", "Queue item paused")
		m.pauseRequestedID = ""
		m.activeItem = nil
		m.mu.Unlock()
		return
	}

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
	item.CancelFunc = nil

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
		item.Error = nil
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

	notifier := m.arrNotifier
	m.mu.Unlock()

	// Arr refresh is best-effort and must not block terminal queue state.
	if notifier != nil {
		go func(item *domain.QueueItem) {
			notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if notifyErr := notifier.NotifyQueueTerminal(notifyCtx, item); notifyErr != nil {
				m.logger.Warn("Arr notifier failed for queue item %s: %v", item.ID, notifyErr)
			}
		}(item)
	}
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

	if item == nil {
		return fmt.Errorf("queue item is required")
	}
	if item.SourceKind == "" {
		return fmt.Errorf("queue item %s missing source kind", item.ID)
	}
	if item.SourceReleaseID == "" {
		item.SourceReleaseID = item.ReleaseID
	}

	// 1. Fetch File Metadata from DB
	if item.Release == nil {
		rel, err := m.resolver.GetRelease(ctx, item.SourceKind, item.SourceReleaseID)
		if err != nil {
			m.logger.Debug("resolver lookup failed for queue item %s (%s/%s): %v", item.ID, item.SourceKind, item.SourceReleaseID, err)
		}

		if rel == nil {
			// CHANGED: snapshot is the first fallback, not ad-hoc inference.
			rel = hydrateReleaseFromSnapshot(item)
		}

		if rel == nil {
			return fmt.Errorf("release metadata missing for queue item %s", item.ID)
		}

		if rel.ID == "" {
			rel.ID = item.SourceReleaseID
		}
		if rel.GUID == "" {
			rel.GUID = item.SourceReleaseID
		}
		if rel.Source == "" {
			rel.Source = item.SourceKind
		}
		if rel.Title == "" {
			rel.Title = item.ReleaseTitle
		}
		if rel.Title == "" {
			rel.Title = item.ID
		}
		if rel.Category == "" {
			rel.Category = "Uncategorized"
		}

		item.Release = rel
	}

	// 2. Fetch NZB using source-kind routing.
	reader, err := m.payloadFetcher.GetNZB(ctx, item.SourceKind, item.Release)
	if err != nil {
		return fmt.Errorf("failed to fetch nzb for queue item %s: %w", item.ID, err)
	}
	defer reader.Close()

	// 3. Parse NZB and prepare downloader tasks.
	nzbModel, err := m.parser.Parse(reader)
	if err != nil {
		return fmt.Errorf("failed to parse nzb payload: %w", err)
	}

	prepRes, err := m.processor.Prepare(ctx, item, nzbModel, item.Release.Title)
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

	// 5. Persist downloader-owned queue item files.
	if err := m.queueFiles.SaveQueueItemFiles(ctx, item.ID, prepRes.Tasks); err != nil {
		m.logger.Warn("failed to save queue files: %v", err)
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

// reconstruct release metadata from persisted queue snapshot.
// This keeps downloader hydration functional without a live aggregator/PG lookup.
func hydrateReleaseFromSnapshot(item *domain.QueueItem) *domain.Release {
	if item == nil {
		return nil
	}

	type releaseSnapshot struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		GUID            string `json:"guid"`
		Source          string `json:"source"`
		DownloadURL     string `json:"download_url"`
		Size            int64  `json:"size"`
		Category        string `json:"category"`
		RedirectAllowed bool   `json:"redirect_allowed"`
	}

	var snap releaseSnapshot
	if item.ReleaseSnapshotJSON != "" && item.ReleaseSnapshotJSON != "{}" {
		if err := json.Unmarshal([]byte(item.ReleaseSnapshotJSON), &snap); err == nil {
			rel := &domain.Release{
				ID:              snap.ID,
				Title:           snap.Title,
				GUID:            snap.GUID,
				Source:          snap.Source,
				DownloadURL:     snap.DownloadURL,
				Size:            snap.Size,
				Category:        snap.Category,
				RedirectAllowed: snap.RedirectAllowed,
			}
			if rel.ID == "" {
				rel.ID = item.SourceReleaseID
			}
			if rel.GUID == "" {
				rel.GUID = item.SourceReleaseID
			}
			if rel.Source == "" {
				rel.Source = item.SourceKind
			}
			if rel.Title == "" {
				rel.Title = item.ReleaseTitle
			}
			if rel.Category == "" {
				rel.Category = "Uncategorized"
			}
			return rel
		}
	}

	// Final fallback stays queue-owned and source-kind aware.
	rel := &domain.Release{
		ID:       item.SourceReleaseID,
		GUID:     item.SourceReleaseID,
		Title:    item.ReleaseTitle,
		Size:     item.ReleaseSize,
		Source:   item.SourceKind,
		Category: "Uncategorized",
	}
	if rel.ID == "" {
		rel.ID = item.ReleaseID
		rel.GUID = item.ReleaseID
	}
	if rel.Title == "" {
		rel.Title = item.ID
	}
	return rel
}

func buildReleaseSnapshotJSON(rel *domain.Release) string {
	if rel == nil {
		return "{}"
	}

	type releaseSnapshot struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		GUID            string `json:"guid"`
		Source          string `json:"source"`
		DownloadURL     string `json:"download_url"`
		Size            int64  `json:"size"`
		Category        string `json:"category"`
		RedirectAllowed bool   `json:"redirect_allowed"`
	}

	payload := releaseSnapshot{
		ID:              rel.ID,
		Title:           rel.Title,
		GUID:            rel.GUID,
		Source:          rel.Source,
		DownloadURL:     rel.DownloadURL,
		Size:            rel.Size,
		Category:        rel.Category,
		RedirectAllowed: rel.RedirectAllowed,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(b)
}

var queueDirUnsafeRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func buildQueueItemOutDir(baseDir, title, id string) string {
	name := sanitizeQueueDirComponent(title)
	if name == "" {
		name = "job"
	}
	if id != "" {
		name = name + "_" + id
	}
	return filepath.Join(baseDir, name)
}

func sanitizeQueueDirComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	value = queueDirUnsafeRE.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._- ")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}
