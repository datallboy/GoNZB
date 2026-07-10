package aggregator

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/domain"
)

func TestSearchAllFiltersCachedGoNZBNetResultsWithoutPermission(t *testing.T) {
	store := &fakeManagerStore{
		searchResults: []*domain.Release{
			{ID: "fed", Source: gonzbnetSourceName, GUID: "rel_fed", Title: "Federated"},
			{ID: "local", Source: "usenet_index", GUID: "rel_local", Title: "Local"},
		},
	}
	manager := NewManager(store, fakeLogger{}, false, true)
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID:      "user-1",
		Permissions: map[string]struct{}{auth.PermissionAggregatorReleasesRead: {}},
	})

	results, err := manager.SearchAllWithRequest(ctx, app.SearchRequest{Type: string(SearchTypeGeneric), Query: "release"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].Source == gonzbnetSourceName {
		t.Fatalf("expected cached gonzbnet result to be filtered, got %+v", results)
	}
}

func TestSearchAllNeverUsesPoollessCachedGoNZBNetResults(t *testing.T) {
	store := &fakeManagerStore{
		searchResults: []*domain.Release{
			{ID: "fed", Source: gonzbnetSourceName, GUID: "rel_fed", Title: "Federated"},
		},
	}
	manager := NewManager(store, fakeLogger{}, false, true)
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID: "user-1",
		Permissions: map[string]struct{}{
			auth.PermissionAggregatorReleasesRead: {},
			auth.PermissionGoNZBNetSearch:         {},
		},
	})

	results, err := manager.SearchAllWithRequest(ctx, app.SearchRequest{Type: string(SearchTypeGeneric), Query: "release"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected poolless cached gonzbnet result to be ignored, got %+v", results)
	}
}

func TestGetNZBDeniesGoNZBNetReleaseWithoutPermission(t *testing.T) {
	store := &fakeManagerStore{}
	manager := NewManager(store, fakeLogger{}, false, false)
	source := &fakeCatalogSource{name: gonzbnetSourceName}
	manager.AddSource(source)

	_, err := manager.GetNZB(context.Background(), &domain.Release{
		ID:     "gonzbnet:rel_fed",
		Source: gonzbnetSourceName,
		GUID:   "rel_fed",
		Title:  "Federated",
	})
	if err == nil {
		t.Fatal("expected gonzbnet get permission denial")
	}
	if source.gets != 0 {
		t.Fatalf("source should not be called after permission denial")
	}
}

func TestGetNZBAuthorizesGoNZBNetBeforeBlobCache(t *testing.T) {
	store := &fakeManagerStore{exists: true}
	manager := NewManager(store, fakeLogger{}, true, false)
	source := &fakeCatalogSource{name: gonzbnetSourceName, authorizeErr: io.EOF}
	manager.AddSource(source)
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		Permissions: map[string]struct{}{auth.PermissionGoNZBNetGet: {}},
	})

	_, err := manager.GetNZB(ctx, &domain.Release{
		ID:     "cached-federated-result",
		Source: gonzbnetSourceName,
		GUID:   "rel_fed",
	})
	if err == nil {
		t.Fatal("expected source authorization denial")
	}
	if source.authorizeCalls != 1 || store.cacheReads != 0 {
		t.Fatalf("authorization must run before cache read: auth=%d reads=%d", source.authorizeCalls, store.cacheReads)
	}
}

func TestGetNZBReturnsAuthorizedGoNZBNetBlobCache(t *testing.T) {
	store := &fakeManagerStore{exists: true}
	manager := NewManager(store, fakeLogger{}, true, false)
	source := &fakeCatalogSource{name: gonzbnetSourceName}
	manager.AddSource(source)
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		Permissions: map[string]struct{}{auth.PermissionGoNZBNetGet: {}},
	})

	reader, err := manager.GetNZB(ctx, &domain.Release{
		ID:     "cached-federated-result",
		Source: gonzbnetSourceName,
		GUID:   "rel_fed",
	})
	if err != nil {
		t.Fatalf("get cached NZB: %v", err)
	}
	_ = reader.Close()
	if source.authorizeCalls != 1 || source.gets != 0 || store.cacheReads != 1 {
		t.Fatalf("unexpected authorized cache path: auth=%d gets=%d reads=%d", source.authorizeCalls, source.gets, store.cacheReads)
	}
}

type fakeManagerStore struct {
	searchResults []*domain.Release
	exists        bool
	cacheReads    int
}

func (s *fakeManagerStore) GetNZBReader(string) (io.ReadCloser, error) {
	s.cacheReads++
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *fakeManagerStore) SaveNZBAtomically(string, []byte) error {
	return nil
}

func (s *fakeManagerStore) Exists(string) bool {
	return s.exists
}

func (s *fakeManagerStore) UpsertAggregatorReleaseCache(context.Context, []*domain.Release) error {
	return nil
}

func (s *fakeManagerStore) SearchAggregatorReleaseCache(context.Context, string, int) ([]*domain.Release, error) {
	return append([]*domain.Release(nil), s.searchResults...), nil
}

func (s *fakeManagerStore) GetAggregatorReleaseCacheByID(context.Context, string) (*domain.Release, error) {
	return nil, nil
}

type fakeLogger struct{}

func (fakeLogger) Debug(string, ...interface{}) {}
func (fakeLogger) Info(string, ...interface{})  {}
func (fakeLogger) Warn(string, ...interface{})  {}
func (fakeLogger) Error(string, ...interface{}) {}

type fakeCatalogSource struct {
	name           string
	gets           int
	authorizeCalls int
	authorizeErr   error
}

func (s *fakeCatalogSource) Name() string {
	return s.name
}

func (s *fakeCatalogSource) Search(context.Context, SearchRequest) ([]*domain.Release, error) {
	return nil, nil
}

func (s *fakeCatalogSource) GetNZB(context.Context, *domain.Release) (io.ReadCloser, error) {
	s.gets++
	return io.NopCloser(bytes.NewReader([]byte("<nzb/>"))), nil
}

func (s *fakeCatalogSource) AuthorizeGet(context.Context, *domain.Release) error {
	s.authorizeCalls++
	return s.authorizeErr
}
