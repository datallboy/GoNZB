package pgindex

import "testing"

func TestParseArticleIngestMetadataSeparatesFileSetAndArticleCountersWithoutYEncWord(t *testing.T) {
	got := parseArticleIngestMetadata(`[21/28] "WPt9ecy6X3Ui4d4GBo5Yzx.vol000+01.par2" (1/1)`)

	if got.FileName != "WPt9ecy6X3Ui4d4GBo5Yzx.vol000+01.par2" {
		t.Fatalf("expected quoted filename, got %q", got.FileName)
	}
	if got.FileIndex != 21 || got.FileTotal != 28 {
		t.Fatalf("expected file-set counter 21/28, got %d/%d", got.FileIndex, got.FileTotal)
	}
	if got.YEncPart != 1 || got.YEncTotalParts != 1 {
		t.Fatalf("expected article counter 1/1, got %d/%d", got.YEncPart, got.YEncTotalParts)
	}
}

func TestParseArticleIngestMetadataKeepsYEncSubjectCounters(t *testing.T) {
	got := parseArticleIngestMetadata(`[1/8] - "rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv" yEnc (7152/28465) 20403308372`)

	if got.FileIndex != 1 || got.FileTotal != 8 {
		t.Fatalf("expected file-set counter 1/8, got %d/%d", got.FileIndex, got.FileTotal)
	}
	if got.YEncPart != 7152 || got.YEncTotalParts != 28465 {
		t.Fatalf("expected article counter 7152/28465, got %d/%d", got.YEncPart, got.YEncTotalParts)
	}
	if got.FileSize != 20403308372 {
		t.Fatalf("expected trailing file size, got %d", got.FileSize)
	}
}
