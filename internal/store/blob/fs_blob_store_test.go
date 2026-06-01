package blob

import (
	"context"
	"io"
	"testing"
)

func TestFSBlobStoreSupportsNestedNZBKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSBlobStore(dir, noopCacheIndexer{})
	if err != nil {
		t.Fatalf("new fs blob store: %v", err)
	}

	if err := store.SaveNZBAtomically("releases/1/rel/hash.nzb", []byte("payload")); err != nil {
		t.Fatalf("save nested nzb: %v", err)
	}
	if !store.Exists("releases/1/rel/hash.nzb") {
		t.Fatalf("expected nested key to exist")
	}

	reader, err := store.GetNZBReader("releases/1/rel/hash.nzb")
	if err != nil {
		t.Fatalf("get nested nzb: %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read nested nzb: %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("payload=%q want payload", string(body))
	}
}

type noopCacheIndexer struct{}

func (noopCacheIndexer) MarkReleaseCached(context.Context, string, int64, int64) error { return nil }
func (noopCacheIndexer) MarkReleaseCacheMissing(context.Context, string, string) error { return nil }
