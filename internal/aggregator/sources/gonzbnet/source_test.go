package gonzbnet

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestSearchRequiresGoNZBNetSearchPermission(t *testing.T) {
	store := &fakeStore{
		pools: []string{"pool.local"},
		cards: []pgindex.FederatedReleaseCardSummary{{ReleaseID: "rel_1", Title: "Example", SizeBytes: 100}},
	}
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID:      "user-1",
		RoleIDs:     []string{"viewer"},
		Permissions: map[string]struct{}{auth.PermissionAggregatorReleasesRead: {}},
	})

	results, err := New(store).Search(ctx, aggregator.SearchRequest{Type: aggregator.SearchTypeGeneric, Query: "example"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no federated results without permission")
	}
	if store.poolLookups != 0 || store.searches != 0 {
		t.Fatalf("source should not query pools/cards without permission")
	}
}

func TestSearchUsesPrincipalPoolAccess(t *testing.T) {
	posted := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		pools: []string{"pool.alpha"},
		cards: []pgindex.FederatedReleaseCardSummary{{
			ReleaseID:         "rel_1",
			Title:             "Example.Release.2026",
			NewznabCategories: []int{2040},
			SizeBytes:         1000,
			PostedAt:          &posted,
			PoolID:            "pool.alpha",
		}},
	}
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID: "user-1",
		RoleIDs: []string{
			"federated-viewer",
		},
		Permissions: map[string]struct{}{
			auth.PermissionAggregatorReleasesRead: {},
			auth.PermissionGoNZBNetSearch:         {},
		},
	})

	results, err := New(store).Search(ctx, aggregator.SearchRequest{Type: aggregator.SearchTypeGeneric, Query: "example"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one federated result, got %d", len(results))
	}
	if results[0].Source != sourceName || results[0].GUID != "rel_1" || results[0].ID == "" {
		t.Fatalf("unexpected release mapping: %+v", results[0])
	}
	if store.lastUserID != "user-1" || len(store.lastRoleIDs) != 1 || store.lastRoleIDs[0] != "federated-viewer" {
		t.Fatalf("principal access was not passed to store")
	}
	if len(store.lastParams.Pools) != 1 || store.lastParams.Pools[0] != "pool.alpha" {
		t.Fatalf("pool access was not applied: %+v", store.lastParams)
	}
}

func TestGetNZBRequiresResolveManifestPermission(t *testing.T) {
	resolver := &fakeResolver{}
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID: "user-1",
		Permissions: map[string]struct{}{
			auth.PermissionGoNZBNetGet: {},
		},
	})

	_, err := NewWithResolver(&fakeStore{}, resolver).GetNZB(ctx, &domain.Release{
		Source: sourceName,
		GUID:   "rel_1",
	})
	if err == nil || !strings.Contains(err.Error(), "resolve manifest permission") {
		t.Fatalf("expected resolve manifest permission denial, got %v", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver should not be called after permission denial")
	}
}

func TestGetNZBRequiresPoolGetAndResolveAccess(t *testing.T) {
	resolver := &fakeResolver{}
	store := &fakeStore{getAllowed: false}
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID:  "user-1",
		RoleIDs: []string{"federated-viewer"},
		Permissions: map[string]struct{}{
			auth.PermissionGoNZBNetGet:             {},
			auth.PermissionGoNZBNetResolveManifest: {},
		},
	})

	_, err := NewWithResolver(store, resolver).GetNZB(ctx, &domain.Release{
		Source: sourceName,
		GUID:   "rel_1",
	})
	if err == nil || !strings.Contains(err.Error(), "pool get and resolve access") {
		t.Fatalf("expected pool access denial, got %v", err)
	}
	if store.getChecks != 1 || resolver.calls != 0 {
		t.Fatalf("unexpected denied get path: checks=%d resolver=%d", store.getChecks, resolver.calls)
	}
}

func TestGetNZBResolvesForAuthorizedPoolPrincipal(t *testing.T) {
	resolver := &fakeResolver{}
	store := &fakeStore{getAllowed: true}
	ctx := auth.ContextWithPrincipal(context.Background(), &auth.Principal{
		UserID:  "user-1",
		RoleIDs: []string{"federated-viewer"},
		Permissions: map[string]struct{}{
			auth.PermissionGoNZBNetGet:             {},
			auth.PermissionGoNZBNetResolveManifest: {},
		},
	})

	reader, err := NewWithResolver(store, resolver).GetNZB(ctx, &domain.Release{
		Source: sourceName,
		GUID:   "rel_1",
	})
	if err != nil {
		t.Fatalf("get NZB: %v", err)
	}
	_ = reader.Close()
	if store.getChecks != 1 || store.lastReleaseID != "rel_1" || resolver.calls != 1 {
		t.Fatalf("unexpected authorized get path: checks=%d release=%q resolver=%d", store.getChecks, store.lastReleaseID, resolver.calls)
	}
}

type fakeStore struct {
	pools         []string
	cards         []pgindex.FederatedReleaseCardSummary
	poolLookups   int
	searches      int
	lastUserID    string
	lastRoleIDs   []string
	lastParams    pgindex.FederatedReleaseCardSearchParams
	getAllowed    bool
	getChecks     int
	lastReleaseID string
}

func (s *fakeStore) ListFederationSearchPoolsForPrincipal(_ context.Context, userID string, roleIDs []string) ([]string, error) {
	s.poolLookups++
	s.lastUserID = userID
	s.lastRoleIDs = append([]string(nil), roleIDs...)
	return append([]string(nil), s.pools...), nil
}

func (s *fakeStore) CanGetFederatedReleaseForPrincipal(_ context.Context, releaseID, userID string, roleIDs []string) (bool, error) {
	s.getChecks++
	s.lastReleaseID = releaseID
	s.lastUserID = userID
	s.lastRoleIDs = append([]string(nil), roleIDs...)
	return s.getAllowed, nil
}

func (s *fakeStore) SearchFederatedReleaseCards(_ context.Context, params pgindex.FederatedReleaseCardSearchParams) ([]pgindex.FederatedReleaseCardSummary, error) {
	s.searches++
	s.lastParams = params
	return append([]pgindex.FederatedReleaseCardSummary(nil), s.cards...), nil
}

type fakeResolver struct {
	calls int
}

func (r *fakeResolver) ResolveNZB(context.Context, string) (io.ReadCloser, error) {
	r.calls++
	return io.NopCloser(strings.NewReader("<nzb/>")), nil
}
