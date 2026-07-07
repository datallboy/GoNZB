package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

const indexerOverviewStreamInterval = 250 * time.Millisecond

type indexerOverviewStreamSnapshot struct {
	NNTPStats       *app.NNTPRuntimeStats           `json:"nntp"`
	StageThroughput *pgindex.IndexerStageThroughput `json:"throughput"`
}

type IndexerAdminController struct {
	Service indexerService
}

func NewIndexerAdminController(service indexerService) *IndexerAdminController {
	return &IndexerAdminController{Service: service}
}

func (ctrl *IndexerAdminController) GetOverview(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	overview, err := ctrl.Service.Overview(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, overview)
}

func (ctrl *IndexerAdminController) GetDashboardStats(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	stats, err := ctrl.Service.DashboardStats(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, stats)
}

func (ctrl *IndexerAdminController) RefreshDashboardStats(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	stats, err := ctrl.Service.RefreshDashboardStats(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, stats)
}

func (ctrl *IndexerAdminController) GetStorageStatus(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	status, err := ctrl.Service.StorageStatus(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, status)
}

func (ctrl *IndexerAdminController) GetStorageAudit(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	report, err := ctrl.Service.StorageAudit(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, report)
}

func (ctrl *IndexerAdminController) GetBackfillProgress(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	items, err := ctrl.Service.BackfillProgress(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *IndexerAdminController) GetRecoveryCapacity(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	item, err := ctrl.Service.RecoveryCapacity(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, item)
}

func (ctrl *IndexerAdminController) ListGroupProfiles(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, _, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	items, err := ctrl.Service.GroupProfiles(c.Request().Context(), limit)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *IndexerAdminController) ListDeferredArticleRanges(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, _, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	items, err := ctrl.Service.DeferredArticleRanges(c.Request().Context(), queryParamTrimmed(c, "state"), limit)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *IndexerAdminController) ListArticleCohorts(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	limit := parseIntDefault(queryParamTrimmed(c, "limit"), 100)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := parseIntDefault(queryParamTrimmed(c, "offset"), 0)
	if offset < 0 {
		offset = 0
	}
	items, total, err := ctrl.Service.ListArticleCohorts(c.Request().Context(), pgindex.IndexerArticleCohortParams{
		Kind:   strings.TrimSpace(c.QueryParam("kind")),
		Status: strings.TrimSpace(c.QueryParam("status")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items), "total": total})
}

func (ctrl *IndexerAdminController) GetStageThroughput(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	items, err := ctrl.Service.StageThroughput(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, items)
}

func (ctrl *IndexerAdminController) GetNNTPStats(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	stats, err := ctrl.Service.NNTPStats(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, stats)
}

func (ctrl *IndexerAdminController) StreamOverview(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	rw := c.Response()
	rc := http.NewResponseController(rw)
	rw.Header().Set(echo.HeaderContentType, "text/event-stream")
	rw.Header().Set(echo.HeaderCacheControl, "no-cache")
	rw.Header().Set(echo.HeaderConnection, "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no")

	if _, err := fmt.Fprintf(rw, "retry: 3000\n\n"); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return nil
	}

	if err := ctrl.writeOverviewStreamEvent(c, rw, rc); err != nil {
		return err
	}

	ticker := time.NewTicker(indexerOverviewStreamInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-ticker.C:
			if err := ctrl.writeOverviewStreamEvent(c, rw, rc); err != nil {
				return err
			}
		}
	}
}

func (ctrl *IndexerAdminController) writeOverviewStreamEvent(c *echo.Context, rw http.ResponseWriter, rc *http.ResponseController) error {
	snapshot, err := ctrl.overviewStreamSnapshot(c.Request().Context())
	if err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(rw, "event: overview\ndata: %s\n\n", string(data)); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return nil
	}
	return nil
}

func (ctrl *IndexerAdminController) overviewStreamSnapshot(ctx context.Context) (*indexerOverviewStreamSnapshot, error) {
	nntp, err := ctrl.Service.NNTPStats(ctx)
	if err != nil {
		return nil, err
	}
	throughput, err := ctrl.Service.StageThroughput(ctx)
	if err != nil {
		return nil, err
	}
	return &indexerOverviewStreamSnapshot{
		NNTPStats:       nntp,
		StageThroughput: throughput,
	}, nil
}

func (ctrl *IndexerAdminController) ListStages(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	items, err := ctrl.Service.ListStages(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (ctrl *IndexerAdminController) GetStage(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	stage, err := ctrl.Service.GetStage(c.Request().Context(), pathParamTrimmed(c, "stage"))
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"stage": stage})
}

