package release

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceSplitsSourceReleaseKeyIntoMultipleGroups(t *testing.T) {
	baseTime := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "shared source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"shared source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "shared source key",
					ReleaseName:     "Movie One 2026",
					FileName:        "movie.one.2026.r00",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime),
					TotalParts:      10,
					ObservedParts:   10,
					TotalBytes:      700_000_000,
					MatchConfidence: 0.92,
				},
				{
					BinaryID:        2,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "shared source key",
					ReleaseName:     "Movie One 2026",
					FileName:        "movie.one.2026.r01",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(45 * time.Minute)),
					TotalParts:      10,
					ObservedParts:   9,
					TotalBytes:      710_000_000,
					MatchConfidence: 0.88,
				},
				{
					BinaryID:        3,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "shared source key",
					ReleaseName:     "Other Show S01E01",
					FileName:        "other.show.s01e01.r00",
					Poster:          "poster-b",
					PostedAt:        ptrTime(baseTime.Add(30 * time.Hour)),
					TotalParts:      8,
					ObservedParts:   8,
					TotalBytes:      550_000_000,
					MatchConfidence: 0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 2}},
			3: {{ArticleHeaderID: 103, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55, RequireExpectedFileCountForContextualObfuscated: true})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 2 {
		t.Fatalf("expected 2 release groups, got %d", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale-delete call, got %d", len(repo.deletedStaleCalls))
	}
	if len(repo.deletedStaleCalls[0].keepGroupNames) != 2 {
		t.Fatalf("expected 2 kept group names, got %v", repo.deletedStaleCalls[0].keepGroupNames)
	}
}

func TestRunOnceGroupsIndexedObfuscatedFilesIntoReleaseSets(t *testing.T) {
	baseTime := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:  1,
			NewsgroupID: 2,
			ReleaseKey:  "coarse contextual key",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"coarse contextual key": {
				{
					BinaryID:           1,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "a1opaque.7z.001",
					FileIndex:          1,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime),
					FirstArticleNumber: 1000,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           2,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "b2opaque.7z.002",
					FileIndex:          2,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(3 * time.Minute)),
					FirstArticleNumber: 1200,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           3,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "c3opaque.7z.003",
					FileIndex:          3,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(6 * time.Minute)),
					FirstArticleNumber: 1400,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           4,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "d4opaque.7z.004",
					FileIndex:          4,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(9 * time.Minute)),
					FirstArticleNumber: 1600,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           5,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "w1opaque.7z.001",
					FileIndex:          1,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(45 * time.Minute)),
					FirstArticleNumber: 5000,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           6,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "x2opaque.7z.002",
					FileIndex:          2,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(48 * time.Minute)),
					FirstArticleNumber: 5200,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           7,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "y3opaque.7z.003",
					FileIndex:          3,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(51 * time.Minute)),
					FirstArticleNumber: 5400,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
				{
					BinaryID:           8,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseKey:         "coarse contextual key",
					FileName:           "z4opaque.7z.004",
					FileIndex:          4,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(54 * time.Minute)),
					FirstArticleNumber: 5600,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
			3: {{ArticleHeaderID: 103, PartNumber: 1}},
			4: {{ArticleHeaderID: 104, PartNumber: 1}},
			5: {{ArticleHeaderID: 105, PartNumber: 1}},
			6: {{ArticleHeaderID: 106, PartNumber: 1}},
			7: {{ArticleHeaderID: 107, PartNumber: 1}},
			8: {{ArticleHeaderID: 108, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 2 {
		t.Fatalf("expected 2 clustered releases, got %d", len(repo.upsertedReleases))
	}
	for _, release := range repo.upsertedReleases {
		if release.FileCount != 4 {
			t.Fatalf("expected each clustered release to contain 4 files, got %d", release.FileCount)
		}
		if release.ExpectedFileCount != 4 {
			t.Fatalf("expected expected_file_count 4, got %d", release.ExpectedFileCount)
		}
	}
}

func TestRunOnceCoolsDownFragmentOnlyFamilyAndAcksDirtyCandidate(t *testing.T) {
	baseTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			KeyKind:          "release_family",
			ReleaseFamilyKey: "fragment-family",
			ReleaseKey:       "fragment-family",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"fragment-family": {
				{
					BinaryID:         1,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "fragment-family",
					ReleaseKey:       "fragment-family",
					FileName:         "fragment-a.rar",
					BinaryName:       "fragment-a.rar",
					PostedAt:         ptrTime(baseTime),
					TotalParts:       10,
					ObservedParts:    4,
					TotalBytes:       1000,
					MatchConfidence:  0.90,
					IsMainPayload:    true,
				},
				{
					BinaryID:         2,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "fragment-family",
					ReleaseKey:       "fragment-family",
					FileName:         "fragment-b.rar",
					BinaryName:       "fragment-b.rar",
					PostedAt:         ptrTime(baseTime.Add(time.Minute)),
					TotalParts:       12,
					ObservedParts:    6,
					TotalBytes:       1000,
					MatchConfidence:  0.90,
					IsMainPayload:    true,
				},
			},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected no persisted releases for fragment-only family, got %d", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale delete call for cooldown, got %d", len(repo.deletedStaleCalls))
	}
	if repo.deletedStaleCalls[0].releaseKey != "fragment-family" {
		t.Fatalf("expected stale delete for fragment-family, got %q", repo.deletedStaleCalls[0].releaseKey)
	}
	if len(repo.ackedCandidates) != 1 {
		t.Fatalf("expected cooled-down family to be acked once, got %d", len(repo.ackedCandidates))
	}
	if repo.ackedCandidates[0].familyKey != "fragment-family" {
		t.Fatalf("expected ack for fragment-family, got %q", repo.ackedCandidates[0].familyKey)
	}
}

