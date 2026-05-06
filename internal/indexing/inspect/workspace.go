package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/segmentio/ksuid"
)

type WorkspaceManager struct {
	opts Options
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

func (m *WorkspaceManager) workspaceRoot() string {
	if m == nil {
		return ""
	}
	if m.opts.WorkspaceBackend == "memory" {
		path := strings.TrimSpace(m.opts.MemoryWorkDir)
		if path != "" {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return path
			}
			if err := os.MkdirAll(path, 0755); err == nil {
				return path
			}
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
