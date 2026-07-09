package gonzbnet

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const sourceName = "gonzbnet"

type Store interface {
	ListFederationSearchPoolsForPrincipal(ctx context.Context, userID string, roleIDs []string) ([]string, error)
	SearchFederatedReleaseCards(ctx context.Context, params pgindex.FederatedReleaseCardSearchParams) ([]pgindex.FederatedReleaseCardSummary, error)
}

type Source struct {
	store Store
}

func New(s Store) *Source {
	return &Source{store: s}
}

func (s *Source) Name() string {
	return sourceName
}

func (s *Source) Search(ctx context.Context, req aggregator.SearchRequest) ([]*domain.Release, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("gonzbnet source is not configured")
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || principal == nil || !principal.Has(auth.PermissionGoNZBNetSearch) {
		return []*domain.Release{}, nil
	}
	pools, err := s.store.ListFederationSearchPoolsForPrincipal(ctx, principal.UserID, principal.RoleIDs)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return []*domain.Release{}, nil
	}

	items, err := s.store.SearchFederatedReleaseCards(ctx, pgindex.FederatedReleaseCardSearchParams{
		Query:  strings.TrimSpace(req.Query),
		IMDBID: strings.TrimSpace(req.IMDbID),
		TVDBID: parseInt64(req.TVDBID),
		Pools:  pools,
		Limit:  100,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Release, 0, len(items))
	for _, item := range items {
		out = append(out, federatedReleaseToDomain(item))
	}
	return out, nil
}

func (s *Source) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || principal == nil || !principal.Has(auth.PermissionGoNZBNetGet) {
		return nil, fmt.Errorf("gonzbnet get permission is required")
	}
	return nil, fmt.Errorf("gonzbnet manifest resolution is not implemented yet")
}

func federatedReleaseToDomain(item pgindex.FederatedReleaseCardSummary) *domain.Release {
	publishDate := time.Now().UTC()
	if item.PostedAt != nil {
		publishDate = item.PostedAt.UTC()
	}
	category := firstNewznabCategory(item.NewznabCategories)
	if category == "" {
		category = strconv.Itoa(newsnab.OtherMisc)
	}
	return &domain.Release{
		ID:          domain.GenerateCompositeID(sourceName, item.ReleaseID),
		Title:       item.Title,
		GUID:        item.ReleaseID,
		Source:      sourceName,
		Size:        item.SizeBytes,
		PublishDate: publishDate,
		Category:    category,
	}
}

func firstNewznabCategory(categories []int) string {
	for _, category := range categories {
		if category > 0 {
			return strconv.Itoa(category)
		}
	}
	return ""
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
