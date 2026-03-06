package blob

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// optional no-cache mode store (process-memory only).
type EphemeralBlobStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewEphemeralBlobStore() *EphemeralBlobStore {
	return &EphemeralBlobStore{
		data: make(map[string][]byte),
	}
}

func (s *EphemeralBlobStore) GetNZBReader(key string) (io.ReadCloser, error) {
	s.mu.RLock()
	b, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("nzb payload not found for key %s", key)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *EphemeralBlobStore) CreateNZBWriter(key string) (io.WriteCloser, error) {
	var buf bytes.Buffer
	return &ephemeralWriter{
		onClose: func(p []byte) {
			s.mu.Lock()
			s.data[key] = append([]byte(nil), p...)
			s.mu.Unlock()
		},
		buf: &buf,
	}, nil
}

func (s *EphemeralBlobStore) SaveNZBAtomically(key string, data []byte) error {
	s.mu.Lock()
	s.data[key] = append([]byte(nil), data...)
	s.mu.Unlock()
	return nil
}

func (s *EphemeralBlobStore) Exists(key string) bool {
	s.mu.RLock()
	_, ok := s.data[key]
	s.mu.RUnlock()
	return ok
}

type ephemeralWriter struct {
	buf     *bytes.Buffer
	closed  bool
	onClose func([]byte)
}

func (w *ephemeralWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("writer is closed")
	}
	return w.buf.Write(p)
}

func (w *ephemeralWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.onClose(w.buf.Bytes())
	return nil
}
