package processor

import (
	"context"
	"fmt"
	"path/filepath"
)

// Repairer defines the behavior for verifying and fixing downloads
type Repairer interface {
	// Verify checks if the file in the directory are healthy.
	// Returns true if healthy, false if repair is needed.
	Verify(path string) (bool, error)

	// Repair attempts to fix the files using available parity volumes.
	Repair(path string) error
}

func (p *Processor) handleRepair(ctx context.Context, primaryPar string) error {
	p.ctx.Logger.Debug("PAR2 Index found: %s. Verifying...", filepath.Base(primaryPar))

	repairer, err := NewCLIPar2()
	if err != nil {
		return fmt.Errorf("cannot initialize repair engine: %w", err)

	}

	healthy, err := repairer.Verify(ctx, primaryPar)
	if err == nil && healthy {
		p.ctx.Logger.Info("All files verified healthy via PAR2.")
		return nil
	}

	// Check for Exit Code 1 (Damanged but repairable)
	p.ctx.Logger.Warn("Files are damanged. Attemting repair...")
	if repairErr := repairer.Repair(ctx, primaryPar); repairErr != nil {
		return fmt.Errorf("PAR2 repair failed: %w", repairErr)
	}

	p.ctx.Logger.Info("Repair complete.")
	return nil
}
