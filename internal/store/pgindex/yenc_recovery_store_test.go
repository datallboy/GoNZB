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

func TestNormalizeYEncHeaderRecoveryRecordUsesRecoveredFileFallbackWhenFamilyMissing(t *testing.T) {
	record := YEncHeaderRecoveryRecord{
		BinaryKey:       "random-subject-token::random-subject-token bin",
		BinaryName:      "random-subject-token.bin",
		FileName:        "u2gQ8P9Sik.dat",
		PartNumber:      636,
		TotalParts:      732,
		FileSize:        524288000,
		IdentityReason:  "yenc_header",
		MatchConfidence: 0.82,
	}

	normalizeYEncHeaderRecoveryRecord(&record)

	wantFamily := "yenc u2gq8p9sik dat parts732 size524288000"
	if record.ReleaseFamilyKey != wantFamily || record.FileSetKey != wantFamily || record.SourceReleaseKey != wantFamily {
		t.Fatalf("expected recovered fallback family %q, got source=%q release=%q file_set=%q", wantFamily, record.SourceReleaseKey, record.ReleaseFamilyKey, record.FileSetKey)
	}
	if record.BinaryKey != wantFamily+"::u2gq8p9sik dat" {
		t.Fatalf("expected recovered fallback binary key, got %q", record.BinaryKey)
	}
}
