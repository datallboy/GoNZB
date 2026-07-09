package identity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadOrCreateWithPasswordPersistsEncryptedIdentity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	first, err := LoadOrCreateWithPassword(dir, "correct horse battery staple")
	if err != nil {
		t.Fatalf("load encrypted identity: %v", err)
	}
	firstID, err := first.NodeID(ctx)
	if err != nil {
		t.Fatalf("first node id: %v", err)
	}
	raw := readKeyFile(t, dir)
	if !strings.Contains(raw, encryptedKeyEnvelopeV1) {
		t.Fatalf("expected encrypted key envelope, got %q", raw)
	}

	second, err := LoadOrCreateWithPassword(dir, "correct horse battery staple")
	if err != nil {
		t.Fatalf("reload encrypted identity: %v", err)
	}
	secondID, err := second.NodeID(ctx)
	if err != nil {
		t.Fatalf("second node id: %v", err)
	}
	if firstID != secondID {
		t.Fatalf("expected encrypted identity to persist node id %q, got %q", firstID, secondID)
	}
}

func TestLoadEncryptedIdentityRejectsWrongPassword(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateWithPassword(dir, "correct password"); err != nil {
		t.Fatalf("create encrypted identity: %v", err)
	}
	if _, err := LoadOrCreateWithPassword(dir, "wrong password"); err == nil {
		t.Fatalf("expected wrong password to fail")
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatalf("expected encrypted key without password to fail")
	}
}

func TestLoadOrCreateWithPasswordMigratesPlaintextIdentity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	plain, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("create plaintext identity: %v", err)
	}
	plainID, err := plain.NodeID(ctx)
	if err != nil {
		t.Fatalf("plaintext node id: %v", err)
	}
	before := readKeyFile(t, dir)
	if strings.Contains(before, encryptedKeyEnvelopeV1) {
		t.Fatalf("expected plaintext key before migration")
	}

	encrypted, err := LoadOrCreateWithPassword(dir, "migration password")
	if err != nil {
		t.Fatalf("migrate plaintext identity: %v", err)
	}
	encryptedID, err := encrypted.NodeID(ctx)
	if err != nil {
		t.Fatalf("encrypted node id: %v", err)
	}
	if plainID != encryptedID {
		t.Fatalf("expected migrated identity to keep node id %q, got %q", plainID, encryptedID)
	}
	after := readKeyFile(t, dir)
	if !strings.Contains(after, encryptedKeyEnvelopeV1) {
		t.Fatalf("expected encrypted key after migration, got %q", after)
	}
}

func readKeyFile(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, DefaultKeyFileName))
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	return string(raw)
}
