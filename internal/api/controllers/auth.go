package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/auth"
	"github.com/labstack/echo/v5"
)

const sessionCookieName = "gonzb_session"

func SessionCookieName() string {
	return sessionCookieName
}

type AuthController struct {
	Service *auth.Service
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type upsertUserRequest struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Password string   `json:"password,omitempty"`
	Enabled  bool     `json:"enabled"`
	RoleIDs  []string `json:"role_ids"`
}

type upsertRoleRequest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

type tokenCreateRequest struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

func (ctrl *AuthController) CreateSession(c *echo.Context) error {
	var req loginRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	session, principal, err := ctrl.Service.AuthenticatePassword(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		return jsonError(c, http.StatusUnauthorized, "invalid username or password")
	}
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	return c.JSON(http.StatusOK, map[string]any{
		"session": map[string]any{
			"user_id":       principal.UserID,
			"username":      principal.Username,
			"permissions":   permissionList(principal),
			"authenticated": true,
		},
	})
}

func (ctrl *AuthController) GetSession(c *echo.Context) error {
	principal, ok := PrincipalFromContext(c)
	if !ok {
		return c.JSON(http.StatusOK, map[string]any{"session": map[string]any{"authenticated": false}})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"session": map[string]any{
			"user_id":       principal.UserID,
			"username":      principal.Username,
			"permissions":   permissionList(principal),
			"authenticated": true,
		},
	})
}

func (ctrl *AuthController) DeleteSession(c *echo.Context) error {
	cookie, err := c.Cookie(sessionCookieName)
	if err == nil && cookie != nil {
		_ = ctrl.Service.LogoutSession(c.Request().Context(), cookie.Value)
	}
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *AuthController) ListUsers(c *echo.Context) error {
	items, err := ctrl.Service.ListUsers(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *AuthController) UpsertUser(c *echo.Context) error {
	var req upsertUserRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	user, err := ctrl.Service.UpsertUser(c.Request().Context(), auth.StoredUser{
		User: auth.User{
			ID:       strings.TrimSpace(req.ID),
			Username: strings.TrimSpace(req.Username),
			Enabled:  req.Enabled,
		},
	}, req.Password, req.RoleIDs)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"user": user})
}

func (ctrl *AuthController) DeleteUser(c *echo.Context) error {
	if err := ctrl.Service.DeleteUser(c.Request().Context(), pathParamTrimmed(c, "id")); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *AuthController) ListRoles(c *echo.Context) error {
	items, err := ctrl.Service.ListRoles(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *AuthController) UpsertRole(c *echo.Context) error {
	var req upsertRoleRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	role, err := ctrl.Service.UpsertRole(c.Request().Context(), auth.Role{
		ID:          strings.TrimSpace(req.ID),
		Name:        strings.TrimSpace(req.Name),
		Permissions: req.Permissions,
	})
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"role": role})
}

func (ctrl *AuthController) DeleteRole(c *echo.Context) error {
	if err := ctrl.Service.DeleteRole(c.Request().Context(), pathParamTrimmed(c, "id")); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (ctrl *AuthController) ListTokens(c *echo.Context) error {
	items, err := ctrl.Service.ListTokens(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *AuthController) CreateToken(c *echo.Context) error {
	var req tokenCreateRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	token, raw, err := ctrl.Service.CreateToken(c.Request().Context(), strings.TrimSpace(req.UserID), strings.TrimSpace(req.Name))
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"token": token, "secret": raw})
}

func (ctrl *AuthController) RevokeToken(c *echo.Context) error {
	if err := ctrl.Service.RevokeToken(c.Request().Context(), pathParamTrimmed(c, "id")); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func permissionList(principal *auth.Principal) []string {
	if principal == nil {
		return nil
	}
	out := make([]string, 0, len(principal.Permissions))
	for permission := range principal.Permissions {
		out = append(out, permission)
	}
	return out
}
