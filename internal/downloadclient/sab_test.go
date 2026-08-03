package downloadclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
)

func TestSendNZBUsesSABAddFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if got := r.FormValue("mode"); got != "addfile" {
			t.Fatalf("mode=%q", got)
		}
		if got := r.FormValue("apikey"); got != "secret" {
			t.Fatalf("apikey=%q", got)
		}
		file, _, err := r.FormFile("name")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "<nzb/>" {
			t.Fatalf("body=%q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true,"nzo_ids":["SABnzbd_nzo_1"]}`))
	}))
	defer server.Close()

	jobID, err := sendNZB(context.Background(), app.DownloadClientRuntimeSettings{
		BaseURL: server.URL, APIKey: "secret", Category: "movies", Priority: 0,
	}, "Movie.nzb", strings.NewReader("<nzb/>"))
	if err != nil {
		t.Fatal(err)
	}
	if jobID != "SABnzbd_nzo_1" {
		t.Fatalf("jobID=%q", jobID)
	}
}

func TestSABEndpointPreservesInstallPrefix(t *testing.T) {
	endpoint, err := sabEndpoint(app.DownloadClientRuntimeSettings{BaseURL: "https://example.test/sabnzbd/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := endpoint.String(); got != "https://example.test/sabnzbd/api" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestSABEndpointRejectsEmbeddedCredentials(t *testing.T) {
	_, err := sabEndpoint(app.DownloadClientRuntimeSettings{BaseURL: "https://user:password@example.test"})
	if err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("expected embedded credentials to be rejected, got %v", err)
	}
}

func TestSendNZBRejectsRedirect(t *testing.T) {
	reachedTarget := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reachedTarget = true
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	_, err := sendNZB(context.Background(), app.DownloadClientRuntimeSettings{
		BaseURL: redirect.URL,
		APIKey:  "secret",
	}, "Movie.nzb", strings.NewReader("<nzb/>"))
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("expected redirect response to be rejected, got %v", err)
	}
	if reachedTarget {
		t.Fatal("redirect target unexpectedly received the NZB")
	}
}

func TestSafeNZBFilenameRemovesHeaderAndPathCharacters(t *testing.T) {
	if got := safeNZBFilename("../Movie\r\nX-Evil: yes\\sample"); got != ".._Movie__X-Evil: yes_sample.nzb" {
		t.Fatalf("safe filename = %q", got)
	}
}

func TestClientTransportErrorDoesNotExposeAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()

	err := testClient(context.Background(), app.DownloadClientRuntimeSettings{BaseURL: baseURL, APIKey: "never-log-this"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "never-log-this") {
		t.Fatalf("transport error exposed API key: %v", err)
	}
}

func TestDefaultClientPrefersExplicitDefault(t *testing.T) {
	client, err := defaultClient([]app.DownloadClientRuntimeSettings{
		{ID: "first", Enabled: true},
		{ID: "preferred", Enabled: true, Default: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.ID != "preferred" {
		t.Fatalf("client=%q", client.ID)
	}
}
