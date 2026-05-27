package pgindex

import (
	"context"
	"strings"
	"sync"
	"time"
)

const defaultBinaryUpsertDBChunkSize = 250

type binaryUpsertChunkSizeContextKey struct{}
type deferReleaseFamilySummaryRefreshContextKey struct{}
type binaryUpsertTelemetryContextKey struct{}
type binaryStatsRefreshTelemetryContextKey struct{}

func WithBinaryUpsertChunkSize(ctx context.Context, size int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if size <= 0 {
		return ctx
	}
	return context.WithValue(ctx, binaryUpsertChunkSizeContextKey{}, size)
}

func binaryUpsertChunkSizeFromContext(ctx context.Context) int {
	if ctx != nil {
		if size, ok := ctx.Value(binaryUpsertChunkSizeContextKey{}).(int); ok && size > 0 {
			return size
		}
	}
	return defaultBinaryUpsertDBChunkSize
}

func WithDeferredReleaseFamilySummaryRefresh(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, deferReleaseFamilySummaryRefreshContextKey{}, true)
}

func deferReleaseFamilySummaryRefreshFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(deferReleaseFamilySummaryRefreshContextKey{}).(bool)
	return value
}

type BinaryUpsertTelemetry struct {
	mu                           sync.Mutex
	ChunkCount                   int
	ChunkRows                    int
	ChunkRetries                 int
	ChunkRetryDeadlocks          int
	ChunkRetrySerialization      int
	ChunkDurationMs              float64
	ChunkDurationMaxMs           float64
	LockDurationMs               float64
	LockDurationMaxMs            float64
	UpsertQueryDurationMs        float64
	UpsertQueryDurationMaxMs     float64
	EvidenceDurationMs           float64
	EvidenceDurationMaxMs        float64
	DeferredSummaryRefreshChunks int
	DeferredSummaryKeyCount      int
}

func (t *BinaryUpsertTelemetry) recordChunk(rows, retries int, elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ChunkCount++
	t.ChunkRows += rows
	t.ChunkRetries += retries
	t.ChunkDurationMs += durationMs
	if durationMs > t.ChunkDurationMaxMs {
		t.ChunkDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordRetry(err error) {
	if t == nil || err == nil {
		return
	}
	text := err.Error()
	t.mu.Lock()
	defer t.mu.Unlock()
	if containsSQLState(text, "40P01") {
		t.ChunkRetryDeadlocks++
	}
	if containsSQLState(text, "40001") {
		t.ChunkRetrySerialization++
	}
}

func (t *BinaryUpsertTelemetry) recordDeferredSummaryRefresh(keys int) {
	if t == nil || keys <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.DeferredSummaryRefreshChunks++
	t.DeferredSummaryKeyCount += keys
}

func (t *BinaryUpsertTelemetry) Snapshot() BinaryUpsertTelemetry {
	if t == nil {
		return BinaryUpsertTelemetry{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return BinaryUpsertTelemetry{
		ChunkCount:                   t.ChunkCount,
		ChunkRows:                    t.ChunkRows,
		ChunkRetries:                 t.ChunkRetries,
		ChunkRetryDeadlocks:          t.ChunkRetryDeadlocks,
		ChunkRetrySerialization:      t.ChunkRetrySerialization,
		ChunkDurationMs:              t.ChunkDurationMs,
		ChunkDurationMaxMs:           t.ChunkDurationMaxMs,
		LockDurationMs:               t.LockDurationMs,
		LockDurationMaxMs:            t.LockDurationMaxMs,
		UpsertQueryDurationMs:        t.UpsertQueryDurationMs,
		UpsertQueryDurationMaxMs:     t.UpsertQueryDurationMaxMs,
		EvidenceDurationMs:           t.EvidenceDurationMs,
		EvidenceDurationMaxMs:        t.EvidenceDurationMaxMs,
		DeferredSummaryRefreshChunks: t.DeferredSummaryRefreshChunks,
		DeferredSummaryKeyCount:      t.DeferredSummaryKeyCount,
	}
}

func (t *BinaryUpsertTelemetry) recordLockDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.LockDurationMs += durationMs
	if durationMs > t.LockDurationMaxMs {
		t.LockDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordUpsertQueryDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.UpsertQueryDurationMs += durationMs
	if durationMs > t.UpsertQueryDurationMaxMs {
		t.UpsertQueryDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordEvidenceDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.EvidenceDurationMs += durationMs
	if durationMs > t.EvidenceDurationMaxMs {
		t.EvidenceDurationMaxMs = durationMs
	}
}

func WithBinaryUpsertTelemetry(ctx context.Context, telemetry *BinaryUpsertTelemetry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if telemetry == nil {
		return ctx
	}
	return context.WithValue(ctx, binaryUpsertTelemetryContextKey{}, telemetry)
}

func binaryUpsertTelemetryFromContext(ctx context.Context) *BinaryUpsertTelemetry {
	if ctx == nil {
		return nil
	}
	telemetry, _ := ctx.Value(binaryUpsertTelemetryContextKey{}).(*BinaryUpsertTelemetry)
	return telemetry
}

func containsSQLState(text, code string) bool {
	return len(text) > 0 && len(code) > 0 && (strings.Contains(text, "SQLSTATE "+code) || strings.Contains(text, "sqlstate "+code))
}

type BinaryStatsRefreshTelemetry struct {
	mu                            sync.Mutex
	BatchCount                    int
	BinaryCount                   int
	SummaryKeyCount               int
	DeferredSummaryRefreshBatches int
	DeferredSummaryKeyCount       int
}

func (t *BinaryStatsRefreshTelemetry) recordBatch(binaryCount, summaryKeys int, deferred bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.BatchCount++
	t.BinaryCount += binaryCount
	t.SummaryKeyCount += summaryKeys
	if deferred {
		t.DeferredSummaryRefreshBatches++
		t.DeferredSummaryKeyCount += summaryKeys
	}
}

func (t *BinaryStatsRefreshTelemetry) Snapshot() BinaryStatsRefreshTelemetry {
	if t == nil {
		return BinaryStatsRefreshTelemetry{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return BinaryStatsRefreshTelemetry{
		BatchCount:                    t.BatchCount,
		BinaryCount:                   t.BinaryCount,
		SummaryKeyCount:               t.SummaryKeyCount,
		DeferredSummaryRefreshBatches: t.DeferredSummaryRefreshBatches,
		DeferredSummaryKeyCount:       t.DeferredSummaryKeyCount,
	}
}

func WithBinaryStatsRefreshTelemetry(ctx context.Context, telemetry *BinaryStatsRefreshTelemetry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if telemetry == nil {
		return ctx
	}
	return context.WithValue(ctx, binaryStatsRefreshTelemetryContextKey{}, telemetry)
}

func binaryStatsRefreshTelemetryFromContext(ctx context.Context) *BinaryStatsRefreshTelemetry {
	if ctx == nil {
		return nil
	}
	telemetry, _ := ctx.Value(binaryStatsRefreshTelemetryContextKey{}).(*BinaryStatsRefreshTelemetry)
	return telemetry
}
