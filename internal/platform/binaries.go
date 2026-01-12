package platform

import (
	"fmt"
	"os/exec"
)

// RequiredBinaries lists external system binaries the app needs to function
var RequiredBinaries = []string{
	"par2",
}

var OptionalExtractorBinaries = map[string]string{
	"unrar": "RAR",
	"unzip": "ZIP",
	"7z":    "7-Zip",
	"7za":   "7-Zip",
}

func ValidateDependencies() error {
	for _, bin := range RequiredBinaries {
		_, err := exec.LookPath(bin)
		if err != nil {
			return fmt.Errorf("required dependency: '%s' not found in PATH", bin)
		}
	}

	// Check optional extractors
	for bin, formatName := range OptionalExtractorBinaries {
		if _, err := exec.LookPath(bin); err != nil {
			fmt.Printf("Info: %s (%s) not found. %s extraction will be disabled.\n", bin, formatName, formatName)
		}
	}

	return nil
}
