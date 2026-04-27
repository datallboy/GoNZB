package controllers

import (
	"net/http"

	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

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

func (ctrl *IndexerAdminController) ListRuns(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}
	stageName := queryParamLower(c, "stage")
	items, err := ctrl.Service.ListRuns(c.Request().Context(), stageName, limit)
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
		"stage":    stageName,
		"has_more": false,
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
		Query:              queryParamTrimmed(c, "q"),
		Limit:              limit,
		Offset:             offset,
		Sort:               queryParamTrimmed(c, "sort"),
		CategoryID:         pgindex.ParseAdminCategoryID(queryParamTrimmed(c, "category_id")),
		Classification:     queryParamTrimmed(c, "classification"),
		ExternalMediaType:  queryParamTrimmed(c, "external_media_type"),
		IdentityStatus:     queryParamTrimmed(c, "identity_status"),
		PasswordState:      queryParamTrimmed(c, "password_state"),
		MediaQualityTier:   queryParamTrimmed(c, "media_quality_tier"),
		Hidden:             queryParamTrimmed(c, "hidden"),
		PublicState:        queryParamTrimmed(c, "public_state"),
		Inspected:          queryParamTrimmed(c, "inspected"),
		Enriched:           queryParamTrimmed(c, "enriched"),
		Uncategorized:      queryParamTrimmed(c, "uncategorized"),
		PasswordCandidates: queryParamTrimmed(c, "password_candidates"),
		MetadataMismatch:   queryParamTrimmed(c, "metadata_mismatch"),
		LowConfidence:      queryParamTrimmed(c, "low_confidence"),
		HasNFO:             parseOptionalBoolQuery(c, "has_nfo"),
		HasPAR2:            parseOptionalBoolQuery(c, "has_par2"),
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
