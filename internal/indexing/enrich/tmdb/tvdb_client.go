package tmdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type tvdbClient struct {
	baseURL    string
	apiKey     string
	pin        string
	httpClient *http.Client

	mu    sync.Mutex
	token string
}

func newTVDBClient(opts Options) *tvdbClient {
	return &tvdbClient{
		baseURL:    strings.TrimRight(opts.TVDBBaseURL, "/"),
		apiKey:     strings.TrimSpace(opts.TVDBAPIKey),
		pin:        strings.TrimSpace(opts.TVDBPIN),
		httpClient: &http.Client{Timeout: opts.HTTPTimeout},
	}
}

func (c *tvdbClient) SearchSeries(ctx context.Context, query string, year int) ([]externalMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("parse tvdb url: %w", err)
	}
	values := u.Query()
	values.Set("query", query)
	values.Set("type", "series")
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build tvdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tvdb request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tvdb response status %d", resp.StatusCode)
	}

	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode tvdb response: %w", err)
	}

	out := make([]externalMatch, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := int64FromAny(item["tvdb_id"])
		if id <= 0 {
			id = int64FromAny(item["id"])
		}
		title := stringFromAny(item["name"])
		if title == "" {
			title = stringFromAny(item["seriesName"])
		}
		originalTitle := stringFromAny(item["originalName"])
		if title == "" || id <= 0 {
			continue
		}
		itemYear := intFromAny(item["year"])
		out = append(out, externalMatch{
			Source:        "tvdb",
			ExternalID:    id,
			MediaType:     "tv",
			Title:         title,
			OriginalTitle: originalTitle,
			Year:          itemYear,
			Payload:       item,
		})
	}
	return out, nil
}

func (c *tvdbClient) getToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}

	body := map[string]any{"apikey": c.apiKey}
	if c.pin != "" {
		body["pin"] = c.pin
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal tvdb login body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build tvdb login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tvdb login request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("tvdb login status %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode tvdb login response: %w", err)
	}

	if token := stringFromAny(payload["token"]); token != "" {
		c.token = token
		return token, nil
	}
	if data, ok := payload["data"].(map[string]any); ok {
		if token := stringFromAny(data["token"]); token != "" {
			c.token = token
			return token, nil
		}
	}
	return "", fmt.Errorf("tvdb login token missing")
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func intFromAny(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		i, _ := value.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(value))
		return i
	default:
		return 0
	}
}

func int64FromAny(v any) int64 {
	switch value := v.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		i, _ := value.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return i
	default:
		return 0
	}
}

func parseYear(v string) int {
	if len(v) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(v[:4])
	return year
}
