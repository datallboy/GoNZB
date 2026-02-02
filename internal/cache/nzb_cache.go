package cache

import (
	"os"
	"path/filepath"
)

// FileCache implements indexer.IndexerCache
type FileCache struct {
	Dir string
}

func (f *FileCache) Get(id string) ([]byte, error) {
	// We use the ID as the filename
	path := filepath.Join(f.Dir, id+".nzb")
	return os.ReadFile(path)
}

func (f *FileCache) Put(id string, data []byte) error {
	// Ensure the directory exists
	if err := os.MkdirAll(f.Dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(f.Dir, id+".nzb")
	return os.WriteFile(path, data, 0644)
}

func (f *FileCache) Exists(key string) bool {
	_, err := os.Stat(filepath.Join(f.Dir, key))
	return err == nil
}
