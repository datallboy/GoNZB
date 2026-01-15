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

// ZIP file signatures (magic bytes)
var zipSignatures = [][]byte{
	{0x50, 0x4B, 0x03, 0x04}, // Standard ZIP
	{0x50, 0x4B, 0x05, 0x06}, // Empty ZIP
	{0x50, 0x4B, 0x07, 0x08}, // Spanned ZIP
}

type CLIUnzip struct {
	BinaryPath string
}

func NewCLIUnzip() (*CLIUnzip, error) {
	path, err := exec.LookPath("unzip")
	if err != nil {
		return nil, fmt.Errorf("unzip binary not found in PATH: %w", err)
	}
	return &CLIUnzip{BinaryPath: path}, nil
}

// Name returns the extractor name
func (u *CLIUnzip) Name() string {
	return "ZIP"
}

// CanExtract checks if the file is a ZIP archive
func (u *CLIUnzip) CanExtract(filePath string) (bool, error) {
	lower := strings.ToLower(filepath.Base(filePath))

	// Extension check
	if !strings.HasSuffix(lower, ".zip") {
		return false, nil
	}

	// Verify ZIP signature
	isZip, err := hasZipSignature(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to verify ZIP signature: %w", err)
	}

	return isZip, nil
}

// Extract extracts the ZIP archive to the destination directory
func (u *CLIUnzip) Extract(ctx context.Context, archivePath string, destDir string) ([]string, error) {
	// unzip -o <archive> -d <destination>
	// -o = overwrite existing files
	// -q = quiet mode
	cmd := exec.CommandContext(ctx, u.BinaryPath, "-o", "-q", archivePath, "-d", destDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("unzip extraction failed: %w\nOutput: %s", err, string(output))
	}

	return []string{}, nil
}

// hasZipSignature checks if the file has a valid ZIP magic byte signature
func hasZipSignature(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	header := make([]byte, 4)
	n, err := file.Read(header)
	if err != nil {
		return false, err
	}

	if n < 4 {
		return false, nil
	}

	// Check against known ZIP signatures
	for _, sig := range zipSignatures {
		if bytes.Equal(header, sig) {
			return true, nil
		}
	}

	return false, nil
}
