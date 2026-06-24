package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/segmentio/ksuid"
)

const WorkspaceStaleTTL = 6 * time.Hour
const workspaceFullSweepInterval = 10 * time.Minute

type WorkspaceManager struct {
	opts            Options
	mu              sync.Mutex
	lastFullCleanup time.Time
}

type Workspace struct {
	Dir               string
	ManifestPath      string
	MaterializedBytes int64
}

func NewWorkspaceManager(opts Options) *WorkspaceManager {
	cfg := DefaultOptions(opts)
	return &WorkspaceManager{opts: cfg}
}

func (m *WorkspaceManager) PrepareBinaryWorkspace(ctx context.Context, stageName string, candidate pgindex.BinaryInspectionCandidate) (*Workspace, error) {
	if m == nil {
		return nil, fmt.Errorf("workspace manager is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stageDir := filepath.Join(m.workspaceRoot(), stageName)
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return nil, fmt.Errorf("create inspect workspace root %s: %w", stageDir, err)
	}
	_, _ = cleanupStaleWorkspaces(stageDir, WorkspaceStaleTTL)
	m.cleanupWorkspaceRootIfDue()

	dir := filepath.Join(stageDir, ksuid.New().String())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create inspect workspace %s: %w", dir, err)
	}

	manifest := map[string]any{
		"stage_name":        stageName,
		"binary_id":         candidate.BinaryID,
		"release_id":        candidate.ReleaseID,
		"release_title":     candidate.ReleaseTitle,
		"file_name":         candidate.FileName,
		"total_bytes":       candidate.TotalBytes,
		"workspace_backend": m.opts.WorkspaceBackend,
		"workspace_root":    stageDir,
		"prepared_at":       time.Now().UTC(),
		"max_materialize":   m.opts.MaxBytes,
		"max_archive_depth": m.opts.MaxArchiveDepth,
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal inspect manifest: %w", err)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, b, 0644); err != nil {
		return nil, fmt.Errorf("write inspect manifest %s: %w", manifestPath, err)
	}

	return &Workspace{
		Dir:               dir,
		ManifestPath:      manifestPath,
		MaterializedBytes: int64(len(b)),
	}, nil
}

func (m *WorkspaceManager) cleanupWorkspaceRootIfDue() {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	if !m.lastFullCleanup.IsZero() && now.Sub(m.lastFullCleanup) < workspaceFullSweepInterval {
		m.mu.Unlock()
		return
	}
	m.lastFullCleanup = now
	m.mu.Unlock()
	_, _ = cleanupWorkspaceRoot(m.workspaceRoot(), WorkspaceStaleTTL)
}

func (m *WorkspaceManager) workspaceRoot() string {
	if m == nil {
		return ""
	}
	if m.opts.WorkspaceBackend == "memory" || m.opts.WorkspaceBackend == "auto" {
		path := strings.TrimSpace(m.opts.MemoryWorkDir)
		if path != "" {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return path
			}
			if err := os.MkdirAll(path, 0755); err == nil {
				return path
			}
		}
		if m.opts.WorkspaceBackend == "memory" {
			return m.opts.WorkDir
		}
	}
	return m.opts.WorkDir
}

func (w *Workspace) Cleanup() error {
	if w == nil || w.Dir == "" {
		return nil
	}
	return os.RemoveAll(w.Dir)
}

func CleanupStaleWorkspaceRoots(ctx context.Context, opts Options) (int, error) {
	cfg := DefaultOptions(opts)
	roots := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, root := range []string{strings.TrimSpace(cfg.WorkDir), strings.TrimSpace(cfg.MemoryWorkDir)} {
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}

	total := 0
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		cleaned, err := cleanupWorkspaceRoot(root, WorkspaceStaleTTL)
		if err != nil {
			if os.IsPermission(err) {
				continue
			}
			return total, err
		}
		total += cleaned
	}
	return total, nil
}

func cleanupWorkspaceRoot(root string, ttl time.Duration) (int, error) {
	if strings.TrimSpace(root) == "" || ttl <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		if os.IsPermission(err) {
			return 0, nil
		}
		return 0, err
	}
	total := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cleaned, err := cleanupStaleWorkspaces(filepath.Join(root, entry.Name()), ttl)
		if err != nil {
			return total, err
		}
		total += cleaned
	}
	return total, nil
}

func cleanupStaleWorkspaces(stageDir string, ttl time.Duration) (int, error) {
	if strings.TrimSpace(stageDir) == "" || ttl <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		if os.IsPermission(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-ttl)
	cleaned := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(stageDir, entry.Name()))
		cleaned++
	}
	return cleaned, nil
}
