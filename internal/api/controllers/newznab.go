package controllers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexer"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/labstack/echo/v5"
)

type NewznabController struct {
	App *app.Context
}

// Handle is the main Newznab entry point
func (ctrl *NewznabController) Handle(c *echo.Context) error {
	t := c.QueryParam("t")

	switch t {
	case "caps":
		return ctrl.handleCaps(c)
	case "search", "tvsearch", "movie":
		return ctrl.handleSearch(c)
	case "get":
		return ctrl.HandleDownload(c)
	default:
		return c.String(http.StatusBadRequest, "Unknown type")
	}
}

// handleCaps returns the indexer capabilities (Categories, search params)
func (ctrl *NewznabController) handleCaps(c *echo.Context) error {
	// Prowlarr/Sonarr need this to know what categories you support
	caps := NewznabCaps{
		Server: ServerInfo{
			Version: "0.5",
			Title:   "GoNZB",
		},
		Limits: Limits{
			Max: 100,
		},
		Retention: Retention{
			Days: 5000,
		},
		Categories: []CapCategory{
			{
				ID:   2000,
				Name: "Movies",
				SubCats: []CapSubCat{
					{ID: 2030, Name: nzb.GetCategoryName("2030")},
					{ID: 2040, Name: nzb.GetCategoryName("2040")},
					{ID: 2045, Name: nzb.GetCategoryName("2045")},
				},
			},
			{
				ID:   5000,
				Name: "TV",
				SubCats: []CapSubCat{
					{ID: 5030, Name: nzb.GetCategoryName("5030")},
					{ID: 5040, Name: nzb.GetCategoryName("5040")},
					{ID: 5045, Name: nzb.GetCategoryName("5045")},
				},
			},
		},
	}
	return c.XML(http.StatusOK, caps)
}

// handleSearch triggers a search across all configured indexers
func (ctrl *NewznabController) handleSearch(c *echo.Context) error {
	query := c.QueryParam("q")

	results, err := ctrl.App.Indexer.SearchAll(c.Request().Context(), query)
	if err != nil {
		return c.XML(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	baseAddr := fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host)

	rssResp := buildRSSResponse(results, baseAddr)
	return c.XML(http.StatusOK, rssResp)
}

// handleDownload serves the actual NZB file from cache or source
func (ctrl *NewznabController) HandleDownload(c *echo.Context) error {
	id := c.Param("id")
	if id == "" {
		id = c.QueryParam("id")
	}

	if id == "" {
		return c.String(http.StatusBadRequest, "Missing ID")
	}

	res, err := ctrl.App.Indexer.GetResultByID(c.Request().Context(), id)
	if err != nil {
		return c.String(http.StatusNotFound, "NZB not found in database")
	}

	// Handle redirect mode
	if res.RedirectAllowed {
		// Send the user directly to NNTmux/Indexer
		return c.Redirect(http.StatusFound, res.DownloadURL)
	}

	// Handle proxy mode
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s.nzb", id))
	return ctrl.App.Indexer.FetchNZB(c.Request().Context(), id, c)
}

// buildRSSResponse maps internal SearchResults to the outgoing Newznab XML format
func buildRSSResponse(results []indexer.SearchResult, baseAddr string) NewznabRSS {
	items := make([]RSSItem, 0, len(results))

	for _, res := range results {
		downloadURL := fmt.Sprintf("%s/api?t=get&id=%s", baseAddr, res.ID)

		items = append(items, RSSItem{
			Title: res.Title,
			GUID: RSSGUID{
				Value:       res.ID,
				IsPermaLink: false,
			},
			Link:     downloadURL,
			Category: nzb.GetCategoryName(res.Category),
			PubDate:  res.PublishDate.Format(time.RFC1123Z),
			Enclosure: Enclosure{
				URL:    downloadURL,
				Length: res.Size,
				Type:   "application/x-nzb",
			},
			Attributes: []Attr{
				{Name: "category", Value: res.Category},
				{Name: "size", Value: fmt.Sprintf("%d", res.Size)},
				{Name: "guid", Value: res.ID},
			},
		})
	}

	return NewznabRSS{
		Version: "2.0",
		NS:      "http://www.newznab.com/DTD/2010/feeds/attributes/",
		Channel: Channel{
			Title:       "GoNZB",
			Description: "GoNZB Aggregated Feed",
			Link:        baseAddr,
			Items:       items,
			Response: Response{
				Offset: 0,
				Total:  len(items),
			},
		},
	}
}
