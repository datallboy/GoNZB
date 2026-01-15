package extraction

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 7z file signature (magic bytes)
var sevenZipSignature = []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}

type CLI7z struct {
	BinaryPath string
}

// NewCLI7z creates a new 7z extractor using the system's 7z binary
func NewCLI7z() (*CLI7z, error) {
	// Try both '7z' and '7za' (7za is often the standalone version)
	path, err := exec.LookPath("7z")
	if err != nil {
		path, err = exec.LookPath("7za")
		if err != nil {
			return nil, fmt.Errorf("7z/7za binary not found in PATH: %w", err)
		}
	}
	return &CLI7z{BinaryPath: path}, nil
}

// Name returns the extractor name
func (z *CLI7z) Name() string {
	return "7-Zip"
}

// CanExtract checks if the file is a 7z archive
func (z *CLI7z) CanExtract(filePath string) (bool, error) {
	lower := strings.ToLower(filepath.Base(filePath))

	// Extension check
	if !strings.HasSuffix(lower, ".7z") {
		return false, nil
	}

	// Verify 7z signature
	is7z, err := has7zSignature(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to verify 7z signature: %w", err)
	}

	return is7z, nil
}

// Extract extracts the 7z archive to the destination directory
func (z *CLI7z) Extract(ctx context.Context, archivePath string, destDir string) ([]string, error) {
	// 7z x -o<destination> -y <archive>
	// x = extract with full paths
	// -o = output directory (no space between -o and path)
	// -y = assume yes on all queries
	cmd := exec.CommandContext(ctx, z.BinaryPath, "x", fmt.Sprintf("-o%s", destDir), "-y", archivePath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("7z extraction failed: %w\nOutput: %s", err, string(output))
	}

	return []string{}, nil
}

// has7zSignature checks if the file has a valid 7z magic byte signature
func has7zSignature(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	header := make([]byte, 6)
	n, err := file.Read(header)
	if err != nil {
		return false, err
	}

	if n < 6 {
		return false, nil
	}

	return bytes.Equal(header, sevenZipSignature), nil
}
