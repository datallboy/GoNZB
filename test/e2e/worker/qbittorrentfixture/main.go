package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type fixtureConfig struct {
	Username     string
	Password     string
	CandidateTag string
	Hash         string
	Name         string
	ContentPath  string
	Size         int64
	Tracker      string
}

type torrentResponse struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	SavePath     string  `json:"save_path"`
	ContentPath  string  `json:"content_path"`
	Size         int64   `json:"size"`
	TotalSize    int64   `json:"total_size"`
	Progress     float64 `json:"progress"`
	CompletionOn int64   `json:"completion_on"`
	Tracker      string  `json:"tracker"`
	Tags         string  `json:"tags"`
	State        string  `json:"state"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18092", "HTTP listen address")
	readyFile := flag.String("ready-file", "", "file created once the listener is ready")
	cfg := fixtureConfig{}
	flag.StringVar(&cfg.Username, "username", "worker", "accepted qBittorrent username")
	flag.StringVar(&cfg.Password, "password", "worker-local", "accepted qBittorrent password")
	flag.StringVar(&cfg.CandidateTag, "tag", "gonzb-candidate", "candidate torrent tag")
	flag.StringVar(&cfg.Hash, "hash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "torrent info hash")
	flag.StringVar(&cfg.Name, "name", "Synthetic.Worker.Conformance.CC0", "torrent name")
	flag.StringVar(&cfg.ContentPath, "content-path", "", "absolute completed torrent path")
	flag.Int64Var(&cfg.Size, "size", 0, "completed torrent payload size")
	flag.StringVar(&cfg.Tracker, "tracker", "https://tracker.example.invalid/announce", "tracker URL")
	flag.Parse()

	if err := cfg.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "qbittorrentfixture:", err)
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qbittorrentfixture: listen:", err)
		os.Exit(1)
	}
	if *readyFile != "" {
		if err := os.WriteFile(*readyFile, []byte(listener.Addr().String()+"\n"), 0o600); err != nil {
			_ = listener.Close()
			fmt.Fprintln(os.Stderr, "qbittorrentfixture: ready file:", err)
			os.Exit(1)
		}
	}
	server := &http.Server{Handler: newHandler(cfg)}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "qbittorrentfixture: serve:", err)
		os.Exit(1)
	}
}

func (c fixtureConfig) validate() error {
	c.Hash = strings.ToLower(strings.TrimSpace(c.Hash))
	if c.Username == "" || c.Password == "" || c.CandidateTag == "" || c.Name == "" {
		return errors.New("username, password, tag, and name are required")
	}
	if len(c.Hash) != 40 && len(c.Hash) != 64 {
		return errors.New("hash must be a 40- or 64-character info hash")
	}
	for _, char := range c.Hash {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return errors.New("hash must be hexadecimal")
		}
	}
	if !filepath.IsAbs(c.ContentPath) || c.Size <= 0 {
		return errors.New("content-path must be absolute and size must be positive")
	}
	return nil
}

func newHandler(cfg fixtureConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.FormValue("username") != cfg.Username || r.FormValue("password") != cfg.Password {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "worker-fixture-session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cookie, err := r.Cookie("SID")
		if err != nil || cookie.Value != "worker-fixture-session" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		query := r.URL.Query()
		if query.Get("filter") != "completed" {
			http.Error(w, "completed filter required", http.StatusBadRequest)
			return
		}
		if hashes := strings.ToLower(strings.TrimSpace(query.Get("hashes"))); hashes != "" {
			if hashes != strings.ToLower(cfg.Hash) {
				http.Error(w, "unexpected torrent hash", http.StatusBadRequest)
				return
			}
		} else if query.Get("tag") != cfg.CandidateTag {
			http.Error(w, "candidate tag required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]torrentResponse{{
			Hash: strings.ToLower(cfg.Hash), Name: cfg.Name, SavePath: filepath.Dir(cfg.ContentPath),
			ContentPath: cfg.ContentPath, Size: cfg.Size, TotalSize: cfg.Size, Progress: 1,
			CompletionOn: 1, Tracker: cfg.Tracker, Tags: cfg.CandidateTag, State: "uploading",
		}})
	})
	return mux
}
