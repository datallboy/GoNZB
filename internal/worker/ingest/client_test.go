package ingest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/uploader"
)

func TestSubmitUsesAuthenticatedIdempotentUploaderContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/uploader/submissions" || r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Idempotency-Key") != "job-1" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fields := map[string][]byte{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fields[part.FormName()], err = io.ReadAll(part)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		var metadata uploader.Metadata
		if err := json.Unmarshal(fields["metadata"], &metadata); err != nil || metadata.Provenance.ExternalID != "torrent-hash" || metadata.Password != "archive-password" {
			http.Error(w, "bad metadata", http.StatusBadRequest)
			return
		}
		var provenance map[string]any
		if err := json.Unmarshal(fields["artifact"], &provenance); err != nil || provenance["worker_node_id"] != "node-1" {
			http.Error(w, "bad provenance", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true,"submission":{"id":"submission-1","release_id":"release-1"}}`))
	}))
	defer server.Close()

	nzbPath := filepath.Join(t.TempDir(), "release.nzb")
	if err := os.WriteFile(nzbPath, []byte("<nzb/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(server.URL, "token", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Submit(t.Context(), Request{
		JobID: "job-1", NZBPath: nzbPath, ReleaseName: "Release", TorrentHash: "torrent-hash",
		SourceTracker: "tracker", SourceSize: 123, PostedAt: time.Now(), WorkerNodeID: "node-1",
		Password: "archive-password", PestoVersion: "pesto 0.5.7", Obfuscated: true, Encrypted: true, HasPAR2: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseID != "release-1" || !result.Created {
		t.Fatalf("unexpected result: %+v", result)
	}
}