func TestRunOnceSkipsWeakContextualObfuscatedClusterWithoutExpectedFileCountByDefault(t *testing.T) {
	baseTime := time.Date(2026, 4, 21, 1, 30, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			ReleaseFamilyKey: "contextual-family",
			ReleaseKey:       "contextual-family",
			ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"contextual-family": {
				{
					BinaryID:         1,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "contextual-family",
					ReleaseKey:       "contextual-family",
					ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
					FileName:         "opaque-a.bin",
					BinaryName:       "opaque-a.bin",
					FamilyKind:       "contextual_obfuscated",
					Poster:           "poster-a",
					PostedAt:         ptrTime(baseTime),
					TotalParts:       1,
					ObservedParts:    1,
					TotalBytes:       740_000,
					MatchConfidence:  0.60,
					IsMainPayload:    true,
				},
				{
					BinaryID:         2,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "contextual-family",
					ReleaseKey:       "contextual-family",
					ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
					FileName:         "opaque-b.bin",
					BinaryName:       "opaque-b.bin",
					FamilyKind:       "contextual_obfuscated",
					Poster:           "poster-a",
					PostedAt:         ptrTime(baseTime.Add(2 * time.Minute)),
					TotalParts:       1,
					ObservedParts:    1,
					TotalBytes:       741_000,
					MatchConfidence:  0.60,
					IsMainPayload:    true,
				},
				{
					BinaryID:         3,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "contextual-family",
					ReleaseKey:       "contextual-family",
					ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
					FileName:         "opaque-c.bin",
					BinaryName:       "opaque-c.bin",
					FamilyKind:       "contextual_obfuscated",
					Poster:           "poster-a",
					PostedAt:         ptrTime(baseTime.Add(4 * time.Minute)),
					TotalParts:       1,
					ObservedParts:    1,
					TotalBytes:       742_000,
					MatchConfidence:  0.60,
					IsMainPayload:    true,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
			3: {{ArticleHeaderID: 103, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{
		BatchSize:            10,
		ReleaseMinConfidence: 0.55,
		RequireExpectedFileCountForContextualObfuscated:    true,
		RequireExpectedFileCountForContextualObfuscatedSet: true,
	})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected weak contextual_obfuscated cluster to be skipped, got %d releases", len(repo.upsertedReleases))
	}
}

func TestRunOnceCanAllowWeakContextualObfuscatedClusterWhenPolicyDisabled(t *testing.T) {
	baseTime := time.Date(2026, 4, 21, 1, 30, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			ReleaseFamilyKey: "contextual-family",
			ReleaseKey:       "contextual-family",
			ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"contextual-family": {
				{
					BinaryID:         1,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "contextual-family",
					ReleaseKey:       "contextual-family",
					ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
					FileName:         "opaque-a.bin",
					BinaryName:       "opaque-a.bin",
					FamilyKind:       "contextual_obfuscated",
					Poster:           "poster-a",
					PostedAt:         ptrTime(baseTime),
					TotalParts:       1,
					ObservedParts:    1,
					TotalBytes:       740_000,
					MatchConfidence:  0.75,
					IsMainPayload:    true,
				},
				{
					BinaryID:         2,
					ProviderID:       1,
					NewsgroupID:      2,
					ReleaseFamilyKey: "contextual-family",
					ReleaseKey:       "contextual-family",
					ReleaseName:      "ZzkVM2DIfYfgAJFFuMebW0gimNrMZ4cdjKNgbj9av2yHM2WPTMA0TSHKc6IJzbhT",
					FileName:         "opaque-b.bin",
					BinaryName:       "opaque-b.bin",
					FamilyKind:       "contextual_obfuscated",
					Poster:           "poster-a",
					PostedAt:         ptrTime(baseTime.Add(2 * time.Minute)),
					TotalParts:       1,
					ObservedParts:    1,
					TotalBytes:       741_000,
					MatchConfidence:  0.75,
					IsMainPayload:    true,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{
		BatchSize:            10,
		ReleaseMinConfidence: 0.55,
		RequireExpectedFileCountForContextualObfuscated:    false,
		RequireExpectedFileCountForContextualObfuscatedSet: true,
	})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected contextual_obfuscated cluster to form when policy is disabled, got %d releases", len(repo.upsertedReleases))
	}
}

func TestRunOnceUsesReleaseFamilyKeyForCandidateWork(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			SourceReleaseKey: "matcher trace key",
			ReleaseFamilyKey: "family key",
			ReleaseKey:       "",
			ReleaseName:      "Example.Release.2026",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"family key": {{
				BinaryID:         1,
				ProviderID:       1,
				NewsgroupID:      2,
				SourceReleaseKey: "matcher trace key",
				ReleaseFamilyKey: "family key",
				ReleaseKey:       "family key",
				ReleaseName:      "Example.Release.2026",
				FileName:         "example.release.2026.mkv",
				ObservedParts:    1,
				TotalParts:       1,
				TotalBytes:       1024,
				MatchConfidence:  0.95,
				IsMainPayload:    true,
			}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release to be formed, got %d", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale-delete call, got %d", len(repo.deletedStaleCalls))
	}
	if repo.deletedStaleCalls[0].releaseKey != "family key" {
		t.Fatalf("expected stale delete to use family key, got %q", repo.deletedStaleCalls[0].releaseKey)
	}
}

func TestRunOnceSkipsFragmentaryMultiFileClustersUntilMultipleMainFilesExist(t *testing.T) {
	baseTime := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			ReleaseFamilyKey: "family-key",
			ReleaseKey:       "family-key",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"family-key": {
				{
					BinaryID:           1,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseFamilyKey:   "family-key",
					ReleaseKey:         "family-key",
					FileName:           "opaque.7z.001",
					FileIndex:          1,
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime),
					FirstArticleNumber: 1000,
					TotalParts:         10,
					ObservedParts:      10,
					TotalBytes:         700_000_000,
					MatchConfidence:    0.92,
					IsMainPayload:      true,
				},
				{
					BinaryID:           2,
					ProviderID:         1,
					NewsgroupID:        2,
					ReleaseFamilyKey:   "family-key",
					ReleaseKey:         "family-key",
					FileName:           "opaque.vol00+01.par2",
					ExpectedFileCount:  4,
					Poster:             "poster-a",
					PostedAt:           ptrTime(baseTime.Add(2 * time.Minute)),
					FirstArticleNumber: 1100,
					TotalParts:         2,
					ObservedParts:      2,
					TotalBytes:         1024,
					MatchConfidence:    0.90,
					IsAuxiliary:        true,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected fragmentary multi-file cluster to be skipped, got %d releases", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale-delete call, got %d", len(repo.deletedStaleCalls))
	}
}

func TestRunOnceSkipsOpaqueStandaloneBinaryWithoutExplicitSingleFileEvidence(t *testing.T) {
	baseTime := time.Date(2026, 4, 20, 18, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			ReleaseFamilyKey: "opaque-standalone-family",
			ReleaseKey:       "opaque-standalone-family",
			ReleaseName:      "ZxZzCeWW8ECJExG13i891fyVBUCommbINJNQNqdTam9KYctnYSWQI7Q1JXWeOPwA",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"opaque-standalone-family": {{
				BinaryID:         1,
				ProviderID:       1,
				NewsgroupID:      2,
				ReleaseFamilyKey: "opaque-standalone-family",
				ReleaseKey:       "opaque-standalone-family",
				ReleaseName:      "ZxZzCeWW8ECJExG13i891fyVBUCommbINJNQNqdTam9KYctnYSWQI7Q1JXWeOPwA",
				FileName:         "opaque.bin",
				BinaryName:       "opaque.bin",
				Poster:           "poster-a",
				PostedAt:         ptrTime(baseTime),
				TotalParts:       807,
				ObservedParts:    99,
				TotalBytes:       73_290_042,
				MatchConfidence:  0.96,
				IsMainPayload:    true,
			}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 156}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected opaque standalone binary to be skipped, got %d releases", len(repo.upsertedReleases))
	}
}

func TestRunOnceAllowsReadableStandaloneMediaWithoutExplicitFileCounter(t *testing.T) {
	baseTime := time.Date(2026, 4, 20, 18, 5, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:       1,
			NewsgroupID:      2,
			ReleaseFamilyKey: "readable-standalone-family",
			ReleaseKey:       "readable-standalone-family",
			ReleaseName:      "Show.S01E01.1080p.WEB-DL.x265",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"readable-standalone-family": {{
				BinaryID:         1,
				ProviderID:       1,
				NewsgroupID:      2,
				ReleaseFamilyKey: "readable-standalone-family",
				ReleaseKey:       "readable-standalone-family",
				ReleaseName:      "Show.S01E01.1080p.WEB-DL.x265",
				FileName:         "show.s01e01.1080p.web-dl.x265.mkv",
				BinaryName:       "show.s01e01.1080p.web-dl.x265.mkv",
				Poster:           "poster-a",
				PostedAt:         ptrTime(baseTime),
				TotalParts:       120,
				ObservedParts:    120,
				TotalBytes:       1_500_000_000,
				MatchConfidence:  0.96,
				IsMainPayload:    true,
			}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 201, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected readable standalone media release to persist, got %d", len(repo.upsertedReleases))
	}
}

func TestBuildReleaseFilesDeduplicatesDuplicateFileNames(t *testing.T) {
	repo := &fakeReleaseRepository{
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 201, PartNumber: 1}, {ArticleHeaderID: 202, PartNumber: 2}},
		},
	}
	svc := NewService(repo, testReleaseLogger{}, Options{})

	files, err := svc.buildReleaseFiles(context.Background(), releaseCluster{
		Binaries: []pgindex.BinarySummary{
			{
				BinaryID:        1,
				FileName:        "duplicate.vol00+01.par2",
				ObservedParts:   1,
				TotalBytes:      100,
				MatchConfidence: 0.80,
				IsAuxiliary:     true,
			},
			{
				BinaryID:        2,
				FileName:        "duplicate.vol00+01.par2",
				ObservedParts:   2,
				TotalBytes:      200,
				MatchConfidence: 0.90,
				IsAuxiliary:     true,
			},
		},
	})
	if err != nil {
		t.Fatalf("build release files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one deduplicated file, got %d", len(files))
	}
	if files[0].BinaryID != 2 {
		t.Fatalf("expected better duplicate binary to win, got %d", files[0].BinaryID)
	}
	if len(files[0].Articles) != 2 {
		t.Fatalf("expected winning binary articles to be kept, got %d", len(files[0].Articles))
	}
}

