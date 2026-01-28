package api

import (
	"github.com/datallboy/gonzb/internal/api/controllers"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/labstack/echo/v5"
)

func RegisterRoutes(e *echo.Echo, app *app.Context) {
	nzbCtrl := &controllers.NewznabController{App: app}

	// Newznab API Endpoint (for Prowlarr/Sonarr)
	e.GET("/api", nzbCtrl.Handle)

	// Direct NZB Download Endpoint
	e.GET("/nzb/:id", nzbCtrl.HandleDownload)
}
