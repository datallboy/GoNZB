package controllers

import (
	"net/http"
	"strings"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/labstack/echo/v5"
)

type AggregatorController struct {
	App *app.Context
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
	query := strings.TrimSpace(c.QueryParam("q"))
	if len(query) < 2 {
		return c.JSON(http.StatusOK, map[string]any{
			"items": []aggregatorReleaseSearchResponse{},
			"count": 0,
		})
	}

	results, err := ctrl.App.Aggregator.SearchAll(c.Request().Context(), query)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
