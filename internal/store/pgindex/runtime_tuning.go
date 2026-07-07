package pgindex

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultBinaryUpsertDBChunkSize = 1000

const (
	defaultRetryableTxAttempts = 3
	defaultRetryableTxDelay    = 200 * time.Millisecond
)

type binaryUpsertChunkSizeContextKey struct{}
type deferReleaseFamilySummaryRefreshContextKey struct{}
type binaryUpsertTelemetryContextKey struct{}
type binaryStatsRefreshTelemetryContextKey struct{}
type skipYEncRecoveryWorkItemSyncContextKey struct{}
type skipYEncRecoveryWorkItemRetireContextKey struct{}

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

func WithSkipYEncRecoveryWorkItemSync(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipYEncRecoveryWorkItemSyncContextKey{}, true)
}

func skipYEncRecoveryWorkItemSyncFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(skipYEncRecoveryWorkItemSyncContextKey{}).(bool)
	return value
}

func WithSkipYEncRecoveryWorkItemRetire(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipYEncRecoveryWorkItemRetireContextKey{}, true)
}

func skipYEncRecoveryWorkItemRetireFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(skipYEncRecoveryWorkItemRetireContextKey{}).(bool)
	return value
}

type BinaryUpsertTelemetry struct {
	mu                             sync.Mutex
	ChunkCount                     int
	ChunkRows                      int
	ChunkRetries                   int
	ChunkRetryDeadlocks            int
	ChunkRetrySerialization        int
	ChunkDurationMs                float64
	ChunkDurationMaxMs             float64
	LockDurationMs                 float64
	LockDurationMaxMs              float64
	StageDurationMs                float64
	StageDurationMaxMs             float64
	ExistingSnapshotDurationMs     float64
	ExistingSnapshotDurationMaxMs  float64
	UpdateDurationMs               float64
	UpdateDurationMaxMs            float64
	InsertDurationMs               float64
	InsertDurationMaxMs            float64
	ObservationStatsDurationMs     float64
	ObservationStatsDurationMaxMs  float64
	IdentityDurationMs             float64
	IdentityDurationMaxMs          float64
	RecoverySeedDurationMs         float64
	RecoverySeedDurationMaxMs      float64
	LifecycleSeedDurationMs        float64
	LifecycleSeedDurationMaxMs     float64
	CompletionKeySyncDurationMs    float64
	CompletionKeySyncDurationMaxMs float64
	ReadbackDurationMs             float64
	ReadbackDurationMaxMs          float64
	UpsertQueryDurationMs          float64
	UpsertQueryDurationMaxMs       float64
	EvidenceDurationMs             float64
	EvidenceDurationMaxMs          float64
	DeferredSummaryRefreshChunks   int
	DeferredSummaryKeyCount        int
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
		ChunkCount:                     t.ChunkCount,
		ChunkRows:                      t.ChunkRows,
		ChunkRetries:                   t.ChunkRetries,
		ChunkRetryDeadlocks:            t.ChunkRetryDeadlocks,
		ChunkRetrySerialization:        t.ChunkRetrySerialization,
		ChunkDurationMs:                t.ChunkDurationMs,
		ChunkDurationMaxMs:             t.ChunkDurationMaxMs,
		LockDurationMs:                 t.LockDurationMs,
		LockDurationMaxMs:              t.LockDurationMaxMs,
		StageDurationMs:                t.StageDurationMs,
		StageDurationMaxMs:             t.StageDurationMaxMs,
		ExistingSnapshotDurationMs:     t.ExistingSnapshotDurationMs,
		ExistingSnapshotDurationMaxMs:  t.ExistingSnapshotDurationMaxMs,
		UpdateDurationMs:               t.UpdateDurationMs,
		UpdateDurationMaxMs:            t.UpdateDurationMaxMs,
		InsertDurationMs:               t.InsertDurationMs,
		InsertDurationMaxMs:            t.InsertDurationMaxMs,
		ObservationStatsDurationMs:     t.ObservationStatsDurationMs,
		ObservationStatsDurationMaxMs:  t.ObservationStatsDurationMaxMs,
		IdentityDurationMs:             t.IdentityDurationMs,
		IdentityDurationMaxMs:          t.IdentityDurationMaxMs,
		RecoverySeedDurationMs:         t.RecoverySeedDurationMs,
		RecoverySeedDurationMaxMs:      t.RecoverySeedDurationMaxMs,
		LifecycleSeedDurationMs:        t.LifecycleSeedDurationMs,
		LifecycleSeedDurationMaxMs:     t.LifecycleSeedDurationMaxMs,
		CompletionKeySyncDurationMs:    t.CompletionKeySyncDurationMs,
		CompletionKeySyncDurationMaxMs: t.CompletionKeySyncDurationMaxMs,
		ReadbackDurationMs:             t.ReadbackDurationMs,
		ReadbackDurationMaxMs:          t.ReadbackDurationMaxMs,
		UpsertQueryDurationMs:          t.UpsertQueryDurationMs,
		UpsertQueryDurationMaxMs:       t.UpsertQueryDurationMaxMs,
		EvidenceDurationMs:             t.EvidenceDurationMs,
		EvidenceDurationMaxMs:          t.EvidenceDurationMaxMs,
		DeferredSummaryRefreshChunks:   t.DeferredSummaryRefreshChunks,
		DeferredSummaryKeyCount:        t.DeferredSummaryKeyCount,
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

func (t *BinaryUpsertTelemetry) recordStageDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.StageDurationMs += durationMs
	if durationMs > t.StageDurationMaxMs {
		t.StageDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordExistingSnapshotDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ExistingSnapshotDurationMs += durationMs
	if durationMs > t.ExistingSnapshotDurationMaxMs {
		t.ExistingSnapshotDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordUpdateDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.UpdateDurationMs += durationMs
	if durationMs > t.UpdateDurationMaxMs {
		t.UpdateDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordInsertDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.InsertDurationMs += durationMs
	if durationMs > t.InsertDurationMaxMs {
		t.InsertDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordObservationStatsDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ObservationStatsDurationMs += durationMs
	if durationMs > t.ObservationStatsDurationMaxMs {
		t.ObservationStatsDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordIdentityDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.IdentityDurationMs += durationMs
	if durationMs > t.IdentityDurationMaxMs {
		t.IdentityDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordRecoverySeedDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.RecoverySeedDurationMs += durationMs
	if durationMs > t.RecoverySeedDurationMaxMs {
		t.RecoverySeedDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordLifecycleSeedDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.LifecycleSeedDurationMs += durationMs
	if durationMs > t.LifecycleSeedDurationMaxMs {
		t.LifecycleSeedDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordCompletionKeySyncDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.CompletionKeySyncDurationMs += durationMs
	if durationMs > t.CompletionKeySyncDurationMaxMs {
		t.CompletionKeySyncDurationMaxMs = durationMs
	}
}

func (t *BinaryUpsertTelemetry) recordReadbackDuration(elapsed time.Duration) {
	if t == nil {
		return
	}
	durationMs := float64(elapsed.Microseconds()) / 1000.0
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ReadbackDurationMs += durationMs
	if durationMs > t.ReadbackDurationMaxMs {
		t.ReadbackDurationMaxMs = durationMs
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

func isRetryablePostgresTxError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return containsSQLState(text, "40P01") || containsSQLState(text, "40001")
}

func retryRetryablePostgresTx(ctx context.Context, attempts int, fn func() error) error {
	if attempts <= 0 {
		attempts = defaultRetryableTxAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if !isRetryablePostgresTxError(err) || attempt == attempts {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * defaultRetryableTxDelay):
			}
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("retryable postgres transaction did not execute")
}

type BinaryStatsRefreshTelemetry struct {
	mu                            sync.Mutex
	TxCount                       int
	BatchCount                    int
	BinaryCount                   int
	SummaryKeyCount               int
	DeferredSummaryRefreshBatches int
	DeferredSummaryKeyCount       int
	StatsUpdateDurationMs         float64
	StatsUpdateDurationMaxMs      float64
	SummaryMarkDurationMs         float64
	SummaryMarkDurationMaxMs      float64
	YEncSyncDurationMs            float64
	YEncSyncDurationMaxMs         float64
	YEncAdmissionDurationMs       float64
	YEncAdmissionDurationMaxMs    float64
	YEncPriorityOpenDurationMs    float64
	YEncPriorityOpenDurationMaxMs float64
	YEncSyncChunkCount            int
	YEncSyncChunkBinaryCount      int
	YEncSyncUpserted              int64
	YEncSyncRetired               int64
	YEncSyncUpsertDurationMs      float64
	YEncSyncUpsertDurationMaxMs   float64
	YEncSyncRetireDurationMs      float64
	YEncSyncRetireDurationMaxMs   float64
	YEncPromotionDurationMs       float64
	YEncPromotionDurationMaxMs    float64
}

func (t *BinaryStatsRefreshTelemetry) recordBatch(binaryCount, summaryKeys int, deferred bool, statsUpdateDuration, summaryMarkDuration, yencSyncDuration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.TxCount++
	t.BatchCount++
	t.BinaryCount += binaryCount
	t.SummaryKeyCount += summaryKeys
	if deferred {
		t.DeferredSummaryRefreshBatches++
		t.DeferredSummaryKeyCount += summaryKeys
	}
	statsUpdateMs := float64(statsUpdateDuration.Microseconds()) / 1000.0
	t.StatsUpdateDurationMs += statsUpdateMs
	if statsUpdateMs > t.StatsUpdateDurationMaxMs {
		t.StatsUpdateDurationMaxMs = statsUpdateMs
	}
	summaryMarkMs := float64(summaryMarkDuration.Microseconds()) / 1000.0
	t.SummaryMarkDurationMs += summaryMarkMs
	if summaryMarkMs > t.SummaryMarkDurationMaxMs {
		t.SummaryMarkDurationMaxMs = summaryMarkMs
	}
	yencSyncMs := float64(yencSyncDuration.Microseconds()) / 1000.0
	t.YEncSyncDurationMs += yencSyncMs
	if yencSyncMs > t.YEncSyncDurationMaxMs {
		t.YEncSyncDurationMaxMs = yencSyncMs
	}
}

func (t *BinaryStatsRefreshTelemetry) recordYEncAdmissionDuration(d time.Duration) {
	t.recordYEncDuration(&t.YEncAdmissionDurationMs, &t.YEncAdmissionDurationMaxMs, d)
}

func (t *BinaryStatsRefreshTelemetry) recordYEncPriorityOpenDuration(d time.Duration) {
	t.recordYEncDuration(&t.YEncPriorityOpenDurationMs, &t.YEncPriorityOpenDurationMaxMs, d)
}

func (t *BinaryStatsRefreshTelemetry) recordYEncPromotionDuration(d time.Duration) {
	t.recordYEncDuration(&t.YEncPromotionDurationMs, &t.YEncPromotionDurationMaxMs, d)
}

func (t *BinaryStatsRefreshTelemetry) recordYEncSyncChunk(binaryCount int, upserted, retired int64, upsertDuration, retireDuration time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.YEncSyncChunkCount++
	t.YEncSyncChunkBinaryCount += binaryCount
	t.YEncSyncUpserted += upserted
	t.YEncSyncRetired += retired
	recordTelemetryDurationLocked(&t.YEncSyncUpsertDurationMs, &t.YEncSyncUpsertDurationMaxMs, upsertDuration)
	recordTelemetryDurationLocked(&t.YEncSyncRetireDurationMs, &t.YEncSyncRetireDurationMaxMs, retireDuration)
}

func (t *BinaryStatsRefreshTelemetry) recordYEncDuration(total *float64, max *float64, d time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	recordTelemetryDurationLocked(total, max, d)
}

func recordTelemetryDurationLocked(total *float64, max *float64, d time.Duration) {
	ms := float64(d.Microseconds()) / 1000.0
	*total += ms
	if ms > *max {
		*max = ms
	}
}

func (t *BinaryStatsRefreshTelemetry) Snapshot() BinaryStatsRefreshTelemetry {
	if t == nil {
		return BinaryStatsRefreshTelemetry{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return BinaryStatsRefreshTelemetry{
		TxCount:                       t.TxCount,
		BatchCount:                    t.BatchCount,
		BinaryCount:                   t.BinaryCount,
		SummaryKeyCount:               t.SummaryKeyCount,
		DeferredSummaryRefreshBatches: t.DeferredSummaryRefreshBatches,
		DeferredSummaryKeyCount:       t.DeferredSummaryKeyCount,
		StatsUpdateDurationMs:         t.StatsUpdateDurationMs,
		StatsUpdateDurationMaxMs:      t.StatsUpdateDurationMaxMs,
		SummaryMarkDurationMs:         t.SummaryMarkDurationMs,
		SummaryMarkDurationMaxMs:      t.SummaryMarkDurationMaxMs,
		YEncSyncDurationMs:            t.YEncSyncDurationMs,
		YEncSyncDurationMaxMs:         t.YEncSyncDurationMaxMs,
		YEncAdmissionDurationMs:       t.YEncAdmissionDurationMs,
		YEncAdmissionDurationMaxMs:    t.YEncAdmissionDurationMaxMs,
		YEncPriorityOpenDurationMs:    t.YEncPriorityOpenDurationMs,
		YEncPriorityOpenDurationMaxMs: t.YEncPriorityOpenDurationMaxMs,
		YEncSyncChunkCount:            t.YEncSyncChunkCount,
		YEncSyncChunkBinaryCount:      t.YEncSyncChunkBinaryCount,
		YEncSyncUpserted:              t.YEncSyncUpserted,
		YEncSyncRetired:               t.YEncSyncRetired,
		YEncSyncUpsertDurationMs:      t.YEncSyncUpsertDurationMs,
		YEncSyncUpsertDurationMaxMs:   t.YEncSyncUpsertDurationMaxMs,
		YEncSyncRetireDurationMs:      t.YEncSyncRetireDurationMs,
		YEncSyncRetireDurationMaxMs:   t.YEncSyncRetireDurationMaxMs,
		YEncPromotionDurationMs:       t.YEncPromotionDurationMs,
		YEncPromotionDurationMaxMs:    t.YEncPromotionDurationMaxMs,
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
