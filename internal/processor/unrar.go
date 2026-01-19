package processor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RAR file signatures (magic bytes)
var rarSignatures = [][]byte{
	{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00},       // RAR 1.5+
	{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00}, // RAR 5.0+
}

type CLIUnrar struct {
	BinaryPath string
}

// NewCLIUnrar creates a new UnRAR extractor using the system's unrar binary.
// Returns an error if the unrar binary is not found in PATH.
func NewCLIUnrar() (*CLIUnrar, error) {
	path, err := exec.LookPath("unrar")
	if err != nil {
		return nil, fmt.Errorf("unrar binary not found in PATH: %w", err)
	}
	return &CLIUnrar{BinaryPath: path}, nil
}

// Name returns the extractor name
func (u *CLIUnrar) Name() string {
	return "RAR"
}

// CanExtract checks if the file is a RAR archive by verifying:
// 1. File extension (.rar)
// 2. Magic bytes (file signature)
// 3. For multi-part archives, only extract the first part
func (u *CLIUnrar) CanExtract(filePath string) (bool, error) {
	lower := strings.ToLower(filepath.Base(filePath))

	// Quick extension check first
	if !strings.HasSuffix(lower, ".rar") {
		return false, nil
	}

	// For multi-part archives, only process the first part
	if strings.Contains(lower, ".part") {
		// Check if it's part01/part001/part1
		if !(strings.Contains(lower, ".part01.rar") ||
			strings.Contains(lower, ".part001.rar") ||
			strings.Contains(lower, ".part1.rar")) {
			return false, nil // Skip other parts
		}
	}

	// Verify RAR signature (magic bytes)
	isRar, err := hasRarSignature(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to verify RAR signature: %w", err)
	}

	return isRar, nil
}

// Extract extracts the RAR archive to the destination directory
func (u *CLIUnrar) Extract(ctx context.Context, archivePath string, destDir string, password string) ([]string, error) {
	return baseExtract(ctx, archivePath, destDir, func(workDir string) *exec.Cmd {
		// unrar x -o+ -y <archive> <destination>
		// x = extract with full paths
		// -o+ = overwrite existing files
		// -y = assume yes on all queries (non-interactive)
		// -kb = keep broken
		args := []string{"x", "-o+", "-y", "-kb"}

		if password != "" {
			args = append(args, "-p"+password)
		} else {
			args = append(args, "-p-")
		}

		args = append(args, archivePath, workDir+string(filepath.Separator))

		cmd := exec.CommandContext(ctx, u.BinaryPath, args...)
		return cmd
	})
}

// hasRarSignature checks if the file has a valid RAR magic byte signature
func hasRarSignature(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// Read first 8 bytes (enough for RAR 5.0 signature)
	header := make([]byte, 8)
	n, err := file.Read(header)
	if err != nil {
		return false, err
	}

	if n < 7 {
		return false, nil // File too small to be RAR
	}

	// Check against known RAR signatures
	for _, sig := range rarSignatures {
		if bytes.Equal(header[:len(sig)], sig) {
			return true, nil
		}
	}

	return false, nil
}
