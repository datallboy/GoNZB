package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

func jsonError(c *echo.Context, status int, message string) error {
	return c.JSON(status, map[string]string{"error": message})
}

func normalizeTrimmed(value string) string {
	return strings.TrimSpace(value)
}

func normalizeLowerTrimmed(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func queryParamTrimmed(c *echo.Context, name string) string {
	return normalizeTrimmed(c.QueryParam(name))
}

func queryParamLower(c *echo.Context, name string) string {
	return normalizeLowerTrimmed(c.QueryParam(name))
}

func pathParamTrimmed(c *echo.Context, name string) string {
	return normalizeTrimmed(c.Param(name))
}

func bindQueryAndBody(c *echo.Context, dst any) error {
	if err := echo.BindQueryParams(c, dst); err != nil {
		return fmt.Errorf("invalid query parameters")
	}

	if !requestMayHaveBody(c) {
		return nil
	}

	if err := echo.BindBody(c, dst); err != nil {
		return fmt.Errorf("invalid request body")
	}

	return nil
}

func requestMayHaveBody(c *echo.Context) bool {
	req := c.Request()
	if req == nil || req.Body == nil {
		return false
	}
	if req.ContentLength > 0 {
		return true
	}
	if len(req.TransferEncoding) > 0 {
		return true
	}

	contentType := normalizeTrimmed(req.Header.Get(echo.HeaderContentType))
	if contentType == "" {
		return false
	}

	switch req.Method {
	case http.MethodGet, http.MethodHead:
		return false
	default:
		return true
	}
}

func decodeJSONBody(c *echo.Context, dst any) error {
	if c.Request().Body == nil {
		return fmt.Errorf("request body is required")
	}

	contentType := normalizeTrimmed(c.Request().Header.Get(echo.HeaderContentType))
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return fmt.Errorf("invalid Content-Type header")
		}
		if mediaType != echo.MIMEApplicationJSON {
			return fmt.Errorf("content-type must be application/json")
		}
	}

	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("request body is required")
		}
		return fmt.Errorf("invalid request body")
	}

	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain a single JSON object")
	}

	return nil
}

func parsePaginationParams(c *echo.Context, defaultLimit, maxLimit int) (int, int, error) {
	limit, err := parseOptionalBoundedInt(queryParamTrimmed(c, "limit"), "limit", defaultLimit, 1, maxLimit)
	if err != nil {
		return 0, 0, err
	}

	offset, err := parseOptionalBoundedInt(queryParamTrimmed(c, "offset"), "offset", 0, 0, 1000000)
	if err != nil {
		return 0, 0, err
	}

	return limit, offset, nil
}

func parseOptionalBoundedInt(raw, field string, fallback, min, max int) (int, error) {
	raw = normalizeTrimmed(raw)
	if raw == "" {
		return fallback, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", field, min, max)
	}

	return n, nil
}

func parseOptionalBool(raw, field string) (*bool, error) {
	raw = normalizeLowerTrimmed(raw)
	if raw == "" {
		return nil, nil
	}
	switch raw {
	case "true", "1", "yes":
		v := true
		return &v, nil
	case "false", "0", "no":
		v := false
		return &v, nil
	default:
		return nil, fmt.Errorf("%s must be a boolean", field)
	}
}

func parseOptionalBoolQuery(c *echo.Context, name string) *bool {
	value, err := parseOptionalBool(queryParamTrimmed(c, name), name)
	if err != nil {
		return nil
	}
	return value
}

func parseOptionalFloat64(raw, field string, min, max float64) (float64, error) {
	raw = normalizeTrimmed(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", field)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s must be between %.0f and %.0f", field, min, max)
	}
	return n, nil
}

func parseOptionalInt64(raw, field string, min, max int64) (int64, error) {
	raw = normalizeTrimmed(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", field, min, max)
	}
	return n, nil
}

func parseOptionalDate(raw, field string) (*time.Time, error) {
	raw = normalizeTrimmed(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be in YYYY-MM-DD format", field)
	}
	utc := t.UTC()
	return &utc, nil
}

func parseIntDefault(raw string, fallback int) int {
	raw = normalizeTrimmed(raw)
	if raw == "" {
		return fallback
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}

	return n
}

func sanitizeUploadFilename(name, fallback string) string {
	name = normalizeTrimmed(name)
	name = strings.ReplaceAll(name, "\x00", "")
	name = filepath.Base(name)

	if name == "" || name == "." || name == "/" {
		name = fallback
	}

	name = strings.Map(func(r rune) rune {
		switch {
		case r < 32:
			return -1
		case r == '/' || r == '\\':
			return -1
		default:
			return r
		}
	}, name)

	name = normalizeTrimmed(name)
	if name == "" {
		return fallback
	}

	return name
}

func writeNewznabError(c *echo.Context, status, code int, description string) error {
	return c.XML(status, NewznabErrorResponse{
		Code:        code,
		Description: description,
	})
}

func buildDownloadFilename(name, fallback string) string {
	filename := sanitizeUploadFilename(name, fallback)
	if !strings.HasSuffix(strings.ToLower(filename), ".nzb") {
		filename += ".nzb"
	}
	return filename
}

func contentDispositionFilename(filename string) string {
	safe := strings.NewReplacer(`\`, "_", `"`, "_", "\r", "_", "\n", "_").Replace(filename)
	return fmt.Sprintf(`attachment; filename="%s"`, safe)
}
