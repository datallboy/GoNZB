package controllers

import (
	"net/http"

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

	if err := bindQueryAndBody(c, &req); err != nil {
		return req, err
	}

	req.Mode = normalizeLowerTrimmed(req.Mode)
	req.Type = normalizeLowerTrimmed(req.Type)

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
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	if req.Mode != "" && req.Type != "" {
		return jsonError(c, http.StatusBadRequest, "compatibility request cannot include both `mode` and `t`")
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
			return writeNewznabError(c, http.StatusNotFound, 100, "Newznab-compatible API is not enabled")
		}
		return ctrl.Newznab.Handle(c)
	}

	return jsonError(c, http.StatusBadRequest, "missing compatibility selector: use `mode` for SAB or `t` for Newznab")
}