func TestBuildReleaseFilesPreservesBinaryPostedAt(t *testing.T) {
	repo := &fakeReleaseRepository{
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}
	svc := NewService(repo, testReleaseLogger{}, Options{})
	postedAt := time.Date(2026, 4, 16, 14, 0, 0, 0, time.UTC)

	files, err := svc.buildReleaseFiles(context.Background(), releaseCluster{
		Binaries: []pgindex.BinarySummary{
			{
				BinaryID:      1,
				FileName:      "release.7z.001",
				PostedAt:      &postedAt,
				ObservedParts: 1,
				TotalBytes:    100,
			},
		},
	})
	if err != nil {
		t.Fatalf("build release files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one release file, got %d", len(files))
	}
	if files[0].PostedAt == nil {
		t.Fatalf("expected file posted_at to be preserved")
	}
	if !files[0].PostedAt.Equal(postedAt) {
		t.Fatalf("expected file posted_at %s, got %s", postedAt, files[0].PostedAt.UTC())
	}
}

func TestRunOnceSkipsLowConfidenceReleaseBelowThreshold(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "weak source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"weak source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "weak source key",
					FileName:        "abcd1234.txt",
					Poster:          "poster-a",
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      2048,
					MatchConfidence: 0.10,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.60})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected no releases to be formed, got %d", len(repo.upsertedReleases))
	}
	if len(repo.deletedStaleCalls) != 1 {
		t.Fatalf("expected one stale-delete call, got %d", len(repo.deletedStaleCalls))
	}
	if len(repo.deletedStaleCalls[0].keepGroupNames) != 0 {
		t.Fatalf("expected no kept group names, got %v", repo.deletedStaleCalls[0].keepGroupNames)
	}
}

