package api

import (
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
			app.Logger.Info("%s %s | %d | %s", v.Method, v.URI, v.Status, v.Latency)
			return nil
		},
	}))

	nzbCtrl := &controllers.NewznabController{App: app}

	// Newznab API Endpoint (for Prowlarr/Sonarr)
	e.GET("/api", nzbCtrl.Handle)

	// Direct NZB Download Endpoint
	e.GET("/nzb/:id", nzbCtrl.HandleDownload)
}
