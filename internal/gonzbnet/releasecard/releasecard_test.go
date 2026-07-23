package releasecard

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
)

func TestMapLocalReleaseIsDeterministicAcrossInputOrdering(t *testing.T) {
	first := testLocalRelease()
	second := testLocalRelease()
	second.Groups = []string{"alt.binaries.movies", "alt.binaries.example"}
	second.Files = []LocalFile{second.Files[1], second.Files[0]}

	firstCard, err := MapLocalRelease(first)
	if err != nil {
		t.Fatalf("map first release: %v", err)
	}
	secondCard, err := MapLocalRelease(second)
	if err != nil {
		t.Fatalf("map second release: %v", err)
	}

	if firstCard.ReleaseID == "" {
		t.Fatalf("expected release id")
	}
	if firstCard.ManifestID == "" {
		t.Fatalf("expected manifest id")
	}
	if firstCard.ReleaseID != secondCard.ReleaseID {
		t.Fatalf("expected stable release id %q, got %q", firstCard.ReleaseID, secondCard.ReleaseID)
	}
	if firstCard.ManifestID != secondCard.ManifestID {
		t.Fatalf("expected stable manifest id %q, got %q", firstCard.ManifestID, secondCard.ManifestID)
	}
	core, err := ManifestCoreForLocalRelease(first)
	if err != nil {
		t.Fatalf("manifest core: %v", err)
	}
	expectedManifestID, _, err := manifest.ComputeID(core)
	if err != nil {
		t.Fatalf("manifest id: %v", err)
	}
	if firstCard.ManifestID != expectedManifestID {
		t.Fatalf("release card manifest id %q does not match manifest id %q", firstCard.ManifestID, expectedManifestID)
	}

	firstHash, err := HashBody(firstCard)
	if err != nil {
		t.Fatalf("hash first card: %v", err)
	}
	secondHash, err := HashBody(secondCard)
	if err != nil {
		t.Fatalf("hash second card: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("expected stable body hash %q, got %q", firstHash, secondHash)
	}
}

func TestMapLocalReleaseDoesNotGenerateManifestIDWithoutSegments(t *testing.T) {
	in := testLocalRelease()
	in.Files[0].Segments = nil

	card, err := MapLocalRelease(in)
	if err != nil {
		t.Fatalf("map release: %v", err)
	}
	if card.ManifestID != "" {
		t.Fatalf("expected no manifest id without complete segment metadata, got %q", card.ManifestID)
	}
	if card.Resolution.Status != "metadata_only" {
		t.Fatalf("expected metadata_only resolution, got %q", card.Resolution.Status)
	}
}

func TestValidateRecomputesReleaseID(t *testing.T) {
	card, err := MapLocalRelease(testLocalRelease())
	if err != nil {
		t.Fatalf("map release: %v", err)
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := Validate(card, now, 2*time.Minute); err != nil {
		t.Fatalf("validate release card: %v", err)
	}

	card.SizeBytes++
	if err := Validate(card, now, 2*time.Minute); err == nil {
		t.Fatal("expected changed release identity core to fail validation")
	}
}

func TestValidateRejectsMalformedReleaseCard(t *testing.T) {
	card, err := MapLocalRelease(testLocalRelease())
	if err != nil {
		t.Fatalf("map release: %v", err)
	}
	card.Groups = []string{"invalid group"}
	if err := Validate(card, time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC), 2*time.Minute); err == nil {
		t.Fatal("expected invalid group to fail validation")
	}
}

func testLocalRelease() LocalRelease {
	posted := time.Date(2026, 7, 7, 10, 55, 0, 0, time.UTC)
	file1Posted := time.Date(2026, 7, 7, 10, 56, 0, 0, time.UTC)
	file2Posted := time.Date(2026, 7, 7, 10, 57, 0, 0, time.UTC)
	return LocalRelease{
		LocalReleaseID:    "local-release-1",
		GUID:              "guid-1",
		Title:             "Example.Release.2026.2160p.WEB-DL",
		Category:          "movies",
		CategoryID:        2040,
		Classification:    "movies",
		SizeBytes:         3000,
		PostedAt:          &posted,
		FileCount:         2,
		CompletionPct:     100,
		Groups:            []string{"alt.binaries.example", "alt.binaries.movies"},
		HasPAR2:           true,
		PasswordState:     "not_passworded",
		Availability:      0.92,
		TMDBID:            12345,
		ExternalMedia:     "movie",
		ExternalYear:      2026,
		PrimaryResolution: "2160p",
		PrimaryVideoCodec: "HEVC",
		PrimaryAudioCodec: "Atmos",
		Files: []LocalFile{
			{
				Name:         "example.part001.rar",
				Subject:      "Example.Release.2026.part001.rar yEnc",
				Poster:       "poster@example.invalid",
				PostedAt:     &file1Posted,
				SizeBytes:    1000,
				FileIndex:    1,
				ArticleCount: 2,
				TotalParts:   2,
				Segments: []LocalSegment{
					{Number: 2, Bytes: 500, MessageID: "<seg2@example.invalid>"},
					{Number: 1, Bytes: 500, MessageID: "<seg1@example.invalid>"},
				},
			},
			{
				Name:         "example.part002.rar",
				Subject:      "Example.Release.2026.part002.rar yEnc",
				Poster:       "poster@example.invalid",
				PostedAt:     &file2Posted,
				SizeBytes:    2000,
				FileIndex:    2,
				ArticleCount: 1,
				TotalParts:   1,
				Segments: []LocalSegment{
					{Number: 1, Bytes: 2000, MessageID: "<seg3@example.invalid>"},
				},
			},
		},
	}
}
