package nntp_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
)

func TestFetchCatalogMessageIDSmoke(t *testing.T) {
	cfgPath := os.Getenv("GONZB_TEST_CONFIG")
	releaseID := os.Getenv("GONZB_TEST_RELEASE_ID")

	if cfgPath == "" || releaseID == "" {
		t.Skip("set GONZB_TEST_CONFIG and GONZB_TEST_RELEASE_ID")
	}

	absCfgPath, err := filepath.Abs(cfgPath)
	if err != nil {
		t.Fatalf("abs config path: %v", err)
	}

	cfg, err := config.Load(absCfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	log, err := logger.New("/dev/null", logger.ParseLevel("debug"), false)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	appCtx, err := app.NewContext(cfg, log)
	if err != nil {
		t.Fatalf("new app context: %v", err)
	}
	defer appCtx.Close()

	if err := wiring.BuildInitialRuntime(appCtx); err != nil {
		t.Fatalf("build initial runtime: %v", err)
	}

	if appCtx.PGIndexStore == nil {
		t.Fatalf("PGIndexStore is nil")
	}

	manager, err := nntp.NewManager(appCtx)
	if err != nil {
		t.Fatalf("new nntp manager: %v", err)
	}

	ctx := context.Background()

	groups, err := appCtx.PGIndexStore.ListCatalogReleaseNewsgroups(ctx, releaseID)
	if err != nil {
		t.Fatalf("list release groups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("no groups found for release %s", releaseID)
	}

	files, err := appCtx.PGIndexStore.ListCatalogReleaseFiles(ctx, releaseID)
	if err != nil {
		t.Fatalf("list release files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no files found for release %s", releaseID)
	}

	var tested int
	var failed int

	for _, f := range files {
		articles, err := appCtx.PGIndexStore.ListCatalogReleaseFileArticles(ctx, f.ID)
		if err != nil {
			t.Fatalf("list release file articles for file %d: %v", f.ID, err)
		}

		for _, article := range articles {
			if article.MessageID == "" {
				continue
			}

			tested++
			seg := &domain.Segment{
				Number:    article.PartNumber,
				Bytes:     article.Bytes,
				MessageID: article.MessageID,
			}

			t.Logf("raw message-id=%q", article.MessageID)

			reader, err := manager.Fetch(ctx, seg, groups)
			if err != nil {
				failed++
				t.Logf("FETCH FAIL file_id=%d part=%d msgid=%q groups=%v err=%v", f.ID, article.PartNumber, article.MessageID, groups, err)
			} else {
				if closer, ok := reader.(io.Closer); ok {
					defer closer.Close()
				}
				buf, readErr := io.ReadAll(io.LimitReader(reader, 128))
				if readErr != nil {
					failed++
					t.Logf("READ FAIL file_id=%d part=%d msgid=%q err=%v", f.ID, article.PartNumber, article.MessageID, readErr)
				} else {
					t.Logf("FETCH OK file_id=%d part=%d msgid=%q bytes=%d", f.ID, article.PartNumber, article.MessageID, len(buf))
				}
			}

			if tested >= 20 {
				break
			}
		}

		if tested >= 20 {
			break
		}
	}

	if tested == 0 {
		t.Fatalf("no articles tested")
	}

	t.Logf("tested=%d failed=%d", tested, failed)

}
