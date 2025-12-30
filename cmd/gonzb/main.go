package main

import (
	"context"
	"gonzb/internal/config"
	"gonzb/internal/downloader"
	"gonzb/internal/nzb"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Load config
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Setup Signal Handling for Graceful Shutdown
	// We create a context that is cancelled when the user hits Ctrl+C
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// TODO - read from cmd line argument
	f, err := os.Open("test_file.nzb")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Initialize application services
	nzbParser := nzb.NewParser()
	nzbDomain, err := nzbParser.Parse(f)
	if err != nil {
		log.Fatalf("Failed to parse NZB: %v", err)
	}

	svc := downloader.NewService(cfg)

	log.Println("Starting download...")
	if err := svc.DownloadNZB(ctx, nzbDomain); err != nil {
		log.Fatalf("Download failed: %v", err)
	}

	log.Println("Process finished successfully.")
}
