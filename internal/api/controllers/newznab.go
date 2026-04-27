package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/labstack/echo/v5"
)

type NewznabController struct {
	Service aggregatorService
}

func NewNewznabController(module app.AggregatorModule) *NewznabController {
	return &NewznabController{
		Service: newAggregatorService(module),
	}
}

// Handle is the main Newznab entry point
func (ctrl *NewznabController) Handle(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return writeNewznabError(c, http.StatusNotFound, 100, "Newznab-compatible API is not enabled")
	}

	t := queryParamLower(c, "t")

	switch t {
	case "caps":
		return ctrl.handleCaps(c)
	case "search", "tvsearch", "movie":
		return ctrl.handleSearch(c)
	case "get":
		return ctrl.HandleDownload(c)
	default:
		return writeNewznabError(c, http.StatusBadRequest, 100, "unknown or missing t parameter")
	}
}

// handleCaps returns the indexer capabilities (categories + search capability block)
func (ctrl *NewznabController) handleCaps(c *echo.Context) error {
	baseAddr := fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host)

	caps := NewznabCaps{
		Server: ServerInfo{
			AppVersion: "0.6.0",
			Version:    "0.5",
			Title:      "GoNZB",
			Strapline:  "Unified Usenet platform",
			Email:      "",
			URL:        baseAddr,
			Image:      "",
		},
		Limits: Limits{
			Max:     100,
			Default: 100,
		},
		Registration: Registration{
			Available: "no",
			Open:      "no",
		},
		Searching: Searching{
			Search: SearchCapability{
				Available:       "yes",
				SupportedParams: "q",
			},
			TVSearch: SearchCapability{
				Available:       "yes",
				SupportedParams: "q,rid,tvdbid,imdbid,tvmazeid,season,ep",
			},
			Movie: SearchCapability{
				Available:       "yes",
				SupportedParams: "q,imdbid,genre",
			},
		},
		Categories: buildCapCategories(),
		Groups:     []CapGroup{},
		Genres:     []CapGenre{},
	}

	return c.XML(http.StatusOK, caps)
}

// handleSearch triggers a search across all configured indexers
func (ctrl *NewznabController) handleSearch(c *echo.Context) error {
	searchType := queryParamLower(c, "t")

	results, err := ctrl.Service.Search(c.Request().Context(), aggregatorSearchRequest{
		Type:     searchType,
		Query:    queryParamTrimmed(c, "q"),
		IMDbID:   queryParamTrimmed(c, "imdbid"),
		TVDBID:   queryParamTrimmed(c, "tvdbid"),
		TVMazeID: queryParamTrimmed(c, "tvmazeid"),
		RageID:   queryParamTrimmed(c, "rid"),
		Season:   queryParamTrimmed(c, "season"),
		Episode:  queryParamTrimmed(c, "ep"),
		Genre:    queryParamTrimmed(c, "genre"),
	})
	if err != nil {
		if aggregatorErrorStatus(err) == http.StatusServiceUnavailable {
			return writeNewznabError(c, http.StatusNotFound, 100, "Newznab-compatible API is not enabled")
		}
		return writeNewznabError(c, http.StatusInternalServerError, 300, "search failed")
	}

	baseAddr := fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host)

	apiKey := queryParamTrimmed(c, "apikey")
	if apiKey == "" {
		apiKey = c.Request().Header.Get("X-API-Key")
	}

	rssResp := buildRSSResponse(results, baseAddr, apiKey)
	return c.XML(http.StatusOK, rssResp)
}

// handleDownload serves the actual NZB file from cache or source
func (ctrl *NewznabController) HandleDownload(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return writeNewznabError(c, http.StatusNotFound, 100, "Newznab-compatible API is not enabled")
	}

	id := pathParamTrimmed(c, "id")
	if id == "" {
		id = queryParamTrimmed(c, "id")
	}
	if id == "" {
		return writeNewznabError(c, http.StatusBadRequest, 100, "missing id parameter")
	}

	result, err := ctrl.Service.PrepareDownload(c.Request().Context(), id)
	if err != nil {
		switch aggregatorErrorStatus(err) {
		case http.StatusServiceUnavailable:
			return writeNewznabError(c, http.StatusNotFound, 100, "Newznab-compatible API is not enabled")
		case http.StatusNotFound:
			return writeNewznabError(c, http.StatusNotFound, 200, "nzb not found")
		default:
			return writeNewznabError(c, http.StatusInternalServerError, 300, "failed to fetch nzb")
		}
	}

	if result.RedirectURL != "" {
		return c.Redirect(http.StatusFound, result.RedirectURL)
	}

	defer result.Reader.Close()

	filename := buildDownloadFilename(result.Release.Title, id)
	c.Response().Header().Set(echo.HeaderContentDisposition, contentDispositionFilename(filename))
	return c.Stream(http.StatusOK, "application/x-nzb", result.Reader)
}

// buildRSSResponse maps internal Releases to the outgoing Newznab XML format
func buildRSSResponse(results []*domain.Release, baseAddr, apiKey string) NewznabRSS {
	items := make([]RSSItem, 0, len(results))

	for _, res := range results {
		categoryAttr := strings.TrimSpace(res.Category)
		categoryLabel := categoryAttr
		if parsed, ok := newsnab.ParseID(categoryAttr); ok {
			categoryAttr = strconv.Itoa(parsed)
			categoryLabel = newsnab.DisplayName(parsed)
		} else if categoryAttr == "" {
			categoryAttr = strconv.Itoa(newsnab.OtherMisc)
			categoryLabel = newsnab.DisplayName(newsnab.OtherMisc)
		}
		downloadURL := fmt.Sprintf("%s/api?t=get&id=%s", baseAddr, res.ID)
		if apiKey != "" {
			downloadURL = fmt.Sprintf("%s&apikey=%s", downloadURL, url.QueryEscape(apiKey))
		}

		items = append(items, RSSItem{
			Title: res.Title,
			GUID: RSSGUID{
				Value:       res.ID,
				IsPermaLink: false,
			},
			Link:     downloadURL,
			Category: categoryLabel,
			PubDate:  res.PublishDate.Format(time.RFC1123Z),
			Enclosure: Enclosure{
				URL:    downloadURL,
				Length: res.Size,
				Type:   "application/x-nzb",
			},
			Attributes: []Attr{
				{Name: "category", Value: categoryAttr},
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

func buildCapCategories() []CapCategory {
	roots := newsnab.Roots()
	out := make([]CapCategory, 0, len(roots))
	for _, root := range roots {
		item := CapCategory{
			ID:      root.ID,
			Name:    root.Name,
			SubCats: make([]CapSubCat, 0, len(root.Subcategories)),
		}
		for _, sub := range root.Subcategories {
			item.SubCats = append(item.SubCats, CapSubCat{
				ID:          sub.ID,
				Name:        sub.Name,
				Description: sub.Description,
			})
		}
		out = append(out, item)
	}
	return out
}
