package predb

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type clubRSSClient struct {
	feedURL    string
	httpClient *http.Client
}

func newClubRSSClient(opts Options) *clubRSSClient {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://predb.club/api/v1"
	}
	return &clubRSSClient{
		feedURL: baseURL + "/rss",
		httpClient: &http.Client{
			Timeout: opts.HTTPTimeout,
		},
	}
}

func (c *clubRSSClient) ProviderName() string { return "predb.club.rss" }

type predbRSSFeed struct {
	Channel predbRSSChannel `xml:"channel"`
}

type predbRSSChannel struct {
	Items []predbRSSItem `xml:"item"`
}

type predbRSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func (c *clubRSSClient) FetchRecent(ctx context.Context, limit int) ([]pgindex.PredbEntryRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build predb.club rss request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request predb.club rss: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("predb.club rss returned status %d", resp.StatusCode)
	}
	var feed predbRSSFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode predb.club rss: %w", err)
	}
	if limit <= 0 || limit > len(feed.Channel.Items) {
		limit = len(feed.Channel.Items)
	}
	out := make([]pgindex.PredbEntryRecord, 0, limit)
	for _, item := range feed.Channel.Items[:limit] {
		entry := pgindex.PredbEntryRecord{
			NormalizedTitle: normalizeFeedEntryTitle(item.Title),
			Title:           strings.TrimSpace(item.Title),
			Source:          "predb.club",
			URL:             strings.TrimSpace(item.Link),
			Payload: map[string]any{
				"description": strings.TrimSpace(item.Description),
			},
		}
		applyDescriptionMetadata(&entry, item.Description)
		if strings.TrimSpace(item.PubDate) != "" {
			if parsed, err := time.Parse(time.RFC1123Z, strings.TrimSpace(item.PubDate)); err == nil {
				t := parsed.UTC()
				entry.PostedAt = &t
			}
		}
		if entry.Title == "" {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func applyDescriptionMetadata(entry *pgindex.PredbEntryRecord, description string) {
	if entry == nil {
		return
	}
	parts := strings.Split(description, "|")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "Cat:"):
			entry.Category = strings.TrimSpace(strings.TrimPrefix(part, "Cat:"))
		case strings.HasPrefix(part, "Size:"):
			var size float64
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(part, "Size:")), "%f", &size); err == nil {
				entry.SizeKB = size * 1024
			}
		case strings.HasPrefix(part, "Files:"):
			var files int
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(part, "Files:")), "%d", &files); err == nil {
				entry.FileCount = files
			}
		case strings.HasPrefix(part, "Genre:"):
			entry.Genre = strings.TrimSpace(strings.TrimPrefix(part, "Genre:"))
		case strings.HasPrefix(part, "Year:"):
			if entry.Payload == nil {
				entry.Payload = map[string]any{}
			}
			entry.Payload["year"] = strings.TrimSpace(strings.TrimPrefix(part, "Year:"))
		case strings.HasPrefix(part, "URL:"):
			entry.URL = strings.TrimSpace(strings.TrimPrefix(part, "URL:"))
		case strings.HasPrefix(part, "ID:"):
			var id int64
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(part, "ID:")), "%d", &id); err == nil {
				entry.ExternalID = id
			}
		}
	}
}
