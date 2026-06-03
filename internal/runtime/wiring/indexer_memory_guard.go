package wiring

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

type IndexerMemoryGuardConfig struct {
	Enabled             bool
	MinAvailableBytes   int64
	MinAvailablePercent float64
	MinSwapFreeBytes    int64
}

type IndexerMemoryStatus struct {
	MemTotalBytes     int64
	MemAvailableBytes int64
	MemAvailablePct   float64
	SwapTotalBytes    int64
	SwapFreeBytes     int64
	Visible           bool
}

type cachedMemoryGuard struct {
	config     IndexerMemoryGuardConfig
	lastCheck  time.Time
	lastResult supervisor.StageGateDecision
	mu         sync.Mutex
}

func (g *cachedMemoryGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if shouldAlwaysAllowOnLowMemory(stage.Name) {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	if time.Since(g.lastCheck) < 5*time.Second {
		result := g.lastResult
		g.mu.Unlock()
		return result, nil
	}
	g.mu.Unlock()

	status, err := readIndexerMemoryStatus(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("check host memory status: %w", err)
	}
	decision := evaluateIndexerMemoryGuard(status, g.config)

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResult = decision
	g.mu.Unlock()
	return decision, nil
}

func shouldAlwaysAllowOnLowMemory(name supervisor.StageName) bool {
	switch name {
	case supervisor.StageMaintenance,
		supervisor.StageReleasePurgeArchivedSources:
		return true
	default:
		return false
	}
}

func readIndexerMemoryStatus(ctx context.Context) (IndexerMemoryStatus, error) {
	if err := ctx.Err(); err != nil {
		return IndexerMemoryStatus{}, err
	}
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return IndexerMemoryStatus{Visible: false}, nil
		}
		return IndexerMemoryStatus{}, err
	}
	defer file.Close()

	values := map[string]int64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	if err := scanner.Err(); err != nil {
		return IndexerMemoryStatus{}, err
	}

	status := IndexerMemoryStatus{
		MemTotalBytes:     values["MemTotal"],
		MemAvailableBytes: values["MemAvailable"],
		SwapTotalBytes:    values["SwapTotal"],
		SwapFreeBytes:     values["SwapFree"],
		Visible:           values["MemTotal"] > 0,
	}
	if status.MemTotalBytes > 0 {
		status.MemAvailablePct = (float64(status.MemAvailableBytes) / float64(status.MemTotalBytes)) * 100
	}
	return status, nil
}

func evaluateIndexerMemoryGuard(status IndexerMemoryStatus, cfg IndexerMemoryGuardConfig) supervisor.StageGateDecision {
	if !cfg.Enabled || !status.Visible {
		return supervisor.StageGateDecision{Allowed: true}
	}
	if cfg.MinAvailableBytes > 0 && status.MemAvailableBytes < cfg.MinAvailableBytes {
		return supervisor.StageGateDecision{
			Allowed: false,
			Reason: fmt.Sprintf(
				"host memory low: available_bytes=%d threshold=%d available_percent=%.2f swap_free_bytes=%d",
				status.MemAvailableBytes,
				cfg.MinAvailableBytes,
				status.MemAvailablePct,
				status.SwapFreeBytes,
			),
		}
	}
	if cfg.MinAvailablePercent > 0 && status.MemAvailablePct < cfg.MinAvailablePercent {
		return supervisor.StageGateDecision{
			Allowed: false,
			Reason: fmt.Sprintf(
				"host memory low: available_percent=%.2f threshold=%.2f available_bytes=%d swap_free_bytes=%d",
				status.MemAvailablePct,
				cfg.MinAvailablePercent,
				status.MemAvailableBytes,
				status.SwapFreeBytes,
			),
		}
	}
	return supervisor.StageGateDecision{Allowed: true}
}
