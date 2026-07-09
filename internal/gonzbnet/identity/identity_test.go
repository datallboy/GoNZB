package identity

import (
	"context"
	"testing"
)

func TestLoadOrCreatePersistsNodeIdentity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("load first identity: %v", err)
	}
	firstID, err := first.NodeID(ctx)
	if err != nil {
		t.Fatalf("first node id: %v", err)
	}

	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("load second identity: %v", err)
	}
	secondID, err := second.NodeID(ctx)
	if err != nil {
		t.Fatalf("second node id: %v", err)
	}

	if firstID == "" {
		t.Fatalf("expected non-empty node id")
	}
	if firstID != secondID {
		t.Fatalf("expected persistent node id %q, got %q", firstID, secondID)
	}

	publicKey, err := first.PublicKey(ctx)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	if got := NodeIDFromPublicKey(publicKey); got != firstID {
		t.Fatalf("expected node id derived from public key %q, got %q", firstID, got)
	}
}