func TestRunOnceBuildsReleaseSummaryState(t *testing.T) {
	baseTime := time.Date(2026, 4, 2, 18, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "show source key",
				ReleaseName: "Show.S01E01.1080p.WEB-DL.x265",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"show source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "show source key",
					ReleaseName:     "Show.S01E01.1080p.WEB-DL.x265",
					FileName:        "show.s01e01.1080p.web-dl.x265.mkv",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime),
					TotalParts:      20,
					ObservedParts:   18,
					TotalBytes:      1_500_000_000,
					MatchConfidence: 0.95,
				},
				{
					BinaryID:        2,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "show source key",
					ReleaseName:     "Show.S01E01.1080p.WEB-DL.x265",
					FileName:        "show.s01e01.1080p.web-dl.x265.nfo",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(10 * time.Minute)),
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      1024,
					MatchConfidence: 0.90,
				},
				{
					BinaryID:        3,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "show source key",
					ReleaseName:     "Show.S01E01.1080p.WEB-DL.x265",
					FileName:        "show.s01e01.1080p.web-dl.x265.par2",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(20 * time.Minute)),
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      2048,
					MatchConfidence: 0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
			3: {{ArticleHeaderID: 103, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}

	got := repo.upsertedReleases[0]
	if !got.HasPAR2 || !got.HasNFO {
		t.Fatalf("expected has_par2 and has_nfo to be true, got has_par2=%v has_nfo=%v", got.HasPAR2, got.HasNFO)
	}
	if got.Passworded || got.PasswordedKnown || got.PasswordedUnknown {
		t.Fatalf("expected password flags to remain false, got %+v", got)
	}
	if got.PasswordState != "unknown" {
		t.Fatalf("expected password_state unknown, got %q", got.PasswordState)
	}
	if got.PrimaryResolution != "1080p" {
		t.Fatalf("expected primary resolution 1080p, got %q", got.PrimaryResolution)
	}
	if got.PrimaryVideoCodec != "x265" {
		t.Fatalf("expected primary video codec x265, got %q", got.PrimaryVideoCodec)
	}
	if got.AvailabilityScore == got.CompletionPct {
		t.Fatalf("expected availability score to differ from completion pct, got %.2f", got.AvailabilityScore)
	}
}

