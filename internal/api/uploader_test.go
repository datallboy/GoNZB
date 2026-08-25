package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/auth"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/sqliteuploader"
	"github.com/datallboy/gonzb/internal/uploader"
	"github.com/labstack/echo/v5"
)

const apiSyntheticNZB = `<?xml version="1.0"?><nzb><file poster="fixture@example.invalid" date="1700000000" subject="fixture.bin yEnc"><groups><group>alt.test.gonzb</group></groups><segments><segment bytes="16" number="1">fixture@example.invalid</segment></segments></file></nzb>`

type rawUploaderMetadata string

func TestUploaderAPICreateDeduplicateAndReview(t *testing.T) {
	appCtx := newAuthTestAppContext(t)
	root := t.TempDir()
	store, err := sqliteuploader.NewStore(filepath.Join(root, "uploader.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	appCtx.UploaderStore = store
	appCtx.Uploader = uploader.NewService(store, nzb.DefaultLimits())
	appCtx.Config.Modules.Uploader.Enabled = true
	appCtx.Config.Uploader = config.UploaderConfig{
		MaxNZBBytes:       1 << 20,
		MaxFiles:          100,
		MaxSegments:       1000,
		MaxXMLDepth:       32,
		MaxMetadataLength: 16 << 10,
		Inbox: config.UploaderInboxConfig{
			ScanIntervalSeconds: 15,
			SettleAgeSeconds:    60,
		},
	}

	authStore := any(appCtx.SettingsStore).(auth.Store)
	authService := auth.NewService(authStore)
	if err := authService.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	session, _, err := authService.SetupInitialUser(t.Context(), "owner", "very-secure-pass")
	if err != nil {
		t.Fatal(err)
	}
	adminToken, err := createAuthTokenForUser(t, authService, session.UserID, "uploader-test")
	if err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	RegisterRoutes(e, appCtx)
	invalidMetadata := performUploaderMultipart(t, e, adminToken, "fixture.nzb", []byte(apiSyntheticNZB), rawUploaderMetadata(`{} {}`))
	if invalidMetadata.Code != http.StatusBadRequest {
		t.Fatalf("expected trailing metadata JSON rejection, got %d body=%s", invalidMetadata.Code, invalidMetadata.Body.String())
	}

	create := performUploaderMultipart(t, e, adminToken, "fixture.nzb", []byte(apiSyntheticNZB), map[string]any{
		"title":       "Synthetic API Release",
		"category_id": 8010,
		"artifacts":   []map[string]any{{"filename": "fixture.nfo", "kind": "nfo", "label": "Fixture NFO"}},
	}, uploader.ArtifactInput{Filename: "fixture.nfo", Payload: []byte("synthetic API artifact")})
	if create.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", create.Code, create.Body.String())
	}
	var createBody struct {
		Submission uploader.Submission `json:"submission"`
		Created    bool                `json:"created"`
	}
	mustDecodeJSON(t, create, &createBody)
	if !createBody.Created || createBody.Submission.State != uploader.StatePendingReview {
		t.Fatalf("unexpected create response: %s", create.Body.String())
	}
	if len(createBody.Submission.Artifacts) != 1 {
		t.Fatalf("expected artifact metadata, got %s", create.Body.String())
	}
	artifactResponse := performJSONRequest(t, e, http.MethodGet,
		"/api/v1/uploader/submissions/"+createBody.Submission.ID+"/artifacts/"+createBody.Submission.Artifacts[0].ID,
		nil, nil, "Bearer "+adminToken)
	if artifactResponse.Code != http.StatusOK || artifactResponse.Body.String() != "synthetic API artifact" {
		t.Fatalf("unexpected artifact response: status=%d body=%q", artifactResponse.Code, artifactResponse.Body.String())
	}

	retry := performUploaderMultipart(t, e, adminToken, "fixture.nzb", []byte(apiSyntheticNZB), nil)
	if retry.Code != http.StatusOK {
		t.Fatalf("expected duplicate 200, got %d body=%s", retry.Code, retry.Body.String())
	}

	unauthorized := performUploaderMultipart(t, e, "", "fixture.nzb", []byte(apiSyntheticNZB), nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized create, got %d", unauthorized.Code)
	}
	invalidUpdate := performJSONRequest(t, e, http.MethodPatch,
		"/api/v1/uploader/submissions/"+createBody.Submission.ID,
		map[string]int{"year": -1}, nil, "Bearer "+adminToken)
	if invalidUpdate.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid review metadata rejection, got %d body=%s", invalidUpdate.Code, invalidUpdate.Body.String())
	}

	approve := performJSONRequest(t, e, http.MethodPost,
		"/api/v1/uploader/submissions/"+createBody.Submission.ID+"/actions/approve",
		map[string]string{"note": "synthetic fixture"}, nil, "Bearer "+adminToken)
	if approve.Code != http.StatusOK {
		t.Fatalf("expected approve 200, got %d body=%s", approve.Code, approve.Body.String())
	}

	list := performJSONRequest(t, e, http.MethodGet, "/api/v1/uploader/submissions?state=approved", nil, nil, "Bearer "+adminToken)
	if list.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d body=%s", list.Code, list.Body.String())
	}
	var listBody struct {
		Items []uploader.Submission `json:"items"`
	}
	mustDecodeJSON(t, list, &listBody)
	if len(listBody.Items) != 1 || listBody.Items[0].Password != "" {
		t.Fatalf("unexpected list response: %s", list.Body.String())
	}
}

func performUploaderMultipart(t *testing.T, e *echo.Echo, token, filename string, payload []byte, metadata any, artifacts ...uploader.ArtifactInput) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("nzb", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if metadata != nil {
		var raw []byte
		if value, ok := metadata.(rawUploaderMetadata); ok {
			raw = []byte(value)
		} else {
			raw, err = json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.WriteField("metadata", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, artifact := range artifacts {
		part, err := writer.CreateFormFile("artifact", artifact.Filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(artifact.Payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploader/submissions", &body)
	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}
