package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerRotatesFileWhenMaxSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gonzb.log")

	log, err := NewWithOptions(path, LevelDebug, false, Options{
		MaxSizeMB:  1,
		MaxBackups: 2,
	})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}
	defer func() {
		if err := log.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	line := strings.Repeat("x", 600_000)
	log.Info("%s", line)
	log.Info("%s", line)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active log missing: %v", err)
	}
	backup, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatalf("backup log missing: %v", err)
	}
	if backup.Size() == 0 {
		t.Fatal("backup log is empty")
	}
}
