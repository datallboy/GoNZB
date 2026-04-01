package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/nzb"
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
		Categories: []CapCategory{
			{
				ID:   1000,
				Name: "Console",
				SubCats: []CapSubCat{
					{ID: 1010, Name: "NDS"},
					{ID: 1020, Name: "PSP"},
					{ID: 1030, Name: "Wii"},
					{ID: 1035, Name: "Switch"},
					{ID: 1040, Name: "Xbox"},
					{ID: 1050, Name: "Xbox 360"},
					{ID: 1080, Name: "PS3"},
					{ID: 1090, Name: "Xbox One"},
					{ID: 1100, Name: "PS4"},
				},
			},
			{
				ID:   2000,
				Name: "Movies",
				SubCats: []CapSubCat{
					{ID: 2060, Name: "3D"},
					{ID: 2050, Name: "BluRay"},
					{ID: 2010, Name: "Foreign"},
					{ID: 2040, Name: "HD"},
					{ID: 2020, Name: "Other"},
					{ID: 2030, Name: "SD"},
					{ID: 2045, Name: "UHD"},
				},
			},
			{
				ID:   3000,
				Name: "Audio",
				SubCats: []CapSubCat{
					{ID: 3030, Name: "Audiobook"},
					{ID: 3040, Name: "Lossless"},
					{ID: 3010, Name: "MP3"},
					{ID: 3050, Name: "Podcast"},
					{ID: 3020, Name: "Video"},
				},
			},
			{
				ID:   4000,
				Name: "PC",
				SubCats: []CapSubCat{
					{ID: 4010, Name: "0day"},
					{ID: 4080, Name: "3dModels", Description: "3dprint stls"},
					{ID: 4050, Name: "Games"},
					{ID: 4020, Name: "ISO"},
					{ID: 4030, Name: "Mac"},
					{ID: 4070, Name: "Mobile-Android"},
					{ID: 4040, Name: "Mobile-Other"},
					{ID: 4060, Name: "Mobile-iOS"},
				},
			},
			{
				ID:   5000,
				Name: "TV",
				SubCats: []CapSubCat{
					{ID: 5070, Name: "Anime"},
					{ID: 5080, Name: "Documentary"},
					{ID: 5020, Name: "Foreign"},
					{ID: 5040, Name: "HD"},
					{ID: 5050, Name: "Other"},
					{ID: 5030, Name: "SD"},
					{ID: 5060, Name: "Sport"},
					{ID: 5045, Name: "UHD"},
				},
			},
			{
				ID:   6000,
				Name: "XXX",
				SubCats: []CapSubCat{
					{ID: 6010, Name: "DVD"},
					{ID: 6040, Name: "HD"},
					{ID: 6060, Name: "ImgSet"},
					{ID: 6070, Name: "Other"},
					{ID: 6050, Name: "Pack"},
					{ID: 6030, Name: "SD"},
					{ID: 6045, Name: "UHD"},
					{ID: 6020, Name: "WMV"},
				},
			},
			{
				ID:   7000,
				Name: "Books",
				SubCats: []CapSubCat{
					{ID: 7030, Name: "Comics"},
					{ID: 7020, Name: "Ebook"},
					{ID: 7010, Name: "Mags"},
				},
			},
			{
				ID:   8000,
				Name: "Other",
				SubCats: []CapSubCat{
					{ID: 8010, Name: "Misc"},
				},
			},
		},
		Groups: []CapGroup{},
		Genres: []CapGenre{},
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
