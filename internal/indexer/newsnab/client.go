package newsnab

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/datallboy/gonzb/internal/domain"
)

type Client struct {
	BaseURL         string
	APIKey          string
	name            string
	redirectAllowed bool
}

func New(name, baseURL, apiKey string, redirect bool) *Client {
	return &Client{
		name:            name,
		BaseURL:         baseURL,
		APIKey:          apiKey,
		redirectAllowed: redirect,
	}
}

func (c *Client) Name() string { return c.name }

func (c *Client) Search(ctx context.Context, query string) ([]*domain.Release, error) {
	// Newsnab API search URL
	searchURL := fmt.Sprintf("%s/api?t=search&q=%s&apikey=%s&o=xml", c.BaseURL, query, c.APIKey)

	// 1. Perform HTTP GET
	req, _ := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer %s returned status: %d", c.name, resp.StatusCode)
	}

	// 2. Unmarshal XML into local structs
	var rss RSSResponse
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, err
	}
	// 3. Convert local structs to domain.Release
	results := make([]*domain.Release, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		res := item.ToRelease(c.name)
		res.RedirectAllowed = c.redirectAllowed
		results = append(results, res)
	}
	return results, nil
}

func (c *Client) DownloadNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error) {
	// Newznab uses t=getnzb and the id (guid) to fetch the file
	u := fmt.Sprintf("%s&apikey=%s", res.DownloadURL, c.APIKey)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)

	req.Header.Set("User-Agent", "GoNZB/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer returned status: %d", resp.StatusCode)
	}

	return resp.Body, nil
}
