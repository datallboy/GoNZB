package processor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Extractor defines the behavior for extracting compress archives
type Extractor interface {
	// Extract extracts the archive at the given path to the destination directory.
	// Returns the list of extracted file paths, or an error if extration fails.
	Extract(ctx context.Context, archivePath string, destDir string, password string) ([]string, error)

	// CanExtract checks if this extractor can handle the given file.
	CanExtract(filename string) (bool, error)

	// Returns the human-readable name of this extractor (e.g. "RAR", "ZIP")
	Name() string
}

type CmdFactory func(workDir string) *exec.Cmd

func baseExtract(ctx context.Context, archivePath, destDir string, factory CmdFactory) ([]string, error) {
	// Use a unique extraction workdir to avoid archive name collisions.
	workDir, err := os.MkdirTemp(destDir, "_extracted_*")
	if err != nil {
		return nil, fmt.Errorf("failed to create extraction workdir: %w", err)
	}

	defer os.RemoveAll(workDir)

	// Execute cli tool provided by factory
	cmd := factory(workDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w\nOutput: %s", err, string(output))
	}

	// Recursive walk and move extracted files
	var finalPaths []string
	err = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return err
		}

		if !d.IsDir() {
			relPath, err := filepath.Rel(workDir, path)
			if err != nil {
				return fmt.Errorf("failed to resolve extracted file path: %w", err)
			}
			if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
				return fmt.Errorf("invalid extracted path outside workdir: %s", relPath)
			}
			targetPath := filepath.Join(destDir, relPath)

			// Move file from extracted dir to main directory
			if err := moveFile(path, targetPath); err != nil {
				return fmt.Errorf("failed to move extracted file %s: %w", relPath, err)
			}

			finalPaths = append(finalPaths, targetPath)
		}
		return nil
	})

	return finalPaths, err
}
