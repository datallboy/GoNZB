package repair

import (
	"context"
	"fmt"
	"os/exec"
)

type CLIPar2 struct {
	BinaryPath string
}

func NewCLIPar2() (*CLIPar2, error) {
	path, err := exec.LookPath("par2")
	if err != nil {
		return nil, fmt.Errorf("par2 binary not found in PATH: %w", err)
	}
	return &CLIPar2{BinaryPath: path}, nil
}

func (c *CLIPar2) Verify(ctx context.Context, path string) (bool, error) {
	// 'v' is verify, '-q' is quiet
	cmd := exec.CommandContext(ctx, c.BinaryPath, "v", "-q", path)
	err := cmd.Run()
	if err == nil {
		return true, nil // Exit code 0
	}

	if exitError, ok := err.(*exec.ExitError); ok {
		// Exit code 1
		if exitError.ExitCode() == 1 {
			return false, nil // Damanged but repairable
		}
	}
	return false, err // Hard error or unrepairable (Exit code 2+)
}

func (c *CLIPar2) Repair(ctx context.Context, path string) error {
	// 'r' is repair
	cmd := exec.CommandContext(ctx, c.BinaryPath, "r", path)
	return cmd.Run()
}
