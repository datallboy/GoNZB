package downloader

import (
	"fmt"
	"os"
	"sync"
)

type FileWriter struct {
	mu    sync.RWMutex
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

func (fw *FileWriter) getOrCreateFile(path string) (*os.File, error) {
	fw.mu.RLock()
	f, ok := fw.files[path]
	fw.mu.RUnlock()
	if ok {
		return f, nil
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Double-check pattern
	if f, ok := fw.files[path]; ok {
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
	defer fw.mu.Unlock()
	for _, f := range fw.files {
		f.Sync() // Ensure data is flushed to Linux disk cache
		f.Close()
	}
}
