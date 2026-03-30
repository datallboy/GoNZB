package engine

import (
	"context"
	"os"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/processor"
)

type nopWriter struct{}

func (nopWriter) CloseFile(string, int64) error   { return nil }
func (nopWriter) PreAllocate(string, int64) error { return nil }

func TestUsenetIndexHydrateSmoke(t *testing.T) {
	cfgPath := os.Getenv("GONZB_TEST_CONFIG")
	releaseID := os.Getenv("GONZB_TEST_RELEASE_ID")

	if cfgPath == "" || releaseID == "" {
		t.Skip("set GONZB_TEST_CONFIG and GONZB_TEST_RELEASE_ID")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	t.Logf("sqlite_path=%s", cfg.Store.SQLitePath)

	log, err := logger.New("/dev/null", logger.ParseLevel("debug"), false)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	appCtx, err := app.NewContext(cfg, log)
	if err != nil {
		t.Fatalf("new app context: %v", err)
	}
	defer appCtx.Close()

	appCtx.NZBParser = nzb.NewParser()
	appCtx.Processor = processor.New(appCtx, nopWriter{})

	ctx := context.Background()

	rel, err := appCtx.Resolver.GetRelease(ctx, "usenet_index", releaseID)
	if err != nil {
		t.Fatalf("resolver get release: %v", err)
	}
	if rel == nil {
		t.Fatalf("release %s not found", releaseID)
	}

	reader, err := appCtx.PayloadFetcher.GetNZB(ctx, "usenet_index", rel)
	if err != nil {
		t.Fatalf("resolver get nzb: %v", err)
	}
	defer reader.Close()

	model, err := appCtx.NZBParser.Parse(reader)
	if err != nil {
		t.Fatalf("parse generated nzb: %v", err)
	}
	if len(model.Files) == 0 {
		t.Fatalf("generated nzb has no files")
	}

	qm := NewQueueManager(appCtx, false)

	item, err := qm.Add(ctx, app.QueueAddRequest{
		SourceKind:      "usenet_index",
		SourceReleaseID: releaseID,
		Release:         rel,
		Title:           rel.Title,
	})
	if err != nil {
		t.Fatalf("queue add: %v", err)
	}

	if err := qm.HydrateItem(ctx, item); err != nil {
		t.Fatalf("hydrate item: %v", err)
	}

	if len(item.Tasks) == 0 {
		t.Fatalf("hydrate produced no tasks")
	}

	files, err := appCtx.QueueFileStore.GetQueueItemFiles(ctx, item.ID)
	if err != nil {
		t.Fatalf("load queue item files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("queue item files were not persisted")
	}

	t.Logf("release=%s title=%q nzb_files=%d queue_files=%d", rel.ID, rel.Title, len(model.Files), len(files))
}
