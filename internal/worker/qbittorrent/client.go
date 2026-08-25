package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Torrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	SavePath     string  `json:"save_path"`
	ContentPath  string  `json:"content_path"`
	Size         int64   `json:"size"`
	TotalSize    int64   `json:"total_size"`
	Progress     float64 `json:"progress"`
	CompletionOn int64   `json:"completion_on"`
	Tracker      string  `json:"tracker"`
	Tags         string  `json:"tags"`
	State        string  `json:"state"`
}

type Client struct {
	baseURL  *url.URL
	username string
	password string
	http     *http.Client
}

func New(rawURL, username, password string, timeout time.Duration) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid qBittorrent URL %q", rawURL)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("qBittorrent URL must use http or https")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create qBittorrent cookie jar: %w", err)
	}
	return &Client{
		baseURL: base, username: username, password: password,
		http: &http.Client{Timeout: timeout, Jar: jar},
	}, nil
}

func (c *Client) Login(ctx context.Context) error {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/v2/auth/login"), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build qBittorrent login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qBittorrent login: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("read qBittorrent login response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("qBittorrent login rejected (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (c *Client) Completed(ctx context.Context, tag, hash string) ([]Torrent, error) {
	query := url.Values{"filter": {"completed"}}
	if strings.TrimSpace(tag) != "" && strings.TrimSpace(hash) == "" {
		query.Set("tag", tag)
	}
	if strings.TrimSpace(hash) != "" {
		query.Set("hashes", hash)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/v2/torrents/info")+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build qBittorrent torrent request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query qBittorrent torrents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("qBittorrent torrent query returned HTTP %d", resp.StatusCode)
	}
	var torrents []Torrent
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := decoder.Decode(&torrents); err != nil {
		return nil, fmt.Errorf("decode qBittorrent torrents: %w", err)
	}
	completed := torrents[:0]
	for _, torrent := range torrents {
		torrent.Hash = strings.ToLower(strings.TrimSpace(torrent.Hash))
		if torrent.Size <= 0 {
			torrent.Size = torrent.TotalSize
		}
		if torrent.Hash != "" && torrent.Name != "" && torrent.Size > 0 && torrent.Progress >= 1 {
			completed = append(completed, torrent)
		}
	}
	sort.Slice(completed, func(i, j int) bool {
		if completed[i].CompletionOn == completed[j].CompletionOn {
			return completed[i].Hash < completed[j].Hash
		}
		return completed[i].CompletionOn < completed[j].CompletionOn
	})
	return completed, nil
}

func (c *Client) endpoint(path string) string {
	rel := &url.URL{Path: path}
	return c.baseURL.ResolveReference(rel).String()
}

// TrackerIdentity removes announce paths, query parameters, and user info,
// which commonly contain private tracker passkeys.
func TrackerIdentity(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	if len(raw) > 255 {
		return raw[:255]
	}
	return raw
}
