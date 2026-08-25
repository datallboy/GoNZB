package qbittorrent

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompletedAuthenticatesAndFiltersCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			if r.FormValue("username") != "user" || r.FormValue("password") != "pass" {
				http.Error(w, "bad auth", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if cookie, err := r.Cookie("SID"); err != nil || cookie.Value != "session" {
				http.Error(w, "missing session", http.StatusForbidden)
				return
			}
			if r.URL.Query().Get("filter") != "completed" || r.URL.Query().Get("tag") != "gonzb-candidate" {
				http.Error(w, "bad filter", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
 {"hash":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB","name":"newer","size":20,"progress":1,"completion_on":2},
 {"hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","name":"older","size":10,"progress":1,"completion_on":1},
 {"hash":"CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC","name":"partial","size":30,"progress":0.9,"completion_on":3}
]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "user", "pass", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	torrents, err := client.Completed(t.Context(), "gonzb-candidate", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(torrents) != 2 || torrents[0].Name != "older" || torrents[0].Hash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected torrents: %+v", torrents)
	}
}

func TestReverseProxyPrefixAndHTTPBasicAuthentication(t *testing.T) {
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-pass"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantBasic {
			http.Error(w, "missing proxy auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/qbittorrent/api/v2/auth/login":
			if r.FormValue("username") != "qbit-user" || r.FormValue("password") != "qbit-pass" {
				http.Error(w, "bad qBittorrent auth", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/qbittorrent"})
			_, _ = w.Write([]byte("Ok."))
		case "/qbittorrent/api/v2/torrents/info":
			if cookie, err := r.Cookie("SID"); err != nil || cookie.Value != "session" {
				http.Error(w, "missing session", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","name":"candidate","size":10,"progress":1}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(
		server.URL+"/qbittorrent/",
		"qbit-user",
		"qbit-pass",
		time.Second,
		WithHTTPBasicAuth("proxy-user", "proxy-pass"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	torrents, err := client.Completed(t.Context(), "gonzb-candidate", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(torrents) != 1 || torrents[0].Name != "candidate" {
		t.Fatalf("unexpected torrents: %+v", torrents)
	}
}

func TestTrackerIdentityRemovesPrivatePasskey(t *testing.T) {
	got := TrackerIdentity("https://user:secret@Tracker.Example/announce/private-passkey?token=also-secret")
	if got != "tracker.example" {
		t.Fatalf("tracker identity=%q", got)
	}
}