func (ctrl *IndexerAdminController) PatchStage(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	var patch indexerStageConfigPatch
	if err := decodeJSONBody(c, &patch); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	stage, err := ctrl.Service.UpdateStageConfig(c.Request().Context(), pathParamTrimmed(c, "stage"), patch)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"stage": stage})
}

func (ctrl *IndexerAdminController) ListMaintenanceTasks(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	items, err := ctrl.Service.ListMaintenanceTasks(c.Request().Context())
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *IndexerAdminController) DryRunMaintenanceTask(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	result, err := ctrl.Service.DryRunMaintenanceTask(c.Request().Context(), pathParamTrimmed(c, "task"))
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

func (ctrl *IndexerAdminController) RunMaintenanceTask(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	result, err := ctrl.Service.RunMaintenanceTask(c.Request().Context(), pathParamTrimmed(c, "task"))
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

func (ctrl *IndexerAdminController) PatchMaintenanceTask(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	var patch indexerMaintenanceTaskPatch
	if err := decodeJSONBody(c, &patch); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	task, err := ctrl.Service.UpdateMaintenanceTask(c.Request().Context(), pathParamTrimmed(c, "task"), patch)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"task": task})
}

func (ctrl *IndexerAdminController) ListRuns(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	params := pgindex.IndexerStageRunListParams{
		StageName:   queryParamLower(c, "stage"),
		Status:      queryParamLower(c, "status"),
		TriggerKind: queryParamLower(c, "trigger_kind"),
		Limit:       limit,
	}
	items, err := ctrl.Service.ListRuns(c.Request().Context(), params)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	if offset > len(items) {
		offset = len(items)
	}
	page := items[offset:]
	return c.JSON(http.StatusOK, map[string]any{
		"items":    page,
		"count":    len(page),
		"limit":    limit,
		"offset":   offset,
		"stage":    params.StageName,
		"status":   params.Status,
		"trigger":  params.TriggerKind,
		"has_more": false,
	})
}

func (ctrl *IndexerAdminController) GetRun(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	runID, err := parseInt64PathParam(c, "id")
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	run, err := ctrl.Service.GetRun(c.Request().Context(), runID)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	if run == nil {
		return jsonError(c, http.StatusNotFound, "run not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"run": run})
}

func (ctrl *IndexerAdminController) ListAttention(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	items, total, err := ctrl.Service.ListAdminAttention(c.Request().Context(), pgindex.IndexerAdminAttentionParams{
		Reason: queryParamTrimmed(c, "reason"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"items":    items,
		"count":    len(items),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(items) < total,
	})
}

func (ctrl *IndexerAdminController) ListReleases(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	items, total, err := ctrl.Service.ListAdminReleases(c.Request().Context(), pgindex.AdminIndexerReleaseListParams{
		Query:                    queryParamTrimmed(c, "q"),
		Newsgroup:                queryParamTrimmed(c, "newsgroup"),
		Limit:                    limit,
		Offset:                   offset,
		Sort:                     queryParamTrimmed(c, "sort"),
		CategoryID:               pgindex.ParseAdminCategoryID(queryParamTrimmed(c, "category_id")),
		Classification:           queryParamTrimmed(c, "classification"),
		ExternalMediaType:        queryParamTrimmed(c, "external_media_type"),
		IdentityStatus:           queryParamTrimmed(c, "identity_status"),
		PasswordState:            queryParamTrimmed(c, "password_state"),
		MediaQualityTier:         queryParamTrimmed(c, "media_quality_tier"),
		Hidden:                   queryParamTrimmed(c, "hidden"),
		PublicState:              queryParamTrimmed(c, "public_state"),
		Inspected:                queryParamTrimmed(c, "inspected"),
		Enriched:                 queryParamTrimmed(c, "enriched"),
		Uncategorized:            queryParamTrimmed(c, "uncategorized"),
		PasswordCandidates:       queryParamTrimmed(c, "password_candidates"),
		MetadataMismatch:         queryParamTrimmed(c, "metadata_mismatch"),
		LowConfidence:            queryParamTrimmed(c, "low_confidence"),
		CompletionState:          queryParamTrimmed(c, "completion_state"),
		PayloadCompletionInclude: queryParamTrimmed(c, "payload_completion_include"),
		PayloadCompletionExclude: queryParamTrimmed(c, "payload_completion_exclude"),
		HasNFO:                   parseOptionalBoolQuery(c, "has_nfo"),
		HasPAR2:                  parseOptionalBoolQuery(c, "has_par2"),
	})
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"items":    items,
		"count":    len(items),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(items) < total,
	})
}

func (ctrl *IndexerAdminController) GetRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	releaseView, err := ctrl.Service.GetAdminRelease(c.Request().Context(), pathParamTrimmed(c, "id"))
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	if releaseView == nil || releaseView.Release == nil {
		return jsonError(c, http.StatusNotFound, "release not found")
	}
	return c.JSON(http.StatusOK, releaseView)
}

