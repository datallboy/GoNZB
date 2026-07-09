package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type GoNZBNetAdminController struct {
	appCtx *app.Context
}

type gonzbnetAdminStore interface {
	ListTrustPools(ctx context.Context) ([]pgindex.TrustPoolRecord, error)
	ListPoolMembers(ctx context.Context, poolID string) ([]pgindex.PoolMemberRecord, error)
	UpsertTrustPool(ctx context.Context, pool pgindex.TrustPoolRecord) error
	UpsertPoolMember(ctx context.Context, member pgindex.PoolMemberRecord) error
	RevokePoolMember(ctx context.Context, poolID, nodeID, eventID string, effectiveAt *time.Time) error
}

type trustPoolRequest struct {
	PoolID                     string   `json:"pool_id"`
	DisplayName                string   `json:"display_name"`
	Description                string   `json:"description"`
	MembershipThreshold        int      `json:"membership_threshold"`
	ModerationThreshold        int      `json:"moderation_threshold"`
	CheckpointWitnessThreshold int      `json:"checkpoint_witness_threshold"`
	AcceptMode                 string   `json:"accept_mode"`
	MinNodeTrustScore          float64  `json:"min_node_trust_score"`
	AcceptedEventTypes         []string `json:"accepted_event_types"`
	Enabled                    *bool    `json:"enabled"`
}

type poolMemberRequest struct {
	NodeID string `json:"node_id"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

func NewGoNZBNetAdminController(appCtx *app.Context) *GoNZBNetAdminController {
	return &GoNZBNetAdminController{appCtx: appCtx}
}

func (ctrl *GoNZBNetAdminController) ListPools(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListTrustPools(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) UpsertPool(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req trustPoolRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(req.PoolID) == "" || strings.TrimSpace(req.DisplayName) == "" {
		return jsonError(c, http.StatusBadRequest, "pool_id and display_name are required")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	acceptedTypes := req.AcceptedEventTypes
	if len(acceptedTypes) == 0 {
		acceptedTypes = []string{"ReleaseCard", "HealthAttestation"}
	}
	policy := pools.Policy{
		MembershipThreshold:        req.MembershipThreshold,
		ModerationThreshold:        req.ModerationThreshold,
		CheckpointWitnessThreshold: req.CheckpointWitnessThreshold,
		AcceptMode:                 req.AcceptMode,
		MinNodeTrustScore:          req.MinNodeTrustScore,
		AcceptedEventTypes:         acceptedTypes,
	}
	policy = pools.NormalizePolicy(policy, req.MembershipThreshold)
	policyJSON, _ := json.Marshal(policy)
	if err := store.UpsertTrustPool(c.Request().Context(), pgindex.TrustPoolRecord{
		PoolID:                     req.PoolID,
		DisplayName:                req.DisplayName,
		Description:                req.Description,
		PolicyJSON:                 policyJSON,
		MembershipThreshold:        policy.MembershipThreshold,
		ModerationThreshold:        policy.ModerationThreshold,
		CheckpointWitnessThreshold: policy.CheckpointWitnessThreshold,
		AcceptMode:                 policy.AcceptMode,
		MinNodeTrustScore:          policy.MinNodeTrustScore,
		AcceptedEventTypes:         policy.AcceptedEventTypes,
		Enabled:                    enabled,
	}); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) ListPoolMembers(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	items, err := store.ListPoolMembers(c.Request().Context(), pathParamTrimmed(c, "pool_id"))
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) UpsertPoolMember(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req poolMemberRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(req.NodeID) == "" {
		return jsonError(c, http.StatusBadRequest, "node_id is required")
	}
	if err := store.UpsertPoolMember(c.Request().Context(), pgindex.PoolMemberRecord{
		PoolID: pathParamTrimmed(c, "pool_id"),
		NodeID: req.NodeID,
		Role:   firstNonBlank(req.Role, pools.RoleMember),
		Status: firstNonBlank(req.Status, pools.StatusActive),
	}); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) RevokePoolMember(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	if err := store.RevokePoolMember(c.Request().Context(), pathParamTrimmed(c, "pool_id"), pathParamTrimmed(c, "node_id"), "", nil); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (ctrl *GoNZBNetAdminController) store() (gonzbnetAdminStore, bool) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.PGIndexStore == nil {
		return nil, false
	}
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetAdminStore)
	return store, ok
}
