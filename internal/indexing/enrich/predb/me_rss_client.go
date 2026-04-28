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

type meRSSClient struct {
	feedURL    string
	httpClient *http.Client
}

func newMeRSSClient(opts Options) *meRSSClient {
	return &meRSSClient{
		feedURL: strings.TrimSpace(opts.FeedURL),
		httpClient: &http.Client{
			Timeout: opts.HTTPTimeout,
		},
	}
}

func (c *meRSSClient) ProviderName() string { return "predb.me" }

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	GUID    string `xml:"guid"`
	PubDate string `xml:"pubDate"`
}

func (c *meRSSClient) fetchFeed(ctx context.Context) (*rssFeed, error) {
	if c == nil || c.feedURL == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build predb.me feed request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request predb.me feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("predb.me feed returned status %d", resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode predb.me feed: %w", err)
	}
	return &feed, nil
}

func (c *meRSSClient) Search(ctx context.Context, query Query) ([]Match, error) {
	feed, err := c.fetchFeed(ctx)
	if err != nil || feed == nil {
		return nil, err
	}

	out := make([]Match, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		if titleSimilarity(query.Title, title) < 0.45 && titleSimilarity(query.CanonicalTitle, title) < 0.45 {
			continue
		}
		var postedAt *time.Time
		if strings.TrimSpace(item.PubDate) != "" {
			if parsed, err := time.Parse(time.RFC1123Z, strings.TrimSpace(item.PubDate)); err == nil {
				t := parsed.UTC()
				postedAt = &t
			}
		}
		out = append(out, Match{
			Title:    title,
			Source:   c.ProviderName(),
			URL:      strings.TrimSpace(item.Link),
			PostedAt: postedAt,
			Payload: map[string]any{
				"guid": strings.TrimSpace(item.GUID),
			},
		})
	}
	return out, nil
}

func (c *meRSSClient) FetchRecent(ctx context.Context, limit int) ([]pgindex.PredbEntryRecord, error) {
	feed, err := c.fetchFeed(ctx)
	if err != nil || feed == nil {
		return nil, err
	}
	if limit <= 0 || limit > len(feed.Channel.Items) {
		limit = len(feed.Channel.Items)
	}
	out := make([]pgindex.PredbEntryRecord, 0, limit)
	for _, item := range feed.Channel.Items[:limit] {
		entry := pgindex.PredbEntryRecord{
			NormalizedTitle: normalizeFeedEntryTitle(item.Title),
			Title:           strings.TrimSpace(item.Title),
			Source:          "predb.me",
			URL:             strings.TrimSpace(item.Link),
			Payload: map[string]any{
				"guid": strings.TrimSpace(item.GUID),
			},
		}
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
