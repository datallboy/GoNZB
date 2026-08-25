package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/buildinfo"
	workerconfig "github.com/datallboy/gonzb/internal/worker/config"
	"github.com/datallboy/gonzb/internal/worker/ingest"
	"github.com/datallboy/gonzb/internal/worker/jobs"
	"github.com/datallboy/gonzb/internal/worker/pesto"
	"github.com/datallboy/gonzb/internal/worker/qbittorrent"
	workerrun "github.com/datallboy/gonzb/internal/worker/run"
	"github.com/datallboy/gonzb/internal/worker/transfer"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gonzb-worker:", err)
		os.Exit(1)
	}
}

func execute() error {
	configPath := flag.String("config", "gonzb-worker-config.yaml", "path to the worker YAML configuration")
	torrentHash := flag.String("torrent-hash", "", "process one specific completed torrent info hash instead of using the candidate tag")
	once := flag.Bool("once", false, "run at most one job pass and exit")
	mountOnly := flag.Bool("mount-only", false, "mount and verify the read-only SSHFS source without discovering or posting jobs")
	showVersion := flag.Bool("version", false, "print version and build time, then exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("GoNZB Worker Version: %s\nBuild Time: %s\n", buildinfo.Version, buildinfo.BuildTime)
		return nil
	}

	cfg, err := workerconfig.Load(*configPath)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	transferClient, err := transfer.New(transfer.Config{
		Type: cfg.Transfer.Type, Binary: cfg.Transfer.RsyncBinary, SSHFSBinary: cfg.Transfer.SSHFSBinary,
		UnmountBinary: cfg.Transfer.UnmountBinary, Host: cfg.Transfer.SSHHost, User: cfg.Transfer.SSHUser,
		Port: cfg.Transfer.SSHPort, KeyPath: cfg.Transfer.SSHKey, SourceRoot: cfg.Transfer.SourceRoot,
		MountPath: cfg.Transfer.MountPath, ManageMount: cfg.Transfer.ManageMount,
		UnmountOnExit: cfg.Transfer.UnmountOnExit, ExtraArgs: cfg.Transfer.ExtraArgs,
		SSHFSOptions: cfg.Transfer.SSHFSOptions,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := transferClient.Close(closeCtx); err != nil {
			logger.Warn("worker source mount cleanup failed", "error", err)
		}
	}()
	if *mountOnly {
		if cfg.Transfer.Type != "sshfs" {
			return fmt.Errorf("--mount-only requires transfer.type: sshfs")
		}
		mountCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := transferClient.Initialize(mountCtx)
		cancel()
		if err != nil {
			return err
		}
		logger.Info("read-only SSHFS source mounted; press Ctrl-C to unmount and exit", "mount_path", cfg.Transfer.MountPath)
		<-ctx.Done()
		return nil
	}

	store, err := jobs.Open(filepath.Join(cfg.Worker.DataDir, "state", "worker.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	qbit, err := qbittorrent.New(cfg.QBittorrent.URL, cfg.QBittorrent.Username, cfg.QBittorrent.Password, cfg.QBittorrentTimeout())
	if err != nil {
		return err
	}
	pestoClient, err := pesto.New(pesto.Config{
		Binary: cfg.Pesto.Binary, ConfigPath: cfg.Pesto.ConfigPath, Compression: cfg.Pesto.Compression,
		Encryption: cfg.Pesto.Encryption, Obfuscation: cfg.Pesto.Obfuscation,
		PAR2Percent: cfg.Pesto.PAR2Percent, ExtraArgs: cfg.Pesto.ExtraArgs,
	})
	if err != nil {
		return err
	}
	ingestClient, err := ingest.New(cfg.GoNZB.URL, cfg.GoNZB.APIToken, cfg.GoNZBTimeout())
	if err != nil {
		return err
	}
	runner := workerrun.New(cfg, store, qbit, transferClient, pestoClient, ingestClient, logger)
	if err := runner.Initialize(ctx); err != nil {
		return err
	}
	if *once {
		return runner.RunOnce(ctx, *torrentHash)
	}

	logger.Info("GoNZB worker started", "node_id", cfg.Worker.NodeID, "poll_interval", cfg.PollInterval())
	for {
		if err := runner.RunOnce(ctx, *torrentHash); err != nil && ctx.Err() == nil {
			logger.Error("worker pass failed", "error", err)
		}
		timer := time.NewTimer(cfg.PollInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
