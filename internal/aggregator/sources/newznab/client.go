package newznab

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/datallboy/gonzb/internal/aggregator"
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

const maxSearchResponseBytes int64 = 16 << 20
const maxCapabilitiesResponseBytes int64 = 1 << 20

func New(name, baseURL, apiPath, apiKey string, redirect bool, policies ...OutboundPolicy) *Client {
	var policy OutboundPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	return &Client{
		name:            name,
		BaseURL:         baseURL,
		ApiPath:         apiPath,
		APIKey:          apiKey,
		redirectAllowed: redirect,
		httpClient:      newPolicyHTTPClient(policy),
	}
}

func (c *Client) Name() string { return c.name }

func (c *Client) TestConnection(ctx context.Context) error {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid indexer base URL %q: %w", c.BaseURL, err)
	}
	if err := validateHTTPURL(base); err != nil {
		return fmt.Errorf("invalid indexer base URL %q: %w", c.BaseURL, err)
	}
	capsURL := base.ResolveReference(&url.URL{Path: c.ApiPath})
	params := capsURL.Query()
	params.Set("t", "caps")
	params.Set("apikey", c.APIKey)
	params.Set("o", "xml")
	capsURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, capsURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create capabilities request: %w", err)
	}
	req.Header.Set("User-Agent", "GoNZB/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("indexer %s capabilities returned status: %d", c.name, resp.StatusCode)
	}
	if resp.ContentLength > maxCapabilitiesResponseBytes {
		return fmt.Errorf("indexer %s capabilities response exceeds %d bytes", c.name, maxCapabilitiesResponseBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCapabilitiesResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read capabilities response: %w", err)
	}
	if len(body) == 0 || int64(len(body)) > maxCapabilitiesResponseBytes {
		return fmt.Errorf("indexer %s returned an invalid capabilities response", c.name)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, req aggregator.SearchRequest) ([]*domain.Release, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid indexer base URL %q: %w", c.BaseURL, err)
	}
	if err := validateHTTPURL(base); err != nil {
		return nil, fmt.Errorf("invalid indexer base URL %q: %w", c.BaseURL, err)
	}

	searchURL := base.ResolveReference(&url.URL{Path: c.ApiPath})
	params := searchURL.Query()

	searchType := string(req.Type)
	if searchType == "" {
		searchType = string(aggregator.SearchTypeGeneric)
	}
	params.Set("t", searchType)
	params.Set("apikey", c.APIKey)
	params.Set("o", "xml")
	if req.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", req.Limit))
	}
	if len(req.Categories) > 0 {
		categories := make([]string, 0, len(req.Categories))
		for _, category := range req.Categories {
			if category > 0 {
				categories = append(categories, fmt.Sprintf("%d", category))
			}
		}
		if len(categories) > 0 {
			params.Set("cat", strings.Join(categories, ","))
		}
	}

	// CHANGED: pass through real Newznab movie/tvsearch parameters.
	switch req.Type {
	case aggregator.SearchTypeMovie:
		setIfNotEmpty(params, "q", req.Query)
		setIfNotEmpty(params, "imdbid", normalizeIMDbID(req.IMDbID))
		setIfNotEmpty(params, "genre", req.Genre)

	case aggregator.SearchTypeTV:
		setIfNotEmpty(params, "q", req.Query)
		setIfNotEmpty(params, "rid", req.RageID)
		setIfNotEmpty(params, "tvdbid", req.TVDBID)
		setIfNotEmpty(params, "imdbid", normalizeIMDbID(req.IMDbID))
		setIfNotEmpty(params, "tvmazeid", req.TVMazeID)
		setIfNotEmpty(params, "season", req.Season)
		setIfNotEmpty(params, "ep", req.Episode)

	default:
		setIfNotEmpty(params, "q", req.Query)
	}

	searchURL.RawQuery = params.Encode()

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	resp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer %s returned status: %d", c.name, resp.StatusCode)
	}

	if resp.ContentLength > maxSearchResponseBytes {
		return nil, fmt.Errorf("indexer %s search response exceeds %d bytes", c.name, maxSearchResponseBytes)
	}
	var rss RSSResponse
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxSearchResponseBytes+1)).Decode(&rss); err != nil {
		return nil, err
	}

	results := make([]*domain.Release, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		res := item.ToRelease(c.name)
		res.RedirectAllowed = c.redirectAllowed
		results = append(results, res)
	}
	return results, nil
}

func (c *Client) GetNZB(ctx context.Context, res *domain.Release) (io.ReadCloser, error) {
	downloadURL, err := url.Parse(res.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("invalid release download URL %q: %w", res.DownloadURL, err)
	}
	if err := validateHTTPURL(downloadURL); err != nil {
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

func validateHTTPURL(value *url.URL) error {
	if value == nil || (value.Scheme != "http" && value.Scheme != "https") {
		return fmt.Errorf("scheme must be http or https")
	}
	if value.Hostname() == "" {
		return fmt.Errorf("host is required")
	}
	if value.User != nil {
		return fmt.Errorf("embedded credentials are not allowed")
	}
	return nil
}

func setIfNotEmpty(values url.Values, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		values.Set(key, value)
	}
}

func normalizeIMDbID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "tt")
	return value
}
