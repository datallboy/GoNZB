package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/uploader"
	"github.com/labstack/echo/v5"
)

type UploaderController struct {
	service            *uploader.Service
	federation         *uploader.FederationService
	maxNZBBytes        int64
	maxMetaBytes       int64
	maxArtifactBytes   int64
	maxSubmissionBytes int64
}

func NewUploaderController(service *uploader.Service, federation *uploader.FederationService, maxNZBBytes, maxMetaBytes, maxArtifactBytes, maxSubmissionBytes int64) *UploaderController {
	if maxNZBBytes <= 0 {
		maxNZBBytes = 64 << 20
	}
	if maxMetaBytes <= 0 {
		maxMetaBytes = 1 << 20
	}
	if maxArtifactBytes <= 0 {
		maxArtifactBytes = 32 << 20
	}
	if maxSubmissionBytes <= 0 {
		maxSubmissionBytes = 128 << 20
	}
	return &UploaderController{service: service, federation: federation, maxNZBBytes: maxNZBBytes, maxMetaBytes: maxMetaBytes, maxArtifactBytes: maxArtifactBytes, maxSubmissionBytes: maxSubmissionBytes}
}

func (ctrl *UploaderController) Create(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	nzbBytes, filename, metadata, artifacts, err := ctrl.readSubmission(c)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	principal, _ := PrincipalFromContext(c)
	intakeKind := uploader.IntakeManual
	if strings.HasPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ") {
		intakeKind = uploader.IntakeHTTP
	}
	result, err := ctrl.service.Submit(c.Request().Context(), uploader.SubmitInput{
		NZBBytes:         nzbBytes,
		OriginalFilename: filename,
		Metadata:         metadata,
		IntakeKind:       intakeKind,
		SubmittedBy:      principalActor(principal),
		IdempotencyKey:   strings.TrimSpace(c.Request().Header.Get("Idempotency-Key")),
		Artifacts:        artifacts,
	})
	if err != nil {
		return uploaderJSONError(c, err)
	}
	result.Submission.Password = ""
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	return c.JSON(status, map[string]any{
		"submission": result.Submission,
		"created":    result.Created,
	})
}

func (ctrl *UploaderController) List(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	state := uploader.State(queryParamTrimmed(c, "state"))
	if state != "" && state != uploader.StatePendingReview && state != uploader.StateApproved && state != uploader.StateRejected {
		return jsonError(c, http.StatusBadRequest, "invalid uploader state")
	}
	categoryID, _ := strconv.Atoi(queryParamTrimmed(c, "category_id"))
	limit := boundedInt(queryParamTrimmed(c, "limit"), 100, 1, 500)
	offset := boundedInt(queryParamTrimmed(c, "offset"), 0, 0, 100000)
	items, err := ctrl.service.List(c.Request().Context(), uploader.ListFilter{
		State: state, Query: queryParamTrimmed(c, "q"), CategoryID: categoryID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *UploaderController) Get(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	reveal := principalHasPermission(c, auth.PermissionUploaderSubmissionsReview)
	item, err := ctrl.service.Get(c.Request().Context(), pathParamTrimmed(c, "id"), reveal)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	events, err := ctrl.service.Events(c.Request().Context(), item.ID)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	publications, err := ctrl.service.Store().ListFederationPublications(c.Request().Context(), item.ID)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"submission": item, "events": events, "federation_publications": publications})
}

func (ctrl *UploaderController) EligibleFederationPools(c *echo.Context) error {
	if ctrl == nil || ctrl.federation == nil {
		return jsonError(c, http.StatusServiceUnavailable, "GoNZBNet publication is unavailable")
	}
	items, err := ctrl.federation.EligiblePools(c.Request().Context())
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *UploaderController) ListFederationPublications(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	items, err := ctrl.service.Store().ListFederationPublications(c.Request().Context(), pathParamTrimmed(c, "id"))
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *UploaderController) CreateFederationPublications(c *echo.Context) error {
	if ctrl == nil || ctrl.federation == nil {
		return jsonError(c, http.StatusServiceUnavailable, "GoNZBNet publication is unavailable")
	}
	var body struct {
		PoolIDs []string `json:"pool_ids"`
	}
	if err := decodeJSONBody(c, &body); err != nil {
		return jsonError(c, http.StatusBadRequest, "invalid request body")
	}
	principal, _ := PrincipalFromContext(c)
	items, err := ctrl.federation.Request(c.Request().Context(), pathParamTrimmed(c, "id"), body.PoolIDs, principalActor(principal))
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"items": items, "count": len(items)})
}

