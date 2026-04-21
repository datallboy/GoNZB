package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInspectCandidateFilterPasswordRequiresEncryptedRelease(t *testing.T) {
	filter, err := inspectCandidateFilter("inspect_password")
	if err != nil {
		t.Fatalf("inspectCandidateFilter() error = %v", err)
	}

	if !strings.Contains(filter, "r.encrypted = TRUE") {
		t.Fatalf("expected inspect_password filter to require encrypted releases, got %q", filter)
	}
}

func TestClaimIndexerStageRecoversExpiredLeaseAndMarksOldRunAbandoned(t *testing.T) {
	store := openTestStore(t)
	stageName := uniqueTestStageName("expired_lease")
	cleanupTestStage(t, store, stageName)
	t.Cleanup(func() { cleanupTestStage(t, store, stageName) })

	ctx := context.Background()
	first, err := store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-a",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim first run: %v", err)
	}
	if first == nil || !first.Claimed || first.Run == nil {
		t.Fatalf("expected claimed first run, got %#v", first)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE indexer_stage_state
		SET lease_expires_at = NOW() - INTERVAL '1 second'
		WHERE stage_name = $1`,
		stageName,
	); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	second, err := store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-b",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim second run: %v", err)
	}
	if second == nil || !second.Claimed || second.Run == nil {
		t.Fatalf("expected reclaimed run, got %#v", second)
	}
	if second.Run.ID == first.Run.ID {
		t.Fatalf("expected a new run id, got %d", second.Run.ID)
	}

	runs, err := store.ListIndexerStageRuns(ctx, stageName, 10)
	if err != nil {
		t.Fatalf("list stage runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	var abandoned *IndexerStageRun
	var running *IndexerStageRun
	for i := range runs {
		run := runs[i]
		switch run.ID {
		case first.Run.ID:
			abandoned = &run
		case second.Run.ID:
			running = &run
		}
	}
	if abandoned == nil || abandoned.Status != "abandoned" {
		t.Fatalf("expected abandoned first run, got %#v", abandoned)
	}
	if !strings.Contains(abandoned.ErrorText, "lease expired before completion") {
		t.Fatalf("expected stale lease error text, got %q", abandoned.ErrorText)
	}
	if running == nil || running.Status != "running" || running.ClaimedBy != "owner-b" {
		t.Fatalf("expected running reclaimed run owned by owner-b, got %#v", running)
	}

	state := findStageState(t, store, stageName)
	if state.LeaseOwner != "owner-b" {
		t.Fatalf("expected lease owner owner-b, got %q", state.LeaseOwner)
	}
	if state.LastRunID != second.Run.ID {
		t.Fatalf("expected last run id %d, got %d", second.Run.ID, state.LastRunID)
	}
}

func TestPauseResumeIndexerStageControlsClaimEligibility(t *testing.T) {
	store := openTestStore(t)
	stageName := uniqueTestStageName("pause_resume")
	cleanupTestStage(t, store, stageName)
	t.Cleanup(func() { cleanupTestStage(t, store, stageName) })

	ctx := context.Background()
	if err := store.PauseIndexerStage(ctx, stageName); err != nil {
		t.Fatalf("pause stage: %v", err)
	}

	claim, err := store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-a",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim paused stage: %v", err)
	}
	if claim == nil || claim.Claimed || claim.Reason != "paused" {
		t.Fatalf("expected paused skip, got %#v", claim)
	}

	if err := store.ResumeIndexerStage(ctx, stageName); err != nil {
		t.Fatalf("resume stage: %v", err)
	}

	claim, err = store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-a",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim resumed stage: %v", err)
	}
	if claim == nil || !claim.Claimed || claim.Run == nil {
		t.Fatalf("expected resumed stage claim, got %#v", claim)
	}

	state := findStageState(t, store, stageName)
	if state.Paused {
		t.Fatalf("expected stage to be resumed, got %+v", state)
	}
}

func TestCompleteIndexerStageRunAllowsImmediateRerunClaim(t *testing.T) {
	store := openTestStore(t)
	stageName := uniqueTestStageName("rerun")
	cleanupTestStage(t, store, stageName)
	t.Cleanup(func() { cleanupTestStage(t, store, stageName) })

	ctx := context.Background()
	first, err := store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-a",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim first run: %v", err)
	}
	if first == nil || !first.Claimed || first.Run == nil {
		t.Fatalf("expected claimed first run, got %#v", first)
	}

	if err := store.CompleteIndexerStageRun(ctx, IndexerStageFinishRequest{
		RunID: first.Run.ID,
		Owner: "owner-a",
	}); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	second, err := store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-b",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim rerun: %v", err)
	}
	if second == nil || !second.Claimed || second.Run == nil {
		t.Fatalf("expected rerun claim, got %#v", second)
	}

	runs, err := store.ListIndexerStageRuns(ctx, stageName, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	var completed *IndexerStageRun
	for i := range runs {
		run := runs[i]
		if run.ID == first.Run.ID {
			completed = &run
			break
		}
	}
	if completed == nil || completed.Status != "completed" {
		t.Fatalf("expected completed first run, got %#v", completed)
	}
	if completed.FinishedAt == nil {
		t.Fatalf("expected completed run to have finished_at, got %#v", completed)
	}
}

func TestRepairIndexerStageRuntimeClearsExpiredLeaseAndAbandonsRun(t *testing.T) {
	store := openTestStore(t)
	stageName := uniqueTestStageName("repair_runtime")
	cleanupTestStage(t, store, stageName)
	t.Cleanup(func() { cleanupTestStage(t, store, stageName) })

	ctx := context.Background()
	claim, err := store.ClaimIndexerStage(ctx, IndexerStageClaimRequest{
		StageName:     stageName,
		Owner:         "owner-a",
		Enabled:       true,
		Interval:      time.Minute,
		BatchSize:     10,
		Concurrency:   1,
		LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim stage run: %v", err)
	}
	if claim == nil || !claim.Claimed || claim.Run == nil {
		t.Fatalf("expected claimed run, got %#v", claim)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE indexer_stage_state
		SET lease_expires_at = NOW() - INTERVAL '1 second'
		WHERE stage_name = $1`,
		stageName,
	); err != nil {
		t.Fatalf("expire stage lease: %v", err)
	}

	result, err := store.RepairIndexerStageRuntime(ctx)
	if err != nil {
		t.Fatalf("repair indexer stage runtime: %v", err)
	}
	if result == nil {
		t.Fatalf("expected repair result")
	}
	if result.AbandonedRuns < 1 {
		t.Fatalf("expected at least 1 abandoned run, got %d", result.AbandonedRuns)
	}
	if result.ClearedStaleLeases < 1 {
		t.Fatalf("expected at least 1 cleared stale lease, got %d", result.ClearedStaleLeases)
	}

	runs, err := store.ListIndexerStageRuns(ctx, stageName, 10)
	if err != nil {
		t.Fatalf("list stage runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "abandoned" {
		t.Fatalf("expected abandoned repaired run, got %#v", runs)
	}
	if !strings.Contains(runs[0].ErrorText, "repair cleanup") {
		t.Fatalf("expected repair cleanup error text, got %q", runs[0].ErrorText)
	}

	state := findStageState(t, store, stageName)
	if state.LeaseOwner != "" {
		t.Fatalf("expected cleared lease owner, got %q", state.LeaseOwner)
	}
	if state.LeaseExpiresAt != nil {
		t.Fatalf("expected cleared lease expiry, got %+v", state.LeaseExpiresAt)
	}
}

