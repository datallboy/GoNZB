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

func NewCLIUnrar() (*CLIUnrar, error) {
	path, err := exec.LookPath("unrar")
	if err != nil {
		return nil, fmt.Errorf("unrar binary not found in PATH: %w", err)
	}
	return &CLIUnrar{BinaryPath: path}, nil
}

func (u *CLIUnrar) Name() string {
	return "RAR"
}

func (u *CLIUnrar) CanExtract(filePath string) (bool, error) {
	lower := strings.ToLower(filepath.Base(filePath))

	if !strings.HasSuffix(lower, ".rar") {
		return false, nil
	}

	if strings.Contains(lower, ".part") {
		if !(strings.Contains(lower, ".part01.rar") ||
			strings.Contains(lower, ".part001.rar") ||
			strings.Contains(lower, ".part1.rar")) {
			return false, nil
		}
	}

	isRar, err := hasRarSignature(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to verify RAR signature: %w", err)
	}

	return isRar, nil
}

func (u *CLIUnrar) Extract(ctx context.Context, archivePath string, destDir string, password string) ([]string, error) {
	return baseExtract(ctx, archivePath, destDir, func(workDir string) *exec.Cmd {
		args := []string{"x", "-o+", "-y"}

		if password != "" {
			args = append(args, "-p"+password)
		} else {
			args = append(args, "-p-")
		}

		args = append(args, archivePath, workDir+string(filepath.Separator))
		return exec.CommandContext(ctx, u.BinaryPath, args...)
	})
}

func hasRarSignature(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	header := make([]byte, 8)
	n, err := file.Read(header)
	if err != nil {
		return false, err
	}

	if n < 7 {
		return false, nil
	}

	for _, sig := range rarSignatures {
		if bytes.Equal(header[:len(sig)], sig) {
			return true, nil
		}
	}

	return false, nil
}
