package controllers

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/moderation"
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
	UpsertFederationNodeIdentity(ctx context.Context, nodeID string, publicKey ed25519.PublicKey) error
	NextFederationEventSequence(ctx context.Context, authorNodeID string) (int64, *string, error)
	FindFederationEventByBodyHash(ctx context.Context, authorNodeID, eventType, bodyHash string) (string, error)
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	ProjectTombstone(ctx context.Context, projection pgindex.TombstoneProjection) error
	ListTombstones(ctx context.Context, activeOnly bool) ([]pgindex.TombstoneRecord, error)
	ProjectCoverageEvent(ctx context.Context, event *events.SignedEvent) error
	ListCoverageDashboard(ctx context.Context, poolID string) (pgindex.CoverageDashboard, error)
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
	NodeID              string          `json:"node_id"`
	Role                string          `json:"role"`
	Status              string          `json:"status"`
	AllowedCapabilities []string        `json:"allowed_capabilities"`
	Limits              json.RawMessage `json:"limits"`
}

type tombstoneRequest struct {
	TargetType       string   `json:"target_type"`
	TargetID         string   `json:"target_id"`
	PoolID           string   `json:"pool_id"`
	Reason           string   `json:"reason"`
	Severity         string   `json:"severity"`
	EvidenceEventIDs []string `json:"evidence_event_ids"`
	EffectiveAt      string   `json:"effective_at"`
	ExpiresAt        *string  `json:"expires_at"`
}

type coverageAssignmentRequest struct {
	AssignmentID   string `json:"assignment_id"`
	PlanID         string `json:"plan_id"`
	PoolID         string `json:"pool_id"`
	Group          string `json:"group"`
	AssignedNodeID string `json:"assigned_node_id"`
	RangeStart     int64  `json:"range_start"`
	RangeEnd       int64  `json:"range_end"`
	WindowStart    string `json:"window_start"`
	WindowEnd      string `json:"window_end"`
	Priority       int    `json:"priority"`
	DueAt          string `json:"due_at"`
}

type coverageClaimRequest struct {
	ClaimID      string `json:"claim_id"`
	AssignmentID string `json:"assignment_id"`
	PoolID       string `json:"pool_id"`
	Group        string `json:"group"`
	RangeStart   int64  `json:"range_start"`
	RangeEnd     int64  `json:"range_end"`
	WindowStart  string `json:"window_start"`
	WindowEnd    string `json:"window_end"`
	ExpiresAt    string `json:"expires_at"`
}

