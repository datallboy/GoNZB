package main

import (
	"context"
	"errors"
	"fmt"
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

	// 1. Create a channel for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Setup Signal Handling for Graceful Shutdown
	// We create a context that is cancelled when the user hits Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-sigChan:
			fmt.Println("\n\r[!] Interrupt received. Shutting down gracefully...")
			cancel()

		case <-ctx.Done():
			// Context was cancelled normally (download finished), just exit
			fmt.Print("\n\r Process finished successfully")
			return
		}
	}()

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

	if err := svc.Download(ctx, nzbDomain); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Println("Download cancelled by user.")
		} else {
			log.Fatalf("Download failed: %v", err)
		}
	}
}
