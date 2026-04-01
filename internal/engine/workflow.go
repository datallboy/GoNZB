package engine

import (
	"context"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

type queueWorkflow struct {
	manager *QueueManager
}

func newQueueWorkflow(manager *QueueManager) *queueWorkflow {
	return &queueWorkflow{manager: manager}
}

func (w *queueWorkflow) Execute(ctx context.Context, item *domain.QueueItem) {
	if w == nil || w.manager == nil || item == nil {
		return
	}

	var jobErr error

	if jobErr = w.runPending(ctx, item); jobErr == nil {
		jobErr = w.runDownload(ctx, item)
	}
	if jobErr == nil {
		jobErr = w.runPostProcessing(ctx, item)
	}

	w.manager.finalizeJob(ctx, item, jobErr)
}

func (w *queueWorkflow) runPending(ctx context.Context, item *domain.QueueItem) error {
	if item.Status != domain.StatusPending {
		return nil
	}
	if len(item.Tasks) == 0 {
		w.manager.recordEvent(ctx, item.ID, "hydrate", "start", "Hydrating queue item")
		w.manager.logger.Debug("Hydrating job - id: %s name: %s", item.ID, releaseTitle(item))
		if err := w.manager.HydrateItem(ctx, item); err != nil {
			return err
		}
	}

	w.manager.recordEvent(ctx, item.ID, "hydrate", "ok", "Hydration complete")
	w.manager.UpdateStatus(ctx, item, domain.StatusDownloading)
	return nil
}

func (w *queueWorkflow) runDownload(ctx context.Context, item *domain.QueueItem) error {
	if isCancelled(ctx) || item.Status != domain.StatusDownloading {
		return nil
	}

	if w.manager.isDownloadAlreadyFinished(item) {
		w.manager.logger.Info("All files present on disk for: %s. Skipping download.", releaseTitle(item))
	} else {
		if err := w.manager.downloader.Download(ctx, item); err != nil {
			return err
		}
	}

	if !isCancelled(ctx) {
		w.manager.UpdateStatus(ctx, item, domain.StatusProcessing)
	}
	return nil
}

func (w *queueWorkflow) runPostProcessing(ctx context.Context, item *domain.QueueItem) error {
	if isCancelled(ctx) || item.Status != domain.StatusProcessing {
		return nil
	}

	if err := w.manager.processor.PostProcess(ctx, item, item.Tasks); err != nil {
		return err
	}

	item.UpdatedAt = time.Now().UTC()
	if err := w.manager.jobStore.SaveQueueItem(ctx, item); err != nil {
		w.manager.logger.Warn("Failed to persist queue item completed path for %s: %v", item.ID, err)
	}

	return nil
}
