package controllers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type AggregatorController struct {
	Service aggregatorService
}

func NewAggregatorController(module app.AggregatorModule) *AggregatorController {
	return &AggregatorController{
		Service: newAggregatorService(module),
	}
}

type aggregatorReleaseSearchResponse struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Size          int64  `json:"size"`
	Category      string `json:"category"`
	Source        string `json:"source"`
	CachePresent  bool   `json:"cache_present"`
	CacheBlobSize int64  `json:"cache_blob_size"`
}

func (ctrl *AggregatorController) SearchReleases(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "aggregator runtime is unavailable")
	}

	query := queryParamTrimmed(c, "q")
	if len(query) < 2 {
		return c.JSON(http.StatusOK, map[string]any{
			"items": []aggregatorReleaseSearchResponse{},
			"count": 0,
		})
	}

	results, err := ctrl.Service.Search(c.Request().Context(), aggregatorSearchRequest{
		Type:  "search",
		Query: query,
	})
	if err != nil {
		return jsonError(c, aggregatorErrorStatus(err), err.Error())
	}

	items := make([]aggregatorReleaseSearchResponse, 0, len(results))
	for _, rel := range results {
		items = append(items, aggregatorReleaseSearchResponse{
			ID:            rel.ID,
			Title:         rel.Title,
			Size:          rel.Size,
			Category:      rel.Category,
			Source:        rel.Source,
			CachePresent:  rel.CachePresent,
			CacheBlobSize: rel.CacheBlobSize,
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

// ListCatalogReleases exposes the aggregator's merged sources through the
// viewer contract used by aggregator-only and all-in-one deployments.
func (ctrl *AggregatorController) ListCatalogReleases(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "aggregator runtime is unavailable")
	}
	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	params, err := parsePublicIndexerListParams(c, limit, offset)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	window := params.Offset + params.Limit + 1
	results, err := ctrl.Service.Search(c.Request().Context(), aggregatorSearchRequest{
		Type:       "search",
		Query:      params.Query,
		Categories: params.CategoryIDs,
		Limit:      window,
	})
	if err != nil {
		return jsonError(c, aggregatorErrorStatus(err), err.Error())
	}

	start := min(params.Offset, len(results))
	end := min(start+params.Limit, len(results))
	items := make([]pgindex.PublicIndexerReleaseSummary, 0, end-start)
	for _, release := range results[start:end] {
		if release != nil {
			items = append(items, catalogReleaseSummary(release))
		}
	}
	hasMore := end < len(results)
	total := end
	if hasMore {
		total++
	}
	return c.JSON(http.StatusOK, map[string]any{
		"items": items, "total": total, "count": len(items), "limit": params.Limit,
		"offset": params.Offset, "sort": params.Sort, "has_more": hasMore,
		"filters": map[string]any{
			"q": params.Query, "browse_category": params.BrowseCategory,
			"browse_subcategory": params.BrowseSubcategory,
		},
	})
}

func (ctrl *AggregatorController) GetCatalogRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "aggregator runtime is unavailable")
	}
	release, err := ctrl.Service.Lookup(c.Request().Context(), pathParamTrimmed(c, "id"))
	if err != nil {
		return jsonError(c, aggregatorErrorStatus(err), err.Error())
	}
	if release == nil {
		return jsonError(c, http.StatusNotFound, "release not found")
	}
	return c.JSON(http.StatusOK, pgindex.PublicIndexerReleaseDetail{
		Release:  catalogReleaseSummary(release),
		Files:    []pgindex.PublicIndexerReleaseFileSummary{},
		Media:    pgindex.PublicIndexerReleaseMediaSummary{},
		External: pgindex.PublicIndexerReleaseExternal{},
		Capabilities: pgindex.PublicIndexerReleaseCapabilities{
			CanSendToDownloadClient: false,
		},
	})
}

func catalogReleaseSummary(release *domain.Release) pgindex.PublicIndexerReleaseSummary {
	categoryID, _ := strconv.Atoi(strings.TrimSpace(release.Category))
	category := newsnab.DisplayName(categoryID)
	if categoryID <= 0 {
		category = strings.TrimSpace(release.Category)
	}
	var postedAt = &release.PublishDate
	if release.PublishDate.IsZero() {
		postedAt = nil
	}
	return pgindex.PublicIndexerReleaseSummary{
		ReleaseID: release.ID, GUID: release.GUID, SourceKind: release.Source,
		Title: release.Title, PostedAt: postedAt, SizeBytes: release.Size,
		CategoryID: categoryID, Category: category, CompletionPct: 100,
		AvailabilityScore: 1, AvailabilityTier: "available",
	}
}
