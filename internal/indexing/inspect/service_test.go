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
		"inspect_discovery": runnerFunc(func(context.Context) error {
			order = append(order, "inspect_discovery")
			return nil
		}),
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

	want := []string{"inspect_discovery", "inspect_par2", "inspect_nfo", "inspect_archive", "inspect_password", "inspect_media"}
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

func TestWorkspaceManagerPrefersMemoryWorkspaceRootWhenConfigured(t *testing.T) {
	diskRoot := t.TempDir()
	memRoot := filepath.Join(t.TempDir(), "mem")
	manager := NewWorkspaceManager(Options{
		WorkDir:          diskRoot,
		WorkspaceBackend: "memory",
		MemoryWorkDir:    memRoot,
		MaxBytes:         4096,
		MaxArchiveDepth:  2,
	})

	ws, err := manager.PrepareBinaryWorkspace(context.Background(), "inspect_media", pgindex.BinaryInspectionCandidate{
		BinaryID:     43,
		ReleaseID:    "rel-2",
		ReleaseTitle: "Memory.Release",
		FileName:     "memory.mkv",
		TotalBytes:   2048,
	})
	if err != nil {
		t.Fatalf("prepare workspace: %v", err)
	}
	defer ws.Cleanup()

	if got := ws.Dir; filepath.Dir(filepath.Dir(got)) != memRoot {
		t.Fatalf("expected workspace under memory root %s, got %s", memRoot, got)
	}
	if _, err := os.Stat(memRoot); err != nil {
		t.Fatalf("expected memory root to exist: %v", err)
	}
}

func TestWorkspaceManagerAutoBackendFallsBackToDiskWhenMemoryRootUnavailable(t *testing.T) {
	diskRoot := t.TempDir()
	manager := NewWorkspaceManager(Options{
		WorkDir:          diskRoot,
		WorkspaceBackend: "auto",
		MemoryWorkDir:    filepath.Join("/proc", "not-writable", "gonzb-inspect"),
		MaxBytes:         4096,
		MaxArchiveDepth:  2,
	})

	ws, err := manager.PrepareBinaryWorkspace(context.Background(), "inspect_media", pgindex.BinaryInspectionCandidate{
		BinaryID:     44,
		ReleaseID:    "rel-3",
		ReleaseTitle: "Disk.Fallback.Release",
		FileName:     "fallback.mkv",
		TotalBytes:   2048,
	})
	if err != nil {
		t.Fatalf("prepare workspace: %v", err)
	}
	defer ws.Cleanup()
	if got := ws.Dir; filepath.Dir(filepath.Dir(got)) != diskRoot {
		t.Fatalf("expected workspace under disk root %s, got %s", diskRoot, got)
	}
}

func TestCleanupStaleWorkspaceRootsPurgesOldDirsAcrossRoots(t *testing.T) {
	diskRoot := t.TempDir()
	memRoot := t.TempDir()
	oldDisk := filepath.Join(diskRoot, "inspect_archive", "old")
	oldMem := filepath.Join(memRoot, "inspect_media", "old")
	newDisk := filepath.Join(diskRoot, "inspect_archive", "new")
	for _, dir := range []string{oldDisk, oldMem, newDisk} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	oldTime := time.Now().Add(-WorkspaceStaleTTL - time.Hour)
	if err := os.Chtimes(oldDisk, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old disk: %v", err)
	}
	if err := os.Chtimes(oldMem, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old mem: %v", err)
	}

	cleaned, err := CleanupStaleWorkspaceRoots(context.Background(), Options{
		WorkDir:       diskRoot,
		MemoryWorkDir: memRoot,
	})
	if err != nil {
		t.Fatalf("cleanup stale workspace roots: %v", err)
	}
	if cleaned != 2 {
		t.Fatalf("expected 2 cleaned workspaces, got %d", cleaned)
	}
	if _, err := os.Stat(oldDisk); !os.IsNotExist(err) {
		t.Fatalf("expected old disk workspace removed, stat err=%v", err)
	}
	if _, err := os.Stat(oldMem); !os.IsNotExist(err) {
		t.Fatalf("expected old mem workspace removed, stat err=%v", err)
	}
	if _, err := os.Stat(newDisk); err != nil {
		t.Fatalf("expected new disk workspace to remain: %v", err)
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

func TestArchiveFamilyFilesFallsBackToObfuscatedSplitRARSequence(t *testing.T) {
	files := []pgindex.CatalogReleaseFile{
		{FileName: "abc.part01.rar", FileIndex: 1},
		{FileName: "def.part02.rar", FileIndex: 2},
		{FileName: "ghi.part03.rar", FileIndex: 3},
		{FileName: "jkl.part04.rar", FileIndex: 4},
	}

	got := ArchiveFamilyFiles("def.part02.rar", files)
	if len(got) != 4 {
		t.Fatalf("expected obfuscated split-rar grouping to return 4 files, got %d", len(got))
	}
	if got[0].FileName != "abc.part01.rar" || got[3].FileName != "jkl.part04.rar" {
		t.Fatalf("expected sorted split-rar family, got %+v", got)
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

func TestBestMediaEntryPrefersPrimaryOverSample(t *testing.T) {
	got := BestMediaEntry([]string{
		"Release.Name/Sample/release.name.sample.mkv",
		"Release.Name/Release.Name.1080p.WEB.H264-GROUP.mkv",
	})
	want := "Release.Name/Release.Name.1080p.WEB.H264-GROUP.mkv"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
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

func TestEvaluateContentFilterBlocksMagicAndSize(t *testing.T) {
	opts := DefaultOptions(Options{
		MinBinaryBytes:  10,
		MaxBinaryBytes:  100,
		BlockedMagicHex: []string{"52 43 4c 4f 4e 45"},
	})

	blocked := EvaluateContentFilter(opts, &BinaryPrefixSample{
		Prefix:    []byte("RCLONE\x00\x00payload"),
		BytesRead: 32,
		ExactSize: 32,
	})
	if !blocked.Filtered || blocked.Reason != "blocked_magic" {
		t.Fatalf("expected blocked magic decision, got %+v", blocked)
	}

	small := EvaluateContentFilter(opts, &BinaryPrefixSample{Prefix: []byte("abc"), BytesRead: 3, ExactSize: 3})
	if !small.Filtered || small.Reason != "below_min_binary_bytes" {
		t.Fatalf("expected min-size decision, got %+v", small)
	}

	large := EvaluateContentFilter(opts, &BinaryPrefixSample{Prefix: []byte("abc"), BytesRead: 3, ExactSize: 101})
	if !large.Filtered || large.Reason != "above_max_binary_bytes" {
		t.Fatalf("expected max-size decision, got %+v", large)
	}
}

type runnerFunc func(context.Context) error

func (fn runnerFunc) RunOnce(ctx context.Context) error { return fn(ctx) }
