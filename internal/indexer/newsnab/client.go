package newsnab

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

type Client struct {
	BaseURL         string
	ApiPath         string
	APIKey          string
	name            string
	redirectAllowed bool
	httpClient      *http.Client
}

func New(name, baseURL, apiPath, apiKey string, redirect bool) *Client {
	return &Client{
		name:            name,
		BaseURL:         baseURL,
		ApiPath:         apiPath,
		APIKey:          apiKey,
		redirectAllowed: redirect,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Name() string { return c.name }

func (c *Client) Search(ctx context.Context, query string) ([]*domain.Release, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid indexer base URL %q: %w", c.BaseURL, err)
	}

	searchURL := base.ResolveReference(&url.URL{Path: c.ApiPath})
	params := searchURL.Query()
	params.Set("t", "search")
	params.Set("q", query)
	params.Set("apikey", c.APIKey)
	params.Set("o", "xml")
	searchURL.RawQuery = params.Encode()

	// 1. Perform HTTP GET
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
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
	downloadURL, err := url.Parse(res.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("invalid release download URL %q: %w", res.DownloadURL, err)
	}
	params := downloadURL.Query()
	params.Set("apikey", c.APIKey)
	downloadURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	req.Header.Set("User-Agent", "GoNZB/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("indexer returned status: %d", resp.StatusCode)
	}

	return resp.Body, nil
}
