package downloader

import (
	"fmt"
	"os"
	"sync"
)

type FileWriter struct {
	mu    sync.Mutex
	files map[string]*os.File
}

func NewFileWriter() *FileWriter {
	return &FileWriter{
		files: make(map[string]*os.File),
	}
}

func (fw *FileWriter) Write(path string, offset int64, data []byte) error {
	f, err := fw.getOrCreateFile(path)
	if err != nil {
		return err
	}

	// WriteAt is thread-safe on Linux/Unix for the same file descriptor
	_, err = f.WriteAt(data, offset)
	return err
}

func (fw *FileWriter) PreAllocate(path string, size int64) error {
	// getOrCreateFile opens the file with os.O_RDWR | os.O_CREATE
	f, err := fw.getOrCreateFile(path)
	if err != nil {
		return err
	}

	// On Linux/Unix, Truncate creates a sparse file.
	// It updates the metadata size but doesn't fill blocks with zeros yet.
	return f.Truncate(size)
}

func (fw *FileWriter) getOrCreateFile(path string) (*os.File, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	f, ok := fw.files[path]
	if ok {
		return f, nil
	}

	// Create the file with Read/Write permissions
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("could not open final file: %w", err)
	}
	fw.files[path] = f
	return f, nil
}

func (fw *FileWriter) CloseAll() {
	fw.mu.Lock()
	// We iterate over keys because CloseFile will be modifying the map
	paths := make([]string, 0, len(fw.files))
	for path := range fw.files {
		paths = append(paths, path)
	}
	fw.mu.Unlock()

	for _, path := range paths {
		_ = fw.CloseFile(path) // Ignore error on global cleanup
	}
}

func (fw *FileWriter) CloseFile(path string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	f, ok := fw.files[path]
	if !ok {
		return nil // Already closed or never opened
	}

	// Sync to disk and close
	f.Sync()
	err := f.Close()

	// Remove from our map so we don't try to use a closed handle later
	delete(fw.files, path)

	return err
}
