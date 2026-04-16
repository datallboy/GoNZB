package pgindex

import (
	"context"
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

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  "test-release-source",
		ReleaseFamilyKey:  "test-release-family",
		FileFamilyKey:     "test-file-family",
		FamilyKind:        "archive_stem",
		BaseStem:          "test",
		PostingBucket:     "20260416-1",
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
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/gonzb?sslmode=disable"
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
