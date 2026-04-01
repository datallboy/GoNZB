package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

type Commands struct {
	provider DependencyProvider
}

func NewCommands(provider DependencyProvider) *Commands {
	return &Commands{provider: provider}
}

func (c *Commands) EnqueueByReleaseID(ctx context.Context, releaseID, title string) (*domain.QueueItem, error) {
	if releaseID == "" {
		return nil, fmt.Errorf("release_id is required")
	}

	resolver := c.provider.Resolver()
	if resolver == nil {
		return nil, fmt.Errorf("release resolver is unavailable")
	}

	queue := c.provider.Queue()
	if queue == nil {
		return nil, fmt.Errorf("downloader queue is unavailable")
	}

	rel, err := resolver.GetRelease(ctx, "aggregator", releaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve aggregator release %s: %w", releaseID, err)
	}
	if rel == nil {
		return nil, fmt.Errorf("release %s not found", releaseID)
	}

	releaseCopy := *rel
	if title != "" {
		releaseCopy.Title = title
	}

	item, err := queue.Add(ctx, app.QueueAddRequest{
		SourceKind:      "aggregator",
		SourceReleaseID: releaseCopy.ID,
		Release:         &releaseCopy,
		Title:           releaseCopy.Title,
	})
	if err != nil {
		return nil, err
	}

	if item.Release == nil {
		item.Release = &releaseCopy
	}

	return item, nil
}

func (c *Commands) EnqueueNZB(ctx context.Context, filename string, file io.Reader) (*domain.QueueItem, error) {
	return c.EnqueueNZBWithCategory(ctx, filename, "", file)
}

func (c *Commands) EnqueueNZBWithCategory(ctx context.Context, filename, category string, file io.Reader) (*domain.QueueItem, error) {
	if filename == "" {
		filename = "manual.nzb"
	}

	queue := c.provider.Queue()
	if queue == nil {
		return nil, fmt.Errorf("downloader queue is unavailable")
	}

	blobStore := c.provider.BlobStore()
	if blobStore == nil {
		return nil, fmt.Errorf("blob store is unavailable")
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded nzb: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("uploaded nzb is empty")
	}

	releaseID, err := domain.CalculateFileHash(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to hash uploaded nzb: %w", err)
	}

	if err := blobStore.SaveNZBAtomically(releaseID, data); err != nil {
		return nil, fmt.Errorf("failed to persist nzb in blob store: %w", err)
	}

	manualRelease := &domain.Release{
		ID:       releaseID,
		GUID:     releaseID,
		Title:    filename,
		Source:   "manual",
		Category: normalizeQueueCategory(category),
	}

	item, err := queue.Add(ctx, app.QueueAddRequest{
		SourceKind:      "manual",
		SourceReleaseID: releaseID,
		Release:         manualRelease,
		Title:           filename,
	})
	if err != nil {
		return nil, err
	}

	if item.Release == nil {
		item.Release = manualRelease
	}

	return item, nil
}

func (c *Commands) Cancel(id string) bool {
	queue := c.provider.Queue()
	if queue == nil {
		return false
	}
	return queue.Cancel(id)
}

func (c *Commands) CancelMany(ids []string) int {
	queue := c.provider.Queue()
	if queue == nil {
		return 0
	}

	cancelled := 0
	for _, id := range ids {
		if queue.Cancel(id) {
			cancelled++
		}
	}
	return cancelled
}

func (c *Commands) DeleteMany(ctx context.Context, ids []string) (int64, error) {
	queue := c.provider.Queue()
	jobStore := c.provider.JobStore()
	if queue == nil || jobStore == nil {
		return 0, fmt.Errorf("downloader stores are unavailable")
	}

	terminal := make([]string, 0, len(ids))
	for _, id := range ids {
		item, ok := queue.GetItem(ctx, id)
		if !ok || item == nil {
			continue
		}
		if item.Status == domain.StatusCompleted || item.Status == domain.StatusFailed {
			terminal = append(terminal, id)
		}
	}

	return jobStore.DeleteQueueItems(ctx, terminal)
}

func (c *Commands) ClearHistory(ctx context.Context) (int64, error) {
	jobStore := c.provider.JobStore()
	if jobStore == nil {
		return 0, fmt.Errorf("job store is unavailable")
	}
	return jobStore.ClearQueueHistory(ctx, []domain.JobStatus{domain.StatusCompleted, domain.StatusFailed})
}

func (c *Commands) Pause() bool {
	queue := c.provider.Queue()
	if queue == nil {
		return false
	}
	return queue.Pause()
}

func (c *Commands) Resume() bool {
	queue := c.provider.Queue()
	if queue == nil {
		return false
	}
	return queue.Resume()
}

func normalizeQueueCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "*"
	}
	return category
}
