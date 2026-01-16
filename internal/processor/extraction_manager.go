package processor

import (
	"fmt"

	"github.com/datallboy/gonzb/internal/nzb"
)

// Manager handles multiple extractors and determines which to use
type Manager struct {
	extractors []Extractor
}

// NewManager creates a new extraction manager and initilizes available extractors
func NewManager() *Manager {
	m := &Manager{
		extractors: make([]Extractor, 0),
	}

	// Try to initialize each extractor
	// If the binary isn't available, skip it

	if unrar, err := NewCLIUnrar(); err == nil {
		m.extractors = append(m.extractors, unrar)
	}

	if unzip, err := NewCLIUnzip(); err == nil {
		m.extractors = append(m.extractors, unzip)
	}

	if sevenZ, err := NewCLI7z(); err == nil {
		m.extractors = append(m.extractors, sevenZ)
	}

	return m
}

// AvailableExtractors returns the names of available extractors
func (m *Manager) AvailableExtractors() []string {
	names := make([]string, len(m.extractors))
	for i, ext := range m.extractors {
		names[i] = ext.Name()
	}
	return names
}

// HasExtractors returns true if any extractors are available
func (m *Manager) HasExtractors() bool {
	return len(m.extractors) > 0
}

// DetectArchives scans the completed tasks and returns archives that need extraction
// Returns a map of task -> extractor for each archive found
func (m *Manager) DetectArchives(tasks []*nzb.DownloadFile) (map[*nzb.DownloadFile]Extractor, error) {
	archives := make(map[*nzb.DownloadFile]Extractor)

	for _, task := range tasks {
		// Try each extractor to see if it can handle this file
		for _, extractor := range m.extractors {
			canExtract, err := extractor.CanExtract(task.FinalPath)
			if err != nil {
				return nil, fmt.Errorf("error checking if %s can extract %s: %w",
					extractor.Name(), task.CleanName, err)
			}

			if canExtract {
				archives[task] = extractor
				break // Found a matching extractor, move to next task
			}
		}
	}

	return archives, nil
}
