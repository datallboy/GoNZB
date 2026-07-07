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

func TestMatchUsesStructuredFileCountersWhenSubjectIsOpaque(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<structured-counters@host.example>",
		Subject:   `opaque post`,
		RawOverview: map[string]any{
			"name":       "episode.7z.001",
			"part":       2,
			"total":      220,
			"size":       int64(157286400),
			"file_index": 1,
			"file_total": 10,
		},
	})

	if got.FileName != "episode.7z.001" {
		t.Fatalf("expected file name from structured data, got %q", got.FileName)
	}
	if got.FileIndex != 1 || got.ExpectedFileCount != 10 {
		t.Fatalf("expected structured file counters 1/10, got %d/%d", got.FileIndex, got.ExpectedFileCount)
	}
	if got.PartNumber != 2 || got.TotalParts != 220 {
		t.Fatalf("expected structured part counters 2/220, got %d/%d", got.PartNumber, got.TotalParts)
	}
}

func TestMatchUsesYEncStructuredNameForObfuscatedMultipartFiles(t *testing.T) {
	svc := NewService()
	postedAt := time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)

	first := svc.Match(Candidate{
		ArticleNumber: 1907917006,
		MessageID:     "<opaque-a@host.example>",
		Subject:       `ZxZzCeWW8ECJExG13i891fyVBUCommbINJNQNqdTam9KYctnYSWQI7Q1JXWeOPwA`,
		Poster:        `Poster <poster@example.com>`,
		PostedAt:      &postedAt,
		Xref:          `alt.binaries.test:1907917006`,
		RawOverview: map[string]any{
			"name":  "kuqn1sj0tdehymt5l4ba7u",
			"part":  156,
			"total": 807,
			"size":  577954475,
		},
	})
	second := svc.Match(Candidate{
		ArticleNumber: 1907917007,
		MessageID:     "<opaque-b@host.example>",
		Subject:       `2SyfBuDdgET6VKdhFnWPdQcQfHIzOdmE2qRrhv43KiKF1YJWfRVJMCFVJcsYV9ue`,
		Poster:        `Poster <poster@example.com>`,
		PostedAt:      &postedAt,
		Xref:          `alt.binaries.test:1907917007`,
		RawOverview: map[string]any{
			"name":  "kuqn1sj0tdehymt5l4ba7u",
			"part":  157,
			"total": 807,
			"size":  577954475,
		},
	})

	if first.FileName != "kuqn1sj0tdehymt5l4ba7u" || second.FileName != "kuqn1sj0tdehymt5l4ba7u" {
		t.Fatalf("expected yEnc name to become file name, got %q / %q", first.FileName, second.FileName)
	}
	if first.BinaryKey != second.BinaryKey {
		t.Fatalf("expected same binary key for same yEnc file, got %q vs %q", first.BinaryKey, second.BinaryKey)
	}
	if first.PartNumber != 156 || second.PartNumber != 157 {
		t.Fatalf("expected structured part numbers, got %d / %d", first.PartNumber, second.PartNumber)
	}
	if first.TotalParts != 807 || second.TotalParts != 807 {
		t.Fatalf("expected structured total parts 807, got %d / %d", first.TotalParts, second.TotalParts)
	}
}

