package pgindex

import (
	"context"
	"testing"
)

func TestSyncBinaryCompletionKeysChunksLargeIDSet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer rollbackTx(tx)

	binaryIDs := make([]int64, 70000)
	for i := range binaryIDs {
		binaryIDs[i] = int64(i + 1)
	}
	if err := syncBinaryCompletionKeysForBinaryIDsInTx(ctx, tx, binaryIDs); err != nil {
		t.Fatalf("sync large binary completion key id set: %v", err)
	}
}

func TestShouldApplyArchiveFamilyRecoveryUsesExpectedFileCount(t *testing.T) {
	seed := binaryRecoverySeed{ExpectedFileCount: 10}
	siblings := []binaryRecoverySeed{
		{FileName: "opaque.bin", TotalBytes: 1024},
		{FileName: "opaque.bin", TotalBytes: 1024},
	}
	if !shouldApplyArchiveFamilyRecovery(seed, siblings) {
		t.Fatalf("expected expected_file_count family recovery to be allowed")
	}
}

func TestShouldApplyArchiveFamilyRecoveryRejectsOvergroupedExpectedCount(t *testing.T) {
	seed := binaryRecoverySeed{ExpectedFileCount: 126}
	siblings := make([]binaryRecoverySeed, 4096)
	for i := range siblings {
		siblings[i] = binaryRecoverySeed{FileName: "opaque.bin", TotalBytes: 1024}
	}
	if shouldApplyArchiveFamilyRecovery(seed, siblings) {
		t.Fatalf("expected overgrouped expected-count family to reject archive family recovery")
	}
}

func TestShouldApplyArchiveFamilyRecoveryRejectsHugeOpaqueFamilies(t *testing.T) {
	seed := binaryRecoverySeed{}
	siblings := make([]binaryRecoverySeed, maxArchiveFamilyRecoverySiblings+1)
	for i := range siblings {
		siblings[i] = binaryRecoverySeed{FileName: "opaque.bin", TotalBytes: 740000}
	}
	if shouldApplyArchiveFamilyRecovery(seed, siblings) {
		t.Fatalf("expected huge opaque family to reject archive family recovery")
	}
}

func TestShouldApplyArchiveFamilyRecoveryAcceptsCoherentOpaqueFamilies(t *testing.T) {
	seed := binaryRecoverySeed{}
	siblings := []binaryRecoverySeed{
		{FileName: "a.bin", TotalBytes: 740000},
		{FileName: "b.bin", TotalBytes: 740100},
		{FileName: "c.bin", TotalBytes: 739900},
		{FileName: "d.bin", TotalBytes: 740050},
		{FileName: "e.bin", TotalBytes: 120000},
	}
	if !shouldApplyArchiveFamilyRecovery(seed, siblings) {
		t.Fatalf("expected coherent opaque family to allow archive family recovery")
	}
}

func TestShouldApplyArchiveFamilyRecoveryRejectsIncoherentFamilies(t *testing.T) {
	seed := binaryRecoverySeed{}
	siblings := []binaryRecoverySeed{
		{FileName: "a.bin", TotalBytes: 1 * 1024 * 1024},
		{FileName: "b.bin", TotalBytes: 12 * 1024 * 1024},
		{FileName: "c.bin", TotalBytes: 36 * 1024 * 1024},
		{FileName: "d.bin", TotalBytes: 80 * 1024 * 1024},
	}
	if shouldApplyArchiveFamilyRecovery(seed, siblings) {
		t.Fatalf("expected incoherent opaque family to reject archive family recovery")
	}
}
