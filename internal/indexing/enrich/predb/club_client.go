package predb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type clubClient struct {
	baseURL    string
	httpClient *http.Client
}

func newClubClient(opts Options) *clubClient {
	return &clubClient{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		httpClient: &http.Client{
			Timeout: opts.HTTPTimeout,
		},
	}
}

func (c *clubClient) ProviderName() string { return "predb.club" }

type clubResponse struct {
	Status  string           `json:"status"`
	Message string           `json:"message"`
	Data    clubResponseData `json:"data"`
}

type clubResponseData struct {
	Rows     []clubRow `json:"rows"`
	Offset   int       `json:"offset"`
	ReqCount int       `json:"reqCount"`
	Total    int       `json:"total"`
}

type clubRow struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Team  string  `json:"team"`
	Cat   string  `json:"cat"`
	Genre string  `json:"genre"`
	URL   string  `json:"url"`
	Size  float64 `json:"size"`
	Files int     `json:"files"`
	PreAt int64   `json:"preAt"`
	Snip  string  `json:"snip"`
}

func (c *clubClient) Search(ctx context.Context, query Query) ([]Match, error) {
	if c == nil || strings.TrimSpace(c.baseURL) == "" {
		return nil, nil
	}
	text := strings.TrimSpace(query.Text)
	if text == "" {
		return nil, nil
	}

	u, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return nil, fmt.Errorf("parse predb.club base url: %w", err)
	}
	values := u.Query()
	values.Set("q", text)
	values.Set("count", strconv.Itoa(25))
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build predb.club request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request predb.club: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("predb.club returned status %d", resp.StatusCode)
	}

	var decoded clubResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode predb.club response: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(decoded.Status)) != "success" {
		if decoded.Message == "" {
			decoded.Message = "unknown predb.club error"
		}
		return nil, fmt.Errorf("predb.club error: %s", decoded.Message)
	}

	out := make([]Match, 0, len(decoded.Data.Rows))
	for _, row := range decoded.Data.Rows {
		match, ok := c.toMatch(row)
		if !ok {
			continue
		}
		out = append(out, match)
	}
	return out, nil
}

func (c *clubClient) FetchPage(ctx context.Context, offset, limit int) ([]pgindex.PredbEntryRecord, bool, error) {
	if c == nil || strings.TrimSpace(c.baseURL) == "" {
		return nil, false, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	u, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return nil, false, fmt.Errorf("parse predb.club base url: %w", err)
	}
	values := u.Query()
	values.Set("count", strconv.Itoa(limit))
	values.Set("offset", strconv.Itoa(offset))
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, fmt.Errorf("build predb.club page request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("request predb.club page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("predb.club page returned status %d", resp.StatusCode)
	}

	var decoded clubResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, false, fmt.Errorf("decode predb.club page response: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(decoded.Status)) != "success" {
		if decoded.Message == "" {
			decoded.Message = "unknown predb.club error"
		}
		return nil, false, fmt.Errorf("predb.club error: %s", decoded.Message)
	}

	out := make([]pgindex.PredbEntryRecord, 0, len(decoded.Data.Rows))
	for _, row := range decoded.Data.Rows {
		entry, ok := c.toEntry(row)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	more := decoded.Data.Total > (decoded.Data.Offset + len(decoded.Data.Rows))
	return out, more, nil
}

func (c *clubClient) toMatch(row clubRow) (Match, bool) {
	if strings.TrimSpace(row.Name) == "" {
		return Match{}, false
	}
	var postedAt *time.Time
	if row.PreAt > 0 {
		t := time.Unix(row.PreAt, 0).UTC()
		postedAt = &t
	}
	return Match{
		ExternalID: row.ID,
		Title:      strings.TrimSpace(row.Name),
		Category:   strings.TrimSpace(row.Cat),
		Source:     c.ProviderName(),
		Team:       strings.TrimSpace(row.Team),
		Genre:      strings.TrimSpace(row.Genre),
		URL:        strings.TrimSpace(row.URL),
		SizeKB:     row.Size,
		FileCount:  row.Files,
		PostedAt:   postedAt,
		Payload: map[string]any{
			"team":   strings.TrimSpace(row.Team),
			"genre":  strings.TrimSpace(row.Genre),
			"url":    strings.TrimSpace(row.URL),
			"size":   row.Size,
			"files":  row.Files,
			"pre_at": row.PreAt,
		},
	}, true
}

func (c *clubClient) toEntry(row clubRow) (pgindex.PredbEntryRecord, bool) {
	match, ok := c.toMatch(row)
	if !ok {
		return pgindex.PredbEntryRecord{}, false
	}
	return pgindex.PredbEntryRecord{
		ExternalID:      match.ExternalID,
		NormalizedTitle: normalizeFeedEntryTitle(match.Title),
		Title:           match.Title,
		Category:        match.Category,
		Source:          match.Source,
		Team:            match.Team,
		Genre:           match.Genre,
		URL:             match.URL,
		SizeKB:          match.SizeKB,
		FileCount:       match.FileCount,
		PostedAt:        match.PostedAt,
		Payload:         match.Payload,
	}, true
}
