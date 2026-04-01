package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/engine"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
)

func (r *Runner) ExecuteDownload(nzbPath string) {
	appCtx := r.setupApp(context.Background())
	defer appCtx.Close()

	if !appCtx.Config.Modules.Downloader.Enabled {
		appCtx.Logger.Fatal("downloader module is disabled")
	}
	if appCtx.NNTP == nil || appCtx.Processor == nil || appCtx.Downloader == nil || appCtx.NZBParser == nil {
		appCtx.Logger.Fatal("downloader runtime is not initialized")
	}

	appCtx.Queue = engine.NewQueueManager(appCtx, false)
	wiring.BindApplicationModules(appCtx)
	if appCtx.DownloaderModule == nil {
		appCtx.Logger.Fatal("downloader module facade is not initialized")
	}

	commands := appCtx.DownloaderModule.Commands()
	if commands == nil {
		appCtx.Logger.Fatal("downloader commands are not initialized")
	}

	filename := filepath.Base(nzbPath)
	setupCtx := context.Background()

	nzbFile, err := os.Open(nzbPath)
	if err != nil {
		appCtx.Logger.Fatal("Failed to read NZB file: %v", err)
	}
	defer nzbFile.Close()

	item, err := commands.EnqueueNZB(setupCtx, filename, nzbFile)
	if err != nil {
		appCtx.Logger.Fatal("Failed to queue NZB: %v", err)
	}

	if err := appCtx.Queue.HydrateItem(setupCtx, item); err != nil {
		appCtx.Logger.Fatal("Hydration failed: %v", err)
	}

	appCtx.Queue.UpdateStatus(setupCtx, item, domain.StatusDownloading)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	appCtx.Downloader.SetProgressHandler(func(item *domain.QueueItem) {
		appCtx.Downloader.RenderCLIProgress(item, 0, true)
		fmt.Println()
	})

	go appCtx.Queue.Start(ctx)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastBytes int64
	var barRendered bool

	for {
		select {
		case <-ctx.Done():
			fmt.Print("\n")
			appCtx.Logger.Info("Cancellation received. Cleaning up...")
			return

		case <-ticker.C:
			itm, ok := appCtx.Queue.GetItem(ctx, item.ID)
			if !ok {
				continue
			}

			switch itm.Status {
			case domain.StatusDownloading:
				current := itm.BytesWritten.Load()
				delta := current - lastBytes
				lastBytes = current

				if !barRendered {
					fmt.Print("\n\n")
				}

				speedMbps := float64(delta) * 8 / (1024 * 1024 * 0.5)
				appCtx.Downloader.RenderCLIProgress(itm, speedMbps, false)
				barRendered = true

			case domain.StatusProcessing:
				if barRendered {
					barRendered = false
				}

			case domain.StatusCompleted, domain.StatusFailed:
				if itm.Status == domain.StatusFailed {
					errText := "Unknown error"
					if itm.Error != nil {
						errText = *itm.Error
					}
					appCtx.Logger.Error("Download failed: %s", errText)
				} else {
					appCtx.Logger.Info("Download completed successfully!")
				}
				stop()
				return
			}
		}
	}
}
