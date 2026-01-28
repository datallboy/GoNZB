package controllers

import (
	"fmt"
	"net/http"

	"github.com/datallboy/gonzb/internal/app"
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
		Server: ServerInfo{Version: "0.5", Title: "GoNZB"},
		Categories: []Category{
			{ID: 5000, Name: "TV", SubCats: []SubCat{{ID: 5040, Name: "TV/HD"}}},
			{ID: 2000, Name: "Movies", SubCats: []SubCat{{ID: 2040, Name: "Movies/HD"}}},
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

	baseURL := fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host)

	rss := NewznabRSS{
		Version: "2.0",
		Channel: Channel{
			Title:       "GoNZB Search Results",
			Description: "Aggregated NZB Search",
			Items:       make([]RSSItem, 0),
		},
	}

	for _, r := range results {
		// GUID: Stays as the unqiue identifier for the release
		// Link/Enclosure: Point to OUR proxy endpoint
		localDownloadURL := fmt.Sprintf("%s/nzb/%s", baseURL, r.ID)

		rss.Channel.Items = append(rss.Channel.Items, RSSItem{
			Title:    r.Title,
			GUID:     r.ID,
			Link:     localDownloadURL,
			Category: r.Category,
			PubDate:  r.PublishDate.Format(http.TimeFormat),
			Enclosure: Enclosure{
				URL:    localDownloadURL,
				Type:   "application/x-nzb",
				Length: r.Size,
			},
		})
	}

	return c.XML(http.StatusOK, rss)
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

	res, err := ctrl.App.Indexer.GetResultByID(id)
	if err != nil {
		return c.String(http.StatusNotFound, "NZB expired from search memory")
	}

	// Handle redirect mode
	if ctrl.App.Config.RedirectDownloads {
		return c.Redirect(http.StatusFound, res.DownloadURL)
	}

	// Handle proxy mode
	data, err := ctrl.App.Indexer.FetchNZB(c.Request().Context(), id)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s.nzb", id))
	return c.Blob(http.StatusOK, "application/x-nzb", data)
}
