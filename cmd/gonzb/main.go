package main

import (
	"context"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"gonzb/internal/downloader"
	"gonzb/internal/nntp"
	"gonzb/internal/nzb"
	"gonzb/internal/provider"
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

	// Initialize Providers
	var providers []domain.Provider
	for _, s := range cfg.Servers {
		providers = append(providers, nntp.NewNNTPProvider(s))
	}

	// Test Providers
	for _, p := range providers {
		log.Printf("Testing connection %s...", p.ID())
		if err := p.TestConnection(); err != nil {
			log.Fatalf("FAILED to connect to %s: %v", p.ID(), err)
		}
		log.Printf("Successfully authenticated with %s", p.ID())
	}

	// Initialize the Manager (The provider load balancer)
	mgr := provider.NewManager(providers)

	// Initialize the Downloader Service
	svc := downloader.NewService(cfg, mgr)

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

	// Initialize nzb parser
	nzbParser := nzb.NewParser()
	nzbDomain, err := nzbParser.Parse(f)
	if err != nil {
		log.Fatalf("Failed to parse NZB: %v", err)
	}

	log.Println("Starting download...")

	if err := svc.Download(ctx, nzbDomain); err != nil {
		log.Fatalf("Download failed: %v", err)
	}

	log.Println("Process finished successfully.")
}
