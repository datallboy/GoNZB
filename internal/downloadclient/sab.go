package downloadclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
)

const maxSABResponseBytes = 1 << 20

var sabHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type sabResponse struct {
	Status  bool     `json:"status"`
	Error   string   `json:"error"`
	IDs     []string `json:"nzo_ids"`
	Version string   `json:"version"`
}

func sendNZB(ctx context.Context, client app.DownloadClientRuntimeSettings, filename string, content io.Reader) (string, error) {
	endpoint, err := sabEndpoint(client)
	if err != nil {
		return "", err
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	multipartWriter := multipart.NewWriter(writer)
	writeErr := make(chan error, 1)
	go func() {
		defer close(writeErr)
		defer writer.Close()
		for key, value := range map[string]string{
			"mode": "addfile", "output": "json", "apikey": client.APIKey,
			"cat": client.Category, "priority": strconv.Itoa(client.Priority),
		} {
			if err := multipartWriter.WriteField(key, value); err != nil {
				_ = writer.CloseWithError(err)
				writeErr <- err
				return
			}
		}
		part, err := multipartWriter.CreateFormFile("name", safeNZBFilename(filename))
		if err == nil {
			_, err = io.Copy(part, content)
		}
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = writer.CloseWithError(err)
		}
		writeErr <- err
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req.Header.Set("User-Agent", "GoNZB")

	resp, err := sabHTTPClient.Do(req)
	if err != nil {
		return "", sanitizedHTTPError(err)
	}
	defer resp.Body.Close()
	_ = reader.Close()
	select {
	case err := <-writeErr:
		if err != nil {
			return "", err
		}
	default:
	}

	parsed, err := decodeSABResponse(resp)
	if err != nil {
		return "", err
	}
	if !parsed.Status {
		if parsed.Error == "" {
			parsed.Error = "SAB-compatible client rejected the NZB"
		}
		return "", fmt.Errorf("%s", parsed.Error)
	}
	if len(parsed.IDs) > 0 {
		return parsed.IDs[0], nil
	}
	return "", nil
}

func testClient(ctx context.Context, client app.DownloadClientRuntimeSettings) error {
	endpoint, err := sabEndpoint(client)
	if err != nil {
		return err
	}
	query := endpoint.Query()
	query.Set("mode", "version")
	query.Set("output", "json")
	query.Set("apikey", client.APIKey)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "GoNZB")
	resp, err := sabHTTPClient.Do(req)
	if err != nil {
		return sanitizedHTTPError(err)
	}
	defer resp.Body.Close()
	parsed, err := decodeSABResponse(resp)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.Version) == "" && !parsed.Status {
		if parsed.Error != "" {
			return fmt.Errorf("%s", parsed.Error)
		}
		return fmt.Errorf("download client did not return a SAB-compatible version response")
	}
	return nil
}

func sabEndpoint(client app.DownloadClientRuntimeSettings) (*url.URL, error) {
	raw := strings.TrimSpace(client.BaseURL)
	if raw == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("base URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("base URL scheme must be http or https")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("base URL must not contain credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/api") {
		parsed.Path = path.Join(parsed.Path, "api")
	}
	return parsed, nil
}

func decodeSABResponse(resp *http.Response) (*sabResponse, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSABResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSABResponseBytes {
		return nil, fmt.Errorf("download client response exceeds %d bytes", maxSABResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download client returned HTTP %d", resp.StatusCode)
	}
	var parsed sabResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode SAB-compatible response: %w", err)
	}
	return &parsed, nil
}

func safeNZBFilename(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, value)
	value = path.Base(value)
	if value == "." || value == "/" || value == "" {
		return "release.nzb"
	}
	if !strings.HasSuffix(strings.ToLower(value), ".nzb") {
		value += ".nzb"
	}
	return value
}

func sanitizedHTTPError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}
