package engine

import (
	"fmt"
	"os"
	"sync"
)

type fileHandle struct {
	mu   sync.Mutex
	file *os.File
}

type FileWriter struct {
	mu      sync.RWMutex
	handles map[string]*fileHandle
}

func NewFileWriter() *FileWriter {
	return &FileWriter{
		handles: make(map[string]*fileHandle),
	}
}

// WriteAt finds the handle and performs a thread-safe write
func (fw *FileWriter) WriteAt(path string, data []byte, offset int64) error {
	h, err := fw.getOrCreateFile(path)
	if err != nil {
		return err
	}

	// Lock the handle to ensure sequential access if needed
	h.mu.Lock()
	defer h.mu.Unlock()

	// WriteAt is thread-safe on Linux/Unix for the same file descriptor
	_, err = h.file.WriteAt(data, offset)
	return err
}

func (fw *FileWriter) PreAllocate(path string, size int64) error {
	// getOrCreateFile opens the file with os.O_RDWR | os.O_CREATE
	h, err := fw.getOrCreateFile(path)
	if err != nil {
		return err
	}

	// On Linux/Unix, Truncate creates a sparse file.
	// It updates the metadata size but doesn't fill blocks with zeros yet.
	return h.file.Truncate(size)
}

func (fw *FileWriter) getOrCreateFile(path string) (*fileHandle, error) {
	// Read-Lock: Check if handle exists
	fw.mu.RLock()
	h, ok := fw.handles[path]
	fw.mu.RUnlock()
	if ok {
		return h, nil
	}

	// Write-Lock: Prepare to create handle
	fw.mu.Lock()
	defer fw.mu.Unlock()

	h, ok = fw.handles[path]
	if ok {
		return h, nil
	}

	// Create the file with Read/Write permissions
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("could not open final file: %w", err)
	}

	h = &fileHandle{
		file: f,
	}

	fw.handles[path] = h

	return h, nil
}

func (fw *FileWriter) CloseAll() {
	fw.mu.RLock()
	// We iterate over keys because CloseFile will be modifying the map
	paths := make([]string, 0, len(fw.handles))
	for path := range fw.handles {
		paths = append(paths, path)
	}
	fw.mu.RUnlock()

	for _, path := range paths {
		_ = fw.CloseFile(path, 0) // Ignore error on global cleanup
	}
}

func (fw *FileWriter) CloseFile(path string, finalSize int64) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	h, ok := fw.handles[path]
	if !ok {
		return nil // Already closed: defer will handle it
	}

	// Remove from our map so we don't try to use a closed handle later
	delete(fw.handles, path)

	// Perform I/O outside the map lock
	h.mu.Lock()
	defer h.mu.Unlock()

	// Ensure the final is exacty the size yEnc reported
	// This removes any extra padding from the initial NZB PreAllocate
	if finalSize > 0 {
		if err := h.file.Truncate(finalSize); err != nil {
			return fmt.Errorf("failed to truncate to final size: %w", err)
		}
	}

	// Sync to disk and close
	h.file.Sync()
	err := h.file.Close()

	return err
}
