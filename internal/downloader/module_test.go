package downloader

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
)

func TestCommandsEnqueueByReleaseIDUsesResolverAndQueue(t *testing.T) {
	queue := &fakeQueueManager{
		addResult: &domain.QueueItem{ID: "queue-1"},
	}
	resolver := &fakeResolver{
		release: &domain.Release{
			ID:       "rel-1",
			GUID:     "guid-1",
			Title:    "Original Title",
			Source:   "aggregator",
			Category: "movies",
		},
	}

	module := NewModule(DependencyProvider{
		Queue:    func() app.QueueManager { return queue },
		Resolver: func() app.ReleaseResolver { return resolver },
	})

	item, err := module.Commands().EnqueueByReleaseID(context.Background(), "rel-1", "Override Title")
	if err != nil {
		t.Fatalf("EnqueueByReleaseID() error = %v", err)
	}

	if item == nil {
		t.Fatal("expected queue item")
	}
	if queue.lastAddRequest.SourceKind != "aggregator" {
		t.Fatalf("expected aggregator source kind, got %q", queue.lastAddRequest.SourceKind)
	}
	if queue.lastAddRequest.SourceReleaseID != "rel-1" {
		t.Fatalf("expected release id rel-1, got %q", queue.lastAddRequest.SourceReleaseID)
	}
	if queue.lastAddRequest.Release == nil || queue.lastAddRequest.Release.Title != "Override Title" {
		t.Fatalf("expected overridden release title, got %#v", queue.lastAddRequest.Release)
	}
	if resolver.lastSourceKind != "aggregator" {
		t.Fatalf("expected resolver source kind aggregator, got %q", resolver.lastSourceKind)
	}
}

func TestCommandsEnqueueNZBWithCategorySavesBlobAndQueuesManualRelease(t *testing.T) {
	queue := &fakeQueueManager{
		addResult: &domain.QueueItem{ID: "queue-2"},
	}
	blobStore := &fakeBlobStore{}

	module := NewModule(DependencyProvider{
		Queue:     func() app.QueueManager { return queue },
		BlobStore: func() app.BlobStore { return blobStore },
	})

	item, err := module.Commands().EnqueueNZBWithCategory(context.Background(), "upload.nzb", "tv", bytes.NewBufferString("sample-nzb"))
	if err != nil {
		t.Fatalf("EnqueueNZBWithCategory() error = %v", err)
	}

	if item == nil {
		t.Fatal("expected queue item")
	}
	if blobStore.savedKey == "" {
		t.Fatal("expected blob store write")
	}
	if queue.lastAddRequest.SourceKind != "manual" {
		t.Fatalf("expected manual source kind, got %q", queue.lastAddRequest.SourceKind)
	}
	if queue.lastAddRequest.Release == nil || queue.lastAddRequest.Release.Category != "tv" {
		t.Fatalf("expected manual release category tv, got %#v", queue.lastAddRequest.Release)
	}
	if queue.lastAddRequest.SourceReleaseID != blobStore.savedKey {
		t.Fatalf("expected source release id to match blob key, got %q vs %q", queue.lastAddRequest.SourceReleaseID, blobStore.savedKey)
	}
}

func TestQueriesListHistoryReversesAndPaginates(t *testing.T) {
	jobStore := &fakeJobStore{
		items: []*domain.QueueItem{
			{ID: "1", Status: domain.StatusCompleted},
			{ID: "2", Status: domain.StatusFailed},
			{ID: "3", Status: domain.StatusCompleted},
		},
	}

	module := NewModule(DependencyProvider{
		JobStore: func() app.JobStore { return jobStore },
	})

	items, total, err := module.Queries().ListHistory(context.Background(), "", 2, 0)
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "3" || items[1].ID != "2" {
		t.Fatalf("expected reverse order [3 2], got [%s %s]", items[0].ID, items[1].ID)
	}
}

type fakeQueueManager struct {
	addResult      *domain.QueueItem
	lastAddRequest app.QueueAddRequest
	items          map[string]*domain.QueueItem
	activeItem     *domain.QueueItem
	paused         bool
}

func (f *fakeQueueManager) Start(context.Context) {}

