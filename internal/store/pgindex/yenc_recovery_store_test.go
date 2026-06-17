package pgindex

import "testing"

func TestNormalizeYEncHeaderRecoveryRecordUsesRecoveredFileBinaryKey(t *testing.T) {
	record := YEncHeaderRecoveryRecord{
		SourceReleaseKey: "random-subject-token.example.com release 12345",
		ReleaseFamilyKey: "BFVOHwfmP29vSW4Zi",
		FileSetKey:       "BFVOHwfmP29vSW4Zi",
		FileFamilyKey:    "",
		ReleaseKey:       "random-subject-token.example.com release 12345",
		BinaryKey:        "random-subject-token.example.com release 12345::BFVOHwfmP29vSW4Zi part080 rar",
		BinaryName:       "BFVOHwfmP29vSW4Zi.part080.rar",
		FileName:         "BFVOHwfmP29vSW4Zi.part080.rar",
	}

	normalizeYEncHeaderRecoveryRecord(&record)

	if record.BinaryKey != "bfvohwfmp29vsw4zi::bfvohwfmp29vsw4zi part080 rar" {
		t.Fatalf("expected recovered file binary key, got %q", record.BinaryKey)
	}
	if record.SourceReleaseKey != "bfvohwfmp29vsw4zi" {
		t.Fatalf("expected recovered file-set source key, got %q", record.SourceReleaseKey)
	}
	if record.FileFamilyKey != "bfvohwfmp29vsw4zi bfvohwfmp29vsw4zi part080 rar" {
		t.Fatalf("expected recovered file family key, got %q", record.FileFamilyKey)
	}
}
