package controllers

import (
	"github.com/datallboy/gonzb/internal/auth"
	"github.com/labstack/echo/v5"
)

const principalContextKey = "auth_principal"

func SetPrincipal(c *echo.Context, principal *auth.Principal) {
	if c == nil {
		return
	}
	c.Set(principalContextKey, principal)
	if principal != nil && c.Request() != nil {
		req := c.Request()
		c.SetRequest(req.WithContext(auth.ContextWithPrincipal(req.Context(), principal)))
	}
}

func PrincipalFromContext(c *echo.Context) (*auth.Principal, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(principalContextKey).(*auth.Principal)
	return value, ok && value != nil
}
