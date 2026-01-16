package processor

// Repairer defines the behavior for verifying and fixing downloads
type Repairer interface {
	// Verify checks if the file in the directory are healthy.
	// Returns true if healthy, false if repair is needed.
	Verify(path string) (bool, error)

	// Repair attempts to fix the files using available parity volumes.
	Repair(path string) error
}
