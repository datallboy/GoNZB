package processor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
)

func TestCLI7zCanExtractPrimarySplitVolume(t *testing.T) {
	t.Parallel()

	extractor, err := NewCLI7z()
	if err != nil {
		t.Skipf("7z unavailable: %v", err)
	}

	dir := t.TempDir()
	primary := filepath.Join(dir, "sample.7z.001")
	if err := os.WriteFile(primary, append([]byte{}, sevenZipSignature...), 0644); err != nil {
		t.Fatalf("write primary split volume: %v", err)
	}

	canExtract, err := extractor.CanExtract(primary)
	if err != nil {
		t.Fatalf("CanExtract(primary) error = %v", err)
	}
	if !canExtract {
		t.Fatal("expected primary split 7z volume to be extractable")
	}

	secondary := filepath.Join(dir, "sample.7z.002")
	if err := os.WriteFile(secondary, append([]byte{}, sevenZipSignature...), 0644); err != nil {
		t.Fatalf("write secondary split volume: %v", err)
	}

	canExtract, err = extractor.CanExtract(secondary)
	if err != nil {
		t.Fatalf("CanExtract(secondary) error = %v", err)
	}
	if canExtract {
		t.Fatal("expected non-primary split 7z volume to be skipped")
	}
}

func TestBuildMoveTaskListDropsArchiveArtifacts(t *testing.T) {
	t.Parallel()

	tasks := []*domain.DownloadFile{
		{FinalPath: "/tmp/release.part01.rar"},
		{FinalPath: "/tmp/release.r00"},
		{FinalPath: "/tmp/release.7z"},
		{FinalPath: "/tmp/release.7z.001"},
		{FinalPath: "/tmp/release.zip"},
		{FinalPath: "/tmp/release.txt"},
	}
	extracted := []*domain.DownloadFile{
		{FinalPath: "/tmp/video.mkv"},
	}

	moveTasks := buildMoveTaskList(tasks, extracted)

	if len(moveTasks) != 2 {
		t.Fatalf("expected 2 move tasks, got %d", len(moveTasks))
	}
	if moveTasks[0].FinalPath != "/tmp/release.txt" {
		t.Fatalf("expected retained original file to be release.txt, got %s", moveTasks[0].FinalPath)
	}
	if moveTasks[1].FinalPath != "/tmp/video.mkv" {
		t.Fatalf("expected extracted file to be retained, got %s", moveTasks[1].FinalPath)
	}
}

func TestPostProcessExtracts7zAndMovesExtractedFilesOnly(t *testing.T) {
	extractor, err := NewCLI7z()
	if err != nil {
		t.Skipf("7z unavailable: %v", err)
	}
	_ = extractor

	workRoot := t.TempDir()
	outDir := filepath.Join(workRoot, "downloads", "work", "job")
	completedDir := filepath.Join(workRoot, "downloads", "completed")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatalf("mkdir outDir: %v", err)
	}

	payloadPath := filepath.Join(outDir, "video.txt")
	if err := os.WriteFile(payloadPath, []byte("payload"), 0644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	archivePath := filepath.Join(outDir, "release.7z")
	cmd := exec.Command("7z", "a", "-y", archivePath, filepath.Base(payloadPath))
	cmd.Dir = outDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create 7z archive: %v\n%s", err, string(output))
	}
	if err := os.Remove(payloadPath); err != nil {
		t.Fatalf("remove original payload: %v", err)
	}

	log, err := logger.New("none", logger.LevelError, false)
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	ctx := &app.Context{
		Config: &config.Config{
			Download: config.DownloadConfig{
				OutDir:            filepath.Join(workRoot, "downloads", "work"),
				CompletedDir:      completedDir,
				CleanupExtensions: []string{"nfo"},
			},
		},
		Logger:            log,
		ExtractionEnabled: true,
	}

	processor := New(ctx, nil)
	item := &domain.QueueItem{
		OutDir:       outDir,
		ReleaseTitle: "Example Release",
		Release: &domain.Release{
			Title:    "Example Release",
			Category: "movies",
		},
	}
	tasks := []*domain.DownloadFile{
		{FinalPath: archivePath, FileName: filepath.Base(archivePath)},
	}

	if err := processor.PostProcess(context.Background(), item, tasks); err != nil {
		t.Fatalf("PostProcess() error = %v", err)
	}

	foundPayload := false
	err = filepath.Walk(item.OutDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		switch filepath.Base(path) {
		case "video.txt":
			foundPayload = true
		case "release.7z":
			t.Fatalf("raw archive should not remain in completed output: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk completed output: %v", err)
	}
	if !foundPayload {
		t.Fatalf("expected extracted payload in completed output, got none under %s", item.OutDir)
	}
}

func TestCleanupWorkDirRemovesNestedEmptyDirectories(t *testing.T) {
	t.Parallel()

	workRoot := t.TempDir()
	downloadRoot := filepath.Join(workRoot, "downloads", "work")
	workDir := filepath.Join(downloadRoot, "job")
	completedDir := filepath.Join(workRoot, "downloads", "completed", "job")

	if err := os.MkdirAll(filepath.Join(workDir, "nested", "empty"), 0755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "archive.7z.001"), []byte("stub"), 0644); err != nil {
		t.Fatalf("write archive stub: %v", err)
	}

	log, err := logger.New("none", logger.LevelError, false)
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	processor := New(&app.Context{
		Config: &config.Config{
			Download: config.DownloadConfig{
				OutDir:            downloadRoot,
				CompletedDir:      filepath.Join(workRoot, "downloads", "completed"),
				CleanupExtensions: []string{"nfo"},
			},
		},
		Logger:            log,
		ExtractionEnabled: true,
	}, nil)

	if err := processor.cleanupWorkDir(workDir, completedDir); err != nil {
		t.Fatalf("cleanupWorkDir() error = %v", err)
	}

	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("expected work dir to be removed, stat err = %v", err)
	}
}
