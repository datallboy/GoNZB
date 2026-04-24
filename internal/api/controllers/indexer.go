package controllers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/labstack/echo/v5"
)

type IndexerController struct {
	Service indexerService
}

const (
	indexerContractScopeHeader        = "X-Gonzb-Indexer-Contract-Scope"
	indexerContractScopePublic        = "public"
	indexerContractScopeInternalDebug = "internal-debug"
)

func NewIndexerController(appCtx *app.Context) *IndexerController {
	return &IndexerController{
		Service: newIndexerService(appCtx),
	}
}

func (ctrl *IndexerController) GetOverview(c *echo.Context) error {
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

func (ctrl *IndexerController) ListStages(c *echo.Context) error {
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

func (ctrl *IndexerController) ListRuns(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	limit, _, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	stageName := queryParamLower(c, "stage")
	items, err := ctrl.Service.ListRuns(c.Request().Context(), stageName, limit)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}

	return c.JSON(http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
		"limit": limit,
		"stage": stageName,
	})
}

func (ctrl *IndexerController) RunStage(c *echo.Context) error {
	return ctrl.runStageAction(c, "run")
}

func (ctrl *IndexerController) PauseStage(c *echo.Context) error {
	return ctrl.runStageAction(c, "pause")
}

func (ctrl *IndexerController) ResumeStage(c *echo.Context) error {
	return ctrl.runStageAction(c, "resume")
}

func (ctrl *IndexerController) ListReleases(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopePublic)

	limit, offset, err := parsePaginationParams(c, defaultPageLimit, maxPageLimit)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	params, err := parsePublicIndexerListParams(c, limit, offset)
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	items, total, err := ctrl.Service.ListReleases(c.Request().Context(), params)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}

	return c.JSON(http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"count":  len(items),
		"limit":  params.Limit,
		"offset": params.Offset,
		"sort":   params.Sort,
		"filters": map[string]any{
			"q":                  params.Query,
			"classification":     params.Classification,
			"has_nfo":            params.HasNFO,
			"has_par2":           params.HasPAR2,
			"password_state":     params.PasswordState,
			"availability_tier":  params.AvailabilityTier,
			"media_quality_tier": params.MediaQualityTier,
			"completion_min":     params.CompletionMin,
			"posted_after":       params.PostedAfter,
			"posted_before":      params.PostedBefore,
			"size_min":           params.SizeMin,
			"size_max":           params.SizeMax,
			"metadata_status":    params.MetadataStatus,
		},
		"has_more": params.Offset+len(items) < total,
	})
}

func (ctrl *IndexerController) GetRelease(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopePublic)

	releaseID := pathParamTrimmed(c, "id")
	release, err := ctrl.Service.GetRelease(c.Request().Context(), releaseID)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	if release == nil {
		return jsonError(c, http.StatusNotFound, "release not found")
	}

	return c.JSON(http.StatusOK, release)
}

func (ctrl *IndexerController) GetBinary(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	binaryID, err := parsePathInt64(c, "id")
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	binary, err := ctrl.Service.GetBinary(c.Request().Context(), binaryID)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	if binary == nil {
		return jsonError(c, http.StatusNotFound, "binary not found")
	}

	return c.JSON(http.StatusOK, binary)
}

func (ctrl *IndexerController) GetFile(c *echo.Context) error {
	if ctrl == nil || ctrl.Service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "indexer api is unavailable")
	}
	setIndexerContractScope(c, indexerContractScopeInternalDebug)

	fileID, err := parsePathInt64(c, "id")
	if err != nil {
		return jsonError(c, http.StatusBadRequest, err.Error())
	}

	file, err := ctrl.Service.GetFile(c.Request().Context(), fileID)
	if err != nil {
		return jsonError(c, indexerErrorStatus(err), err.Error())
	}
	if file == nil {
		return jsonError(c, http.StatusNotFound, "file not found")
	}

	return c.JSON(http.StatusOK, file)
}

func (ctrl *IndexerController) runStageAction(c *echo.Context, action string) error {
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
		return c.JSON(http.StatusOK, map[string]any{
			"stage":  stage,
			"action": action,
		})
	case "resume":
		stage, err := ctrl.Service.ResumeStage(c.Request().Context(), stageName)
		if err != nil {
			return jsonError(c, indexerErrorStatus(err), err.Error())
		}
		return c.JSON(http.StatusOK, map[string]any{
			"stage":  stage,
			"action": action,
		})
	default:
		return jsonError(c, http.StatusBadRequest, "unsupported stage action")
	}
}

func parsePathInt64(c *echo.Context, name string) (int64, error) {
	raw := pathParamTrimmed(c, name)
	if raw == "" {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func parsePublicIndexerListParams(c *echo.Context, limit, offset int) (pgindex.PublicIndexerReleaseListParams, error) {
	hasNFO, err := parseOptionalBool(queryParamTrimmed(c, "has_nfo"), "has_nfo")
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	hasPAR2, err := parseOptionalBool(queryParamTrimmed(c, "has_par2"), "has_par2")
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	completionMin, err := parseOptionalFloat64(queryParamTrimmed(c, "completion_min"), "completion_min", 0, 100)
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	postedAfter, err := parseOptionalDate(queryParamTrimmed(c, "posted_after"), "posted_after")
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	postedBefore, err := parseOptionalDate(queryParamTrimmed(c, "posted_before"), "posted_before")
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	sizeMin, err := parseOptionalInt64(queryParamTrimmed(c, "size_min"), "size_min", 0, 1<<62)
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	sizeMax, err := parseOptionalInt64(queryParamTrimmed(c, "size_max"), "size_max", 0, 1<<62)
	if err != nil {
		return pgindex.PublicIndexerReleaseListParams{}, err
	}
	return pgindex.PublicIndexerReleaseListParams{
		Query:            queryParamTrimmed(c, "q"),
		Limit:            limit,
		Offset:           offset,
		Sort:             pgindexSortOrDefault(queryParamTrimmed(c, "sort")),
		Classification:   queryParamTrimmed(c, "classification"),
		HasNFO:           hasNFO,
		HasPAR2:          hasPAR2,
		PasswordState:    queryParamTrimmed(c, "password_state"),
		AvailabilityTier: queryParamTrimmed(c, "availability_tier"),
		MediaQualityTier: queryParamTrimmed(c, "media_quality_tier"),
		CompletionMin:    completionMin,
		PostedAfter:      postedAfter,
		PostedBefore:     postedBefore,
		SizeMin:          sizeMin,
		SizeMax:          sizeMax,
		MetadataStatus:   queryParamTrimmed(c, "metadata_status"),
	}, nil
}

func pgindexSortOrDefault(sort string) string {
	sort = normalizeTrimmed(sort)
	if sort == "" {
		return "posted_at_desc"
	}
	return sort
}

func setIndexerContractScope(c *echo.Context, scope string) {
	if c == nil || c.Response() == nil {
		return
	}
	c.Response().Header().Set(indexerContractScopeHeader, scope)
}
