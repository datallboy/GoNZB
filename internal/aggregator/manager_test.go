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

func TestSearchAllAllowsCachedGoNZBNetResultsWithPermission(t *testing.T) {
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
	if len(results) != 1 || results[0].Source != gonzbnetSourceName {
		t.Fatalf("expected cached gonzbnet result, got %+v", results)
	}
}

type fakeManagerStore struct {
	searchResults []*domain.Release
}

func (s *fakeManagerStore) GetNZBReader(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *fakeManagerStore) SaveNZBAtomically(string, []byte) error {
	return nil
}

func (s *fakeManagerStore) Exists(string) bool {
	return false
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