func TestMatchSubjectMultipartObfuscatedFileIgnoresRandomPosterContext(t *testing.T) {
	svc := NewService()
	firstPosted := time.Date(2026, 6, 25, 14, 28, 29, 0, time.UTC)
	secondPosted := time.Date(2026, 6, 25, 16, 44, 2, 0, time.UTC)

	first := svc.Match(Candidate{
		ArticleNumber: 2961163479,
		MessageID:     "<TxVq9M3lZw0BXeSOK5lY8tq9eOkIXcDM@xI9O2SXR.zm5>",
		Subject:       `[1/8] - "rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv" yEnc (7152/28465) 20403308372`,
		Poster:        `ZZY5wdELKQYA7W <ChrqPqF0fcAwPv@0r2Px.uOc>`,
		PostedAt:      &firstPosted,
		Bytes:         740354,
		Lines:         5691,
		Xref:          `news.easynews.com alt.binaries.newznzb.bravo:2961163479`,
	})
	second := svc.Match(Candidate{
		ArticleNumber: 2961166034,
		MessageID:     "<XHmxauRue9IRwOdLNjPHePyIr4KoqzdJ@NGzwvBZW.0T2>",
		Subject:       `[1/8] - "rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv" yEnc (8996/28465) 20403308372`,
		Poster:        `zzwRVvdHpvTUN3 <vujC9maTnSIGI9@g1hmM.h62>`,
		PostedAt:      &secondPosted,
		Bytes:         740276,
		Lines:         5691,
		Xref:          `news.easynews.com alt.binaries.newznzb.bravo:2961166034`,
	})

	if first.BinaryKey != second.BinaryKey {
		t.Fatalf("expected complete subject multipart evidence to share one binary key, got %q vs %q", first.BinaryKey, second.BinaryKey)
	}
	if first.SourceReleaseKey != second.SourceReleaseKey {
		t.Fatalf("expected source release key to ignore randomized context, got %q vs %q", first.SourceReleaseKey, second.SourceReleaseKey)
	}
	if first.ReleaseFamilyKey != second.ReleaseFamilyKey {
		t.Fatalf("expected release family key to ignore randomized context, got %q vs %q", first.ReleaseFamilyKey, second.ReleaseFamilyKey)
	}
	if first.FileName != "rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv" || second.FileName != first.FileName {
		t.Fatalf("expected quoted filename to be the binary file name, got %q / %q", first.FileName, second.FileName)
	}
	if first.PartNumber != 7152 || second.PartNumber != 8996 {
		t.Fatalf("expected subject part numbers 7152 and 8996, got %d / %d", first.PartNumber, second.PartNumber)
	}
	if first.TotalParts != 28465 || second.TotalParts != 28465 {
		t.Fatalf("expected subject total parts 28465, got %d / %d", first.TotalParts, second.TotalParts)
	}
	if first.FileIndex != 1 || first.ExpectedFileCount != 8 {
		t.Fatalf("expected file counter 1/8, got %d/%d", first.FileIndex, first.ExpectedFileCount)
	}
	if first.FamilyKind != "subject_multipart_obfuscated" {
		t.Fatalf("expected subject multipart family kind, got %q", first.FamilyKind)
	}
	if first.IdentityReason != "subject_multipart_obfuscated" {
		t.Fatalf("expected subject multipart identity reason, got %q", first.IdentityReason)
	}
	if first.IdentityStrength == "weak" {
		t.Fatalf("expected subject multipart evidence to avoid weak identity")
	}
	summary, ok := first.GroupingEvidence["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary evidence map, got %#v", first.GroupingEvidence["summary"])
	}
	if summary["subject_multipart"] != true {
		t.Fatalf("expected subject multipart evidence marker, got %#v", summary)
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

func TestMatchCanonicalizesReadablePunctuationAcrossReleaseFamilies(t *testing.T) {
	svc := NewService()

	dotted := svc.Match(Candidate{
		MessageID: "<directory-opus-dotted@host.example>",
		Subject:   `[1/8] - "Directory.Opus.13.23.part01.rar" yEnc (1/10)`,
	})
	spaced := svc.Match(Candidate{
		MessageID: "<directory-opus-spaced@host.example>",
		Subject:   `[2/8] - "Directory Opus 13 23.part02.rar" yEnc (1/10)`,
	})

	if dotted.ReleaseFamilyKey != "directory opus 13 23" {
		t.Fatalf("expected dotted family key to canonicalize punctuation, got %q", dotted.ReleaseFamilyKey)
	}
	if spaced.ReleaseFamilyKey != dotted.ReleaseFamilyKey {
		t.Fatalf("expected punctuation variants to share family key, got %q vs %q", spaced.ReleaseFamilyKey, dotted.ReleaseFamilyKey)
	}
	if spaced.SourceReleaseKey != dotted.SourceReleaseKey {
		t.Fatalf("expected punctuation variants to share source key, got %q vs %q", spaced.SourceReleaseKey, dotted.SourceReleaseKey)
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

func TestMatchSeparatesFileSetCounterFromSingleArticleCounterWithoutYEncWord(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<par2-single@indexer.test>",
		Subject:   `[21/28] "WPt9ecy6X3Ui4d4GBo5Yzx.vol000+01.par2" (1/1)`,
	})

	if got.FileName != "WPt9ecy6X3Ui4d4GBo5Yzx.vol000+01.par2" {
		t.Fatalf("expected quoted filename, got %q", got.FileName)
	}
	if got.FileIndex != 21 || got.ExpectedFileCount != 28 {
		t.Fatalf("expected file-set counter 21/28, got %d/%d", got.FileIndex, got.ExpectedFileCount)
	}
	if got.PartNumber != 1 || got.TotalParts != 1 {
		t.Fatalf("expected article counter 1/1, got %d/%d", got.PartNumber, got.TotalParts)
	}
	if !got.IsAuxiliary {
		t.Fatalf("expected PAR2 volume to be auxiliary")
	}
	if got.IdentityStrength == "weak" {
		t.Fatalf("expected explicit subject coordinates to avoid weak identity")
	}
}

func TestMatchUsesMessageIDPartCounterWhenSubjectCounterIsWeak(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: `<Part84of700.88B60C1037DB48589E2DC79BE09F92DA@1778298129.local>`,
		Subject:   `opaque "opaque-token" yEnc (1/1)`,
	})

	if got.PartNumber != 84 || got.TotalParts != 700 {
		t.Fatalf("expected message-id part counter 84/700, got %d/%d", got.PartNumber, got.TotalParts)
	}
	evidence, ok := got.GroupingEvidence["message_host"].(map[string]any)
	if !ok {
		t.Fatalf("expected message_host evidence, got %#v", got.GroupingEvidence["message_host"])
	}
	if evidence["part"] != 84 || evidence["total"] != 700 {
		t.Fatalf("expected message-id counter evidence, got %#v", evidence)
	}
}