func (f *fakeQueueManager) Add(_ context.Context, req app.QueueAddRequest) (*domain.QueueItem, error) {
	f.lastAddRequest = req
	if f.items == nil {
		f.items = map[string]*domain.QueueItem{}
	}
	if f.addResult != nil {
		f.addResult.Release = req.Release
		f.items[f.addResult.ID] = f.addResult
	}
	return f.addResult, nil
}

func (f *fakeQueueManager) GetActiveItem() *domain.QueueItem { return f.activeItem }

func (f *fakeQueueManager) GetItem(_ context.Context, id string) (*domain.QueueItem, bool) {
	if f.items == nil {
		return nil, false
	}
	item, ok := f.items[id]
	return item, ok
}

func (f *fakeQueueManager) GetAllItems() []*domain.QueueItem {
	out := make([]*domain.QueueItem, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, item)
	}
	return out
}

func (f *fakeQueueManager) Cancel(string) bool { return true }
func (f *fakeQueueManager) Delete(string) bool { return true }
func (f *fakeQueueManager) Stop()              {}
func (f *fakeQueueManager) Pause() bool {
	f.paused = true
	return true
}
func (f *fakeQueueManager) Resume() bool {
	f.paused = false
	return true
}
func (f *fakeQueueManager) IsPaused() bool { return f.paused }
func (f *fakeQueueManager) HydrateItem(context.Context, *domain.QueueItem) error {
	return nil
}
func (f *fakeQueueManager) UpdateStatus(context.Context, *domain.QueueItem, domain.JobStatus) {}
func (f *fakeQueueManager) ReloadRuntime(*app.Context)                                        {}

type fakeResolver struct {
	release        *domain.Release
	lastSourceKind string
	lastReleaseID  string
}

func (f *fakeResolver) GetRelease(_ context.Context, sourceKind, sourceReleaseID string) (*domain.Release, error) {
	f.lastSourceKind = sourceKind
	f.lastReleaseID = sourceReleaseID
	return f.release, nil
}

func (f *fakeResolver) GetNZB(context.Context, string, *domain.Release) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

type fakeBlobStore struct {
	savedKey  string
	savedData []byte
}

func (f *fakeBlobStore) GetNZBReader(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (f *fakeBlobStore) CreateNZBWriter(string) (io.WriteCloser, error) {
	return nopWriteCloser{Writer: io.Discard}, nil
}
func (f *fakeBlobStore) SaveNZBAtomically(key string, data []byte) error {
	f.savedKey = key
	f.savedData = append([]byte(nil), data...)
	return nil
}
func (f *fakeBlobStore) Exists(string) bool { return false }

type fakeJobStore struct {
	items []*domain.QueueItem
}

func (f *fakeJobStore) SaveQueueItem(context.Context, *domain.QueueItem) error { return nil }
func (f *fakeJobStore) GetQueueItem(context.Context, string) (*domain.QueueItem, error) {
	return nil, nil
}
func (f *fakeJobStore) GetQueueItems(context.Context) ([]*domain.QueueItem, error) {
	return f.items, nil
}
func (f *fakeJobStore) GetActiveQueueItems(context.Context) ([]*domain.QueueItem, error) {
	return nil, nil
}
func (f *fakeJobStore) DeleteQueueItems(context.Context, []string) (int64, error) { return 0, nil }
func (f *fakeJobStore) ClearQueueHistory(context.Context, []domain.JobStatus) (int64, error) {
	return 0, nil
}
func (f *fakeJobStore) SaveQueueEvent(context.Context, *domain.QueueItemEvent) error { return nil }
func (f *fakeJobStore) GetQueueEvents(context.Context, string) ([]*domain.QueueItemEvent, error) {
	return nil, nil
}
func (f *fakeJobStore) ResetStuckQueueItems(context.Context, domain.JobStatus, ...domain.JobStatus) error {
	return nil
}
func (f *fakeJobStore) Ping(context.Context) error                 { return nil }
func (f *fakeJobStore) SchemaVersion(context.Context) (int, error) { return 1, nil }
func (f *fakeJobStore) ExpectedSchemaVersion() int                 { return 1 }
func (f *fakeJobStore) ValidateSchema(context.Context) error       { return nil }

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

var _ app.QueueManager = (*fakeQueueManager)(nil)
var _ app.ReleaseResolver = (*fakeResolver)(nil)
var _ app.BlobStore = (*fakeBlobStore)(nil)
var _ app.JobStore = (*fakeJobStore)(nil)