func TestApplyReleaseInspectionUpdateKnownPasswordClearsUnknownRollup(t *testing.T) {
	store := openTestStore(t)
	releaseID := seedTestRelease(t, store, "mixed_password_rollup")

	ctx := context.Background()
	passworded := true
	passwordedUnknown := true
	if err := store.ApplyReleaseInspectionUpdate(ctx, ReleaseInspectionUpdate{
		ReleaseID:         releaseID,
		Encrypted:         boolPtr(true),
		Passworded:        &passworded,
		PasswordedUnknown: &passwordedUnknown,
		PasswordState:     "passworded_unknown",
	}); err != nil {
		t.Fatalf("apply unresolved password state: %v", err)
	}

	passwordedKnown := true
	passwordedUnknown = false
	if err := store.ApplyReleaseInspectionUpdate(ctx, ReleaseInspectionUpdate{
		ReleaseID:         releaseID,
		Passworded:        &passworded,
		PasswordedKnown:   &passwordedKnown,
		PasswordedUnknown: &passwordedUnknown,
		PasswordState:     "passworded_known",
	}); err != nil {
		t.Fatalf("apply verified password state: %v", err)
	}

	release, err := store.GetIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get release detail: %v", err)
	}
	if release == nil {
		t.Fatalf("expected release %s", releaseID)
	}
	if !release.Release.Passworded || !release.Release.PasswordedKnown || release.Release.PasswordedUnknown {
		t.Fatalf("expected known password rollup to win, got %+v", release.Release)
	}
	if release.Release.PasswordState != "passworded_known" {
		t.Fatalf("expected passworded_known state, got %q", release.Release.PasswordState)
	}
	if !release.Release.Encrypted {
		t.Fatalf("expected encrypted flag to remain true, got %+v", release.Release)
	}
}

func TestApplyReleaseInspectionUpdateUnknownPasswordReducesAvailabilityWhileCompletionStaysHigh(t *testing.T) {
	store := openTestStore(t)
	releaseID := seedTestRelease(t, store, "unknown_password_availability")

	ctx := context.Background()
	passworded := true
	passwordedUnknown := true
	if err := store.ApplyReleaseInspectionUpdate(ctx, ReleaseInspectionUpdate{
		ReleaseID:         releaseID,
		Encrypted:         boolPtr(true),
		Passworded:        &passworded,
		PasswordedUnknown: &passwordedUnknown,
		PasswordState:     "passworded_unknown",
	}); err != nil {
		t.Fatalf("apply unresolved password state: %v", err)
	}

	release, err := store.GetIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get release detail: %v", err)
	}
	if release == nil {
		t.Fatalf("expected release %s", releaseID)
	}
	if release.Release.CompletionPct != 100 {
		t.Fatalf("expected completion_pct to stay 100, got %.2f", release.Release.CompletionPct)
	}
	if release.Release.AvailabilityScore >= release.Release.CompletionPct {
		t.Fatalf("expected availability_score %.2f to drop below completion_pct %.2f", release.Release.AvailabilityScore, release.Release.CompletionPct)
	}
	if release.Release.PasswordState != "passworded_unknown" {
		t.Fatalf("expected passworded_unknown state, got %q", release.Release.PasswordState)
	}
}

func TestUpsertReleaseReplacesAvailabilityScoreOnLaterWorseSnapshot(t *testing.T) {
	store := openTestStore(t)

	ctx := context.Background()
	now := time.Now().UTC()
	releaseKey := fmt.Sprintf("test-release-availability-%d", now.UnixNano())
	groupName := fmt.Sprintf("alt.binaries.test.%d", now.UnixNano())

	record := ReleaseRecord{
		ProviderID:              1,
		ReleaseKey:              releaseKey,
		GroupName:               groupName,
		Title:                   "Example Release 2026",
		SourceTitle:             "Example.Release.2026",
		DeobfuscatedTitle:       "Example.Release.2026",
		TitleSource:             "source",
		TitleConfidence:         0.90,
		SearchTitle:             "example release 2026",
		Category:                "usenet",
		Classification:          "video",
		Poster:                  "poster-a",
		SizeBytes:               1000,
		PostedAt:                &now,
		FileCount:               86,
		ExpectedFileCount:       86,
		ParFileCount:            0,
		CompletionPct:           100,
		MatchConfidence:         0.90,
		IdentityStatus:          "identified",
		PasswordState:           "unknown",
		ArchiveCount:            1,
		VideoCount:              1,
		AudioCount:              1,
		AvailabilityScore:       88,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       50,
		MediaQualityTier:        "good",
		IdentityConfidenceScore: 50,
		MetadataUpdatedAt:       &now,
	}

	releaseID, err := store.UpsertRelease(ctx, record)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	record.FileCount = 2
	record.CompletionPct = 2.33
	record.AvailabilityScore = 9.25
	record.AvailabilityTier = "poor"
	record.MetadataUpdatedAt = &now

	if _, err := store.UpsertRelease(ctx, record); err != nil {
		t.Fatalf("upsert worse availability snapshot: %v", err)
	}

	release, err := store.GetIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get release detail: %v", err)
	}
	if release == nil {
		t.Fatalf("expected release %s", releaseID)
	}
	if release.Release.AvailabilityScore != 9.25 {
		t.Fatalf("expected availability_score to be replaced with 9.25, got %.2f", release.Release.AvailabilityScore)
	}
	if release.Release.CompletionPct != 2.33 {
		t.Fatalf("expected completion_pct to be updated to 2.33, got %.2f", release.Release.CompletionPct)
	}
	if release.Release.ExpectedFileCount != 86 {
		t.Fatalf("expected expected_file_count to remain 86, got %d", release.Release.ExpectedFileCount)
	}
}

