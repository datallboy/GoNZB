package controllers

import (
	"net/http"

	"github.com/datallboy/gonzb/internal/app"
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
