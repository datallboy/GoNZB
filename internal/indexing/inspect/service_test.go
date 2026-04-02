package inspect

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestServiceRunOnceHonorsInspectOrder(t *testing.T) {
	var order []string
	svc := NewService(nil, map[string]Runner{
		"inspect_archive": runnerFunc(func(context.Context) error {
			order = append(order, "inspect_archive")
			return nil
		}),
		"inspect_media": runnerFunc(func(context.Context) error {
			order = append(order, "inspect_media")
			return nil
		}),
		"inspect_nfo": runnerFunc(func(context.Context) error {
			order = append(order, "inspect_nfo")
			return nil
		}),
		"inspect_par2": runnerFunc(func(context.Context) error {
			order = append(order, "inspect_par2")
			return nil
		}),
		"inspect_password": runnerFunc(func(context.Context) error {
			order = append(order, "inspect_password")
			return nil
		}),
	})

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	want := []string{"inspect_par2", "inspect_nfo", "inspect_archive", "inspect_password", "inspect_media"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("expected order %v, got %v", want, order)
	}
}

func TestWorkspaceManagerCreatesManifest(t *testing.T) {
	root := t.TempDir()
	manager := NewWorkspaceManager(Options{
		WorkDir:         root,
		MaxBytes:        4096,
		MaxArchiveDepth: 2,
	})

	postedAt := time.Now().UTC()
	ws, err := manager.PrepareBinaryWorkspace(context.Background(), "inspect_media", pgindex.BinaryInspectionCandidate{
		BinaryID:     42,
		ReleaseID:    "rel-1",
		ReleaseTitle: "Example.Release",
		FileName:     "example.mkv",
		TotalBytes:   1024,
		PostedAt:     &postedAt,
	})
	if err != nil {
		t.Fatalf("prepare workspace: %v", err)
	}
	defer ws.Cleanup()

	if ws.MaterializedBytes <= 0 {
		t.Fatalf("expected positive materialized bytes, got %d", ws.MaterializedBytes)
	}
	if _, err := os.Stat(ws.ManifestPath); err != nil {
		t.Fatalf("expected manifest at %s: %v", ws.ManifestPath, err)
	}
	if got := filepath.Dir(ws.ManifestPath); got != ws.Dir {
		t.Fatalf("expected manifest dir %s, got %s", ws.Dir, got)
	}
}

func TestExtractPasswordCandidatesFindsStructuredHints(t *testing.T) {
	got := ExtractPasswordCandidates(
		"Some.Release password:open-sesame",
		"Other text pw_supersecret",
		"irrelevant",
	)

	want := []string{"open-sesame", "supersecret"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

type runnerFunc func(context.Context) error

func (fn runnerFunc) RunOnce(ctx context.Context) error { return fn(ctx) }
