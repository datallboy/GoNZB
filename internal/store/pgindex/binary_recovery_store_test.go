package pgindex

import "testing"

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
