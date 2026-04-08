package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type tmdbClient struct {
	baseURL     string
	apiKey      string
	accessToken string
	httpClient  *http.Client
}

func newTMDBClient(opts Options) *tmdbClient {
	return &tmdbClient{
		baseURL:     strings.TrimRight(opts.TMDBBaseURL, "/"),
		apiKey:      strings.TrimSpace(opts.TMDBAPIKey),
		accessToken: strings.TrimSpace(opts.TMDBAccessToken),
		httpClient:  &http.Client{Timeout: opts.HTTPTimeout},
	}
}

func (c *tmdbClient) SearchMovie(ctx context.Context, query string, year int) ([]externalMatch, error) {
	return c.search(ctx, "/search/movie", query, year, "movie")
}

func (c *tmdbClient) SearchTV(ctx context.Context, query string, year int) ([]externalMatch, error) {
	return c.search(ctx, "/search/tv", query, year, "tv")
}

func (c *tmdbClient) search(ctx context.Context, path, query string, year int, mediaType string) ([]externalMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse tmdb url: %w", err)
	}
	values := u.Query()
	values.Set("query", query)
	if year > 0 {
		if mediaType == "movie" {
			values.Set("year", strconv.Itoa(year))
		} else {
			values.Set("first_air_date_year", strconv.Itoa(year))
		}
	}
	if c.accessToken == "" && c.apiKey != "" {
		values.Set("api_key", c.apiKey)
	}
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build tmdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tmdb request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tmdb response status %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			ID            int64  `json:"id"`
			Title         string `json:"title"`
			OriginalTitle string `json:"original_title"`
			ReleaseDate   string `json:"release_date"`
			Name          string `json:"name"`
			OriginalName  string `json:"original_name"`
			FirstAirDate  string `json:"first_air_date"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode tmdb response: %w", err)
	}

	out := make([]externalMatch, 0, len(payload.Results))
	for _, item := range payload.Results {
		title := strings.TrimSpace(item.Title)
		originalTitle := strings.TrimSpace(item.OriginalTitle)
		year := parseYear(item.ReleaseDate)
		if mediaType == "tv" {
			title = strings.TrimSpace(item.Name)
			originalTitle = strings.TrimSpace(item.OriginalName)
			year = parseYear(item.FirstAirDate)
		}
		if title == "" || item.ID <= 0 {
			continue
		}
		out = append(out, externalMatch{
			Source:        "tmdb",
			ExternalID:    item.ID,
			MediaType:     mediaType,
			Title:         title,
			OriginalTitle: originalTitle,
			Year:          year,
			Payload: map[string]any{
				"id":             item.ID,
				"title":          item.Title,
				"original_title": item.OriginalTitle,
				"release_date":   item.ReleaseDate,
				"name":           item.Name,
				"original_name":  item.OriginalName,
				"first_air_date": item.FirstAirDate,
			},
		})
	}
	return out, nil
}