func TestUpsertReleasePreservesInspectionDerivedTitleAgainstLaterSourceSnapshot(t *testing.T) {
	store := openTestStore(t)

	ctx := context.Background()
	now := time.Now().UTC()
	releaseKey := fmt.Sprintf("test-release-title-preserve-%d", now.UnixNano())
	groupName := fmt.Sprintf("alt.binaries.titlepreserve.%d", now.UnixNano())

	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		ReleaseKey:              releaseKey,
		GroupName:               groupName,
		Title:                   "Kevin S01E06 Fourth of July 720p AMZN WEB-DL DDP5 1 H 264 playWEB",
		SourceTitle:             "iwYYd3MaV3XRQddVFk7krGVUf38ZGhbn.7z",
		DeobfuscatedTitle:       "Kevin.S01E06.Fourth.of.July.720p.AMZN.WEB-DL.DDP5.1.H.264-playWEB",
		TitleSource:             "archive_entry",
		TitleConfidence:         0.92,
		SearchTitle:             "kevin s01e06 fourth of july 720p amzn web dl ddp5 1 h 264 playweb",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  "poster-a",
		SizeBytes:               1000,
		PostedAt:                &now,
		FileCount:               4,
		ExpectedFileCount:       4,
		CompletionPct:           100,
		MatchConfidence:         0.90,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		VideoCount:              1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	if _, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		ReleaseKey:              releaseKey,
		GroupName:               groupName,
		Title:                   "iwYYd3MaV3XRQddVFk7krGVUf38ZGhbn 7z",
		SourceTitle:             "iwYYd3MaV3XRQddVFk7krGVUf38ZGhbn.7z",
		DeobfuscatedTitle:       "",
		TitleSource:             "source",
		TitleConfidence:         0.30,
		SearchTitle:             "iwyyd3mav3xrqddvfk7krgvuf38zghbn 7z",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  "poster-a",
		SizeBytes:               1000,
		PostedAt:                &now,
		FileCount:               4,
		ExpectedFileCount:       4,
		CompletionPct:           100,
		MatchConfidence:         0.90,
		IdentityStatus:          "probable",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 88,
		MetadataUpdatedAt:       &now,
	}); err != nil {
		t.Fatalf("upsert later source-only snapshot: %v", err)
	}

	release, err := store.GetIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get release detail: %v", err)
	}
	if release.Release.TitleSource != "archive_entry" {
		t.Fatalf("expected title_source archive_entry to be preserved, got %q", release.Release.TitleSource)
	}
	if release.Release.Title != "Kevin S01E06 Fourth of July 720p AMZN WEB-DL DDP5 1 H 264 playWEB" {
		t.Fatalf("expected inspection-derived title to be preserved, got %q", release.Release.Title)
	}
	if release.Release.DeobfuscatedTitle != "Kevin.S01E06.Fourth.of.July.720p.AMZN.WEB-DL.DDP5.1.H.264-playWEB" {
		t.Fatalf("expected deobfuscated title to be preserved, got %q", release.Release.DeobfuscatedTitle)
	}
}

func TestUpsertReleaseNormalizesBlankFamilyIdentity(t *testing.T) {
	store := openTestStore(t)

	ctx := context.Background()
	now := time.Now().UTC()
	releaseKey := fmt.Sprintf("test-release-identity-%d", now.UnixNano())
	groupName := fmt.Sprintf("alt.binaries.identity.%d", now.UnixNano())

	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		ReleaseKey:              releaseKey,
		GroupName:               groupName,
		Title:                   "Identity Repair Example 2026",
		SourceTitle:             "Identity.Repair.Example.2026",
		TitleSource:             "source",
		TitleConfidence:         0.90,
		SearchTitle:             "identity repair example 2026",
		Category:                "usenet",
		Classification:          "video",
		Poster:                  "poster-a",
		SizeBytes:               1000,
		PostedAt:                &now,
		FileCount:               1,
		ExpectedFileCount:       1,
		CompletionPct:           100,
		MatchConfidence:         0.90,
		IdentityStatus:          "identified",
		PasswordState:           "unknown",
		ArchiveCount:            1,
		VideoCount:              1,
		AudioCount:              1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       50,
		MediaQualityTier:        "good",
		IdentityConfidenceScore: 50,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release with blank family identity: %v", err)
	}

	var sourceReleaseKey string
	var releaseFamilyKey string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT source_release_key, release_family_key
		FROM releases
		WHERE release_id = $1`, releaseID,
	).Scan(&sourceReleaseKey, &releaseFamilyKey); err != nil {
		t.Fatalf("query release identity: %v", err)
	}

	if sourceReleaseKey != releaseKey {
		t.Fatalf("expected source_release_key fallback %q, got %q", releaseKey, sourceReleaseKey)
	}
	if releaseFamilyKey != releaseKey {
		t.Fatalf("expected release_family_key fallback %q, got %q", releaseKey, releaseFamilyKey)
	}
}

func TestUpsertBinaryMirrorsReleaseFamilyKeyIntoLegacyReleaseKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.binary.identity.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binaries WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup binaries: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "matcher trace key",
		ReleaseFamilyKey:  "family key",
		ReleaseKey:        "legacy source alias",
		BinaryKey:         fmt.Sprintf("binary-identity-%d", time.Now().UnixNano()),
		BinaryName:        "example.release.2026.mkv",
		FileName:          "example.release.2026.mkv",
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	var releaseKey string
	var sourceReleaseKey string
	var releaseFamilyKey string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT release_key, source_release_key, release_family_key
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&releaseKey, &sourceReleaseKey, &releaseFamilyKey); err != nil {
		t.Fatalf("query binary identity: %v", err)
	}

	if releaseFamilyKey != "family key" {
		t.Fatalf("expected release_family_key to remain family key, got %q", releaseFamilyKey)
	}
	if releaseKey != "family key" {
		t.Fatalf("expected legacy release_key mirror family key, got %q", releaseKey)
	}
	if sourceReleaseKey != "matcher trace key" {
		t.Fatalf("expected source_release_key matcher trace to be preserved, got %q", sourceReleaseKey)
	}
}

func TestUpsertBinaryStoresGroupingEvidenceInSideTable(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.binary.evidence.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binaries WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup binaries: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "matcher trace key",
		ReleaseFamilyKey:  "family key",
		ReleaseKey:        "family key",
		BinaryKey:         fmt.Sprintf("binary-evidence-%d", time.Now().UnixNano()),
		BinaryName:        "example.release.2026.mkv",
		FileName:          "example.release.2026.mkv",
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
		GroupingEvidence: map[string]any{
			"summary": map[string]any{
				"kind": "readable_title",
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	var inlineEvidence []byte
	if err := store.DB().QueryRowContext(ctx, `
		SELECT grouping_evidence_json
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&inlineEvidence); err != nil {
		t.Fatalf("query inline grouping evidence: %v", err)
	}
	if strings.TrimSpace(string(inlineEvidence)) != "{}" {
		t.Fatalf("expected inline grouping evidence to be cleared, got %s", string(inlineEvidence))
	}

	var sideEvidence []byte
	if err := store.DB().QueryRowContext(ctx, `
		SELECT payload_json
		FROM binary_grouping_evidence
		WHERE binary_id = $1`, binaryID,
	).Scan(&sideEvidence); err != nil {
		t.Fatalf("query side-table grouping evidence: %v", err)
	}
	if !strings.Contains(string(sideEvidence), "\"readable_title\"") {
		t.Fatalf("expected side-table grouping evidence payload, got %s", string(sideEvidence))
	}

	detail, err := store.GetIndexerBinaryDetail(ctx, binaryID)
	if err != nil {
		t.Fatalf("get indexer binary detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected binary detail for %d", binaryID)
	}
	if !strings.Contains(string(detail.GroupingEvidence), "\"readable_title\"") {
		t.Fatalf("expected binary detail grouping evidence from side table, got %s", string(detail.GroupingEvidence))
	}
}