type coverageOutcomeRequest struct {
	OutcomeID    string `json:"outcome_id"`
	ClaimID      string `json:"claim_id"`
	AssignmentID string `json:"assignment_id"`
	PoolID       string `json:"pool_id"`
	Group        string `json:"group"`
	RangeStart   int64  `json:"range_start"`
	RangeEnd     int64  `json:"range_end"`
	ReleaseCount int    `json:"release_count"`
	Reason       string `json:"reason"`
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
		acceptedTypes = []string{
			pools.EventTypeReleaseCard,
			pools.EventTypeHealthAttestation,
			pools.EventTypeTombstone,
			pools.EventTypeValidatorCapacity,
			pools.EventTypeArticleAvailabilityAttestation,
			pools.EventTypeChecksumAttestation,
			pools.EventTypeManifestAvailability,
			pools.EventTypeScannerCapacity,
			pools.EventTypeGroupObservation,
			pools.EventTypeCoveragePlan,
			pools.EventTypeCoverageAssignment,
			pools.EventTypeRangeClaim,
			pools.EventTypeTimeWindowClaim,
			pools.EventTypeCoverageCheckpoint,
			pools.EventTypeRangeComplete,
			pools.EventTypeRangeFailed,
		}
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
	if len(req.Limits) > 0 && !json.Valid(req.Limits) {
		return jsonError(c, http.StatusBadRequest, "limits must be a JSON object")
	}
	if err := store.UpsertPoolMember(c.Request().Context(), pgindex.PoolMemberRecord{
		PoolID:              pathParamTrimmed(c, "pool_id"),
		NodeID:              req.NodeID,
		Role:                firstNonBlank(req.Role, pools.RoleMember),
		Status:              firstNonBlank(req.Status, pools.StatusActive),
		AllowedCapabilities: capability.Normalize(req.AllowedCapabilities),
		LimitsJSON:          req.Limits,
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

func (ctrl *GoNZBNetAdminController) ListTombstones(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	activeOnly := strings.EqualFold(queryParamTrimmed(c, "active"), "true")
	items, err := store.ListTombstones(c.Request().Context(), activeOnly)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *GoNZBNetAdminController) CreateTombstone(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	var req tombstoneRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	severity := firstNonBlank(req.Severity, moderation.SeverityReject)
	if strings.TrimSpace(req.PoolID) == "" && strings.TrimSpace(req.Severity) == "" {
		severity = moderation.SeverityLocalOnly
	}
	effectiveAt := firstNonBlank(req.EffectiveAt, now.Format(time.RFC3339))
	body := moderation.Tombstone{
		SchemaVersion:    "1.0",
		Type:             moderation.Type,
		TargetType:       strings.TrimSpace(req.TargetType),
		TargetID:         strings.TrimSpace(req.TargetID),
		PoolID:           strings.TrimSpace(req.PoolID),
		Reason:           strings.TrimSpace(req.Reason),
		Severity:         severity,
		EvidenceEventIDs: req.EvidenceEventIDs,
		EffectiveAt:      effectiveAt,
		ExpiresAt:        req.ExpiresAt,
	}
	if err := moderation.Validate(body, now, 2*time.Minute); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	nodeIdentity, err := identity.LoadOrCreate(ctrl.appCtx.Config.GoNZBNet.KeysDir)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	nodeID, err := nodeIdentity.NodeID(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	publicKey, err := nodeIdentity.PublicKey(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.UpsertFederationNodeIdentity(c.Request().Context(), nodeID, publicKey); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	bodyHash, err := moderation.HashBody(body)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if existingEventID, err := store.FindFederationEventByBodyHash(c.Request().Context(), nodeID, moderation.Type, bodyHash); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	} else if existingEventID != "" {
		if err := store.ProjectTombstone(c.Request().Context(), pgindex.TombstoneProjection{
			Tombstone:    body,
			EventID:      existingEventID,
			AuthorNodeID: nodeID,
		}); err != nil {
			return jsonError(c, http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "event_id": existingEventID})
	}
	sequence, previousEventID, err := store.NextFederationEventSequence(c.Request().Context(), nodeID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolIDs := []string{}
	visibility := "local"
	if strings.TrimSpace(body.PoolID) != "" && body.Severity != moderation.SeverityLocalOnly {
		poolIDs = []string{body.PoolID}
		visibility = "pool"
	}
	event, validation, err := events.Create(c.Request().Context(), nodeIdentity, events.CreateOptions{
		EventType:       moderation.Type,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       now,
		PoolIDs:         poolIDs,
		Visibility:      visibility,
		BodySchema:      moderation.BodySchema,
		Body:            body,
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if validation == nil || !validation.OK {
		return jsonError(c, http.StatusInternalServerError, "signed tombstone did not verify")
	}
	if err := store.AppendVerifiedFederationEvent(c.Request().Context(), event, validation); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.ProjectTombstone(c.Request().Context(), pgindex.TombstoneProjection{
		Tombstone:    body,
		EventID:      event.EventID,
		AuthorNodeID: nodeID,
	}); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, map[string]string{"status": "ok", "event_id": event.EventID})
}

func (ctrl *GoNZBNetAdminController) CoverageDashboard(c *echo.Context) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	poolID := firstNonBlank(queryParamTrimmed(c, "pool_id"), ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	dashboard, err := store.ListCoverageDashboard(c.Request().Context(), poolID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, dashboard)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageAssignment(c *echo.Context) error {
	var req coverageAssignmentRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	body := coverage.CoverageAssignment{
		SchemaVersion:  "1.0",
		Type:           coverage.TypeCoverageAssignment,
		AssignmentID:   firstNonBlank(req.AssignmentID, coverageID("assign", now)),
		PlanID:         strings.TrimSpace(req.PlanID),
		PoolID:         firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local"),
		Group:          strings.TrimSpace(req.Group),
		AssignedNodeID: strings.TrimSpace(req.AssignedNodeID),
		RangeStart:     req.RangeStart,
		RangeEnd:       req.RangeEnd,
		WindowStart:    strings.TrimSpace(req.WindowStart),
		WindowEnd:      strings.TrimSpace(req.WindowEnd),
		Priority:       req.Priority,
		DueAt:          strings.TrimSpace(req.DueAt),
		CreatedAt:      now.Format(time.RFC3339),
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeCoverageAssignment, body, body.PoolID)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageClaim(c *echo.Context) error {
	var req coverageClaimRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	nodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolID := firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	expiresAt := firstNonBlank(req.ExpiresAt, now.Add(30*time.Minute).Format(time.RFC3339))
	if strings.TrimSpace(req.WindowStart) != "" || strings.TrimSpace(req.WindowEnd) != "" {
		body := coverage.TimeWindowClaim{
			SchemaVersion: "1.0",
			Type:          coverage.TypeTimeWindowClaim,
			ClaimID:       firstNonBlank(req.ClaimID, coverageID("claim", now)),
			AssignmentID:  strings.TrimSpace(req.AssignmentID),
			PoolID:        poolID,
			Group:         strings.TrimSpace(req.Group),
			NodeID:        nodeID,
			WindowStart:   strings.TrimSpace(req.WindowStart),
			WindowEnd:     strings.TrimSpace(req.WindowEnd),
			ClaimedAt:     now.Format(time.RFC3339),
			ExpiresAt:     expiresAt,
		}
		return ctrl.signAndProjectCoverage(c, coverage.TypeTimeWindowClaim, body, poolID)
	}
	body := coverage.RangeClaim{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeClaim,
		ClaimID:       firstNonBlank(req.ClaimID, coverageID("claim", now)),
		AssignmentID:  strings.TrimSpace(req.AssignmentID),
		PoolID:        poolID,
		Group:         strings.TrimSpace(req.Group),
		NodeID:        nodeID,
		RangeStart:    req.RangeStart,
		RangeEnd:      req.RangeEnd,
		ClaimedAt:     now.Format(time.RFC3339),
		ExpiresAt:     expiresAt,
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeRangeClaim, body, poolID)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageComplete(c *echo.Context) error {
	var req coverageOutcomeRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	nodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolID := firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	body := coverage.RangeComplete{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeComplete,
		OutcomeID:     firstNonBlank(req.OutcomeID, coverageID("complete", now)),
		ClaimID:       strings.TrimSpace(req.ClaimID),
		AssignmentID:  strings.TrimSpace(req.AssignmentID),
		PoolID:        poolID,
		Group:         strings.TrimSpace(req.Group),
		NodeID:        nodeID,
		RangeStart:    req.RangeStart,
		RangeEnd:      req.RangeEnd,
		ReleaseCount:  req.ReleaseCount,
		CompletedAt:   now.Format(time.RFC3339),
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeRangeComplete, body, poolID)
}

func (ctrl *GoNZBNetAdminController) CreateCoverageFailed(c *echo.Context) error {
	var req coverageOutcomeRequest
	if err := decodeJSONBody(c, &req); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	now := time.Now().UTC()
	nodeID, err := ctrl.localNodeID(c)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	poolID := firstNonBlank(req.PoolID, ctrl.appCtx.Config.GoNZBNet.LocalPoolID, "pool.local")
	body := coverage.RangeFailed{
		SchemaVersion: "1.0",
		Type:          coverage.TypeRangeFailed,
		OutcomeID:     firstNonBlank(req.OutcomeID, coverageID("failed", now)),
		ClaimID:       strings.TrimSpace(req.ClaimID),
		AssignmentID:  strings.TrimSpace(req.AssignmentID),
		PoolID:        poolID,
		Group:         strings.TrimSpace(req.Group),
		NodeID:        nodeID,
		RangeStart:    req.RangeStart,
		RangeEnd:      req.RangeEnd,
		Reason:        strings.TrimSpace(req.Reason),
		FailedAt:      now.Format(time.RFC3339),
	}
	return ctrl.signAndProjectCoverage(c, coverage.TypeRangeFailed, body, poolID)
}

func (ctrl *GoNZBNetAdminController) signAndProjectCoverage(c *echo.Context, eventType string, body any, poolID string) error {
	store, ok := ctrl.store()
	if !ok {
		return jsonError(c, http.StatusServiceUnavailable, "gonzbnet admin store is unavailable")
	}
	now := time.Now().UTC()
	if err := coverage.Validate(eventType, body, now, 2*time.Minute); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	nodeIdentity, err := identity.LoadOrCreate(ctrl.appCtx.Config.GoNZBNet.KeysDir)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	nodeID, err := nodeIdentity.NodeID(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	publicKey, err := nodeIdentity.PublicKey(c.Request().Context())
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.UpsertFederationNodeIdentity(c.Request().Context(), nodeID, publicKey); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	sequence, previousEventID, err := store.NextFederationEventSequence(c.Request().Context(), nodeID)
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	event, validation, err := events.Create(c.Request().Context(), nodeIdentity, events.CreateOptions{
		EventType:       eventType,
		Sequence:        sequence,
		PreviousEventID: previousEventID,
		CreatedAt:       now,
		PoolIDs:         []string{poolID},
		Visibility:      "pool",
		BodySchema:      coverage.BodySchema(eventType),
		Body:            body,
	})
	if err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if validation == nil || !validation.OK {
		return jsonError(c, http.StatusInternalServerError, "signed coverage event did not verify")
	}
	if err := store.AppendVerifiedFederationEvent(c.Request().Context(), event, validation); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	if err := store.ProjectCoverageEvent(c.Request().Context(), event); err != nil {
		return jsonError(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok", "event_id": event.EventID})
}

func (ctrl *GoNZBNetAdminController) localNodeID(c *echo.Context) (string, error) {
	nodeIdentity, err := identity.LoadOrCreate(ctrl.appCtx.Config.GoNZBNet.KeysDir)
	if err != nil {
		return "", err
	}
	return nodeIdentity.NodeID(c.Request().Context())
}

func coverageID(prefix string, now time.Time) string {
	return fmt.Sprintf("%s_%d", prefix, now.UnixNano())
}

func (ctrl *GoNZBNetAdminController) store() (gonzbnetAdminStore, bool) {
	if ctrl == nil || ctrl.appCtx == nil || ctrl.appCtx.PGIndexStore == nil {
		return nil, false
	}
	store, ok := ctrl.appCtx.PGIndexStore.(gonzbnetAdminStore)
	return store, ok
}
