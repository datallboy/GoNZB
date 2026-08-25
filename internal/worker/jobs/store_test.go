package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReserveDeduplicatesTorrentAndReconcilesInterruptedPost(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state", "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	input := CreateInput{
		TorrentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReleaseName: "release",
		SourcePath: "/downloads/release", WorkspacePath: "/tmp/jobs/one",
		InputPath: "/tmp/jobs/one/source/release", SourceSize: 42,
	}
	job, created, err := store.Reserve(context.Background(), input)
	if err != nil || !created {
		t.Fatalf("first reservation: created=%v err=%v", created, err)
	}
	duplicate, created, err := store.Reserve(context.Background(), input)
	if err != nil || created || duplicate.ID != job.ID {
		t.Fatalf("duplicate reservation: got=%+v created=%v err=%v", duplicate, created, err)
	}
	if err := store.MarkPestoStarted(context.Background(), job.ID, 1234, time.Now()); err != nil {
		t.Fatal(err)
	}
	count, err := store.ReconcileInterruptedPosts(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile: count=%d err=%v", count, err)
	}
	reconciled, err := store.ByID(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != StateReconciliationRequired {
		t.Fatalf("state=%q", reconciled.State)
	}
	runnable, err := store.NextRunnable(context.Background())
	if err != nil || runnable != nil {
		t.Fatalf("ambiguous post became runnable: job=%+v err=%v", runnable, err)
	}
}

func TestRetryableFailureRetainsCheckpoint(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, _, err := store.Reserve(context.Background(), CreateInput{
		TorrentHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ReleaseName: "release",
		SourcePath: "/downloads/release", WorkspacePath: "/tmp/jobs/two",
		InputPath: "/tmp/jobs/two/source/release", SourceSize: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRetryableFailure(context.Background(), job.ID, StateSubmitting, errors.New("temporary outage")); err != nil {
		t.Fatal(err)
	}
	job, err = store.ByID(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != StateFailed || job.RetryFrom != StateSubmitting || job.RetryCount != 1 {
		t.Fatalf("unexpected retry state: %+v", job)
	}
}
