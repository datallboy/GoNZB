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

// 7z file signature (magic bytes)
var sevenZipSignature = []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}

type CLI7z struct {
	BinaryPath string
}

// NewCLI7z creates a new 7z extractor using the system's 7z binary
func NewCLI7z() (*CLI7z, error) {
	path, err := exec.LookPath("7z")
	if err != nil {
		path, err = exec.LookPath("7za")
		if err != nil {
			return nil, fmt.Errorf("7z/7za binary not found in PATH: %w", err)
		}
	}
	return &CLI7z{BinaryPath: path}, nil
}

func (z *CLI7z) Name() string {
	return "7-Zip"
}

func (z *CLI7z) CanExtract(filePath string) (bool, error) {
	lower := strings.ToLower(filepath.Base(filePath))

	if !strings.HasSuffix(lower, ".7z") {
		return false, nil
	}

	is7z, err := has7zSignature(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to verify 7z signature: %w", err)
	}

	return is7z, nil
}

// Extract extracts the archive to the destination directory.
func (z *CLI7z) Extract(ctx context.Context, archivePath string, destDir string, password string) ([]string, error) {
	return baseExtract(ctx, archivePath, destDir, func(workDir string) *exec.Cmd {
		// 7z x -o<destination> -y <archive>
		args := []string{"x", fmt.Sprintf("-o%s", workDir), "-y", "-aoa"}

		if password != "" {
			args = append(args, "-p"+password)
		} else {
			// 7z treats -p without a password awkwardly; omit it entirely when not needed.
		}

		args = append(args, archivePath)

		return exec.CommandContext(ctx, z.BinaryPath, args...)
	})
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
