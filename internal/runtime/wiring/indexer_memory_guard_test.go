package wiring

import (
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

func TestEvaluateIndexerMemoryGuardBlocksLowAvailableBytes(t *testing.T) {
	decision := evaluateIndexerMemoryGuard(IndexerMemoryStatus{
		Visible:           true,
		MemTotalBytes:     16 * 1024,
		MemAvailableBytes: 1024,
		MemAvailablePct:   6.25,
		SwapTotalBytes:    8 * 1024,
		SwapFreeBytes:     4 * 1024,
	}, IndexerMemoryGuardConfig{
		Enabled:           true,
		MinAvailableBytes: 2048,
	})
	if decision.Allowed {
		t.Fatalf("expected low available bytes to block")
	}
	if !strings.Contains(decision.Reason, "available_bytes") {
		t.Fatalf("expected available-bytes reason, got %q", decision.Reason)
	}
}

func TestEvaluateIndexerMemoryGuardAllowsLowSwapWhenMemoryHealthy(t *testing.T) {
	decision := evaluateIndexerMemoryGuard(IndexerMemoryStatus{
		Visible:           true,
		MemTotalBytes:     16 * 1024,
		MemAvailableBytes: 8 * 1024,
		MemAvailablePct:   50,
		SwapTotalBytes:    8 * 1024,
		SwapFreeBytes:     128,
	}, IndexerMemoryGuardConfig{
		Enabled:          true,
		MinSwapFreeBytes: 1024,
	})
	if !decision.Allowed {
		t.Fatalf("expected low swap alone to be allowed, got %q", decision.Reason)
	}
}

func TestEvaluateIndexerMemoryGuardBlocksLowMemoryEvenWhenSwapIsAlsoLow(t *testing.T) {
	decision := evaluateIndexerMemoryGuard(IndexerMemoryStatus{
		Visible:           true,
		MemTotalBytes:     16 * 1024,
		MemAvailableBytes: 1024,
		MemAvailablePct:   6.25,
		SwapTotalBytes:    8 * 1024,
		SwapFreeBytes:     128,
	}, IndexerMemoryGuardConfig{
		Enabled:             true,
		MinAvailableBytes:   2048,
		MinAvailablePercent: 10,
		MinSwapFreeBytes:    1024,
	})
	if decision.Allowed {
		t.Fatalf("expected low memory plus low swap to block")
	}
	if !strings.Contains(decision.Reason, "available_bytes") {
		t.Fatalf("expected memory reason, got %q", decision.Reason)
	}
}

func TestShouldAlwaysAllowOnLowMemory(t *testing.T) {
	if !shouldAlwaysAllowOnLowMemory(supervisor.StageMaintenance) {
		t.Fatalf("expected maintenance to be allowed")
	}
	if !shouldAlwaysAllowOnLowMemory(supervisor.StageReleasePurgeArchivedSources) {
		t.Fatalf("expected purge to be allowed")
	}
	if shouldAlwaysAllowOnLowMemory(supervisor.StageInspectMedia) {
		t.Fatalf("expected inspect_media to be blocked under low memory")
	}
}
