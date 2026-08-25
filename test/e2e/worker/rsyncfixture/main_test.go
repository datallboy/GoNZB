package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCopiesOnlyConfinedWorkerSource(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	destinationRoot := filepath.Join(root, "worker")
	release := filepath.Join(sourceRoot, "Synthetic.Release")
	if err := os.MkdirAll(release, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "payload.txt"), []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(destinationRoot, "jobs", "job", "source")
	args := []string{"-a", "--partial", "--partial-dir=.rsync-partial", "--protect-args", "--stats", "-e", "ssh -p 22", "--", "worker@fixture:" + release, destination + string(filepath.Separator)}
	if err := run(args, sourceRoot, destinationRoot); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(destination, "Synthetic.Release", "payload.txt"))
	if err != nil || string(payload) != "synthetic" {
		t.Fatalf("payload=%q err=%v", payload, err)
	}
}

func TestRunRejectsSourceOutsideFixtureRoot(t *testing.T) {
	root := t.TempDir()
	args := []string{"-a", "--partial", "--protect-args", "--stats", "--", "worker@fixture:/outside", filepath.Join(root, "worker")}
	if err := run(args, filepath.Join(root, "source"), filepath.Join(root, "worker")); err == nil {
		t.Fatal("expected source confinement failure")
	}
}
