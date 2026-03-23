package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

const (
	defaultJSONBodyLimit      int64 = 1 << 20  // 1 MiB
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
