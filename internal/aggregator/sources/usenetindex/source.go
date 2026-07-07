package usenetindex

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/resolver"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const sourceName = "usenet_index"

type store interface {
	ListPublicIndexerReleases(ctx context.Context, params pgindex.PublicIndexerReleaseListParams) ([]pgindex.PublicIndexerReleaseSummary, int, error)
	GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error)
	ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]pgindex.CatalogReleaseFile, error)
	ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]pgindex.CatalogArticleRef, error)
	ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error)
	GetReleaseArchiveState(ctx context.Context, releaseID string) (*pgindex.ReleaseArchiveState, error)
}

type archiveStore interface {
	GetNZBReader(key string) (io.ReadCloser, error)
}

type Source struct {
	store    store
	settings app.SettingsAdmin
	resolver interface {
		GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error)
	}
}

func New(s store, settings app.SettingsAdmin, archive archiveStore) *Source {
	return &Source{
		store:    s,
		settings: settings,
		resolver: resolver.NewUsenetIndexResolver(s, archive),
	}
}

func (s *Source) Name() string {
	return sourceName
}

func (s *Source) Search(ctx context.Context, req aggregator.SearchRequest) ([]*domain.Release, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("usenet index source is not configured")
	}

	params := pgindex.PublicIndexerReleaseListParams{
		Query:       strings.TrimSpace(req.Query),
		Limit:       100,
		Sort:        "posted_at_desc",
		IMDBID:      req.IMDbID,
		TVDBID:      parseInt64(req.TVDBID),
		Season:      parseInt(req.Season),
		Episode:     parseInt(req.Episode),
		ReadyPolicy: s.releaseReadyPolicy(ctx),
	}

	switch req.Type {
	case aggregator.SearchTypeMovie:
		params.BrowseCategory = "movies"
	case aggregator.SearchTypeTV:
		params.BrowseCategory = "tv"
	}

	items, _, err := s.store.ListPublicIndexerReleases(ctx, params)
	if err != nil {
		return nil, err
	}

	out := make([]*domain.Release, 0, len(items))
	for _, item := range items {
		out = append(out, publicReleaseToDomain(item))
	}
	return out, nil
}

func (s *Source) releaseReadyPolicy(ctx context.Context) pgindex.ReleaseReadyPolicy {
	policy := pgindex.DefaultReleaseReadyPolicy()
	if s == nil || s.settings == nil {
		return policy
	}
	runtime, err := s.settings.Get(ctx)
	if err != nil || runtime == nil || runtime.Indexing == nil {
		return policy
	}
	release := runtime.Indexing.Release
	return pgindex.NormalizeReleaseReadyPolicy(pgindex.ReleaseReadyPolicy{
		MinMatchConfidence: release.PublicMinMatchConfidence,
		MinCompletionPct:   release.PublicMinCompletionPct,
		MinIdentityStatus:  release.PublicMinIdentityStatus,
		RequireInspection:  release.PublicRequireInspection,
		RequireEnrichment:  release.PublicRequireEnrichment,
		RequireClearTitle:  release.PublicRequireClearTitle,
	})
}

func (s *Source) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	if s == nil || s.resolver == nil {
		return nil, fmt.Errorf("usenet index source is not configured")
	}
	return s.resolver.GetNZB(ctx, rel)
}

func publicReleaseToDomain(item pgindex.PublicIndexerReleaseSummary) *domain.Release {
	publishDate := time.Now().UTC()
	if item.PostedAt != nil {
		publishDate = item.PostedAt.UTC()
	} else if item.AddedAt != nil {
		publishDate = item.AddedAt.UTC()
	}

	guid := strings.TrimSpace(item.GUID)
	if guid == "" {
		guid = item.ReleaseID
	}

	category := item.Category
	if item.CategoryID > 0 {
		category = strconv.Itoa(item.CategoryID)
	} else if category == "" {
		category = strconv.Itoa(newsnab.OtherMisc)
	}

	return &domain.Release{
		ID:          item.ReleaseID,
		Title:       item.Title,
		GUID:        guid,
		Source:      sourceName,
		Size:        item.SizeBytes,
		PublishDate: publishDate,
		Category:    category,
	}
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
