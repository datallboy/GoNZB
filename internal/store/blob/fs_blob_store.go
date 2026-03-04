package blob

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type CacheIndexer interface {
	MarkReleaseCached(ctx context.Context, releaseID string, blobSize int64, blobMtimeUnix int64) error
	MarkReleaseCacheMissing(ctx context.Context, releaseID, reason string) error
}

type FSBlobStore struct {
	blobDir string
	cache   CacheIndexer
}

func NewFSBlobStore(blobDir string, cache CacheIndexer) (*FSBlobStore, error) {
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create blob directory: %w", err)
	}
	return &FSBlobStore{
		blobDir: blobDir,
		cache:   cache,
	}, nil
}

func (s *FSBlobStore) GetNZBReader(id string) (io.ReadCloser, error) {
	path := filepath.Join(s.blobDir, id+".nzb")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = s.cache.MarkReleaseCacheMissing(context.Background(), id, "missing blob on disk")
		}
		return nil, err
	}
	info, statErr := f.Stat()
	if statErr == nil {
		_ = s.cache.MarkReleaseCached(context.Background(), id, info.Size(), info.ModTime().Unix())
	}
	return f, nil
}

func (s *FSBlobStore) CreateNZBWriter(id string) (io.WriteCloser, error) {
	return os.Create(filepath.Join(s.blobDir, id+".nzb"))
}

func (s *FSBlobStore) SaveNZBAtomically(id string, data []byte) (err error) {
	finalPath := filepath.Join(s.blobDir, id+".nzb")

	tmpFile, err := os.CreateTemp(s.blobDir, id+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp nzb file: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp nzb file: %w", err)
	}

	if err = tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp nzb file: %w", err)
	}

	if err = tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp nzb file: %w", err)
	}

	if err = os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to commit nzb cache file: %w", err)
	}

	info, statErr := os.Stat(finalPath)
	if statErr == nil {
		_ = s.cache.MarkReleaseCached(context.Background(), id, info.Size(), info.ModTime().Unix())
	}

	return nil
}

func (s *FSBlobStore) Exists(id string) bool {
	path := filepath.Join(s.blobDir, id+".nzb")
	info, err := os.Stat(path)
	if err == nil {
		_ = s.cache.MarkReleaseCached(context.Background(), id, info.Size(), info.ModTime().Unix())
		return true
	}
	if os.IsNotExist(err) {
		_ = s.cache.MarkReleaseCacheMissing(context.Background(), id, "missing blob on disk")
	}
	return false
}
