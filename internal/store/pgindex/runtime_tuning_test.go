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
