package controllers

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// shared compatibility selector for `/api`.
// We bind once, normalize once, then dispatch to the appropriate compatibility surface.
type compatAPIRequest struct {
	Type string `query:"t" form:"t"`
}

func bindCompatAPIRequest(c *echo.Context) (compatAPIRequest, error) {
	var req compatAPIRequest

	if err := bindQueryAndBody(c, &req); err != nil {
		return req, err
	}

	req.Type = normalizeLowerTrimmed(req.Type)

	return req, nil
}

// CompatAPIController serves the shared Newznab `/api` transport.
type CompatAPIController struct {
	NewznabEnabled bool
	Newznab        *NewznabController
}

func (ctrl *CompatAPIController) Handle(c *echo.Context) error {
	req, err := bindCompatAPIRequest(c)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	if req.Type != "" {
		if !ctrl.NewznabEnabled || ctrl.Newznab == nil {
			return writeNewznabError(c, http.StatusNotFound, 100, "Newznab-compatible API is not enabled")
		}
		return ctrl.Newznab.Handle(c)
	}

	return jsonError(c, http.StatusBadRequest, "missing Newznab selector `t`")
}
