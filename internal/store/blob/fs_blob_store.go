package blob

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func (s *FSBlobStore) pathForObjectKey(key string) string {
	key = strings.TrimSpace(strings.TrimPrefix(key, "/"))
	if key == "" {
		key = "unknown.bin"
	}
	return filepath.Join(s.blobDir, filepath.FromSlash(key))
}

func (s *FSBlobStore) pathForNZBKey(key string) string {
	key = strings.TrimSpace(strings.TrimPrefix(key, "/"))
	if key == "" {
		key = "unknown.nzb"
	}
	if !strings.HasSuffix(strings.ToLower(key), ".nzb") {
		key += ".nzb"
	}
	return filepath.Join(s.blobDir, filepath.FromSlash(key))
}

func (s *FSBlobStore) GetObjectReader(id string) (io.ReadCloser, error) {
	path := s.pathForObjectKey(id)
	return os.Open(path)
}

func (s *FSBlobStore) CreateObjectWriter(id string) (io.WriteCloser, error) {
	path := s.pathForObjectKey(id)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

func (s *FSBlobStore) SaveObjectAtomically(id string, data []byte) (err error) {
	finalPath := s.pathForObjectKey(id)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(finalPath), filepath.Base(finalPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp object file: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp object file: %w", err)
	}
	if err = tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp object file: %w", err)
	}
	if err = tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp object file: %w", err)
	}
	if err = os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to commit object file: %w", err)
	}
	return nil
}

func (s *FSBlobStore) ExistsObject(id string) bool {
	path := s.pathForObjectKey(id)
	_, err := os.Stat(path)
	return err == nil
}

func (s *FSBlobStore) GetNZBReader(id string) (io.ReadCloser, error) {
	path := s.pathForNZBKey(id)
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
	path := s.pathForNZBKey(id)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

func (s *FSBlobStore) SaveNZBAtomically(id string, data []byte) (err error) {
	finalPath := s.pathForNZBKey(id)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(finalPath), filepath.Base(finalPath)+".*.tmp")
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
	path := s.pathForNZBKey(id)
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