func (ctrl *UploaderController) WithdrawFederationPublication(c *echo.Context) error {
	if ctrl == nil || ctrl.federation == nil {
		return jsonError(c, http.StatusServiceUnavailable, "GoNZBNet publication is unavailable")
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if c.Request().ContentLength != 0 {
		if err := decodeJSONBody(c, &body); err != nil {
			return jsonError(c, http.StatusBadRequest, "invalid request body")
		}
	}
	principal, _ := PrincipalFromContext(c)
	item, err := ctrl.federation.Withdraw(c.Request().Context(), pathParamTrimmed(c, "id"), pathParamTrimmed(c, "pool_id"), principalActor(principal), body.Reason)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"publication": item})
}

func (ctrl *UploaderController) Update(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	var body struct {
		Title       *string `json:"title"`
		CategoryID  *int    `json:"category_id"`
		PostedAt    *string `json:"posted_at"`
		Password    *string `json:"password"`
		IMDBID      *string `json:"imdb_id"`
		TMDBID      *int64  `json:"tmdb_id"`
		TVDBID      *int64  `json:"tvdb_id"`
		Year        *int    `json:"year"`
		Resolution  *string `json:"resolution"`
		MediaSource *string `json:"media_source"`
		VideoCodec  *string `json:"video_codec"`
		AudioCodec  *string `json:"audio_codec"`
		Note        string  `json:"note"`
	}
	if err := decodeJSONBody(c, &body); err != nil {
		return jsonError(c, http.StatusBadRequest, "invalid request body")
	}
	var postedAt *time.Time
	if body.PostedAt != nil {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.PostedAt))
		if err != nil {
			return jsonError(c, http.StatusBadRequest, "posted_at must be RFC3339")
		}
		parsed = parsed.UTC()
		postedAt = &parsed
	}
	principal, _ := PrincipalFromContext(c)
	item, err := ctrl.service.Update(c.Request().Context(), pathParamTrimmed(c, "id"), uploader.Update{
		Title: body.Title, CategoryID: body.CategoryID, PostedAt: postedAt, Password: body.Password,
		IMDBID: body.IMDBID, TMDBID: body.TMDBID, TVDBID: body.TVDBID, Year: body.Year,
		Resolution: body.Resolution, MediaSource: body.MediaSource, VideoCodec: body.VideoCodec,
		AudioCodec: body.AudioCodec, Note: body.Note, Actor: principalActor(principal),
	}, true)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"submission": item})
}

func (ctrl *UploaderController) Approve(c *echo.Context) error {
	return ctrl.transition(c, uploader.StateApproved)
}

func (ctrl *UploaderController) Reject(c *echo.Context) error {
	return ctrl.transition(c, uploader.StateRejected)
}

func (ctrl *UploaderController) ReturnToPending(c *echo.Context) error {
	return ctrl.transition(c, uploader.StatePendingReview)
}

func (ctrl *UploaderController) DownloadNZB(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	item, err := ctrl.service.Get(c.Request().Context(), pathParamTrimmed(c, "id"), false)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	reader, err := ctrl.service.OpenNZB(c.Request().Context(), item.ID, false)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	defer reader.Close()
	filename := item.OriginalFilename
	if !strings.HasSuffix(strings.ToLower(filename), ".nzb") {
		filename = item.ID + ".nzb"
	}
	c.Response().Header().Set(echo.HeaderContentDisposition, contentDispositionFilename(filename))
	return c.Stream(http.StatusOK, "application/x-nzb", reader)
}

func (ctrl *UploaderController) DownloadArtifact(c *echo.Context) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	item, reader, err := ctrl.service.OpenArtifact(c.Request().Context(), pathParamTrimmed(c, "id"), pathParamTrimmed(c, "artifact_id"))
	if err != nil {
		return uploaderJSONError(c, err)
	}
	defer reader.Close()
	c.Response().Header().Set(echo.HeaderContentDisposition, contentDispositionFilename(item.OriginalFilename))
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, firstNonBlankString(item.DetectedMediaType, "application/octet-stream"), reader)
}

