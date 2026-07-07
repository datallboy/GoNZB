package pgindex

import "testing"

func TestEvaluateDatabaseStorageGuardBlocksOnFreeBytes(t *testing.T) {
	evaluation := EvaluateDatabaseStorageGuard(
		DatabaseStorageStatus{FilesystemFreeBytes: 1024, FilesystemTotalBytes: 10 * 1024, FilesystemFreePercent: 10, FilesystemVisible: true},
		DatabaseStorageGuardConfig{Enabled: true, MinFreeBytes: 2048},
	)
	if !evaluation.Blocked {
		t.Fatalf("expected evaluation to block, got %+v", evaluation)
	}
}

func TestEvaluateDatabaseStorageGuardBlocksOnFreePercent(t *testing.T) {
	evaluation := EvaluateDatabaseStorageGuard(
		DatabaseStorageStatus{FilesystemFreeBytes: 10 * 1024, FilesystemTotalBytes: 100 * 1024, FilesystemFreePercent: 10, FilesystemVisible: true},
		DatabaseStorageGuardConfig{Enabled: true, MinFreePercent: 15},
	)
	if !evaluation.Blocked {
		t.Fatalf("expected evaluation to block, got %+v", evaluation)
	}
}

func TestEvaluateDatabaseStorageGuardAllowsHealthyStatus(t *testing.T) {
	evaluation := EvaluateDatabaseStorageGuard(
		DatabaseStorageStatus{FilesystemFreeBytes: 20 * 1024, FilesystemTotalBytes: 100 * 1024, FilesystemFreePercent: 20, FilesystemVisible: true},
		DatabaseStorageGuardConfig{Enabled: true, MinFreeBytes: 8 * 1024, MinFreePercent: 5},
	)
	if evaluation.Blocked {
		t.Fatalf("expected evaluation to allow stage execution, got %+v", evaluation)
	}
}

func TestEvaluateDatabaseStorageGuardBlocksWhenFilesystemUnavailable(t *testing.T) {
	evaluation := EvaluateDatabaseStorageGuard(
		DatabaseStorageStatus{FilesystemVisible: false},
		DatabaseStorageGuardConfig{Enabled: true, MinFreeBytes: 8 * 1024, MinFreePercent: 5},
	)
	if !evaluation.Blocked {
		t.Fatalf("expected evaluation to block without filesystem visibility, got %+v", evaluation)
	}
}
