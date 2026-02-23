package api

import (
	"crypto/subtle"
	"net/http"
	"net/url"

	"github.com/datallboy/gonzb/internal/api/controllers"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func RegisterRoutes(e *echo.Echo, app *app.Context) {

	// Middleware: Request Logger
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:  true,
		LogURI:     true,
		LogMethod:  true,
		LogLatency: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			app.Logger.Info("%s %s | %d | %s", v.Method, redactSensitiveURI(v.URI), v.Status, v.Latency)
			return nil
		},
	}))

	nzbCtrl := &controllers.NewznabController{App: app}
	apiKeyMW := apiKeyMiddleware(app.Config.API.Key)

	// Newznab API Endpoint (for Prowlarr/Sonarr)
	e.GET("/api", nzbCtrl.Handle, apiKeyMW)

	// Direct NZB Download Endpoint
	e.GET("/nzb/:id", nzbCtrl.HandleDownload, apiKeyMW)
}

func apiKeyMiddleware(requiredKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if requiredKey == "" {
				return next(c)
			}

			provided := c.QueryParam("apikey")
			if provided == "" {
				provided = c.Request().Header.Get("X-API-Key")
			}

			if subtle.ConstantTimeCompare([]byte(provided), []byte(requiredKey)) != 1 {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

			return next(c)
		}
	}
}

func redactSensitiveURI(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}

	query := parsed.Query()
	if query.Has("apikey") {
		query.Set("apikey", "REDACTED")
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}
