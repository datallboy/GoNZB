package pgindex

import "testing"

func TestNormalizeBinaryIdentityCanonicalizesOperationalKeys(t *testing.T) {
	record := BinaryRecord{
		SourceReleaseKey: "Directory.Opus.13.23",
		ReleaseFamilyKey: "Directory Opus 13.23",
		FileSetKey:       "Directory.Opus.13.23 files 8",
		FileFamilyKey:    "Directory.Opus.13.23.part01",
		SubjectSetToken:  "Directory-Opus-13.23",
		BaseStem:         "Directory.Opus.13.23",
		ReleaseKey:       "Directory.Opus.13.23",
	}

	normalizeBinaryIdentity(&record)

	if record.SourceReleaseKey != "directory opus 13 23" {
		t.Fatalf("unexpected source release key %q", record.SourceReleaseKey)
	}
	if record.ReleaseFamilyKey != "directory opus 13 23" {
		t.Fatalf("unexpected release family key %q", record.ReleaseFamilyKey)
	}
	if record.ReleaseKey != "directory opus 13 23" {
		t.Fatalf("unexpected release key %q", record.ReleaseKey)
	}
	if record.FileSetKey != "directory opus 13 23 files 8" {
		t.Fatalf("unexpected file set key %q", record.FileSetKey)
	}
	if record.FileFamilyKey != "directory opus 13 23 part01" {
		t.Fatalf("unexpected file family key %q", record.FileFamilyKey)
	}
	if record.SubjectSetToken != "directory opus 13 23" {
		t.Fatalf("unexpected subject set token %q", record.SubjectSetToken)
	}
	if record.BaseStem != "directory opus 13 23" {
		t.Fatalf("unexpected base stem %q", record.BaseStem)
	}
}
