package newsnab

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/datallboy/gonzb/internal/indexer"
)

type Client struct {
	BaseURL string
	APIKey  string
	name    string
}

func New(name, baseURL, apiKey string) *Client {
	return &Client{name: name, BaseURL: baseURL, APIKey: apiKey}
}

func (c *Client) Name() string { return c.name }

func (c *Client) Search(ctx context.Context, query string) ([]indexer.SearchResult, error) {
	// Newsnab API search URL
	searchURL := fmt.Sprintf("%s/api?t=search&q=%s&apikey=%s&o=xml", c.BaseURL, query, c.APIKey)

	// 1. Perform HTTP GET
	req, _ := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 2. Unmarshal XML into local structs
	var rss RSSResponse
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, err
	}
	// 3. Convert local structs to indexer.SearchResult
	results := make([]indexer.SearchResult, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		res := item.ToSearchResult(c.name)

		results = append(results, res)
	}
	return results, nil
}

func (c *Client) DownloadNZB(ctx context.Context, id string) ([]byte, error) {
	// Newznab uses t=getnzb and the id (guid) to fetch the file
	u := fmt.Sprintf("%s/api?t=getnzb&id=%s&apikey=%s", c.BaseURL, id, c.APIKey)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer returned status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
