package controllers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/auth"
	"github.com/labstack/echo/v5"
	"github.com/segmentio/ksuid"
)

const sessionCookieName = "gonzb_session"
const csrfCookieName = "gonzb_csrf"

func SessionCookieName() string {
	return sessionCookieName
}

func CSRFCookieName() string {
	return csrfCookieName
}

type AuthController struct {
	Service *auth.Service
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupRequest struct {
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

type authUserDetailResponse struct {
	User   *auth.User   `json:"user"`
	Tokens []auth.Token `json:"tokens"`
}

func (ctrl *AuthController) GetSetupStatus(c *echo.Context) error {
	required, err := ctrl.Service.SetupRequired(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"setup_required": required})
}

func (ctrl *AuthController) CreateInitialUser(c *echo.Context) error {
	var req setupRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	session, principal, err := ctrl.Service.SetupInitialUser(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSetupCompleted):
			return jsonError(c, http.StatusConflict, "initial setup already completed")
		default:
			return jsonError(c, http.StatusBadRequest, err.Error())
		}
	}
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestUsesHTTPS(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	csrfToken := ensureCSRFCookie(c, session.ExpiresAt)
	return c.JSON(http.StatusCreated, map[string]any{
		"session": sessionPayload(principal, csrfToken, false),
	})
}

func (ctrl *AuthController) CreateSession(c *echo.Context) error {
	var req loginRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	session, principal, err := ctrl.Service.AuthenticatePassword(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrSetupRequired) {
			return c.JSON(http.StatusConflict, map[string]any{
				"error": "initial setup required",
				"session": map[string]any{
					"authenticated":  false,
					"setup_required": true,
					"permissions":    []string{},
				},
			})
		}
		return jsonError(c, http.StatusUnauthorized, "invalid username or password")
	}
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestUsesHTTPS(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	csrfToken := ensureCSRFCookie(c, session.ExpiresAt)
	return c.JSON(http.StatusOK, map[string]any{"session": sessionPayload(principal, csrfToken, false)})
}

func (ctrl *AuthController) GetSession(c *echo.Context) error {
	required, err := ctrl.Service.SetupRequired(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	principal, ok := PrincipalFromContext(c)
	if !ok {
		return c.JSON(http.StatusOK, map[string]any{"session": map[string]any{"authenticated": false, "setup_required": required, "permissions": []string{}}})
	}
	csrfToken := ensureCSRFCookie(c, time.Now().UTC().Add(7*24*time.Hour))
	return c.JSON(http.StatusOK, map[string]any{"session": sessionPayload(principal, csrfToken, false)})
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
		Secure:   requestUsesHTTPS(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		Secure:   requestUsesHTTPS(c),
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
	if items == nil {
		items = []auth.StoredUser{}
	}
	views := make([]auth.User, 0, len(items))
	for _, item := range items {
		views = append(views, sanitizeStoredUser(item))
	}
	return c.JSON(http.StatusOK, map[string]any{"items": views, "count": len(views)})
}

func (ctrl *AuthController) GetUser(c *echo.Context) error {
	userID := pathParamTrimmed(c, "id")
	user, err := ctrl.Service.GetUser(c.Request().Context(), userID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if user == nil {
		return jsonError(c, http.StatusNotFound, "user not found")
	}
	tokens, err := ctrl.Service.ListTokensByUser(c.Request().Context(), userID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if tokens == nil {
		tokens = []auth.Token{}
	}
	userView := sanitizeStoredUser(*user)
	return c.JSON(http.StatusOK, authUserDetailResponse{User: &userView, Tokens: tokens})
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
	userView := sanitizeStoredUser(*user)
	return c.JSON(http.StatusOK, map[string]any{"user": userView})
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
	if items == nil {
		items = []auth.Role{}
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
	if items == nil {
		items = []auth.Token{}
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *AuthController) ListCurrentUserTokens(c *echo.Context) error {
	principal, ok := PrincipalFromContext(c)
	if !ok || principal == nil {
		return jsonError(c, http.StatusUnauthorized, "authentication required")
	}
	items, err := ctrl.Service.ListTokensByUser(c.Request().Context(), principal.UserID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if items == nil {
		items = []auth.Token{}
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

func (ctrl *AuthController) CreateCurrentUserToken(c *echo.Context) error {
	principal, ok := PrincipalFromContext(c)
	if !ok || principal == nil {
		return jsonError(c, http.StatusUnauthorized, "authentication required")
	}
	var req tokenCreateRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	token, raw, err := ctrl.Service.CreateToken(c.Request().Context(), principal.UserID, strings.TrimSpace(req.Name))
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

func (ctrl *AuthController) RevokeCurrentUserToken(c *echo.Context) error {
	principal, ok := PrincipalFromContext(c)
	if !ok || principal == nil {
		return jsonError(c, http.StatusUnauthorized, "authentication required")
	}
	if err := ctrl.Service.RevokeTokenForUser(c.Request().Context(), principal.UserID, pathParamTrimmed(c, "id")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrForbidden) {
			status = http.StatusForbidden
		}
		return jsonError(c, status, err.Error())
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

func sessionPayload(principal *auth.Principal, csrfToken string, setupRequired bool) map[string]any {
	payload := map[string]any{
		"authenticated":  principal != nil,
		"setup_required": setupRequired,
		"permissions":    []string{},
	}
	if principal == nil {
		return payload
	}
	payload["user_id"] = principal.UserID
	payload["username"] = principal.Username
	payload["permissions"] = permissionList(principal)
	payload["csrf_token"] = csrfToken
	return payload
}

func sanitizeStoredUser(user auth.StoredUser) auth.User {
	return auth.User{
		ID:          user.ID,
		Username:    user.Username,
		Enabled:     user.Enabled,
		RoleIDs:     append([]string(nil), user.RoleIDs...),
		Permissions: append([]string(nil), user.Permissions...),
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func ensureCSRFCookie(c *echo.Context, expiresAt time.Time) string {
	if c == nil || c.Response() == nil {
		return ""
	}
	if cookie, err := c.Cookie(csrfCookieName); err == nil && cookie != nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value
	}
	token := ksuid.New().String()
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   requestUsesHTTPS(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
	return token
}

func requestUsesHTTPS(c *echo.Context) bool {
	return c != nil && strings.EqualFold(strings.TrimSpace(c.Scheme()), "https")
}
