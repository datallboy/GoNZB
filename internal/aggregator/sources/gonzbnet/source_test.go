package gonzbnet

import (
	"context"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/auth"
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

type fakeStore struct {
	pools       []string
	cards       []pgindex.FederatedReleaseCardSummary
	poolLookups int
	searches    int
	lastUserID  string
	lastRoleIDs []string
	lastParams  pgindex.FederatedReleaseCardSearchParams
}

func (s *fakeStore) ListFederationSearchPoolsForPrincipal(_ context.Context, userID string, roleIDs []string) ([]string, error) {
	s.poolLookups++
	s.lastUserID = userID
	s.lastRoleIDs = append([]string(nil), roleIDs...)
	return append([]string(nil), s.pools...), nil
}

func (s *fakeStore) SearchFederatedReleaseCards(_ context.Context, params pgindex.FederatedReleaseCardSearchParams) ([]pgindex.FederatedReleaseCardSummary, error) {
	s.searches++
	s.lastParams = params
	return append([]pgindex.FederatedReleaseCardSummary(nil), s.cards...), nil
}
