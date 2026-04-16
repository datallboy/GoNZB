package match

import (
	"testing"
	"time"
)

func TestMatchHighConfidenceQuotedFilename(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 2, 14, 0, 0, 0, time.UTC)

	got := svc.Match(Candidate{
		ArticleNumber: 123456,
		MessageID:     "<part1@upload.example>",
		Subject:       `Cool.Movie.2025 "cool.movie.2025.r00" yEnc (3/20)`,
		Poster:        `Uploader <poster@example.com>`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.movies:12345 alt.binaries.hdtv:12346`,
	})

	if got.ReleaseName != "Cool.Movie.2025" {
		t.Fatalf("expected release name %q, got %q", "Cool.Movie.2025", got.ReleaseName)
	}
	if got.FileName != "cool.movie.2025.r00" {
		t.Fatalf("expected file name %q, got %q", "cool.movie.2025.r00", got.FileName)
	}
	if got.PartNumber != 3 || got.TotalParts != 20 {
		t.Fatalf("expected part 3/20, got %d/%d", got.PartNumber, got.TotalParts)
	}
	if got.MatchStatus != "matched" {
		t.Fatalf("expected matched status, got %q", got.MatchStatus)
	}
	if got.MatchConfidence < 0.85 {
		t.Fatalf("expected high confidence, got %f", got.MatchConfidence)
	}

	summary, ok := got.GroupingEvidence["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary evidence map, got %#v", got.GroupingEvidence["summary"])
	}
	if summary["short_circuited_after"] == "" {
		t.Fatalf("expected short-circuit marker in summary, got %#v", summary)
	}
}

func TestMatchLowConfidenceContextFallbackStaysDeterministic(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 2, 16, 20, 0, 0, time.UTC)

	first := svc.Match(Candidate{
		ArticleNumber: 200101,
		MessageID:     "<alpha@host.example>",
		Subject:       `[]`,
		Poster:        `weak.poster@example.com`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.misc:200101`,
	})
	second := svc.Match(Candidate{
		ArticleNumber: 200199,
		MessageID:     "<beta@host.example>",
		Subject:       `[]`,
		Poster:        `weak.poster@example.com`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.misc:200199`,
	})

	if first.MatchStatus != "low_confidence" {
		t.Fatalf("expected low confidence status, got %q", first.MatchStatus)
	}
	if first.BinaryKey != second.BinaryKey {
		t.Fatalf("expected deterministic contextual grouping, got %q vs %q", first.BinaryKey, second.BinaryKey)
	}

	fallback, ok := first.GroupingEvidence["fallback"].(map[string]any)
	if !ok {
		t.Fatalf("expected fallback evidence map, got %#v", first.GroupingEvidence["fallback"])
	}
	if fallback["used"] != true {
		t.Fatalf("expected fallback used marker, got %#v", fallback)
	}
}

func TestMatchUsesStructuredOverviewEvidence(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<structured@host.example>",
		Subject:   `obfuscated post`,
		RawOverview: map[string]any{
			"name":  "episode.part01.rar",
			"part":  1,
			"total": 12,
			"size":  7340032,
		},
	})

	if got.FileName != "episode.part01.rar" {
		t.Fatalf("expected file name from structured data, got %q", got.FileName)
	}
	if got.TotalParts != 12 {
		t.Fatalf("expected total parts 12, got %d", got.TotalParts)
	}
	if got.MatchConfidence <= 0 {
		t.Fatalf("expected structured evidence to contribute confidence, got %f", got.MatchConfidence)
	}
	if _, ok := got.GroupingEvidence["structured_markers"]; !ok {
		t.Fatalf("expected structured markers evidence, got %#v", got.GroupingEvidence)
	}
}

func TestMatchCanonicalizesReleaseKeyAcrossArchiveFamilies(t *testing.T) {
	svc := NewService()

	archive := svc.Match(Candidate{
		MessageID: "<archive@host.example>",
		Subject:   `09YqM2ra1RwajAXakAXy57xfGGhOOe8y.7z.001 yEnc (1/4)`,
		RawOverview: map[string]any{
			"name":  "09YqM2ra1RwajAXakAXy57xfGGhOOe8y.7z.001",
			"part":  1,
			"total": 4,
			"size":  7340032,
		},
	})
	parity := svc.Match(Candidate{
		MessageID: "<parity@host.example>",
		Subject:   `09YqM2ra1RwajAXakAXy57xfGGhOOe8y.vol00+01.par2 yEnc (1/4)`,
		RawOverview: map[string]any{
			"name":  "09YqM2ra1RwajAXakAXy57xfGGhOOe8y.vol00+01.par2",
			"part":  1,
			"total": 4,
			"size":  1024,
		},
	})

	if archive.ReleaseKey != "09yqm2ra1rwajaxakaxy57xfgghooe8y" {
		t.Fatalf("expected canonical archive release key, got %q", archive.ReleaseKey)
	}
	if parity.ReleaseKey != archive.ReleaseKey {
		t.Fatalf("expected PAR2 release key %q to match archive key", archive.ReleaseKey)
	}
	if archive.BinaryKey == parity.BinaryKey {
		t.Fatalf("expected distinct binary keys for archive and parity files, got %q", archive.BinaryKey)
	}
}

func TestMatchPrefersLargestNestedPartMarker(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<nested@host.example>",
		Subject:   `[13/15] - "3dAmzdk2T8i2KSfKzFMzCMiwrn3sfdTX.7z.013" yEnc (113/220) 157286400`,
	})

	if got.FileName != "3dAmzdk2T8i2KSfKzFMzCMiwrn3sfdTX.7z.013" {
		t.Fatalf("expected file name from quoted filename, got %q", got.FileName)
	}
	if got.PartNumber != 113 || got.TotalParts != 220 {
		t.Fatalf("expected inner segment marker 113/220, got %d/%d", got.PartNumber, got.TotalParts)
	}
	if got.FileIndex != 13 || got.ExpectedFileCount != 15 {
		t.Fatalf("expected outer file marker 13/15, got %d/%d", got.FileIndex, got.ExpectedFileCount)
	}
}

func TestMatchSeparatesFilesWithinSameReleaseByOuterFileMarker(t *testing.T) {
	svc := NewService()

	first := svc.Match(Candidate{
		MessageID: "<same-release-file13@host.example>",
		Subject:   `[13/15] - "same.release.7z.013" yEnc (3/220) 157286400`,
	})
	second := svc.Match(Candidate{
		MessageID: "<same-release-file2@host.example>",
		Subject:   `[2/15] - "same.release.7z.002" yEnc (92/220) 157286400`,
	})

	if first.ReleaseKey != second.ReleaseKey {
		t.Fatalf("expected same release key, got %q vs %q", first.ReleaseKey, second.ReleaseKey)
	}
	if first.BinaryKey == second.BinaryKey {
		t.Fatalf("expected different binary keys for different file indexes, got %q", first.BinaryKey)
	}
}

func TestMatchKeepsSameExplicitFileNameInOneBinaryDespiteOuterFileMarker(t *testing.T) {
	svc := NewService()

	first := svc.Match(Candidate{
		MessageID: "<same-file-1@host.example>",
		Subject:   `[2/85] - "XKKizlbwrCzK3UHM8LyA6r2U7BSCFeMx.7z.077" yEnc (1/86)`,
	})
	second := svc.Match(Candidate{
		MessageID: "<same-file-2@host.example>",
		Subject:   `[47/85] - "XKKizlbwrCzK3UHM8LyA6r2U7BSCFeMx.7z.077" yEnc (52/86)`,
	})

	if first.FileName != second.FileName {
		t.Fatalf("expected same explicit file name, got %q vs %q", first.FileName, second.FileName)
	}
	if first.BinaryKey != second.BinaryKey {
		t.Fatalf("expected same binary key for same explicit file name, got %q vs %q", first.BinaryKey, second.BinaryKey)
	}
	if first.FileIndex != 2 || second.FileIndex != 47 {
		t.Fatalf("expected outer file markers to remain as metadata, got %d and %d", first.FileIndex, second.FileIndex)
	}
}

func TestMatchPrefersYEncInnerCounterWhenOuterFileCounterIsLarger(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<swapped-counters@host.example>",
		Subject:   `[11/14] - "DWuzHaj5fRPH8xbHcX23hbLJdHaWDXfu.7z.011" yEnc (5/6) 3806935`,
	})

	if got.PartNumber != 5 || got.TotalParts != 6 {
		t.Fatalf("expected inner yEnc counter 5/6, got %d/%d", got.PartNumber, got.TotalParts)
	}
	if got.FileIndex != 11 || got.ExpectedFileCount != 14 {
		t.Fatalf("expected outer file counter 11/14, got %d/%d", got.FileIndex, got.ExpectedFileCount)
	}
}

func TestMatchDoesNotMergeNearbyPostsWithDifferentExplicitFilenames(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 9, 21, 0, 0, 0, time.UTC)

	first := svc.Match(Candidate{
		MessageID: "<movie-one@host.example>",
		Subject:   `[1/10] - "Movie.One.2026.1080p.BluRay.x265-GRP.r00" yEnc (1/100)`,
		Poster:    `same.poster@example.com`,
		PostedAt:  &postedAt,
		Xref:      `news.example alt.binaries.movies:10001`,
	})
	secondPostedAt := postedAt.Add(2 * time.Minute)
	second := svc.Match(Candidate{
		MessageID: "<movie-two@host.example>",
		Subject:   `[1/10] - "Movie.Two.2026.1080p.BluRay.x265-GRP.r00" yEnc (1/100)`,
		Poster:    `same.poster@example.com`,
		PostedAt:  &secondPostedAt,
		Xref:      `news.example alt.binaries.movies:10002`,
	})

	if first.ReleaseKey == second.ReleaseKey {
		t.Fatalf("expected different release keys, got %q", first.ReleaseKey)
	}
	if first.BinaryKey == second.BinaryKey {
		t.Fatalf("expected different binary keys, got %q", first.BinaryKey)
	}
}

func TestMatchUsesContextualReleaseKeyForObfuscatedMultiFilePosts(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 9, 21, 0, 0, 0, time.UTC)

	first := svc.Match(Candidate{
		ArticleNumber: 10001,
		MessageID:     "<opaque-a@host.example>",
		Subject:       `[001/287] - "hZ7i0SlcYTqKw0NySlolEljNiSIfzgQI.7z.001" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		PostedAt:      &postedAt,
		Xref:          `news.example alt.binaries.test:10001`,
	})
	secondPostedAt := postedAt.Add(2 * time.Minute)
	second := svc.Match(Candidate{
		ArticleNumber: 42001,
		MessageID:     "<opaque-b@host.example>",
		Subject:       `[002/287] - "gY8j1TmcZUuLr3MxPq9AnVaKoEdXcRpw.7z.002" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		PostedAt:      &secondPostedAt,
		Xref:          `news.example alt.binaries.test:42001`,
	})

	if first.ReleaseKey != second.ReleaseKey {
		t.Fatalf("expected contextual release key to group obfuscated files, got %q vs %q", first.ReleaseKey, second.ReleaseKey)
	}
	if first.BinaryKey == second.BinaryKey {
		t.Fatalf("expected distinct binary keys for distinct files, got %q", first.BinaryKey)
	}
	if first.ReleaseKey == "hz7i0slcytqkw0nysloleljnisifzgqi" || second.ReleaseKey == "gy8j1tmczuulr3mxpq9anvakoedxcrpw" {
		t.Fatalf("expected contextual release key instead of per-file opaque stem, got %q / %q", first.ReleaseKey, second.ReleaseKey)
	}
}

func TestMatchSplitsSmallObfuscatedReleaseFamiliesByArticleLocality(t *testing.T) {
	svc := NewService()

	first := svc.Match(Candidate{
		ArticleNumber: 2348912960,
		MessageID:     "<opaque-a@host.example>",
		Subject:       `[001/8] - "UwQtVWAaOxNHrRMXve53q3fgOlNLK5jr.7z.001" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		Xref:          `news.example alt.binaries.test:2348912960`,
	})
	second := svc.Match(Candidate{
		ArticleNumber: 2348958172,
		MessageID:     "<opaque-b@host.example>",
		Subject:       `[001/8] - "xxJ08j5Ul4KxEebRbd7K32ghm1Z1t3ok.7z.001" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		Xref:          `news.example alt.binaries.test:2348958172`,
	})

	if first.ReleaseKey == second.ReleaseKey {
		t.Fatalf("expected repeated small obfuscated sets to split by article locality, got %q", first.ReleaseKey)
	}
}

func TestMatchKeepsSmallIndexedArchiveFamilyTogetherByStem(t *testing.T) {
	svc := NewService()

	first := svc.Match(Candidate{
		ArticleNumber: 2348958172,
		MessageID:     "<opaque-a@host.example>",
		Subject:       `[001/8] - "xxJ08j5Ul4KxEebRbd7K32ghm1Z1t3ok.7z.001" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		Xref:          `news.example alt.binaries.test:2348958172`,
	})
	second := svc.Match(Candidate{
		ArticleNumber: 2348958764,
		MessageID:     "<opaque-b@host.example>",
		Subject:       `[005/8] - "xxJ08j5Ul4KxEebRbd7K32ghm1Z1t3ok.7z.005" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		Xref:          `news.example alt.binaries.test:2348958764`,
	})

	if first.ReleaseKey != second.ReleaseKey {
		t.Fatalf("expected small indexed archive family to stay together, got %q vs %q", first.ReleaseKey, second.ReleaseKey)
	}
}