func (ctrl *IndexerAdminController) ListBinaries(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	items, total, err := ctrl.Service.ListBinaries(c.Request().Context(), pgindex.IndexerBinaryListParams{
		Query:            queryParamTrimmed(c, "q"),
		GroupName:        queryParamTrimmed(c, "newsgroup"),
		IdentityStrength: queryParamTrimmed(c, "identity_strength"),
		ReadinessBucket:  queryParamTrimmed(c, "readiness_bucket"),
		MatchStatus:      queryParamTrimmed(c, "match_status"),
		ReleaseState:     queryParamTrimmed(c, "release_state"),
		Sort:             queryParamTrimmed(c, "sort"),
		Limit:            limit,
		Offset:           offset,
	})
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"items":    items,
		"count":    len(items),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(items) < total,
	})
}

func (ctrl *IndexerAdminController) PatchRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	var patch indexerReleaseOverridePatch
	if err := decodeJSONBody(c, &patch); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	override, err := ctrl.Service.UpdateReleaseOverride(c.Request().Context(), pathParamTrimmed(c, "id"), patch)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"override": override})
}

func (ctrl *IndexerAdminController) IdentifyRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	var patch indexerReleaseIdentityPatch
	if err := decodeJSONBody(c, &patch); err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	release, err := ctrl.Service.IdentifyRelease(c.Request().Context(), pathParamTrimmed(c, "id"), patch)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"release": release})
}

func (ctrl *IndexerAdminController) HideRelease(c *echo.Context) error {
	v := true
	override, err := ctrl.Service.UpdateReleaseOverride(c.Request().Context(), pathParamTrimmed(c, "id"), indexerReleaseOverridePatch{Hidden: &v})
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"override": override, "action": "hide"})
}

func (ctrl *IndexerAdminController) UnhideRelease(c *echo.Context) error {
	v := false
	override, err := ctrl.Service.UpdateReleaseOverride(c.Request().Context(), pathParamTrimmed(c, "id"), indexerReleaseOverridePatch{Hidden: &v})
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"override": override, "action": "unhide"})
}

func (ctrl *IndexerAdminController) ReinspectRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	releaseID := pathParamTrimmed(c, "id")
	if err := ctrl.Service.ReinspectRelease(c.Request().Context(), releaseID); err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusAccepted, map[string]any{
		"release_id": releaseID,
		"action":     "reinspect",
		"status":     "accepted",
	})
}

func (ctrl *IndexerAdminController) ReenrichRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)
	releaseID := pathParamTrimmed(c, "id")
	if err := ctrl.Service.ReenrichRelease(c.Request().Context(), releaseID); err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	return c.JSON(http.StatusAccepted, map[string]any{
		"release_id": releaseID,
		"action":     "reenrich",
		"status":     "accepted",
	})
}

func (ctrl *IndexerAdminController) RunStage(c *echo.Context) error {
	return ctrl.runStageAction(c, "run")
}

func (ctrl *IndexerAdminController) PauseStage(c *echo.Context) error {
	return ctrl.runStageAction(c, "pause")
}

func (ctrl *IndexerAdminController) ResumeStage(c *echo.Context) error {
	return ctrl.runStageAction(c, "resume")
}

func (ctrl *IndexerAdminController) runStageAction(c *echo.Context, action string) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	stageName := pathParamTrimmed(c, "stage")
	switch action {
	case "run":
		if err := ctrl.Service.RunStage(c.Request().Context(), stageName); err != nil {
			return jsonError(c, indexerErrorStatus(err), err.Error())
		}
		return c.JSON(http.StatusAccepted, map[string]any{
			"stage_name": normalizeStageName(stageName),
			"action":     action,
			"status":     "accepted",
		})
	case "pause":
		stage, err := ctrl.Service.PauseStage(c.Request().Context(), stageName)
		if err != nil {
			return jsonError(c, indexerErrorStatus(err), err.Error())
		}
		return c.JSON(http.StatusOK, map[string]any{"stage": stage, "action": action})
	case "resume":
		stage, err := ctrl.Service.ResumeStage(c.Request().Context(), stageName)
		if err != nil {
			return jsonError(c, indexerErrorStatus(err), err.Error())
		}
		return c.JSON(http.StatusOK, map[string]any{"stage": stage, "action": action})
	default:
		return jsonError(c, http.StatusBadRequest, "unsupported stage action")
	}
}
