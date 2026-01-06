package platform

import (
	"fmt"
	"os/exec"
)

// RequiredBinaries lists external system binaries the app needs to function
var RequiredBinaries = []string{
	"par2",
}

func ValidateDependencies() error {
	for _, bin := range RequiredBinaries {
		_, err := exec.LookPath(bin)
		if err != nil {
			return fmt.Errorf("required dependency: '%s' not found in PATH", bin)
		}
	}

	return nil
}