func (ctrl *UploaderController) transition(c *echo.Context, next uploader.State) error {
	if ctrl == nil || ctrl.service == nil {
		return jsonError(c, http.StatusServiceUnavailable, "uploader runtime is unavailable")
	}
	var body struct {
		Note string `json:"note"`
	}
	if c.Request().ContentLength != 0 {
		if err := decodeJSONBody(c, &body); err != nil {
			return jsonError(c, http.StatusBadRequest, "invalid request body")
		}
	}
	principal, _ := PrincipalFromContext(c)
	item, err := ctrl.service.Transition(c.Request().Context(), pathParamTrimmed(c, "id"), next, principalActor(principal), body.Note, true)
	if err != nil {
		return uploaderJSONError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"submission": item})
}

func (ctrl *UploaderController) readSubmission(c *echo.Context) ([]byte, string, uploader.Metadata, []uploader.ArtifactInput, error) {
	reader, err := c.Request().MultipartReader()
	if err != nil {
		return nil, "", uploader.Metadata{}, nil, fmt.Errorf("request must be multipart/form-data")
	}
	var nzbBytes []byte
	var filename string
	var metadata uploader.Metadata
	seenMetadata := false
	artifacts := make([]uploader.ArtifactInput, 0)
	var totalBytes int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", metadata, nil, fmt.Errorf("read multipart request: %w", err)
		}
		switch part.FormName() {
		case "nzb":
			if nzbBytes != nil {
				_ = part.Close()
				return nil, "", metadata, nil, fmt.Errorf("exactly one nzb file is required")
			}
			nzbBytes, err = readMultipartPart(part, ctrl.maxNZBBytes)
			filename = part.FileName()
			totalBytes += int64(len(nzbBytes))
		case "metadata":
			if seenMetadata {
				_ = part.Close()
				return nil, "", metadata, nil, fmt.Errorf("metadata must be supplied at most once")
			}
			seenMetadata = true
			var raw []byte
			raw, err = readMultipartPart(part, ctrl.maxMetaBytes)
			if err == nil && len(bytes.TrimSpace(raw)) > 0 {
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.DisallowUnknownFields()
				err = decoder.Decode(&metadata)
				if err == nil {
					var trailing any
					if trailingErr := decoder.Decode(&trailing); !errors.Is(trailingErr, io.EOF) {
						err = fmt.Errorf("metadata JSON must contain exactly one value")
					}
				}
				if err != nil {
					err = fmt.Errorf("invalid metadata JSON: %w", err)
				}
			}
			totalBytes += int64(len(raw))
		case "artifact":
			var payload []byte
			payload, err = readMultipartPart(part, ctrl.maxArtifactBytes)
			if err == nil {
				artifacts = append(artifacts, uploader.ArtifactInput{
					Filename: part.FileName(), DeclaredMediaType: part.Header.Get(echo.HeaderContentType), Payload: payload,
				})
				totalBytes += int64(len(payload))
			}
		default:
			err = fmt.Errorf("unsupported multipart field %q", part.FormName())
		}
		_ = part.Close()
		if err != nil {
			return nil, "", metadata, nil, err
		}
		if totalBytes > ctrl.maxSubmissionBytes {
			return nil, "", metadata, nil, fmt.Errorf("submission exceeds %d byte limit", ctrl.maxSubmissionBytes)
		}
	}
	if nzbBytes == nil {
		return nil, "", metadata, nil, fmt.Errorf("exactly one nzb file is required")
	}
	return nzbBytes, filename, metadata, artifacts, nil
}

func firstNonBlankString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func readMultipartPart(part *multipart.Part, limit int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("multipart field exceeds %d byte limit", limit)
	}
	return payload, nil
}

func uploaderJSONError(c *echo.Context, err error) error {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, uploader.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, uploader.ErrConflict), errors.Is(err, uploader.ErrInvalidTransition):
		status = http.StatusConflict
	case strings.Contains(strings.ToLower(err.Error()), "exceeds"):
		status = http.StatusRequestEntityTooLarge
	case strings.Contains(strings.ToLower(err.Error()), "unavailable"), strings.Contains(strings.ToLower(err.Error()), "not ready"):
		status = http.StatusServiceUnavailable
	}
	return jsonError(c, status, err.Error())
}

func principalActor(principal *auth.Principal) string {
	if principal == nil {
		return "unknown"
	}
	if strings.TrimSpace(principal.Username) != "" {
		return principal.Username
	}
	return principal.UserID
}

func principalHasPermission(c *echo.Context, permission string) bool {
	principal, ok := PrincipalFromContext(c)
	return ok && principal.Has(permission)
}

func boundedInt(raw string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