func TestRefreshBinaryStatsBackfillsPostedAtFromArticleHeaders(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.refresh.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_parts WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup binary parts: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binaries WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup binaries: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM article_headers WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup article headers: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM posters WHERE id = $1`, posterID); err != nil {
			t.Fatalf("cleanup poster: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	earliest := time.Date(2026, 4, 16, 10, 5, 0, 0, time.UTC)
	later := earliest.Add(12 * time.Minute)
	suffix := time.Now().UnixNano()
	inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 101,
			MessageID:     fmt.Sprintf("<binary-refresh-%d-1@test>", suffix),
			Subject:       `Test Release [1/2] - "test.7z.001" yEnc (1/20)`,
			Poster:        posterName,
			DateUTC:       &earliest,
			Bytes:         500,
			Lines:         10,
		},
		{
			ArticleNumber: 102,
			MessageID:     fmt.Sprintf("<binary-refresh-%d-2@test>", suffix),
			Subject:       `Test Release [1/2] - "test.7z.001" yEnc (2/20)`,
			Poster:        posterName,
			DateUTC:       &later,
			Bytes:         700,
			Lines:         12,
		},
	})
	if err != nil {
		t.Fatalf("insert article headers: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected 2 inserted headers, got %d", inserted)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT id, article_number
		FROM article_headers
		WHERE newsgroup_id = $1
		ORDER BY article_number`, newsgroupID)
	if err != nil {
		t.Fatalf("query article headers: %v", err)
	}
	defer rows.Close()

	type articleRow struct {
		id            int64
		articleNumber int64
	}
	articles := make([]articleRow, 0, 2)
	for rows.Next() {
		var item articleRow
		if err := rows.Scan(&item.id, &item.articleNumber); err != nil {
			t.Fatalf("scan article header: %v", err)
		}
		articles = append(articles, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate article headers: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 stored article headers, got %d", len(articles))
	}

	var (
		payloadPosterID   int64
		payloadPosterText string
		subjectFileName   string
		subjectFileIndex  int
		subjectFileTotal  int
		yencPartNumber    int
		yencTotalParts    int
		yencFileSize      int64
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			COALESCE(poster_id, 0),
			poster,
			subject_file_name,
			subject_file_index,
			subject_file_total,
			yenc_part_number,
			yenc_total_parts,
			yenc_file_size
		FROM article_header_ingest_payloads
		WHERE article_header_id = $1`, articles[0].id).Scan(
		&payloadPosterID,
		&payloadPosterText,
		&subjectFileName,
		&subjectFileIndex,
		&subjectFileTotal,
		&yencPartNumber,
		&yencTotalParts,
		&yencFileSize,
	); err != nil {
		t.Fatalf("query ingest payload: %v", err)
	}
	if payloadPosterID != posterID {
		t.Fatalf("expected poster_id %d, got %d", posterID, payloadPosterID)
	}
	if payloadPosterText != "" {
		t.Fatalf("expected payload poster text to be normalized away, got %q", payloadPosterText)
	}
	if subjectFileName != "test.7z.001" || subjectFileIndex != 1 || subjectFileTotal != 2 {
		t.Fatalf("unexpected parsed file info: name=%q index=%d total=%d", subjectFileName, subjectFileIndex, subjectFileTotal)
	}
	if yencPartNumber != 1 || yencTotalParts != 20 || yencFileSize != 0 {
		t.Fatalf("unexpected parsed yenc info: part=%d total=%d size=%d", yencPartNumber, yencTotalParts, yencFileSize)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  "test-release-source",
		ReleaseFamilyKey:  "test-release-family",
		FileFamilyKey:     "test-file-family",
		FamilyKind:        "archive_stem",
		BaseStem:          "test",
		IsMainPayload:     true,
		ReleaseKey:        "test-release-family",
		ReleaseName:       "Test Release",
		BinaryKey:         fmt.Sprintf("test-release-source::binary-%d", suffix),
		BinaryName:        "test.7z.001",
		FileName:          "test.7z.001",
		FileIndex:         1,
		ExpectedFileCount: 2,
		TotalParts:        20,
		MatchConfidence:   0.90,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	for idx, article := range articles {
		if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: article.id,
			MessageID:       fmt.Sprintf("<part-%d-%d@test>", suffix, idx+1),
			PartNumber:      idx + 1,
			TotalParts:      20,
			SegmentBytes:    int64(500 + idx),
			FileName:        "test.7z.001",
		}); err != nil {
			t.Fatalf("upsert binary part %d: %v", idx, err)
		}
	}

	if err := store.RefreshBinaryStats(ctx, binaryID); err != nil {
		t.Fatalf("refresh binary stats: %v", err)
	}

	var gotPostedAt time.Time
	var firstArticleNumber int64
	var lastArticleNumber int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT posted_at, first_article_number, last_article_number
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&gotPostedAt, &firstArticleNumber, &lastArticleNumber); err != nil {
		t.Fatalf("query refreshed binary: %v", err)
	}
	if !gotPostedAt.Equal(earliest) {
		t.Fatalf("expected earliest posted_at %s, got %s", earliest, gotPostedAt.UTC())
	}
	if firstArticleNumber != 101 || lastArticleNumber != 102 {
		t.Fatalf("expected article number range 101-102, got %d-%d", firstArticleNumber, lastArticleNumber)
	}

	var dirtyCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_stage_dirty_families
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'test-release-family'`, newsgroupID,
	).Scan(&dirtyCount); err != nil {
		t.Fatalf("query release dirty queue: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("expected refreshed binary stats to requeue release family once, got %d rows", dirtyCount)
	}
}

func TestStartBinaryInspectionIgnoresMissingReleaseID(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		PosterID:         posterID,
		SourceReleaseKey: "inspect-missing-release-source",
		ReleaseFamilyKey: "inspect-missing-release-family",
		FileFamilyKey:    "inspect-missing-release-file",
		FamilyKind:       "archive_stem",
		IsMainPayload:    true,
		ReleaseKey:       "inspect-missing-release-family",
		ReleaseName:      "Inspect Missing Release",
		BinaryKey:        fmt.Sprintf("inspect-missing-release::%d", time.Now().UnixNano()),
		BinaryName:       "inspect-missing-release.bin",
		FileName:         "inspect-missing-release.bin",
		TotalParts:       1,
		MatchConfidence:  0.90,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.StartBinaryInspection(ctx, "inspect_discovery", binaryID, "missing-release-id", nil); err != nil {
		t.Fatalf("start binary inspection with missing release id: %v", err)
	}

	var releaseID sql.NullString
	if err := store.DB().QueryRowContext(ctx, `
		SELECT release_id
		FROM binary_inspections
		WHERE stage_name = 'inspect_discovery' AND binary_id = $1`, binaryID,
	).Scan(&releaseID); err != nil {
		t.Fatalf("query binary inspection row: %v", err)
	}
	if releaseID.Valid {
		t.Fatalf("expected missing release id to be normalized to NULL, got %q", releaseID.String)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM binary_inspections
		WHERE stage_name = 'inspect_discovery' AND binary_id = $1`, binaryID,
	); err != nil {
		t.Fatalf("cleanup binary inspection row: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("cleanup binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM posters WHERE id = $1`, posterID); err != nil {
		t.Fatalf("cleanup poster: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesPrefersFamiliesWithCompleteBinaries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.queue.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	makeBinary := func(family string, expected, totalParts, observedParts int) int64 {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			SourceReleaseKey:  family,
			ReleaseFamilyKey:  family,
			FileFamilyKey:     family + "::file",
			FamilyKind:        "readable_title",
			IsMainPayload:     true,
			ReleaseKey:        family,
			ReleaseName:       family,
			BinaryKey:         fmt.Sprintf("%s::%d", family, time.Now().UnixNano()),
			BinaryName:        family + ".mkv",
			FileName:          family + ".mkv",
			ExpectedFileCount: expected,
			TotalParts:        totalParts,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", family, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE binaries
			SET observed_parts = $2, total_bytes = 12345, updated_at = NOW()
			WHERE id = $1`, binaryID, observedParts,
		); err != nil {
			t.Fatalf("update binary %s stats: %v", family, err)
		}
		return binaryID
	}

	incompleteFamily := fmt.Sprintf("queue-incomplete-%d", time.Now().UnixNano())
	completeFamily := fmt.Sprintf("queue-complete-%d", time.Now().UnixNano())
	incompleteBinaryID := makeBinary(incompleteFamily, 1, 10, 0)
	completeBinaryID := makeBinary(completeFamily, 1, 10, 10)

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE release_stage_dirty_families
		SET updated_at = NOW() - INTERVAL '2 minutes'
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, incompleteFamily,
	); err != nil {
		t.Fatalf("age incomplete queue row: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE release_stage_dirty_families
		SET updated_at = NOW() - INTERVAL '1 minute'
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, completeFamily,
	); err != nil {
		t.Fatalf("age complete queue row: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1)
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != completeFamily {
		t.Fatalf("expected complete family %q, got %q", completeFamily, candidates[0].ReleaseFamilyKey)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id IN ($1, $2)`, incompleteBinaryID, completeBinaryID); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_stage_dirty_families
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key IN ($2, $3)`, newsgroupID, incompleteFamily, completeFamily,
	); err != nil {
		t.Fatalf("cleanup dirty families: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListUnassembledArticleHeadersPrioritizesHeadersThatMatchExistingBinaries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.assemble.priority.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-priority-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_parts WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup binary parts: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binaries WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup binaries: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM article_headers WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup article headers: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM posters WHERE id = $1`, posterID); err != nil {
			t.Fatalf("cleanup poster: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	progressFile := "priority.release.part01.rar"
	progressFamily := fmt.Sprintf("priority-family-%d", time.Now().UnixNano())
	if _, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  progressFamily,
		ReleaseFamilyKey:  progressFamily,
		FileFamilyKey:     progressFamily + "::file",
		FamilyKind:        "archive_stem",
		BaseStem:          "priority.release",
		IsMainPayload:     true,
		ReleaseKey:        progressFamily,
		ReleaseName:       "Priority Release",
		BinaryKey:         fmt.Sprintf("%s::existing", progressFamily),
		BinaryName:        progressFile,
		FileName:          progressFile,
		FileIndex:         1,
		ExpectedFileCount: 2,
		TotalParts:        20,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	}); err != nil {
		t.Fatalf("upsert seed binary: %v", err)
	}

	baseTime := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 101,
			MessageID:     fmt.Sprintf("<priority-progress-%d@test>", time.Now().UnixNano()),
			Subject:       `Priority Release [1/2] - "priority.release.part01.rar" yEnc (11/20)`,
			Poster:        posterName,
			DateUTC:       &baseTime,
			Bytes:         2048,
			Lines:         20,
		},
		{
			ArticleNumber: 102,
			MessageID:     fmt.Sprintf("<priority-fresh-%d@test>", time.Now().UnixNano()),
			Subject:       `Fresh Release [1/1] - "fresh.release.r00" yEnc (1/10)`,
			Poster:        posterName,
			DateUTC:       func() *time.Time { t := baseTime.Add(time.Minute); return &t }(),
			Bytes:         1024,
			Lines:         10,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	got, err := store.ListUnassembledArticleHeaders(ctx, 2)
	if err != nil {
		t.Fatalf("list unassembled article headers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
	if got[0].FileName != progressFile {
		t.Fatalf("expected progress-improving header first, got %q", got[0].FileName)
	}
	if got[1].FileName != "fresh.release.r00" {
		t.Fatalf("expected fresh header second, got %q", got[1].FileName)
	}
	if got[0].ID >= got[1].ID {
		t.Fatalf("expected prioritized lane to override raw newest-first ordering, got ids %d then %d", got[0].ID, got[1].ID)
	}
}

func TestListBinaryInspectionCandidatesInspectArchiveDedupesArchiveFamilies(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-archive-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-dedupe-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive Dedupe Test",
		SourceTitle:             "Archive.Dedupe.Test",
		SearchTitle:             "archive dedupe test",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  posterName,
		FileCount:               4,
		ExpectedFileCount:       4,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	fileNames := []string{
		"archive.test.part01.rar",
		"archive.test.part38.rar",
		"archive.test.part39.rar",
		"archive.test.part40.rar",
	}
	releaseFiles := make([]ReleaseFileRecord, 0, len(fileNames))
	for idx, name := range fileNames {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			PosterID:          posterID,
			SourceReleaseKey:  baseKey,
			ReleaseFamilyKey:  baseKey,
			FileFamilyKey:     baseKey + "::archive",
			FamilyKind:        "archive_stem",
			BaseStem:          "archive.test",
			IsMainPayload:     true,
			ReleaseKey:        baseKey,
			ReleaseName:       "Archive Dedupe Test",
			BinaryKey:         fmt.Sprintf("%s::%d", baseKey, idx+1),
			BinaryName:        name,
			FileName:          name,
			FileIndex:         idx + 1,
			ExpectedFileCount: len(fileNames),
			TotalParts:        1,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", name, err)
		}
		releaseFiles = append(releaseFiles, ReleaseFileRecord{
			BinaryID:  binaryID,
			FileName:  name,
			SizeBytes: 716800,
			FileIndex: idx + 1,
		})
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, releaseFiles); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_archive", 20)
	if err != nil {
		t.Fatalf("list inspect archive candidates: %v", err)
	}

	filtered := make([]BinaryInspectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ReleaseID == releaseID {
			filtered = append(filtered, candidate)
		}
	}

	if len(filtered) != 1 {
		t.Fatalf("expected one archive candidate for release family, got %d: %+v", len(filtered), filtered)
	}
	if filtered[0].FileName != "archive.test.part01.rar" {
		t.Fatalf("expected representative archive candidate part01.rar, got %+v", filtered[0])
	}
}

func TestListBinaryInspectionCandidatesInspectArchiveDoesNotFallThroughToLaterRARVolumes(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.nofallthrough.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-archive-nofallthrough-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-nofallthrough-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive No Fallthrough Test",
		SourceTitle:             "Archive.No.Fallthrough.Test",
		SearchTitle:             "archive no fallthrough test",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  posterName,
		FileCount:               3,
		ExpectedFileCount:       3,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	fileNames := []string{
		"archive.fallthrough.part01.rar",
		"archive.fallthrough.part02.rar",
		"archive.fallthrough.part03.rar",
	}
	releaseFiles := make([]ReleaseFileRecord, 0, len(fileNames))
	var representativeBinaryID int64
	for idx, name := range fileNames {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			PosterID:          posterID,
			SourceReleaseKey:  baseKey,
			ReleaseFamilyKey:  baseKey,
			FileFamilyKey:     baseKey + "::archive",
			FamilyKind:        "archive_stem",
			BaseStem:          "archive.fallthrough",
			IsMainPayload:     true,
			ReleaseKey:        baseKey,
			ReleaseName:       "Archive No Fallthrough Test",
			BinaryKey:         fmt.Sprintf("%s::%d", baseKey, idx+1),
			BinaryName:        name,
			FileName:          name,
			FileIndex:         idx + 1,
			ExpectedFileCount: len(fileNames),
			TotalParts:        1,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", name, err)
		}
		if idx == 0 {
			representativeBinaryID = binaryID
		}
		releaseFiles = append(releaseFiles, ReleaseFileRecord{
			BinaryID:  binaryID,
			FileName:  name,
			SizeBytes: 716800,
			FileIndex: idx + 1,
		})
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, releaseFiles); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_archive",
		BinaryID:        representativeBinaryID,
		ReleaseID:       releaseID,
		Status:          "completed",
		Summary:         map[string]any{"archive": true},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete representative inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_archive", 20)
	if err != nil {
		t.Fatalf("list inspect archive candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.ReleaseID == releaseID {
			t.Fatalf("expected later archive volumes to stay excluded after part01 completion, got %+v", candidate)
		}
	}
}

func TestListBinaryInspectionCandidatesInspectArchiveSkipsCompletedProbeErrorDetailsUntilSourceChanges(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.retry.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-archive-retry-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-retry-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive Retry Test",
		SourceTitle:             "Archive.Retry.Test",
		SearchTitle:             "archive retry test",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  posterName,
		FileCount:               1,
		ExpectedFileCount:       1,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::archive",
		FamilyKind:        "archive_stem",
		BaseStem:          "archive.retry",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Archive Retry Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "archive.retry.part01.rar",
		FileName:          "archive.retry.part01.rar",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "archive.retry.part01.rar",
		SizeBytes: 716800,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_archive",
		BinaryID:        binaryID,
		ReleaseID:       releaseID,
		Status:          "completed",
		Summary:         map[string]any{"probe_error_detail": "Cannot open the file as archive", "probe_skip_reason": "not_archive_or_unsupported"},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete binary inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_archive", 20)
	if err != nil {
		t.Fatalf("list inspect archive candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			t.Fatalf("expected completed probe-error-detail archive candidate to be skipped until source changes, got %+v", candidate)
		}
	}
}

func TestListBinaryInspectionCandidatesInspectArchiveRetriesCompletedProbeErrorRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.retryrow.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := store.EnsurePoster(ctx, fmt.Sprintf("poster-archive-retryrow-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-retryrow-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive Retry Row Test",
		SourceTitle:             "Archive.Retry.Row.Test",
		SearchTitle:             "archive retry row test",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  "poster-a",
		FileCount:               1,
		ExpectedFileCount:       1,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::archive",
		FamilyKind:        "archive_stem",
		BaseStem:          "archive.retry.row",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Archive Retry Row Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "archive.retry.row.7z.001",
		FileName:          "archive.retry.row.7z.001",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "archive.retry.row.7z.001",
		SizeBytes: 716800,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO binary_inspections (stage_name, binary_id, release_id, status, started_at, finished_at, error_text, materialized_bytes, tool_provenance_json, summary_json, updated_at)
		VALUES ('inspect_archive', $1, $2, 'completed', NOW(), NOW(), '', 0, '{}'::jsonb, jsonb_build_object('probe_error', 'fetch article <x@y>: write tcp 1.2.3.4:123->5.6.7.8:563: write: broken pipe'), NOW())
		ON CONFLICT (stage_name, binary_id) DO UPDATE
		SET status = EXCLUDED.status,
		    summary_json = EXCLUDED.summary_json,
		    updated_at = NOW()`, binaryID, releaseID); err != nil {
		t.Fatalf("seed invalid completed probe_error row: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_archive", 20)
	if err != nil {
		t.Fatalf("list inspect archive candidates: %v", err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected invalid completed probe_error row to be retried")
	}
}

func TestCompleteBinaryInspectionCoercesRecoverableProbeErrorToFailed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.recoverable.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := store.EnsurePoster(ctx, fmt.Sprintf("poster-archive-recoverable-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-recoverable-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive Recoverable Test",
		SourceTitle:             "Archive.Recoverable.Test",
		SearchTitle:             "archive recoverable test",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  "poster-a",
		FileCount:               1,
		ExpectedFileCount:       1,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::archive",
		FamilyKind:        "archive_stem",
		BaseStem:          "archive.recoverable",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Archive Recoverable Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "archive.recoverable.7z.001",
		FileName:          "archive.recoverable.7z.001",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "archive.recoverable.7z.001",
		SizeBytes: 716800,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	errMsg := "fetch article <abc@example>: write tcp 10.0.0.1:1234->1.2.3.4:563: write: broken pipe"
	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_archive",
		BinaryID:        binaryID,
		ReleaseID:       releaseID,
		Status:          "completed",
		Summary:         map[string]any{"probe_error": errMsg},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete binary inspection: %v", err)
	}

	var status, errorText string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT status, error_text
		FROM binary_inspections
		WHERE stage_name = 'inspect_archive' AND binary_id = $1`, binaryID,
	).Scan(&status, &errorText); err != nil {
		t.Fatalf("query inspection row: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected status failed, got %q", status)
	}
	if errorText != errMsg {
		t.Fatalf("expected error_text to be preserved, got %q", errorText)
	}
}

func TestListBinaryInspectionCandidatesInspectMediaRerunsAfterArchiveInspectionRefresh(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.media.retry.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-media-retry-%d@example.com", time.Now().UnixNano())
	posterID, err := store.EnsurePoster(ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("media-rerun-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Media Archive Retry Test",
		SourceTitle:             "Media.Archive.Retry.Test",
		SearchTitle:             "media archive retry test",
		Category:                "usenet",
		Classification:          "video_archive",
		Poster:                  posterName,
		FileCount:               1,
		ExpectedFileCount:       1,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		ArchiveCount:            1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::archive",
		FamilyKind:        "archive_stem",
		BaseStem:          "media.retry",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Media Archive Retry Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "media.retry.7z.001",
		FileName:          "media.retry.7z.001",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "media.retry.7z.001",
		SizeBytes: 716800,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	mediaUpdatedAt := now.Add(-2 * time.Hour)
	archiveUpdatedAt := now
	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_media",
		BinaryID:        binaryID,
		ReleaseID:       releaseID,
		Status:          "completed",
		Summary:         map[string]any{"file_extension": ".001", "probe_mode": "heuristic"},
		SourceUpdatedAt: &mediaUpdatedAt,
	}); err != nil {
		t.Fatalf("complete media inspection: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binary_inspections SET updated_at = $2 WHERE stage_name = 'inspect_media' AND binary_id = $1`, binaryID, mediaUpdatedAt); err != nil {
		t.Fatalf("rewind media updated_at: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_archive",
		BinaryID:        binaryID,
		ReleaseID:       releaseID,
		Status:          "completed",
		Summary:         map[string]any{"archive_entries": []any{"Example.Release.2026/Example.Release.2026.mkv"}},
		SourceUpdatedAt: &archiveUpdatedAt,
	}); err != nil {
		t.Fatalf("complete archive inspection: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binary_inspections SET updated_at = $2 WHERE stage_name = 'inspect_archive' AND binary_id = $1`, binaryID, archiveUpdatedAt); err != nil {
		t.Fatalf("set archive updated_at: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_media", 20)
	if err != nil {
		t.Fatalf("list inspect media candidates: %v", err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected inspect_media candidate to rerun after fresher archive inspection")
	}
}

func TestListReleaseTitleCandidatesIncludesArchiveMediaEntries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       2,
		ReleaseKey:        "title.archive.release",
		ReleaseFamilyKey:  "title.archive.release",
		BinaryName:        "opaque.7z.001",
		FileName:          "opaque.7z.001",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName: "inspect_archive",
		BinaryID:  binaryID,
		ReleaseID: "release-1",
		Status:    "completed",
		Summary: map[string]any{
			"archive_entries": []any{
				"Show.Name.S01E01.1080p.WEB.H264-GROUP/Show.Name.S01E01.1080p.WEB.H264-GROUP.mkv",
			},
		},
	}); err != nil {
		t.Fatalf("complete archive inspection: %v", err)
	}

	candidates, err := store.ListReleaseTitleCandidates(ctx, []int64{binaryID})
	if err != nil {
		t.Fatalf("list release title candidates: %v", err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.Source == "archive_entry" && strings.Contains(candidate.Value, ".mkv") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected archive media entry candidate, got %#v", candidates)
	}
}

func TestReplaceReleaseFilesEvictsStaleCrossReleaseBinaryLinks(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	groupOne := fmt.Sprintf("alt.test.releasefiles.one.%d", now.UnixNano())
	groupTwo := fmt.Sprintf("alt.test.releasefiles.two.%d", now.UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupOne)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := store.EnsurePoster(ctx, fmt.Sprintf("poster-releasefiles-%d@example.com", now.UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	releaseOne, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:        1,
		ReleaseKey:        fmt.Sprintf("releasefiles-one-%d", now.UnixNano()),
		GroupName:         groupOne,
		Title:             "Release Files One",
		SourceTitle:       "Release.Files.One",
		TitleSource:       "source",
		SearchTitle:       "release files one",
		Category:          "usenet",
		Classification:    "video",
		Poster:            "poster-a",
		PostedAt:          &now,
		FileCount:         1,
		ExpectedFileCount: 1,
		CompletionPct:     100,
		MatchConfidence:   0.9,
		IdentityStatus:    "identified",
		AvailabilityScore: 100,
		AvailabilityTier:  "excellent",
		MetadataUpdatedAt: &now,
	})
	if err != nil {
		t.Fatalf("upsert release one: %v", err)
	}
	releaseTwo, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:        1,
		ReleaseKey:        fmt.Sprintf("releasefiles-two-%d", now.UnixNano()),
		GroupName:         groupTwo,
		Title:             "Release Files Two",
		SourceTitle:       "Release.Files.Two",
		TitleSource:       "source",
		SearchTitle:       "release files two",
		Category:          "usenet",
		Classification:    "video",
		Poster:            "poster-a",
		PostedAt:          &now,
		FileCount:         1,
		ExpectedFileCount: 1,
		CompletionPct:     100,
		MatchConfidence:   0.9,
		IdentityStatus:    "identified",
		AvailabilityScore: 100,
		AvailabilityTier:  "excellent",
		MetadataUpdatedAt: &now,
	})
	if err != nil {
		t.Fatalf("upsert release two: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  "releasefiles",
		ReleaseFamilyKey:  "releasefiles",
		FileFamilyKey:     "releasefiles::binary",
		FamilyKind:        "archive_stem",
		BaseStem:          "releasefiles",
		IsMainPayload:     true,
		ReleaseKey:        "releasefiles",
		ReleaseName:       "Release Files",
		BinaryKey:         "releasefiles::binary",
		BinaryName:        "releasefiles.part01.rar",
		FileName:          "releasefiles.part01.rar",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseOne, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "releasefiles.part01.rar",
		SizeBytes: 1234,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release one files: %v", err)
	}
	if err := store.ReplaceReleaseFiles(ctx, releaseTwo, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "releasefiles.part01.rar",
		SizeBytes: 1234,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release two files: %v", err)
	}

	var countOne, countTwo int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM release_files WHERE release_id = $1`, releaseOne).Scan(&countOne); err != nil {
		t.Fatalf("count release one files: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM release_files WHERE release_id = $1`, releaseTwo).Scan(&countTwo); err != nil {
		t.Fatalf("count release two files: %v", err)
	}
	if countOne != 0 {
		t.Fatalf("expected stale binary link to be evicted from first release, got %d rows", countOne)
	}
	if countTwo != 1 {
		t.Fatalf("expected second release to retain binary link, got %d rows", countTwo)
	}
}

func TestRunIndexerMaintenancePurgesOrphanReleases(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:        1,
		ReleaseKey:        fmt.Sprintf("orphan-release-%d", now.UnixNano()),
		GroupName:         fmt.Sprintf("alt.test.orphan.release.%d", now.UnixNano()),
		Title:             "Orphan Release Test",
		SourceTitle:       "Orphan.Release.Test",
		TitleSource:       "source",
		SearchTitle:       "orphan release test",
		Category:          "usenet",
		Classification:    "video",
		Poster:            "poster-orphan",
		PostedAt:          &now,
		FileCount:         1,
		ExpectedFileCount: 1,
		CompletionPct:     100,
		MatchConfidence:   0.9,
		IdentityStatus:    "identified",
		AvailabilityScore: 100,
		AvailabilityTier:  "excellent",
		MetadataUpdatedAt: &now,
	})
	if err != nil {
		t.Fatalf("upsert orphan release: %v", err)
	}

	out, err := store.RunIndexerMaintenance(ctx)
	if err != nil {
		t.Fatalf("run indexer maintenance: %v", err)
	}
	if out == nil || out.PurgedOrphanReleases < 1 {
		t.Fatalf("expected orphan release purge count, got %+v", out)
	}

	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE release_id = $1`, releaseID).Scan(&count); err != nil {
		t.Fatalf("count orphan release: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected orphan release to be purged, got %d rows", count)
	}
}

func TestListPublicIndexerReleasesReturnsStableVisibleContract(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("publicvisible%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.PasswordState = "passworded_known"
	})

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{
		{
			FileName:  fmt.Sprintf("%s.7z.001", token),
			SizeBytes: 700,
			FileIndex: 1,
			PostedAt:  record.PostedAt,
		},
		{
			FileName:  fmt.Sprintf("%s.par2", token),
			SizeBytes: 128,
			FileIndex: 2,
			IsPars:    true,
			PostedAt:  record.PostedAt,
		},
	}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	items, total, err := store.ListPublicIndexerReleases(ctx, token, 50, 0)
	if err != nil {
		t.Fatalf("list public indexer releases: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected one visible release, got total=%d items=%d", total, len(items))
	}
	if items[0].ReleaseID != releaseID {
		t.Fatalf("expected release %s, got %+v", releaseID, items[0])
	}
	if items[0].PasswordState != "passworded_known" {
		t.Fatalf("expected stable password state, got %+v", items[0])
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public indexer release detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected detail for %s", releaseID)
	}
	if detail.Release.ReleaseID != releaseID || detail.Release.Title != record.Title {
		t.Fatalf("unexpected public detail release payload: %+v", detail.Release)
	}
	if len(detail.Files) != 2 {
		t.Fatalf("expected 2 public files, got %d", len(detail.Files))
	}
	if detail.Files[0].FileName == "" || detail.Files[0].SizeBytes <= 0 {
		t.Fatalf("expected stable file summary payload, got %+v", detail.Files[0])
	}
}

func TestPublicIndexerReleaseVisibilitySuppressesWeakFragmentRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("weakfragment%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.FileCount = 1
		in.ExpectedFileCount = 86
		in.CompletionPct = 2.33
		in.MatchConfidence = 0.60
		in.IdentityStatus = "unknown"
		in.AvailabilityScore = 9.25
		in.AvailabilityTier = "poor"
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, token, 50, 0)
	if err != nil {
		t.Fatalf("list public weak fragment releases: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected weak fragment release to be hidden, got total=%d items=%d", total, len(items))
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public weak fragment detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected weak fragment detail to be hidden, got %+v", detail)
	}
}

func TestPublicIndexerReleaseVisibilitySuppressesSeedRowsFromSearchAndDetail(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("seedfilter%d", time.Now().UnixNano())
	visibleID, _ := seedVisibilityTestRelease(t, store, token, nil)
	hiddenID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.GroupName = fmt.Sprintf("seed.group.%s", token)
		in.Title = fmt.Sprintf("Seed Release %s 2026 1080p BluRay x265-GRP", token)
		in.SourceTitle = strings.ReplaceAll(in.Title, " ", ".")
		in.SearchTitle = strings.ToLower(in.Title)
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, token, 50, 0)
	if err != nil {
		t.Fatalf("list public releases with seed row: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected only one visible release, got total=%d items=%d", total, len(items))
	}
	if items[0].ReleaseID != visibleID {
		t.Fatalf("expected visible release %s, got %+v", visibleID, items[0])
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, hiddenID)
	if err != nil {
		t.Fatalf("get public hidden seed detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected hidden seed detail to be suppressed, got %+v", detail)
	}
}

func TestPublicIndexerReleaseVisibilitySuppressesPlaceholderTitles(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("unknownrelease%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.Title = "unknown-release"
		in.SourceTitle = "unknown-release"
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, token, 50, 0)
	if err != nil {
		t.Fatalf("list public placeholder-title releases: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected placeholder-title release to be hidden, got total=%d items=%d", total, len(items))
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public placeholder-title detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected placeholder-title detail to be hidden, got %+v", detail)
	}
}

func TestPublicIndexerReleaseSummarySuppressesUnstablePasswordState(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("publicpasswordunknown%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.PasswordState = "unknown"
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, token, 50, 0)
	if err != nil {
		t.Fatalf("list public releases with unstable password state: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected one visible release, got total=%d items=%d", total, len(items))
	}
	if items[0].ReleaseID != releaseID {
		t.Fatalf("expected release %s, got %+v", releaseID, items[0])
	}
	if items[0].PasswordState != "" {
		t.Fatalf("expected unstable password state to be suppressed, got %+v", items[0])
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public release detail with unstable password state: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected visible detail for %s", releaseID)
	}
	if detail.Release.PasswordState != "" {
		t.Fatalf("expected detail password state to be suppressed, got %+v", detail.Release)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set GONZB_TEST_PG_DSN to run pgindex integration tests without touching the dev database")
	}

	store, err := NewStore(dsn)
	if err != nil {
		t.Skipf("pgindex integration store unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close test store: %v", err)
		}
	})
	return store
}

func cleanupTestStage(t *testing.T, store *Store, stageName string) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM indexer_stage_state WHERE stage_name = $1`, stageName); err != nil {
		t.Fatalf("delete stage state for %s: %v", stageName, err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM indexer_stage_runs WHERE stage_name = $1`, stageName); err != nil {
		t.Fatalf("delete stage runs for %s: %v", stageName, err)
	}
}

func findStageState(t *testing.T, store *Store, stageName string) IndexerStageState {
	t.Helper()
	states, err := store.ListIndexerStageStates(context.Background())
	if err != nil {
		t.Fatalf("list stage states: %v", err)
	}
	for _, state := range states {
		if state.StageName == stageName {
			return state
		}
	}
	t.Fatalf("stage state %q not found", stageName)
	return IndexerStageState{}
}

func uniqueTestStageName(suffix string) string {
	return fmt.Sprintf("test_stage_%s_%d", suffix, time.Now().UnixNano())
}

func seedVisibilityTestRelease(t *testing.T, store *Store, token string, mutate func(*ReleaseRecord)) (string, ReleaseRecord) {
	t.Helper()

	now := time.Now().UTC()
	record := ReleaseRecord{
		ProviderID:              1,
		ReleaseKey:              fmt.Sprintf("public-release-key-%s", token),
		GroupName:               fmt.Sprintf("alt.binaries.public.%s", token),
		Title:                   fmt.Sprintf("Public Visible %s 2026 1080p BluRay x265-GRP", token),
		SourceTitle:             fmt.Sprintf("Public.Visible.%s.2026.1080p.BluRay.x265-GRP", token),
		SearchTitle:             strings.ToLower(fmt.Sprintf("public visible %s 2026 1080p bluray x265 grp", token)),
		Category:                "usenet",
		Classification:          "video",
		Poster:                  "poster-public",
		SizeBytes:               1_500_000_000,
		PostedAt:                &now,
		FileCount:               2,
		ExpectedFileCount:       2,
		ParFileCount:            1,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		PasswordState:           "unknown",
		HasPAR2:                 true,
		HasNFO:                  true,
		ArchiveCount:            1,
		VideoCount:              1,
		AudioCount:              1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		MetadataUpdatedAt:       &now,
	}
	if mutate != nil {
		mutate(&record)
	}

	releaseID, err := store.UpsertRelease(context.Background(), record)
	if err != nil {
		t.Fatalf("seed visibility test release: %v", err)
	}

	return releaseID, record
}

func seedTestRelease(t *testing.T, store *Store, suffix string) string {
	t.Helper()

	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(context.Background(), ReleaseRecord{
		ProviderID:              1,
		ReleaseKey:              fmt.Sprintf("seed-release-key-%s-%d", suffix, now.UnixNano()),
		GroupName:               fmt.Sprintf("seed.group.%s.%d", suffix, now.UnixNano()),
		Title:                   "Seed Release 2026 1080p BluRay x265-GRP",
		SourceTitle:             "Seed.Release.2026.1080p.BluRay.x265-GRP",
		DeobfuscatedTitle:       "Seed.Release.2026.1080p.BluRay.x265-GRP",
		TitleSource:             "source",
		TitleConfidence:         0.95,
		SearchTitle:             "seed release 2026 1080p bluray x265 grp",
		Category:                "usenet",
		Classification:          "video",
		Poster:                  "poster-a",
		SizeBytes:               1_500_000_000,
		PostedAt:                &now,
		FileCount:               1,
		ExpectedFileCount:       1,
		ParFileCount:            0,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
		PasswordState:           "unknown",
		ArchiveCount:            1,
		VideoCount:              1,
		AudioCount:              1,
		AvailabilityScore:       100,
		AvailabilityTier:        "excellent",
		MediaQualityScore:       90,
		MediaQualityTier:        "premium",
		IdentityConfidenceScore: 90,
		PrimaryResolution:       "1080p",
		PrimaryVideoCodec:       "x265",
		PrimaryAudioCodec:       "aac",
		MetadataUpdatedAt:       &now,
	})
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	return releaseID
}

func boolPtr(v bool) *bool { return &v }
