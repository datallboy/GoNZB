package pgindex

import (
	"context"
	"errors"
	"testing"
)

func TestRetryRetryablePostgresTxRetriesDeadlock(t *testing.T) {
	attempts := 0
	err := retryRetryablePostgresTx(context.Background(), 3, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("ERROR: deadlock detected (SQLSTATE 40P01)")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryRetryablePostgresTx returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryRetryablePostgresTxDoesNotRetryNonRetryableError(t *testing.T) {
	attempts := 0
	want := errors.New("boom")
	err := retryRetryablePostgresTx(context.Background(), 3, func() error {
		attempts++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected error %v, got %v", want, err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestAssemblyChunkSizesFromContext(t *testing.T) {
	ctx := context.Background()
	if got := binaryUpsertChunkSizeFromContext(ctx); got != 1000 {
		t.Fatalf("default binary upsert chunk = %d, want 1000", got)
	}
	if got := binaryPartUpsertChunkSizeFromContext(ctx); got != 5000 {
		t.Fatalf("default binary part upsert chunk = %d, want 5000", got)
	}
	if got := binaryStatsRefreshChunkSizeFromContext(ctx); got != 500 {
		t.Fatalf("default binary stats refresh chunk = %d, want 500", got)
	}

	ctx = WithBinaryUpsertChunkSize(ctx, 200)
	ctx = WithBinaryPartUpsertChunkSize(ctx, 300)
	ctx = WithBinaryStatsRefreshChunkSize(ctx, 400)
	if got := binaryUpsertChunkSizeFromContext(ctx); got != 200 {
		t.Fatalf("binary upsert chunk = %d, want 200", got)
	}
	if got := binaryPartUpsertChunkSizeFromContext(ctx); got != 300 {
		t.Fatalf("binary part upsert chunk = %d, want 300", got)
	}
	if got := binaryStatsRefreshChunkSizeFromContext(ctx); got != 400 {
		t.Fatalf("binary stats refresh chunk = %d, want 400", got)
	}
}
