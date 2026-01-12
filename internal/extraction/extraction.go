package extraction

import "context"

// Extractor defines the behavior for extracting compress archives
type Extractor interface {
	// Extract extracts the archive at the given path to the destination directory.
	// Returns the list of extracted file paths, or an error if extration fails.
	Extract(ctx context.Context, archivePath string, destDir string) ([]string, error)

	// CanExtract checks if this extractor can handle the given file.
	CanExtract(filename string) (bool, error)

	// Returns the human-readable name of this extractor (e.g. "RAR", "ZIP")
	Name() string
}
