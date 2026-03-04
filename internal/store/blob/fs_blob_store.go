package blob

import "io"

type legacyBlobStore interface {
	GetNZBReader(key string) (io.ReadCloser, error)
	CreateNZBWriter(key string) (io.WriteCloser, error)
	SaveNZBAtomically(key string, data []byte) error
	Exists(key string) bool
}

type FSBlobStore struct {
	legacy legacyBlobStore
}

func NewFSBlobStore(legacy legacyBlobStore) *FSBlobStore {
	return &FSBlobStore{legacy: legacy}
}

func (s *FSBlobStore) GetNZBReader(key string) (io.ReadCloser, error) {
	return s.legacy.GetNZBReader(key)
}

func (s *FSBlobStore) CreateNZBWriter(key string) (io.WriteCloser, error) {
	return s.legacy.CreateNZBWriter(key)
}

func (s *FSBlobStore) SaveNZBAtomically(key string, data []byte) error {
	return s.legacy.SaveNZBAtomically(key, data)
}

func (s *FSBlobStore) Exists(key string) bool {
	return s.legacy.Exists(key)
}