func TestRunOncePrefersInspectArchiveEntryForObfuscatedTitles(t *testing.T) {
	baseTime := time.Date(2026, 4, 7, 1, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:  1,
			NewsgroupID: 2,
			ReleaseKey:  "obfuscated source key",
			ReleaseName: "ZwL0GNkCujTrnihx9MLvgT8IMx92t0H2.vol00+01",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"obfuscated source key": {{
				BinaryID:        41,
				ProviderID:      1,
				NewsgroupID:     2,
				ReleaseKey:      "obfuscated source key",
				ReleaseName:     "ZwL0GNkCujTrnihx9MLvgT8IMx92t0H2.vol00+01",
				FileName:        "ZwL0GNkCujTrnihx9MLvgT8IMx92t0H2.7z.001",
				Poster:          "poster-a",
				PostedAt:        ptrTime(baseTime),
				TotalParts:      12,
				ObservedParts:   12,
				TotalBytes:      900_000_000,
				MatchConfidence: 0.93,
			}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			41: {{ArticleHeaderID: 301, PartNumber: 1}},
		},
		titleCandidatesByBinaryID: map[int64][]pgindex.ReleaseTitleCandidate{
			41: {{
				BinaryID:   41,
				Source:     "archive_entry",
				Value:      "From.Russia.With.Love.1963.1080p.BluRay.x265-YAWNTiC/From.Russia.With.Love.1963.1080p.BluRay.x265-YAWNTiC.mkv",
				Confidence: 0.98,
			}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}

	got := repo.upsertedReleases[0]
	if got.SourceTitle != "ZwL0GNkCujTrnihx9MLvgT8IMx92t0H2.vol00+01" {
		t.Fatalf("expected source title to stay raw, got %q", got.SourceTitle)
	}
	if got.DeobfuscatedTitle != "From.Russia.With.Love.1963.1080p.BluRay.x265-YAWNTiC" {
		t.Fatalf("expected inspect-derived deobfuscated title, got %q", got.DeobfuscatedTitle)
	}
	if got.Title != "From Russia With Love 1963 1080p BluRay x265-YAWNTiC" {
		t.Fatalf("expected display title to adopt inspect title, got title=%q deobf=%q", got.Title, got.DeobfuscatedTitle)
	}
	if got.TitleSource != "archive_entry" {
		t.Fatalf("expected title_source archive_entry, got %q", got.TitleSource)
	}
	if got.TitleConfidence < 0.9 {
		t.Fatalf("expected high title confidence, got %.2f", got.TitleConfidence)
	}
	if got.IdentityStatus != "identified" {
		t.Fatalf("expected identified identity status, got %q", got.IdentityStatus)
	}
}

func TestRunOnceCompletionRespectsExpectedFileCount(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "expected file count source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"expected file count source key": {
				{
					BinaryID:          1,
					ProviderID:        1,
					NewsgroupID:       2,
					ReleaseKey:        "expected file count source key",
					FileName:          "example.7z.001",
					FileIndex:         1,
					ExpectedFileCount: 4,
					TotalParts:        10,
					ObservedParts:     10,
					TotalBytes:        700_000_000,
					MatchConfidence:   0.95,
				},
				{
					BinaryID:          2,
					ProviderID:        1,
					NewsgroupID:       2,
					ReleaseKey:        "expected file count source key",
					FileName:          "example.par2",
					FileIndex:         2,
					ExpectedFileCount: 4,
					TotalParts:        1,
					ObservedParts:     1,
					TotalBytes:        1024,
					MatchConfidence:   0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
			2: {{ArticleHeaderID: 102, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}
	if repo.upsertedReleases[0].ExpectedFileCount != 4 {
		t.Fatalf("expected expected_file_count 4, got %d", repo.upsertedReleases[0].ExpectedFileCount)
	}
	if repo.upsertedReleases[0].CompletionPct != 50 {
		t.Fatalf("expected completion_pct 50 due to file-count gate, got %.2f", repo.upsertedReleases[0].CompletionPct)
	}
}

func TestRunOncePAR2BackedReleaseBoostsAvailabilityAboveCompletion(t *testing.T) {
	baseTime := time.Date(2026, 4, 9, 20, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:  1,
			NewsgroupID: 2,
			ReleaseKey:  "repairable source key",
			ReleaseName: "Repairable.Release.2026.1080p.BluRay.x265",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"repairable source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "repairable source key",
					ReleaseName:     "Repairable.Release.2026.1080p.BluRay.x265",
					FileName:        "repairable.release.2026.1080p.bluray.x265.mkv",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime),
					TotalParts:      10,
					ObservedParts:   5,
					TotalBytes:      1_500_000_000,
					MatchConfidence: 0.92,
				},
				{
					BinaryID:        2,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "repairable source key",
					ReleaseName:     "Repairable.Release.2026.1080p.BluRay.x265",
					FileName:        "repairable.release.2026.par2",
					Poster:          "poster-a",
					PostedAt:        ptrTime(baseTime.Add(5 * time.Minute)),
					TotalParts:      1,
					ObservedParts:   1,
					TotalBytes:      1_048_576,
					MatchConfidence: 0.88,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 401, PartNumber: 1}},
			2: {{ArticleHeaderID: 402, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}

	got := repo.upsertedReleases[0]
	if !got.HasPAR2 {
		t.Fatalf("expected has_par2 to be true, got %+v", got)
	}
	if got.AvailabilityScore <= got.CompletionPct {
		t.Fatalf("expected availability %.2f to exceed completion %.2f for PAR2-backed release", got.AvailabilityScore, got.CompletionPct)
	}
	if got.MediaQualityScore == got.CompletionPct || got.MediaQualityScore == got.AvailabilityScore {
		t.Fatalf("expected media_quality_score to remain independent, got completion=%.2f availability=%.2f media=%.2f", got.CompletionPct, got.AvailabilityScore, got.MediaQualityScore)
	}
}

func TestRunOnceAdoptsNFOTitleCandidateForObfuscatedSource(t *testing.T) {
	baseTime := time.Date(2026, 4, 9, 22, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:  1,
			NewsgroupID: 2,
			ReleaseKey:  "nfo source key",
			ReleaseName: "Qw9pLm82ZxvK",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"nfo source key": {{
				BinaryID:        81,
				ProviderID:      1,
				NewsgroupID:     2,
				ReleaseKey:      "nfo source key",
				ReleaseName:     "Qw9pLm82ZxvK",
				FileName:        "qw9plm82zxvk.nfo",
				Poster:          "poster-a",
				PostedAt:        ptrTime(baseTime),
				TotalParts:      1,
				ObservedParts:   1,
				TotalBytes:      4096,
				MatchConfidence: 0.92,
			}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			81: {{ArticleHeaderID: 901, PartNumber: 1}},
		},
		titleCandidatesByBinaryID: map[int64][]pgindex.ReleaseTitleCandidate{
			81: {{
				BinaryID:   81,
				Source:     "nfo",
				Value:      "Random preamble\nExample.Feature.2026.1080p.BluRay.x265-GRP\nMore text",
				Confidence: 0.95,
			}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one release, got %d", len(repo.upsertedReleases))
	}

	got := repo.upsertedReleases[0]
	if got.TitleSource != "nfo" {
		t.Fatalf("expected nfo title source, got %q", got.TitleSource)
	}
	if got.DeobfuscatedTitle != "Example.Feature.2026.1080p.BluRay.x265-GRP" {
		t.Fatalf("expected deobfuscated title from nfo, got %q", got.DeobfuscatedTitle)
	}
	if got.Title != "Example Feature 2026 1080p BluRay x265-GRP" {
		t.Fatalf("expected display title from nfo, got %q", got.Title)
	}
	if got.IdentityStatus != "identified" {
		t.Fatalf("expected identified identity status, got %q", got.IdentityStatus)
	}
}

func TestRunReformOnceUpdatesReleaseIdentityAfterNewEvidence(t *testing.T) {
	baseTime := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{{
			ProviderID:  1,
			NewsgroupID: 2,
			ReleaseKey:  "reform evidence key",
			ReleaseName: "Qw9pLm82ZxvK",
		}},
		existingCandidates: []pgindex.ReleaseCandidate{{
			ProviderID:  1,
			NewsgroupID: 2,
			ReleaseKey:  "reform evidence key",
			ReleaseName: "Qw9pLm82ZxvK",
		}},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"reform evidence key": {{
				BinaryID:        91,
				ProviderID:      1,
				NewsgroupID:     2,
				ReleaseKey:      "reform evidence key",
				ReleaseName:     "Qw9pLm82ZxvK",
				FileName:        "qw9plm82zxvk.7z.001",
				Poster:          "poster-a",
				PostedAt:        ptrTime(baseTime),
				TotalParts:      10,
				ObservedParts:   10,
				TotalBytes:      900_000_000,
				MatchConfidence: 0.92,
			}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			91: {{ArticleHeaderID: 1001, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected first release formation, got %d rows", len(repo.upsertedReleases))
	}
	first := repo.upsertedReleases[0]
	if first.TitleSource == "nfo" || first.DeobfuscatedTitle != "" {
		t.Fatalf("expected initial release to stay obfuscated, got %+v", first)
	}

	repo.titleCandidatesByBinaryID = map[int64][]pgindex.ReleaseTitleCandidate{
		91: {{
			BinaryID:   91,
			Source:     "nfo",
			Value:      "Header\nExample.Feature.2026.1080p.BluRay.x265-GRP\nFooter",
			Confidence: 0.95,
		}},
	}

	if err := svc.RunReformOnce(context.Background()); err != nil {
		t.Fatalf("run reform once: %v", err)
	}

	if len(repo.upsertedReleases) != 2 {
		t.Fatalf("expected re-formed release row, got %d rows", len(repo.upsertedReleases))
	}
	second := repo.upsertedReleases[1]
	if second.TitleSource != "nfo" {
		t.Fatalf("expected nfo title source after reform, got %q", second.TitleSource)
	}
	if second.DeobfuscatedTitle != "Example.Feature.2026.1080p.BluRay.x265-GRP" {
		t.Fatalf("expected deobfuscated title after reform, got %q", second.DeobfuscatedTitle)
	}
	if second.Title != "Example Feature 2026 1080p BluRay x265-GRP" {
		t.Fatalf("expected display title after reform, got %q", second.Title)
	}
	if second.IdentityStatus != "identified" {
		t.Fatalf("expected identified status after reform, got %q", second.IdentityStatus)
	}
}

func TestBuildReleaseRecordLeavesDeobfuscatedTitleEmptyForObfuscatedSource(t *testing.T) {
	cluster := releaseCluster{
		MatchConfidence: 0.90,
		Binaries: []pgindex.BinarySummary{
			{
				BinaryID:          1,
				ProviderID:        1,
				NewsgroupID:       2,
				ReleaseKey:        "obfuscated source key",
				ReleaseName:       "Ko2GU4qPjsTdBQdZ3vzjvL0K5TghWJrW.7z",
				FileName:          "Ko2GU4qPjsTdBQdZ3vzjvL0K5TghWJrW.7z.001",
				ExpectedFileCount: 5,
				ObservedParts:     10,
				TotalParts:        10,
				MatchConfidence:   0.90,
			},
		},
	}

	record := buildReleaseRecord(pgindex.ReleaseCandidate{
		ProviderID:  1,
		NewsgroupID: 2,
		ReleaseKey:  "obfuscated source key",
		ReleaseName: "Ko2GU4qPjsTdBQdZ3vzjvL0K5TghWJrW.7z",
	}, cluster, nil)

	if record.DeobfuscatedTitle != "" {
		t.Fatalf("expected empty deobfuscated title, got %q", record.DeobfuscatedTitle)
	}
	if record.Title != "Ko2GU4qPjsTdBQdZ3vzjvL0K5TghWJrW 7z" {
		t.Fatalf("expected display title to humanize source title, got %q", record.Title)
	}
	if record.IdentityStatus == "identified" {
		t.Fatalf("expected obfuscated source title to avoid identified status, got %q", record.IdentityStatus)
	}
}

func TestBuildReleaseRecordPopulatesDeobfuscatedTitleForReadableSource(t *testing.T) {
	cluster := releaseCluster{
		MatchConfidence: 0.92,
		Binaries: []pgindex.BinarySummary{
			{
				BinaryID:          1,
				ProviderID:        1,
				NewsgroupID:       2,
				ReleaseKey:        "readable source key",
				ReleaseName:       "Show.S01E01.1080p.WEB-DL.x265",
				FileName:          "show.s01e01.1080p.web-dl.x265.mkv",
				ExpectedFileCount: 1,
				ObservedParts:     20,
				TotalParts:        20,
				MatchConfidence:   0.92,
			},
		},
	}

	record := buildReleaseRecord(pgindex.ReleaseCandidate{
		ProviderID:  1,
		NewsgroupID: 2,
		ReleaseKey:  "readable source key",
		ReleaseName: "Show.S01E01.1080p.WEB-DL.x265",
	}, cluster, nil)

	if record.DeobfuscatedTitle != "Show.S01E01.1080p.WEB-DL.x265" {
		t.Fatalf("expected deobfuscated title for readable source, got %q", record.DeobfuscatedTitle)
	}
	if record.Title != "Show S01E01 1080p WEB-DL x265" {
		t.Fatalf("expected humanized display title, got %q", record.Title)
	}
	if record.IdentityStatus != "identified" {
		t.Fatalf("expected readable source title to keep identified status, got %q", record.IdentityStatus)
	}
	if record.CategoryID != 5040 {
		t.Fatalf("expected TVHD category id, got %+v", record)
	}
}

func TestBuildReleaseRecordNeverLeavesFamilyIdentityBlank(t *testing.T) {
	record := buildReleaseRecord(pgindex.ReleaseCandidate{
		ProviderID:  1,
		NewsgroupID: 2,
		ReleaseKey:  "legacy family key",
		ReleaseName: "Example.Release.2026",
	}, releaseCluster{
		MatchConfidence: 0.90,
		Binaries: []pgindex.BinarySummary{
			{
				BinaryID:          1,
				ProviderID:        1,
				NewsgroupID:       2,
				ReleaseName:       "Example.Release.2026",
				FileName:          "example.release.2026.mkv",
				ExpectedFileCount: 1,
				ObservedParts:     10,
				TotalParts:        10,
				MatchConfidence:   0.90,
			},
		},
	}, nil)

	if record.ReleaseFamilyKey != "legacy family key" {
		t.Fatalf("expected release family key fallback, got %q", record.ReleaseFamilyKey)
	}
	if record.SourceReleaseKey != "legacy family key" {
		t.Fatalf("expected source release key fallback, got %q", record.SourceReleaseKey)
	}
	if record.ReleaseKey != "legacy family key" {
		t.Fatalf("expected release key fallback, got %q", record.ReleaseKey)
	}
}

func TestRunReformOncePagesThroughExistingCandidates(t *testing.T) {
	repo := &fakeReleaseRepository{
		existingCandidates: []pgindex.ReleaseCandidate{
			{ProviderID: 1, NewsgroupID: 2, ReleaseKey: "k1"},
			{ProviderID: 1, NewsgroupID: 2, ReleaseKey: "k2"},
			{ProviderID: 1, NewsgroupID: 2, ReleaseKey: "k3"},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"k1": {{BinaryID: 1, ProviderID: 1, NewsgroupID: 2, ReleaseKey: "k1", FileName: "show.one.mkv", TotalParts: 1, ObservedParts: 1, TotalBytes: 1, MatchConfidence: 0.9}},
			"k2": {{BinaryID: 2, ProviderID: 1, NewsgroupID: 2, ReleaseKey: "k2", FileName: "show.two.mkv", TotalParts: 1, ObservedParts: 1, TotalBytes: 1, MatchConfidence: 0.9}},
			"k3": {{BinaryID: 3, ProviderID: 1, NewsgroupID: 2, ReleaseKey: "k3", FileName: "show.three.mkv", TotalParts: 1, ObservedParts: 1, TotalBytes: 1, MatchConfidence: 0.9}},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 1, PartNumber: 1}},
			2: {{ArticleHeaderID: 2, PartNumber: 1}},
			3: {{ArticleHeaderID: 3, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 2, ReleaseMinConfidence: 0.55})
	if err := svc.RunReformOnce(context.Background()); err != nil {
		t.Fatalf("run reform once: %v", err)
	}

	if len(repo.upsertedReleases) != 3 {
		t.Fatalf("expected 3 releases to reform across pages, got %d", len(repo.upsertedReleases))
	}
	if repo.listExistingReleaseCandidatesCalls < 2 {
		t.Fatalf("expected paged existing-candidate calls, got %d", repo.listExistingReleaseCandidatesCalls)
	}
}

func TestRunOnceSkipsReleaseBelowCompletionThreshold(t *testing.T) {
	repo := &fakeReleaseRepository{
		candidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "sparse source key",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"sparse source key": {
				{
					BinaryID:        1,
					ProviderID:      1,
					NewsgroupID:     2,
					ReleaseKey:      "sparse source key",
					FileName:        "sparse.release.7z.001",
					Poster:          "poster-a",
					TotalParts:      10,
					ObservedParts:   2,
					TotalBytes:      700_000_000,
					MatchConfidence: 0.90,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{
		BatchSize:            10,
		ReleaseMinConfidence: 0.55,
		ReleaseMinCompletion: 25,
	})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.upsertedReleases) != 0 {
		t.Fatalf("expected no releases to be formed, got %d", len(repo.upsertedReleases))
	}
}

func TestRunReformOnceUsesExistingReleaseCandidates(t *testing.T) {
	repo := &fakeReleaseRepository{
		existingCandidates: []pgindex.ReleaseCandidate{
			{
				ProviderID:  1,
				NewsgroupID: 2,
				ReleaseKey:  "existing source key",
				ReleaseName: "Show.S01E01.1080p.WEB-DL.x265",
			},
		},
		binariesByKey: map[string][]pgindex.BinarySummary{
			"existing source key": {
				{
					BinaryID:          1,
					ProviderID:        1,
					NewsgroupID:       2,
					ReleaseKey:        "existing source key",
					ReleaseName:       "Show.S01E01.1080p.WEB-DL.x265",
					FileName:          "show.s01e01.1080p.web-dl.x265.mkv",
					ExpectedFileCount: 1,
					ObservedParts:     20,
					TotalParts:        20,
					TotalBytes:        1_500_000_000,
					MatchConfidence:   0.92,
				},
			},
		},
		articlesByBinaryID: map[int64][]pgindex.ReleaseFileArticleRecord{
			1: {{ArticleHeaderID: 101, PartNumber: 1}},
		},
	}

	svc := NewService(repo, testReleaseLogger{}, Options{BatchSize: 10, ReleaseMinConfidence: 0.55})
	if err := svc.RunReformOnce(context.Background()); err != nil {
		t.Fatalf("run reform once: %v", err)
	}

	if repo.listReleaseCandidatesCalls != 0 {
		t.Fatalf("expected incremental candidate query to be skipped during reform, got %d calls", repo.listReleaseCandidatesCalls)
	}
	if repo.listExistingReleaseCandidatesCalls != 1 {
		t.Fatalf("expected reform candidate query to be used once, got %d calls", repo.listExistingReleaseCandidatesCalls)
	}
	if len(repo.upsertedReleases) != 1 {
		t.Fatalf("expected one re-formed release, got %d", len(repo.upsertedReleases))
	}
}

func TestSummarizeFilesCountsSplitArchiveFamilyOnce(t *testing.T) {
	hasPAR2, _, archiveCount, _, _, _ := summarizeFiles([]pgindex.BinarySummary{
		{FileName: "example.7z.001"},
		{FileName: "example.7z.002"},
		{FileName: "example.par2"},
	})

	if !hasPAR2 {
		t.Fatalf("expected PAR2 presence to be detected")
	}
	if archiveCount != 1 {
		t.Fatalf("expected one archive family, got %d", archiveCount)
	}
}

type fakeReleaseRepository struct {
	candidates                         []pgindex.ReleaseCandidate
	existingCandidates                 []pgindex.ReleaseCandidate
	binariesByKey                      map[string][]pgindex.BinarySummary
	articlesByBinaryID                 map[int64][]pgindex.ReleaseFileArticleRecord
	titleCandidatesByBinaryID          map[int64][]pgindex.ReleaseTitleCandidate
	upsertedReleases                   []pgindex.ReleaseRecord
	replaceFileCalls                   int
	newsgroupCalls                     int
	nzbCalls                           int
	listReleaseCandidatesCalls         int
	listExistingReleaseCandidatesCalls int
	deletedStaleCalls                  []staleDeleteCall
	ackedCandidates                    []ackedReleaseCandidate
}

type staleDeleteCall struct {
	providerID     int64
	releaseKey     string
	keepGroupNames []string
}

type ackedReleaseCandidate struct {
	providerID  int64
	newsgroupID int64
	keyKind     string
	familyKey   string
}

func (f *fakeReleaseRepository) ListReleaseCandidates(context.Context, int) ([]pgindex.ReleaseCandidate, error) {
	f.listReleaseCandidatesCalls++
	return f.candidates, nil
}

func (f *fakeReleaseRepository) ListExistingReleaseCandidates(_ context.Context, limit, offset int) ([]pgindex.ReleaseCandidate, error) {
	f.listExistingReleaseCandidatesCalls++
	if offset >= len(f.existingCandidates) {
		return nil, nil
	}
	if limit <= 0 {
		limit = len(f.existingCandidates)
	}
	end := offset + limit
	if end > len(f.existingCandidates) {
		end = len(f.existingCandidates)
	}
	return append([]pgindex.ReleaseCandidate(nil), f.existingCandidates[offset:end]...), nil
}

func (f *fakeReleaseRepository) ListBinariesForReleaseCandidate(_ context.Context, _ int64, _ int64, releaseKey string) ([]pgindex.BinarySummary, error) {
	return f.binariesByKey[releaseKey], nil
}

func (f *fakeReleaseRepository) ListBinaryPartArticles(_ context.Context, binaryID int64) ([]pgindex.ReleaseFileArticleRecord, error) {
	return f.articlesByBinaryID[binaryID], nil
}

func (f *fakeReleaseRepository) ListReleaseTitleCandidates(_ context.Context, binaryIDs []int64) ([]pgindex.ReleaseTitleCandidate, error) {
	out := make([]pgindex.ReleaseTitleCandidate, 0, len(binaryIDs))
	for _, binaryID := range binaryIDs {
		out = append(out, f.titleCandidatesByBinaryID[binaryID]...)
	}
	return out, nil
}

func (f *fakeReleaseRepository) UpsertRelease(_ context.Context, in pgindex.ReleaseRecord) (string, error) {
	f.upsertedReleases = append(f.upsertedReleases, in)
	return fmt.Sprintf("rel-%d", len(f.upsertedReleases)), nil
}

func (f *fakeReleaseRepository) DeleteStaleReleasesForSourceKey(_ context.Context, providerID int64, releaseKey string, keepGroupNames []string) error {
	f.deletedStaleCalls = append(f.deletedStaleCalls, staleDeleteCall{
		providerID:     providerID,
		releaseKey:     releaseKey,
		keepGroupNames: append([]string(nil), keepGroupNames...),
	})
	return nil
}

func (f *fakeReleaseRepository) ReplaceReleaseFiles(context.Context, string, []pgindex.ReleaseFileRecord) error {
	f.replaceFileCalls++
	return nil
}

func (f *fakeReleaseRepository) ReplaceReleaseNewsgroups(context.Context, string, []int64) error {
	f.newsgroupCalls++
	return nil
}

func (f *fakeReleaseRepository) UpsertNZBCache(context.Context, string, string, string, string) error {
	f.nzbCalls++
	return nil
}

func (f *fakeReleaseRepository) AckReleaseCandidate(_ context.Context, providerID, newsgroupID int64, keyKind, familyKey string) error {
	f.ackedCandidates = append(f.ackedCandidates, ackedReleaseCandidate{
		providerID:  providerID,
		newsgroupID: newsgroupID,
		keyKind:     keyKind,
		familyKey:   familyKey,
	})
	return nil
}

type testReleaseLogger struct{}

func (testReleaseLogger) Debug(string, ...interface{}) {}
func (testReleaseLogger) Info(string, ...interface{})  {}
func (testReleaseLogger) Warn(string, ...interface{})  {}
func (testReleaseLogger) Error(string, ...interface{}) {}

func ptrTime(v time.Time) *time.Time {
	return &v
}
