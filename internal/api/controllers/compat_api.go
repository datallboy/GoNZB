package controllers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// shared compatibility selector for `/api`.
// We bind once, normalize once, then dispatch to the appropriate compatibility surface.
type compatAPIRequest struct {
	Mode string `query:"mode" form:"mode"`
	Type string `query:"t" form:"t"`
}

func bindCompatAPIRequest(c *echo.Context) (compatAPIRequest, error) {
	var req compatAPIRequest

	if err := echo.BindQueryParams(c, &req); err != nil {
		return req, fmt.Errorf("invalid query parameters")
	}
	if err := echo.BindBody(c, &req); err != nil {
		return req, fmt.Errorf("invalid request body")
	}

	req.Mode = strings.TrimSpace(req.Mode)
	req.Type = strings.TrimSpace(req.Type)

	return req, nil
}

//	compatibility multiplexer for shared `/api` transport.
//
// Dispatches by query intent:
// - `mode=...` => SAB-compatible downloader API
// - `t=...` => Newznab-compatible aggregator API
type CompatAPIController struct {
	SABEnabled     bool
	NewznabEnabled bool

	SAB     *SABController
	Newznab *NewznabController
}

func (ctrl *CompatAPIController) Handle(c *echo.Context) error {
	req, err := bindCompatAPIRequest(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
	}

	if req.Mode != "" {
		if !ctrl.SABEnabled || ctrl.SAB == nil {
			return c.JSON(http.StatusNotFound, sabStatusResponse{
				Status: false,
				Error:  "SAB-compatible API is not enabled",
			})
		}
		return ctrl.SAB.Handle(c)
	}

	if req.Type != "" {
		if !ctrl.NewznabEnabled || ctrl.Newznab == nil {
			return c.String(http.StatusNotFound, "Newznab-compatible API is not enabled")
		}
		return ctrl.Newznab.Handle(c)
	}

	return c.JSON(http.StatusBadRequest, map[string]string{
		"error": "missing compatibility selector: use `mode` for SAB or `t` for Newznab",
	})
}
