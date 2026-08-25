package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFixtureRequiresLoginAndReturnsSelectedCompletedTorrent(t *testing.T) {
	cfg := fixtureConfig{
		Username: "worker", Password: "secret", CandidateTag: "candidate",
		Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Synthetic",
		ContentPath: "/fixture/source/Synthetic", Size: 123, Tracker: "https://tracker.example/announce",
	}
	server := httptest.NewServer(newHandler(cfg))
	defer server.Close()

	form := url.Values{"username": {"worker"}, "password": {"secret"}}
	response, err := http.PostForm(server.URL+"/api/v2/auth/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) != 1 {
		t.Fatalf("login status=%d cookies=%d", response.StatusCode, len(response.Cookies()))
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/torrents/info?filter=completed&hashes="+cfg.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(response.Cookies()[0])
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var torrents []torrentResponse
	if err := json.NewDecoder(response.Body).Decode(&torrents); err != nil {
		t.Fatal(err)
	}
	if len(torrents) != 1 || torrents[0].ContentPath != cfg.ContentPath || torrents[0].Progress != 1 {
		t.Fatalf("unexpected torrents: %+v", torrents)
	}
}

func TestFixtureRejectsWrongCandidateSelection(t *testing.T) {
	cfg := fixtureConfig{
		Username: "worker", Password: "secret", CandidateTag: "candidate",
		Hash: strings.Repeat("a", 40), Name: "Synthetic", ContentPath: "/fixture/source/Synthetic", Size: 123,
	}
	server := httptest.NewServer(newHandler(cfg))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v2/torrents/info?filter=completed&tag=wrong", nil)
	request.AddCookie(&http.Cookie{Name: "SID", Value: "worker-fixture-session"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