func TestMatchKeepsSubjectYEncCounterWhenItIsStrongerThanMessageID(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: `<Part2of10.88B60C1037DB48589E2DC79BE09F92DA@1778298129.local>`,
		Subject:   `Example [1/1] - "example.part01.rar" yEnc (84/700)`,
	})

	if got.PartNumber != 84 || got.TotalParts != 700 {
		t.Fatalf("expected subject yEnc counter 84/700, got %d/%d", got.PartNumber, got.TotalParts)
	}
}

func TestMatchInfersArchiveVolumeIndexFromRecoveredYEncName(t *testing.T) {
	svc := NewService()

	cases := []struct {
		name string
		want int
	}{
		{name: "Wbostp9Yf138Oybk1yc93o.part02.rar", want: 2},
		{name: "Wbostp9Yf138Oybk1yc93o.part001.rar", want: 1},
		{name: "archive.r00", want: 2},
		{name: "archive.r04", want: 6},
		{name: "archive.7z.003", want: 3},
		{name: "archive.zip.005", want: 5},
	}

	for _, tc := range cases {
		got := svc.Match(Candidate{
			MessageID: "<recovered@indexer.test>",
			Subject:   `opaque`,
			RawOverview: map[string]any{
				"name":  tc.name,
				"part":  1,
				"total": 10,
			},
		})
		if got.FileIndex != tc.want {
			t.Fatalf("%s: expected inferred file index %d, got %d", tc.name, tc.want, got.FileIndex)
		}
	}
}

func TestMatchKeepsExplicitFileCounterOverArchiveVolumeIndex(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		MessageID: "<explicit@indexer.test>",
		Subject:   `[12/40] - "archive.part02.rar" yEnc (1/10)`,
	})

	if got.FileIndex != 12 || got.ExpectedFileCount != 40 {
		t.Fatalf("expected explicit file counter 12/40, got %d/%d", got.FileIndex, got.ExpectedFileCount)
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

func TestMatchPromotesLargeIndexedArchiveFamilyBySharedStem(t *testing.T) {
	svc := NewService()

	first := svc.Match(Candidate{
		ArticleNumber: 10001,
		MessageID:     "<large-archive-a@host.example>",
		Subject:       `[001/287] - "sharedopaquearchive.part001.rar" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		Xref:          `news.example alt.binaries.test:10001`,
	})
	second := svc.Match(Candidate{
		ArticleNumber: 10002,
		MessageID:     "<large-archive-b@host.example>",
		Subject:       `[002/287] - "sharedopaquearchive.part002.rar" yEnc (1/220) 157286400`,
		Poster:        `same.poster@example.com`,
		Xref:          `news.example alt.binaries.test:10002`,
	})

	if first.FamilyKind != "archive_stem" || second.FamilyKind != "archive_stem" {
		t.Fatalf("expected archive_stem family kind, got %q / %q", first.FamilyKind, second.FamilyKind)
	}
	if first.ReleaseKey != second.ReleaseKey {
		t.Fatalf("expected shared archive stem to group large family, got %q vs %q", first.ReleaseKey, second.ReleaseKey)
	}
	if first.BaseStem != "sharedopaquearchive" || second.BaseStem != "sharedopaquearchive" {
		t.Fatalf("expected base stem %q, got %q / %q", "sharedopaquearchive", first.BaseStem, second.BaseStem)
	}
}

func TestMatchClassifiesNumericPrefixAsWeakSubjectSet(t *testing.T) {
	svc := NewService()

	got := svc.Match(Candidate{
		ArticleNumber: 80894690,
		MessageID:     "<rclone-crypt@host.example>",
		Subject:       `80894690-n-YuO [1/4] - "bkkm2n3j3pl45ts1taahvgmj4bd6c0uthe84f72v8o8tn0or9icg" yEnc (1/1) 144259`,
		Poster:        `backup.poster@example.com`,
		Xref:          `news.example alt.binaries.misc:80894690`,
	})

	if got.SubjectSetToken != "80894690 n yuo" {
		t.Fatalf("expected numeric subject set token, got %q", got.SubjectSetToken)
	}
	if got.SubjectSetKind != "numeric_obfuscated_set" {
		t.Fatalf("expected numeric_obfuscated_set, got %q", got.SubjectSetKind)
	}
	if got.FamilyKind != "numeric_obfuscated_set" {
		t.Fatalf("expected family kind numeric_obfuscated_set, got %q", got.FamilyKind)
	}
	if got.IdentityStrength != "provisional" || got.IdentityReason != "numeric_obfuscated_set" {
		t.Fatalf("expected provisional numeric identity, got %q/%q", got.IdentityStrength, got.IdentityReason)
	}
	if got.ReleaseKey != "80894690 n yuo" {
		t.Fatalf("expected release key to preserve set token, got %q", got.ReleaseKey)
	}
	if got.ReleaseFamilyKey != "" {
		t.Fatalf("expected promotable release family identity to be deferred, got %q", got.ReleaseFamilyKey)
	}
	if got.FileSetKey != "" {
		t.Fatalf("expected promotable file set identity to be deferred, got %q", got.FileSetKey)
	}
}
