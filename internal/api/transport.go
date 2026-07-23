package api

import (
	"net/http"
	"strings"

	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/labstack/echo/v5"
)

const (
	defaultJSONBodyLimit      int64 = 1 << 20  // 1 MiB
	adminJSONBodyLimit        int64 = 32 << 20 // 32 MiB
	defaultMultipartBodyLimit int64 = 64 << 20 // 64 MiB
)

func bodyLimitMiddleware(jsonLimit, multipartLimit int64) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			if req == nil || req.Body == nil {
				return next(c)
			}

			limit := jsonLimit
			contentType := strings.ToLower(strings.TrimSpace(req.Header.Get(echo.HeaderContentType)))
			if strings.HasPrefix(contentType, "multipart/form-data") {
				limit = multipartLimit
			}

			if req.ContentLength > 0 && req.ContentLength > limit {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "request body too large")
			}

			req.Body = http.MaxBytesReader(c.Response(), req.Body, limit)
			return next(c)
		}
	}
}

func federationBodyLimitMiddleware(cfg config.GoNZBNetConfig) echo.MiddlewareFunc {
	limit := federationBodyLimit(cfg)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			if req == nil || req.Body == nil {
				return next(c)
			}
			if req.ContentLength > 0 && req.ContentLength > limit {
				return federationTransportError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
			}
			req.Body = http.MaxBytesReader(c.Response(), req.Body, limit)
			return next(c)
		}
	}
}

func federationBodyLimit(cfg config.GoNZBNetConfig) int64 {
	maxEventBytes := cfg.MaxEventBytes
	if maxEventBytes <= 0 {
		maxEventBytes = 262144
	}
	maxManifestBytes := cfg.MaxManifestBytes
	if maxManifestBytes <= 0 {
		maxManifestBytes = 10485760
	}
	maxBatchEvents := cfg.MaxBatchEvents
	if maxBatchEvents <= 0 {
		maxBatchEvents = 100
	}

	limit := int64(maxManifestBytes)
	batchLimit := int64(maxEventBytes) * int64(maxBatchEvents)
	if batchLimit > limit {
		limit = batchLimit
	}
	if limit < defaultJSONBodyLimit {
		limit = defaultJSONBodyLimit
	}
	return limit
}

func federationTransportError(c *echo.Context, status int, code, message string) error {
	return c.JSON(status, map[string]string{
		"error":   code,
		"code":    code,
		"message": message,
	})
}
