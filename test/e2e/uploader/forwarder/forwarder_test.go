package forwarder_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatcherPersistsDeliveryReceipt(t *testing.T) {
	repoRoot := repositoryRoot(t)
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	stateDir := filepath.Join(tempDir, "state")
	binDir := filepath.Join(tempDir, "bin")
	for _, path := range []string{outputDir, binDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create test directory: %v", err)
		}
	}

	nzb := []byte("synthetic completed NZB")
	nzbPath := filepath.Join(outputDir, "Synthetic.Release.nzb")
	if err := os.WriteFile(nzbPath, nzb, 0o600); err != nil {
		t.Fatalf("write NZB: %v", err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(nzbPath, old, old); err != nil {
		t.Fatalf("age NZB: %v", err)
	}

	curlLog := filepath.Join(tempDir, "curl.log")
	fakeCurl := filepath.Join(binDir, "curl")
	writeExecutable(t, fakeCurl, `#!/bin/sh
printf '%s\n' "$*" >>"$FORWARDER_CURL_LOG"
printf '%s\n' '{"created":true}'
`)

	watcher := filepath.Join(repoRoot, "scripts", "gonzb-submit-nzb-watch.sh")
	command := exec.Command(watcher, outputDir, stateDir)
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FORWARDER_CURL_LOG="+curlLog,
		"GONZB_URL=https://gonzb.example.invalid",
		"GONZB_TOKEN=synthetic-test-token",
		"GONZB_WATCH_ONCE=1",
		"GONZB_WATCH_SETTLE_SECONDS=0",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("first watcher scan: %v\n%s", err, output)
	}

	digest := sha256.Sum256(nzb)
	receipt := filepath.Join(stateDir, "delivered", hex.EncodeToString(digest[:]))
	if _, err := os.Stat(receipt); err != nil {
		t.Fatalf("delivery receipt was not persisted: %v", err)
	}
	firstLog, err := os.ReadFile(curlLog)
	if err != nil {
		t.Fatalf("read curl log: %v", err)
	}
	if strings.Contains(string(firstLog), "Idempotency-Key:") {
		t.Fatalf("helper must not derive an idempotency key from a possibly repeated filename: %s", firstLog)
	}

	command = exec.Command(watcher, outputDir, stateDir)
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FORWARDER_CURL_LOG="+curlLog,
		"GONZB_URL=https://gonzb.example.invalid",
		"GONZB_TOKEN=synthetic-test-token",
		"GONZB_WATCH_ONCE=1",
		"GONZB_WATCH_SETTLE_SECONDS=0",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("second watcher scan: %v\n%s", err, output)
	}
	secondLog, err := os.ReadFile(curlLog)
	if err != nil {
		t.Fatalf("read second curl log: %v", err)
	}
	if string(secondLog) != string(firstLog) {
		t.Fatalf("delivered NZB was submitted again: first=%q second=%q", firstLog, secondLog)
	}
}

func TestWatcherPersistsRetryBackoffAcrossRuns(t *testing.T) {
	repoRoot := repositoryRoot(t)
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	stateDir := filepath.Join(tempDir, "state")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatalf("create output directory: %v", err)
	}
	nzb := []byte("synthetic retry NZB")
	nzbPath := filepath.Join(outputDir, "Retry.nzb")
	if err := os.WriteFile(nzbPath, nzb, 0o600); err != nil {
		t.Fatalf("write NZB: %v", err)
	}

	attemptLog := filepath.Join(tempDir, "attempts.log")
	failingHelper := filepath.Join(tempDir, "submit-fail")
	writeExecutable(t, failingHelper, `#!/bin/sh
printf 'attempt\n' >>"$FORWARDER_ATTEMPT_LOG"
exit 1
`)

	runWatcher := func(helper string) []byte {
		t.Helper()
		command := exec.Command(filepath.Join(repoRoot, "scripts", "gonzb-submit-nzb-watch.sh"), outputDir, stateDir)
		command.Env = append(os.Environ(),
			"FORWARDER_ATTEMPT_LOG="+attemptLog,
			"GONZB_SUBMIT_HELPER="+helper,
			"GONZB_WATCH_ONCE=1",
			"GONZB_WATCH_SETTLE_SECONDS=0",
			"GONZB_WATCH_RETRY_BASE_SECONDS=60",
			"GONZB_WATCH_RETRY_MAX_SECONDS=60",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("watcher scan: %v\n%s", err, output)
		}
		return output
	}

	runWatcher(failingHelper)
	runWatcher(failingHelper)
	attempts, err := os.ReadFile(attemptLog)
	if err != nil {
		t.Fatalf("read attempt log: %v", err)
	}
	if got := strings.Count(string(attempts), "attempt\n"); got != 1 {
		t.Fatalf("retry backoff did not survive restart; attempts=%d", got)
	}

	digest := sha256.Sum256(nzb)
	retryPath := filepath.Join(stateDir, "retry", hex.EncodeToString(digest[:]))
	if err := os.WriteFile(retryPath, []byte("1 0\n"), 0o600); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	successHelper := filepath.Join(tempDir, "submit-success")
	writeExecutable(t, successHelper, `#!/bin/sh
printf 'attempt\n' >>"$FORWARDER_ATTEMPT_LOG"
exit 0
`)
	runWatcher(successHelper)

	if _, err := os.Stat(filepath.Join(stateDir, "delivered", hex.EncodeToString(digest[:]))); err != nil {
		t.Fatalf("successful retry did not persist receipt: %v", err)
	}
	if _, err := os.Stat(retryPath); !os.IsNotExist(err) {
		t.Fatalf("successful retry state was not cleared: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDir, "../../../.."))
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
