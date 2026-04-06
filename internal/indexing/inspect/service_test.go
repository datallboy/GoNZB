package inspect

import (
	"context"
	"encoding/binary"
	"hash/crc32"
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

func TestIsArchiveFileRecognizesSplitArchives(t *testing.T) {
	tests := map[string]bool{
		"example.7z.001":     true,
		"example.zip.001":    true,
		"example.part01.rar": true,
		"example.r00":        true,
		"example.par2":       false,
	}

	for fileName, want := range tests {
		if got := IsArchiveFile(fileName); got != want {
			t.Fatalf("expected IsArchiveFile(%q)=%v, got %v", fileName, want, got)
		}
	}
}

func TestIsArchiveRepresentativeUsesFirstSplitVolume(t *testing.T) {
	tests := map[string]bool{
		"example.7z.001":     true,
		"example.7z.002":     false,
		"example.part01.rar": true,
		"example.part02.rar": false,
		"example.rar":        true,
		"example.r00":        false,
	}

	for fileName, want := range tests {
		if got := IsArchiveRepresentative(fileName); got != want {
			t.Fatalf("expected IsArchiveRepresentative(%q)=%v, got %v", fileName, want, got)
		}
	}
}

func TestArchiveEntryNamesFromSummaryReadsEntryList(t *testing.T) {
	raw := []byte(`{"archive_entries":["video/sample.mkv","proof.nfo","video/sample.mkv"]}`)
	got := ArchiveEntryNamesFromSummary(raw)
	want := []string{"proof.nfo", "video/sample.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseSevenZipNextHeaderRange(t *testing.T) {
	head := make([]byte, 32)
	copy(head[:6], []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C})
	binary.LittleEndian.PutUint64(head[12:20], 1024)
	binary.LittleEndian.PutUint64(head[20:28], 256)
	binary.LittleEndian.PutUint32(head[28:32], 0xA1B2C3D4)
	binary.LittleEndian.PutUint32(head[8:12], crc32.ChecksumIEEE(head[12:32]))

	start, end, total, crc, err := parseSevenZipNextHeaderRange(head)
	if err != nil {
		t.Fatalf("parse start header: %v", err)
	}
	if start != 1056 || end != 1312 || total != 1312 {
		t.Fatalf("expected range 1056-1312 total 1312, got %d-%d total %d", start, end, total)
	}
	if crc != 0xA1B2C3D4 {
		t.Fatalf("expected crc %08X, got %08X", uint32(0xA1B2C3D4), crc)
	}
}

type runnerFunc func(context.Context) error

func (fn runnerFunc) RunOnce(ctx context.Context) error { return fn(ctx) }
