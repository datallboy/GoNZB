package releasetitle

import "testing"

func TestShouldAdoptInspectionTitleRejectsUnrelatedReadableSource(t *testing.T) {
	candidate, ok := ChooseBestInspectionTitle("Steinberg Cubase Pro 15.0.21 (x64) Multilingual", []InspectionCandidate{{
		Source:     "archive_entry",
		Value:      "Activation.Manager.Unlocker.b14.exe",
		Confidence: 0.98,
	}})
	if !ok {
		t.Fatalf("expected archive entry candidate")
	}
	if ShouldAdoptInspectionTitle("Steinberg Cubase Pro 15.0.21 (x64) Multilingual", candidate) {
		t.Fatalf("unrelated archive entry should not replace readable source title: %+v", candidate)
	}
}

func TestShouldAdoptInspectionTitleAllowsObfuscatedSource(t *testing.T) {
	candidate, ok := ChooseBestInspectionTitle("ZwL0GNkCujTrnihx9MLvgT8IMx92t0H2.vol00+01", []InspectionCandidate{{
		Source:     "archive_entry",
		Value:      "From.Russia.With.Love.1963.1080p.BluRay.x265-YAWNTiC/From.Russia.With.Love.1963.1080p.BluRay.x265-YAWNTiC.mkv",
		Confidence: 0.98,
	}})
	if !ok {
		t.Fatalf("expected archive entry candidate")
	}
	if !ShouldAdoptInspectionTitle("ZwL0GNkCujTrnihx9MLvgT8IMx92t0H2.vol00+01", candidate) {
		t.Fatalf("archive entry should replace obfuscated source title: %+v", candidate)
	}
}

func TestShouldAdoptInspectionTitleAllowsRelatedReadableSource(t *testing.T) {
	candidate, ok := ChooseBestInspectionTitle("Example Show S01E01 1080p", []InspectionCandidate{{
		Source:     "archive_entry",
		Value:      "Example.Show.S01E01.1080p.WEB-DL.x265-GRP.mkv",
		Confidence: 0.98,
	}})
	if !ok {
		t.Fatalf("expected archive entry candidate")
	}
	if !ShouldAdoptInspectionTitle("Example Show S01E01 1080p", candidate) {
		t.Fatalf("related archive entry should refine readable source title: %+v", candidate)
	}
}
