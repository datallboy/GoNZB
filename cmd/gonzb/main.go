package main

import (
	"gonzb/internal/config"
	"gonzb/internal/downloader"
	"gonzb/internal/nntp"
	"gonzb/internal/nzb"
	"log"
	"os"
)

func main() {
	// Load config
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Initialize Infrastructure repo
	repo := nntp.NewRepository(cfg.Server)
	if err := repo.Authenticate(); err != nil {
		log.Fatalf("Failed to auth: %v", err)
	}

	// Initialize application services
	nzbParser := nzb.NewParser()
	downloadSvc := downloader.NewService(repo, nzbParser)

	// TODO - read from cmd line argument
	f, err := os.Open("test_file.nzb")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if err := downloadSvc.DownloadNZB(f); err != nil {
		log.Fatalf("Download failed: %v", err)
	}
}
