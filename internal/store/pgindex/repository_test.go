package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
)

func ptrTime(v time.Time) *time.Time { return &v }

func ensureTestPoster(t *testing.T, store *Store, ctx context.Context, posterName string) (int64, error) {
	t.Helper()
	posterName = strings.TrimSpace(posterName)
	if posterName == "" {
		return 0, nil
	}

	var id int64
	err := store.DB().QueryRowContext(ctx, `
		WITH inserted AS (
			INSERT INTO posters (poster_name)
			VALUES ($1)
			ON CONFLICT (poster_name) DO NOTHING
			RETURNING id
		)
		SELECT id
		FROM inserted
		UNION ALL
		SELECT p.id
		FROM posters p
		WHERE p.poster_name = $1
		LIMIT 1`,
		posterName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure test poster %q: %w", posterName, err)
	}
	return id, nil
}

func TestInspectCandidateFilterPasswordRequiresEncryptedRelease(t *testing.T) {
	filter, err := inspectCandidateFilter("inspect_password", false)
	if err != nil {
		t.Fatalf("inspectCandidateFilter() error = %v", err)
	}

	if !strings.Contains(filter, "r.encrypted = TRUE") {
		t.Fatalf("expected inspect_password filter to require encrypted releases, got %q", filter)
	}
}

func TestInspectCandidateFilterArchiveExpectedFileCountGateIsConfigurable(t *testing.T) {
	filter, err := inspectCandidateFilter("inspect_archive", false)
	if err != nil {
		t.Fatalf("inspectCandidateFilter(false) error = %v", err)
	}
	if strings.Contains(filter, "r.file_count >= r.expected_file_count") {
		t.Fatalf("expected inspect_archive filter without expected-file gate to omit file-count match, got %q", filter)
	}

	filter, err = inspectCandidateFilter("inspect_archive", true)
	if err != nil {
		t.Fatalf("inspectCandidateFilter(true) error = %v", err)
	}
	if !strings.Contains(filter, "r.file_count >= r.expected_file_count") {
		t.Fatalf("expected inspect_archive filter with expected-file gate to require file-count match, got %q", filter)
	}
}

func TestReleaseDetailDiagnosticsTreatsArchivePayloadAsCompleteWhenNonPARFilesMeetExpectedArchiveCount(t *testing.T) {
	release := IndexerReleaseSummary{
		FileCount:                11,
		ParFileCount:             6,
		ExpectedArchiveFileCount: 5,
		ExpectedFileCount:        12,
		HasPAR2:                  true,
	}
	files := []IndexerReleaseFileSummary{{FileName: "example.vol00+01.par2", IsPars: true}}

	diag := buildReleaseDetailDiagnostics(release, files)
	if !diag.PayloadComplete {
		t.Fatalf("expected payload to be complete when non-PAR file count meets expected archive count, got %+v", diag)
	}
	if diag.ExpectedFileCountComplete {
		t.Fatalf("expected total expected file count to remain incomplete, got %+v", diag)
	}
	if !strings.Contains(diag.ReadinessNote, "sidecar-only") {
		t.Fatalf("expected sidecar-only readiness note, got %q", diag.ReadinessNote)
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

func TestApplyReleaseInspectionUpdateReturnsReleaseNotFoundForMissingRelease(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	hasNFO := true

	err := store.ApplyReleaseInspectionUpdate(ctx, ReleaseInspectionUpdate{
		ReleaseID: "missing-release-id",
		HasNFO:    &hasNFO,
	})
	if err == nil {
		t.Fatal("expected missing release error")
	}
	if !IsReleaseNotFound(err) {
		t.Fatalf("expected ErrReleaseNotFound, got %v", err)
	}
}

func TestApplyReleaseEnrichmentUpdateRefreshesTVCategory(t *testing.T) {
	store := openTestStore(t)
	releaseID := seedTestRelease(t, store, "enrich_tv_category")
	ctx := context.Background()

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE releases
		SET title = 'Example Show S01E01 1080p x265',
		    source_title = 'Example.Show.S01E01.1080p.x265',
		    deobfuscated_title = 'Example.Show.S01E01.1080p.x265',
		    search_title = 'example show s01e01 1080p x265',
		    classification = 'video',
		    category_id = $2,
		    category = $3
		WHERE release_id = $1`,
		releaseID,
		newsnab.OtherMisc,
		newsnab.DisplayName(newsnab.OtherMisc),
	); err != nil {
		t.Fatalf("prepare release for tv enrichment category refresh: %v", err)
	}

	now := time.Now().UTC()
	if err := store.ApplyReleaseEnrichmentUpdate(ctx, ReleaseEnrichmentUpdate{
		ReleaseID:         releaseID,
		ExternalMediaType: "tv",
		TVDBID:            42,
		SeasonNumber:      1,
		EpisodeNumber:     1,
		MetadataUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("apply release enrichment update: %v", err)
	}

	release, err := store.GetIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get release detail: %v", err)
	}
	if release.Release.CategoryID != newsnab.TVHD {
		t.Fatalf("expected TVHD category after enrichment, got %+v", release.Release)
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

func TestReplaceBinaryInspectionPersistenceHelpersBatchAndClearRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.replace.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_inspection_artifacts WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup inspection artifacts: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_archive_entries WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup archive entries: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_media_streams WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup media streams: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_text_evidence WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup text evidence: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_par2_sets WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID); err != nil {
			t.Fatalf("cleanup par2 sets: %v", err)
		}
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
		SourceReleaseKey:  "inspect-replace-source",
		ReleaseFamilyKey:  "inspect-replace-family",
		BinaryKey:         fmt.Sprintf("inspect-replace-%d", time.Now().UnixNano()),
		BinaryName:        "inspect.replace.sample.bin",
		FileName:          "inspect.replace.sample.bin",
		ExpectedFileCount: 1,
		TotalParts:        2,
		MatchConfidence:   0.99,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.ReplaceBinaryInspectionArtifacts(ctx, "inspect_archive", binaryID, []BinaryInspectionArtifactRecord{
		{ReleaseID: "", ArtifactRole: "primary", ArtifactName: "sample.7z", ArtifactPath: "/tmp/sample.7z", BytesTotal: 100, MIMEType: "application/x-7z-compressed", Signature: "7z", SourceKind: "archive", Metadata: map[string]any{"kind": "archive"}},
		{ReleaseID: "", ArtifactRole: "secondary", ArtifactName: "sample.nfo", ArtifactPath: "/tmp/sample.nfo", BytesTotal: 50, MIMEType: "text/plain", Signature: "nfo", SourceKind: "sidecar", Metadata: map[string]any{"kind": "text"}},
	}); err != nil {
		t.Fatalf("replace inspection artifacts: %v", err)
	}
	if err := store.ReplaceBinaryArchiveEntries(ctx, binaryID, []BinaryArchiveEntryRecord{
		{EntryName: "video.mkv", IsDir: false, UncompressedBytes: 1000, CompressedBytes: 800, Encrypted: false, Comment: "main", MediaType: "video", Signature: "mkv", Metadata: map[string]any{"track": 1}},
		{EntryName: "proof/", IsDir: true, UncompressedBytes: 0, CompressedBytes: 0, Encrypted: false, Comment: "", MediaType: "", Signature: "", Metadata: map[string]any{"dir": true}},
	}); err != nil {
		t.Fatalf("replace archive entries: %v", err)
	}
	if err := store.ReplaceBinaryMediaStreams(ctx, binaryID, []BinaryMediaStreamRecord{
		{StreamIndex: 0, StreamType: "video", CodecName: "h264", CodecLongName: "H.264", Profile: "High", Width: 1920, Height: 1080, Channels: 0, Language: "eng", DurationSeconds: 10.5, BitRate: 1000, DefaultDisposition: true, ForcedDisposition: false, Metadata: map[string]any{"title": "video"}},
		{StreamIndex: 1, StreamType: "audio", CodecName: "aac", CodecLongName: "AAC", Profile: "LC", Width: 0, Height: 0, Channels: 2, Language: "eng", DurationSeconds: 10.5, BitRate: 192000, DefaultDisposition: true, ForcedDisposition: false, Metadata: map[string]any{"title": "audio"}},
	}); err != nil {
		t.Fatalf("replace media streams: %v", err)
	}
	if err := store.ReplaceBinaryTextEvidence(ctx, "inspect_nfo", binaryID, []BinaryTextEvidenceRecord{
		{EvidenceKind: "nfo_text", TextValue: "Example Release", Tokens: []string{"Example", "Release", "Example"}, Metadata: map[string]any{"source": "nfo"}},
		{EvidenceKind: "tag", TextValue: "x264", Tokens: []string{"x264"}, Metadata: map[string]any{"source": "scan"}},
	}); err != nil {
		t.Fatalf("replace text evidence: %v", err)
	}
	if err := store.ReplaceBinaryPAR2Sets(ctx, binaryID, []BinaryPAR2SetRecord{
		{SetName: "sample.par2", BaseName: "sample", IsVolume: false, VolumeNumber: 0, RecoveryBlocks: 0, SignatureOK: true, Metadata: map[string]any{"kind": "index"}},
		{SetName: "sample.vol00+01.par2", BaseName: "sample", IsVolume: true, VolumeNumber: 1, RecoveryBlocks: 1, SignatureOK: true, Metadata: map[string]any{"kind": "volume"}},
	}); err != nil {
		t.Fatalf("replace par2 sets: %v", err)
	}

	assertTableCount := func(table string, want int) {
		t.Helper()
		var got int
		if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE binary_id = $1`, binaryID).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("expected %d rows in %s for binary %d, got %d", want, table, binaryID, got)
		}
	}

	assertTableCount("binary_inspection_artifacts", 2)
	assertTableCount("binary_archive_entries", 2)
	assertTableCount("binary_media_streams", 2)
	assertTableCount("binary_text_evidence", 2)
	assertTableCount("binary_par2_sets", 2)

	var tokensJSON []byte
	if err := store.DB().QueryRowContext(ctx, `
		SELECT tokens_json
		FROM binary_text_evidence
		WHERE binary_id = $1 AND stage_name = 'inspect_nfo' AND evidence_kind = 'nfo_text'`,
		binaryID,
	).Scan(&tokensJSON); err != nil {
		t.Fatalf("query text evidence tokens: %v", err)
	}
	if got := decodeJSONStringSlice(tokensJSON); len(got) != 2 || got[0] != "Example" || got[1] != "Release" {
		t.Fatalf("expected deduped token slice, got %#v", got)
	}

	if err := store.ReplaceBinaryInspectionArtifacts(ctx, "inspect_archive", binaryID, nil); err != nil {
		t.Fatalf("clear inspection artifacts: %v", err)
	}
	if err := store.ReplaceBinaryArchiveEntries(ctx, binaryID, nil); err != nil {
		t.Fatalf("clear archive entries: %v", err)
	}
	if err := store.ReplaceBinaryMediaStreams(ctx, binaryID, nil); err != nil {
		t.Fatalf("clear media streams: %v", err)
	}
	if err := store.ReplaceBinaryTextEvidence(ctx, "inspect_nfo", binaryID, nil); err != nil {
		t.Fatalf("clear text evidence: %v", err)
	}
	if err := store.ReplaceBinaryPAR2Sets(ctx, binaryID, nil); err != nil {
		t.Fatalf("clear par2 sets: %v", err)
	}

	assertTableCount("binary_inspection_artifacts", 0)
	assertTableCount("binary_archive_entries", 0)
	assertTableCount("binary_media_streams", 0)
	assertTableCount("binary_text_evidence", 0)
	assertTableCount("binary_par2_sets", 0)
}

func TestReplaceBinaryInspectionPersistenceHelpersDropStaleReleaseIDs(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.stale.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	releaseID := seedTestRelease(t, store, "inspect-stale-artifacts")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_inspection_artifacts WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID)
		_, _ = store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_archive_entries WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID)
		_, _ = store.DB().ExecContext(cleanupCtx, `DELETE FROM binary_media_streams WHERE binary_id IN (SELECT id FROM binaries WHERE newsgroup_id = $1)`, newsgroupID)
		_, _ = store.DB().ExecContext(cleanupCtx, `DELETE FROM binaries WHERE newsgroup_id = $1`, newsgroupID)
		_, _ = store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID)
		_, _ = store.DB().ExecContext(cleanupCtx, `DELETE FROM releases WHERE release_id = $1`, releaseID)
	})

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "inspect-stale-source",
		ReleaseFamilyKey:  "inspect-stale-family",
		BinaryKey:         fmt.Sprintf("inspect-stale-%d", time.Now().UnixNano()),
		BinaryName:        "inspect.stale.sample.bin",
		FileName:          "inspect.stale.sample.bin",
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.99,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	staleReleaseID := "missing-release-id"
	if err := store.ReplaceBinaryInspectionArtifacts(ctx, "inspect_archive", binaryID, []BinaryInspectionArtifactRecord{
		{ReleaseID: staleReleaseID, ArtifactRole: "primary", ArtifactName: "sample.7z", SourceKind: "archive"},
		{ReleaseID: releaseID, ArtifactRole: "sidecar", ArtifactName: "sample.nfo", SourceKind: "archive"},
	}); err != nil {
		t.Fatalf("replace inspection artifacts with stale release id: %v", err)
	}
	if err := store.ReplaceBinaryArchiveEntries(ctx, binaryID, []BinaryArchiveEntryRecord{
		{ReleaseID: staleReleaseID, EntryName: "video.mkv", MediaType: "video"},
	}); err != nil {
		t.Fatalf("replace archive entries with stale release id: %v", err)
	}
	if err := store.ReplaceBinaryMediaStreams(ctx, binaryID, []BinaryMediaStreamRecord{
		{ReleaseID: staleReleaseID, StreamIndex: 0, StreamType: "video", CodecName: "h264"},
	}); err != nil {
		t.Fatalf("replace media streams with stale release id: %v", err)
	}

	var staleArtifacts int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_inspection_artifacts
		WHERE binary_id = $1 AND release_id IS NULL`, binaryID,
	).Scan(&staleArtifacts); err != nil {
		t.Fatalf("count null artifact release ids: %v", err)
	}
	if staleArtifacts != 1 {
		t.Fatalf("expected one stale artifact release id to be stored as NULL, got %d", staleArtifacts)
	}

	var validArtifacts int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_inspection_artifacts
		WHERE binary_id = $1 AND release_id = $2`, binaryID, releaseID,
	).Scan(&validArtifacts); err != nil {
		t.Fatalf("count retained artifact release ids: %v", err)
	}
	if validArtifacts != 1 {
		t.Fatalf("expected valid artifact release id to be retained, got %d", validArtifacts)
	}

	for _, table := range []string{"binary_archive_entries", "binary_media_streams"} {
		var nullRows int
		if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE binary_id = $1 AND release_id IS NULL`, binaryID).Scan(&nullRows); err != nil {
			t.Fatalf("count null release ids in %s: %v", table, err)
		}
		if nullRows != 1 {
			t.Fatalf("expected stale release id in %s to be stored as NULL, got %d", table, nullRows)
		}
	}
}

func TestEnrichmentPersistenceHelpersBatchAndPreserveSemantics(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	releaseID := seedTestRelease(t, store, "enrichment-batch")
	normalizedA := normalizePredbTitle("Scene.Release.2026-GRP")
	normalizedB := normalizePredbTitle("Another.Release.2026-GRP")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM release_tmdb_matches WHERE release_id = $1`, releaseID); err != nil {
			t.Fatalf("cleanup tmdb matches: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM release_tvdb_matches WHERE release_id = $1`, releaseID); err != nil {
			t.Fatalf("cleanup tvdb matches: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM release_predb_matches WHERE release_id = $1`, releaseID); err != nil {
			t.Fatalf("cleanup predb matches: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM predb_entries WHERE normalized_title IN ($1,$2)`, normalizedA, normalizedB); err != nil {
			t.Fatalf("cleanup predb entries: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM releases WHERE release_id = $1`, releaseID); err != nil {
			t.Fatalf("cleanup release: %v", err)
		}
	})

	if err := store.ReplaceReleaseTMDBMatches(ctx, releaseID, []ReleaseTMDBMatchRecord{
		{TMDBID: 101, MediaType: "movie", Title: "Example Movie", OriginalTitle: "Original Movie", Year: 2026, Confidence: 0.91, Chosen: true, Payload: map[string]any{"provider": "tmdb"}},
		{TMDBID: 102, MediaType: "movie", Title: "Backup Movie", OriginalTitle: "Backup Original", Year: 2025, Confidence: 0.55, Chosen: false, Payload: map[string]any{"provider": "tmdb"}},
	}); err != nil {
		t.Fatalf("replace tmdb matches: %v", err)
	}

	if err := store.ReplaceReleaseTVDBMatches(ctx, releaseID, []ReleaseTVDBMatchRecord{
		{TVDBID: 201, MediaType: "tv", Title: "Example Show", OriginalTitle: "Example Show", Year: 2026, Confidence: 0.88, Chosen: true, Payload: map[string]any{"provider": "tvdb"}},
		{TVDBID: 202, MediaType: "tv", Title: "Backup Show", OriginalTitle: "Backup Show", Year: 2024, Confidence: 0.42, Chosen: false, Payload: map[string]any{"provider": "tvdb"}},
	}); err != nil {
		t.Fatalf("replace tvdb matches: %v", err)
	}

	if err := store.UpsertPredbEntries(ctx, []PredbEntryRecord{
		{Title: "Scene.Release.2026-GRP", Category: "TV", Source: "predb", ExternalID: 5001, Team: "GRP", Genre: "Drama", URL: "https://predb.example/a", SizeKB: 1024, FileCount: 12, Payload: map[string]any{"source": "initial"}},
		{NormalizedTitle: normalizedA, Title: "Scene.Release.2026-GRP", Category: "", Source: "", ExternalID: 0, Team: "", Genre: "", URL: "", SizeKB: 0, FileCount: 0, Payload: map[string]any{}},
	}); err != nil {
		t.Fatalf("upsert predb entries: %v", err)
	}

	if err := store.ReplaceReleasePredbMatches(ctx, releaseID, []ReleasePredbMatchRecord{
		{Title: "Scene.Release.2026-GRP", Category: "TV", Source: "predb", ExternalID: 5001, Team: "GRP", Genre: "Drama", URL: "https://predb.example/a", SizeKB: 1024, FileCount: 12, Confidence: 0.77, Chosen: true, Payload: map[string]any{"kind": "scene"}},
		{NormalizedTitle: normalizedB, Title: "Another.Release.2026-GRP", Category: "Movies", Source: "predb", ExternalID: 5002, Team: "GRP2", Genre: "Action", URL: "https://predb.example/b", SizeKB: 2048, FileCount: 20, Confidence: 0.44, Chosen: false, Payload: map[string]any{"kind": "backup"}},
	}); err != nil {
		t.Fatalf("replace predb matches: %v", err)
	}

	assertReleaseCount := func(table string, want int) {
		t.Helper()
		var got int
		if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE release_id = $1`, releaseID).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("expected %d rows in %s for release %s, got %d", want, table, releaseID, got)
		}
	}

	assertReleaseCount("release_tmdb_matches", 2)
	assertReleaseCount("release_tvdb_matches", 2)
	assertReleaseCount("release_predb_matches", 2)

	var tmdbChosen bool
	var tmdbPayload []byte
	if err := store.DB().QueryRowContext(ctx, `
		SELECT chosen, payload_json
		FROM release_tmdb_matches
		WHERE release_id = $1 AND tmdb_id = 101`, releaseID,
	).Scan(&tmdbChosen, &tmdbPayload); err != nil {
		t.Fatalf("query tmdb match: %v", err)
	}
	var tmdbPayloadMap map[string]any
	if err := json.Unmarshal(tmdbPayload, &tmdbPayloadMap); err != nil {
		t.Fatalf("decode tmdb payload: %v", err)
	}
	if !tmdbChosen || tmdbPayloadMap["provider"] != "tmdb" {
		t.Fatalf("expected chosen tmdb payload to be preserved, got chosen=%v payload=%s", tmdbChosen, string(tmdbPayload))
	}

	var category string
	var source string
	var fileCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT category, source, file_count
		FROM predb_entries
		WHERE normalized_title = $1`, normalizedA,
	).Scan(&category, &source, &fileCount); err != nil {
		t.Fatalf("query normalized predb entry: %v", err)
	}
	if category != "TV" || source != "predb" || fileCount != 12 {
		t.Fatalf("expected predb fallback fields to be preserved, got category=%q source=%q file_count=%d", category, source, fileCount)
	}

	if err := store.ReplaceReleaseTMDBMatches(ctx, releaseID, nil); err != nil {
		t.Fatalf("clear tmdb matches: %v", err)
	}
	if err := store.ReplaceReleaseTVDBMatches(ctx, releaseID, nil); err != nil {
		t.Fatalf("clear tvdb matches: %v", err)
	}
	if err := store.ReplaceReleasePredbMatches(ctx, releaseID, nil); err != nil {
		t.Fatalf("clear predb matches: %v", err)
	}

	assertReleaseCount("release_tmdb_matches", 0)
	assertReleaseCount("release_tvdb_matches", 0)
	assertReleaseCount("release_predb_matches", 0)
}

func TestUpsertBinaryStoresCompactGroupingEvidenceInlineWhenStable(t *testing.T) {
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

	var summaryKind string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT grouping_summary_kind
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&summaryKind); err != nil {
		t.Fatalf("query grouping summary kind: %v", err)
	}
	if summaryKind != "readable_title" {
		t.Fatalf("expected scalar grouping summary kind, got %q", summaryKind)
	}

	var sideEvidence []byte
	if err := store.DB().QueryRowContext(ctx, `
		SELECT payload_json
		FROM binary_grouping_evidence
		WHERE binary_id = $1`, binaryID,
	).Scan(&sideEvidence); err != nil && err != sql.ErrNoRows {
		t.Fatalf("query side-table grouping evidence: %v", err)
	}
	if len(sideEvidence) != 0 {
		t.Fatalf("expected stable evidence to skip side-table retention, got %s", string(sideEvidence))
	}

	detail, err := store.GetIndexerBinaryDetail(ctx, binaryID)
	if err != nil {
		t.Fatalf("get indexer binary detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected binary detail for %d", binaryID)
	}
	if !strings.Contains(string(detail.GroupingEvidence), "\"readable_title\"") {
		t.Fatalf("expected binary detail grouping evidence from inline summary fallback, got %s", string(detail.GroupingEvidence))
	}
}

func TestUpsertBinarySkipsDetailedGroupingEvidenceSideTableForWeakMatches(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.binary.evidence.weak.%d", time.Now().UnixNano())
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
		ReleaseFamilyKey:  "weak family key",
		ReleaseKey:        "weak family key",
		BinaryKey:         fmt.Sprintf("binary-evidence-weak-%d", time.Now().UnixNano()),
		BinaryName:        "obfuscated.bin",
		FileName:          "obfuscated.bin",
		IdentityStrength:  "weak",
		FamilyKind:        "contextual_obfuscated",
		IsMainPayload:     true,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.70,
		MatchStatus:       "probable",
		GroupingEvidence: map[string]any{
			"summary": map[string]any{
				"kind":          "contextual_obfuscated",
				"status":        "probable",
				"fallback_used": true,
			},
			"fallback": map[string]any{
				"used": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	var summaryKind, summaryStatus string
	var fallbackUsed bool
	if err := store.DB().QueryRowContext(ctx, `
		SELECT grouping_summary_kind, grouping_summary_status, grouping_summary_fallback_used
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&summaryKind, &summaryStatus, &fallbackUsed); err != nil {
		t.Fatalf("query scalar grouping summary: %v", err)
	}
	if summaryKind != "contextual_obfuscated" || summaryStatus != "probable" || !fallbackUsed {
		t.Fatalf("expected scalar fallback summary, got kind=%q status=%q fallback=%v", summaryKind, summaryStatus, fallbackUsed)
	}

	var sideEvidence []byte
	if err := store.DB().QueryRowContext(ctx, `
		SELECT payload_json
		FROM binary_grouping_evidence
		WHERE binary_id = $1`, binaryID,
	).Scan(&sideEvidence); err != nil && err != sql.ErrNoRows {
		t.Fatalf("query side-table grouping evidence: %v", err)
	}
	if len(sideEvidence) != 0 {
		t.Fatalf("expected weak detailed evidence to skip side-table retention, got %s", string(sideEvidence))
	}

	detail, err := store.GetIndexerBinaryDetail(ctx, binaryID)
	if err != nil {
		t.Fatalf("get indexer binary detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected binary detail for %d", binaryID)
	}
	if !strings.Contains(string(detail.GroupingEvidence), "\"fallback_used\":true") &&
		!strings.Contains(string(detail.GroupingEvidence), "\"fallback_used\": true") {
		t.Fatalf("expected binary detail grouping evidence from inline summary, got %s", string(detail.GroupingEvidence))
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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
		FROM release_family_readiness_summaries s
		LEFT JOIN release_family_readiness_acks a
		  ON a.provider_id = s.provider_id
		 AND a.newsgroup_id = s.newsgroup_id
		 AND a.key_kind = s.key_kind
		 AND a.family_key = s.family_key
		WHERE s.provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'test-release-family'
		  AND s.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch')`, newsgroupID,
	).Scan(&dirtyCount); err != nil {
		t.Fatalf("query release summary queue state: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("expected refreshed binary stats to requeue release family once, got %d rows", dirtyCount)
	}
}

func TestRefreshBinaryStatsAllowsBlankSummaryKeys(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.refresh.blankkeys.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-refresh-blankkeys-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	now := time.Now().UTC()
	messageID := fmt.Sprintf("<refresh-blankkeys-%d@test>", time.Now().UnixNano())
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
		ArticleNumber: 101,
		MessageID:     messageID,
		Subject:       `Blank Summary Keys [1/1] - "blank.bin" yEnc (1/1)`,
		Poster:        "poster-refresh-blankkeys@example.com",
		DateUTC:       &now,
		Bytes:         1234,
		Lines:         10,
	}}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	var headerID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM article_headers
		WHERE newsgroup_id = $1
		  AND message_id = $2`, newsgroupID, messageID,
	).Scan(&headerID); err != nil {
		t.Fatalf("lookup inserted article header: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		PosterID:         posterID,
		SourceReleaseKey: "refresh-blankkeys-source",
		ReleaseKey:       "refresh-blankkeys-release",
		ReleaseName:      "Refresh Blank Keys",
		BinaryKey:        fmt.Sprintf("refresh-blankkeys-binary-%d", time.Now().UnixNano()),
		BinaryName:       "blank.bin",
		FileName:         "blank.bin",
		FileIndex:        1,
		TotalParts:       1,
		MatchConfidence:  0.5,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET release_family_key = '',
		    base_stem = '',
		    expected_file_count = 0,
		    expected_archive_file_count = 0
		WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("blank binary summary keys: %v", err)
	}

	if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
		BinaryID:        binaryID,
		ArticleHeaderID: headerID,
		MessageID:       fmt.Sprintf("<refresh-blankkeys-part-%d@test>", time.Now().UnixNano()),
		PartNumber:      1,
		TotalParts:      1,
		SegmentBytes:    1234,
		FileName:        "blank.bin",
	}); err != nil {
		t.Fatalf("upsert binary part: %v", err)
	}

	if err := store.RefreshBinaryStats(ctx, binaryID); err != nil {
		t.Fatalf("refresh binary stats with blank summary keys: %v", err)
	}

	var observedParts int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT observed_parts
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&observedParts); err != nil {
		t.Fatalf("query refreshed binary observed_parts: %v", err)
	}
	if observedParts != 1 {
		t.Fatalf("expected observed_parts=1, got %d", observedParts)
	}
}

func TestInsertArticleHeadersBatchDedupesDuplicateRowsLastPayloadWins(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.scrape.batch.duplicates.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM article_headers WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup article headers: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	now := time.Date(2026, 5, 5, 15, 0, 0, 0, time.UTC)
	messageID := fmt.Sprintf("<scrape-batch-duplicate-%d@test>", time.Now().UnixNano())
	inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 501,
			MessageID:     messageID,
			Subject:       `Duplicate Batch [1/1] - "batch.first.rar" yEnc (1/10)`,
			Poster:        "poster-batch-first@example.com",
			DateUTC:       &now,
			Bytes:         1000,
			Lines:         10,
			Xref:          "xref-first",
			RawOverview: map[string]any{
				"Bytes": int64(1000),
			},
		},
		{
			ArticleNumber: 501,
			MessageID:     messageID,
			Subject:       `Duplicate Batch [1/1] - "batch.second.rar" yEnc (2/10)`,
			Poster:        "poster-batch-second@example.com",
			DateUTC:       &now,
			Bytes:         2000,
			Lines:         20,
			Xref:          "xref-second",
			RawOverview: map[string]any{
				"Bytes": int64(2000),
			},
		},
	})
	if err != nil {
		t.Fatalf("insert duplicate article headers batch: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected 2 processed headers, got %d", inserted)
	}

	var headerCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_headers
		WHERE newsgroup_id = $1`, newsgroupID,
	).Scan(&headerCount); err != nil {
		t.Fatalf("count article headers: %v", err)
	}
	if headerCount != 1 {
		t.Fatalf("expected 1 stored article header after duplicate batch, got %d", headerCount)
	}

	var (
		subject         string
		posterText      string
		subjectFileName string
		yencPartNumber  int
		xref            string
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			p.subject,
			p.poster,
			p.subject_file_name,
			p.yenc_part_number,
			p.xref
		FROM article_header_ingest_payloads p
		JOIN article_headers ah ON ah.id = p.article_header_id
		WHERE ah.newsgroup_id = $1`, newsgroupID,
	).Scan(&subject, &posterText, &subjectFileName, &yencPartNumber, &xref); err != nil {
		t.Fatalf("query duplicate batch payload: %v", err)
	}

	if subject != `Duplicate Batch [1/1] - "batch.second.rar" yEnc (2/10)` {
		t.Fatalf("expected last duplicate subject to win, got %q", subject)
	}
	if posterText != "" {
		t.Fatalf("expected poster text normalized away after duplicate batch, got %q", posterText)
	}
	if subjectFileName != "batch.second.rar" || yencPartNumber != 2 {
		t.Fatalf("expected last duplicate parsed metadata to win, got name=%q part=%d", subjectFileName, yencPartNumber)
	}
	if xref != "xref-second" {
		t.Fatalf("expected last duplicate xref to win, got %q", xref)
	}
}

func TestInsertArticleHeadersStripsNULBytesFromTextFields(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.scrape.nul.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM article_headers WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup article headers: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	now := time.Date(2026, 6, 11, 10, 30, 0, 0, time.UTC)
	inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
		ArticleNumber: 7001,
		MessageID:     "<nul-byte-test@test>\x00",
		Subject:       "Bad\x00Subject [1/1] - \"nul.rar\" yEnc (1/1)",
		Poster:        "bad\x00poster@example.com",
		DateUTC:       &now,
		Bytes:         1234,
		Lines:         12,
		Xref:          "xref\x00value",
	}})
	if err != nil {
		t.Fatalf("insert article headers with nul bytes: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected 1 processed header, got %d", inserted)
	}

	var (
		messageID string
		subject   string
		poster    string
		xref      string
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			ah.message_id,
			p.subject,
			COALESCE(po.poster_name, p.poster, ''),
			p.xref
		FROM article_headers ah
		JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
		LEFT JOIN posters po ON po.id = p.poster_id
		WHERE ah.newsgroup_id = $1`, newsgroupID,
	).Scan(&messageID, &subject, &poster, &xref); err != nil {
		t.Fatalf("query sanitized header payload: %v", err)
	}

	if strings.ContainsRune(messageID, '\x00') || strings.ContainsRune(subject, '\x00') || strings.ContainsRune(poster, '\x00') || strings.ContainsRune(xref, '\x00') {
		t.Fatalf("expected nul bytes stripped, got message_id=%q subject=%q poster=%q xref=%q", messageID, subject, poster, xref)
	}
}

func TestInsertArticleHeadersBatchResolvesExistingRowsWithoutPerRowProbe(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.scrape.batch.resolve.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM article_headers WHERE newsgroup_id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup article headers: %v", err)
		}
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	now := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	messageID := fmt.Sprintf("<scrape-existing-resolve-%d@test>", time.Now().UnixNano())
	firstBatch := []ArticleHeader{{
		ArticleNumber: 777,
		MessageID:     messageID,
		Subject:       `Existing Resolve [1/1] - "resolve.part1.rar" yEnc (1/4)`,
		Poster:        "poster-existing@example.com",
		DateUTC:       &now,
		Bytes:         1000,
		Lines:         10,
		Xref:          "xref-existing",
	}}
	if inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, firstBatch); err != nil {
		t.Fatalf("insert first batch: %v", err)
	} else if inserted != 1 {
		t.Fatalf("expected first batch count 1, got %d", inserted)
	}

	secondBatch := []ArticleHeader{
		{
			ArticleNumber: 777,
			MessageID:     messageID,
			Subject:       `Existing Resolve [1/1] - "resolve.part1.rar" yEnc (2/4)`,
			Poster:        "poster-existing-second@example.com",
			DateUTC:       &now,
			Bytes:         1200,
			Lines:         12,
			Xref:          "xref-existing-second",
		},
		{
			ArticleNumber: 778,
			MessageID:     fmt.Sprintf("<scrape-existing-resolve-fresh-%d@test>", time.Now().UnixNano()),
			Subject:       `Existing Resolve [2/2] - "resolve.part2.rar" yEnc (3/4)`,
			Poster:        "poster-fresh@example.com",
			DateUTC:       &now,
			Bytes:         1300,
			Lines:         13,
			Xref:          "xref-fresh",
		},
	}
	if inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, secondBatch); err != nil {
		t.Fatalf("insert second batch: %v", err)
	} else if inserted != 2 {
		t.Fatalf("expected second batch count 2, got %d", inserted)
	}

	var count int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_headers
		WHERE newsgroup_id = $1`, newsgroupID,
	).Scan(&count); err != nil {
		t.Fatalf("count article headers: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 stored article headers, got %d", count)
	}
}

func TestUpsertBinaryDeferredSummaryRefreshMarksFamilyDirtyWithoutInlineRecompute(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.defer.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	deferredCtx := WithDeferredReleaseFamilySummaryRefresh(ctx)
	if _, err := store.UpsertBinary(deferredCtx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		ReleaseFamilyKey:  "deferred-release-family",
		SourceReleaseKey:  "deferred-source",
		ReleaseKey:        "deferred-release-family",
		ReleaseName:       "Deferred Release Family",
		BinaryKey:         "deferred-binary",
		BinaryName:        "deferred.part01.rar",
		FileName:          "deferred.part01.rar",
		ExpectedFileCount: 4,
		TotalParts:        12,
		MatchConfidence:   0.99,
		MatchStatus:       "strong",
	}); err != nil {
		t.Fatalf("upsert binary with deferred summary refresh: %v", err)
	}

	var (
		summaryCount int
		queueCount   int
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_readiness_summaries
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'deferred-release-family'`, newsgroupID,
	).Scan(&summaryCount); err != nil {
		t.Fatalf("count deferred summary rows: %v", err)
	}
	if summaryCount != 0 {
		t.Fatalf("expected deferred summary refresh to avoid inline summary row writes, got %d rows", summaryCount)
	}
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_summary_refresh_queue
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'deferred-release-family'`, newsgroupID,
	).Scan(&queueCount); err != nil {
		t.Fatalf("count deferred summary queue rows: %v", err)
	}
	if queueCount != 1 {
		t.Fatalf("expected deferred summary refresh to enqueue one family key, got %d", queueCount)
	}
}

func TestUpsertBinaryDeferredSummaryRefreshMarksOldAndNewFamiliesDirtyOnIdentityChange(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.change.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	record := BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "original-source",
		ReleaseFamilyKey:  "original-family",
		ReleaseKey:        "original-family",
		ReleaseName:       "Original Family",
		BinaryKey:         "moving-binary",
		BinaryName:        "moving.part01.rar",
		FileName:          "moving.part01.rar",
		BaseStem:          "original-stem",
		ExpectedFileCount: 2,
		TotalParts:        10,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	}
	if _, err := store.UpsertBinary(ctx, record); err != nil {
		t.Fatalf("seed original binary: %v", err)
	}

	cleanAt := time.Now().UTC().Add(-time.Hour)
	for _, key := range []struct {
		keyKind   string
		familyKey string
	}{
		{keyKind: "release_family", familyKey: "original-family"},
		{keyKind: "release_family", familyKey: "renamed-family"},
		{keyKind: "base_stem", familyKey: "original-stem"},
		{keyKind: "base_stem", familyKey: "renamed-stem"},
	} {
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO release_family_readiness_summaries (
				provider_id, newsgroup_id, key_kind, family_key,
				source_release_key, release_key, release_name,
				binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
				expected_file_count, expected_archive_file_count, has_expected_file_count, has_expected_archive_file_count,
				total_bytes, earliest_posted_at, dominant_family_kind, dominant_file_name, dominant_match_confidence,
				readiness_bucket, expected_file_coverage_pct, archive_file_coverage_pct, updated_at, processed_at
			)
			VALUES (
				1, $1, $2, $3,
				'', '', '',
				0, 0, 0, 0,
				0, 0, false, false,
				0, NULL, '', '', 0,
				$4, 0, 0, $5, $5
			)
			ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
			SET updated_at = EXCLUDED.updated_at,
			    processed_at = EXCLUDED.processed_at`,
			newsgroupID, key.keyKind, key.familyKey, releaseReadinessStaleCleanupOnly, cleanAt,
		); err != nil {
			t.Fatalf("seed readiness row %s/%s: %v", key.keyKind, key.familyKey, err)
		}
	}

	changed := record
	changed.SourceReleaseKey = "renamed-source"
	changed.ReleaseFamilyKey = "renamed-family"
	changed.ReleaseKey = "renamed-family"
	changed.ReleaseName = "Renamed Family"
	changed.BaseStem = "renamed-stem"
	changed.ExpectedFileCount = 3

	deferredCtx := WithDeferredReleaseFamilySummaryRefresh(ctx)
	if _, err := store.UpsertBinary(deferredCtx, changed); err != nil {
		t.Fatalf("upsert binary with changed deferred identity: %v", err)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT key_kind, family_key
		FROM release_family_summary_refresh_queue
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND (
		  	(key_kind = 'release_family' AND family_key IN ('original-family', 'renamed-family'))
		  	OR (key_kind = 'base_stem' AND family_key IN ('original-stem', 'renamed-stem'))
		  )`, newsgroupID,
	)
	if err != nil {
		t.Fatalf("query changed refresh queue rows: %v", err)
	}
	defer rows.Close()

	got := make(map[string]bool, 4)
	for rows.Next() {
		var keyKind, familyKey string
		if err := rows.Scan(&keyKind, &familyKey); err != nil {
			t.Fatalf("scan changed refresh queue row: %v", err)
		}
		got[keyKind+":"+familyKey] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate changed refresh queue rows: %v", err)
	}

	for _, key := range []string{
		"release_family:original-family",
		"release_family:renamed-family",
		"base_stem:original-stem",
		"base_stem:renamed-stem",
	} {
		if !got[key] {
			t.Fatalf("expected readiness row %s to be marked dirty, got=%v", key, got)
		}
	}
}

func TestUpsertBinarySkipsUnchangedRowRewriteButUpdatesOnRealChange(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.binary.upsert.noop.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	record := BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "noop-source",
		ReleaseFamilyKey:  "noop-family",
		FileSetKey:        "noop-fileset",
		FileFamilyKey:     "noop-family::file",
		IdentityStrength:  "strong",
		IdentityReason:    "test",
		SubjectSetToken:   "noop-token",
		SubjectSetKind:    "subject",
		FamilyKind:        "archive_stem",
		BaseStem:          "noop",
		IsMainPayload:     true,
		ReleaseKey:        "noop-family",
		ReleaseName:       "Noop Family",
		BinaryKey:         "noop-binary",
		BinaryName:        "noop.rar",
		FileName:          "noop.rar",
		FileIndex:         1,
		ExpectedFileCount: 2,
		TotalParts:        3,
		MatchConfidence:   0.95,
		MatchStatus:       "strong",
		GroupingEvidence: map[string]any{
			"summary": map[string]any{
				"status": "matched",
			},
		},
	}

	binaryID, err := store.UpsertBinary(ctx, record)
	if err != nil {
		t.Fatalf("initial upsert binary: %v", err)
	}

	var firstUpdatedAt time.Time
	if err := store.DB().QueryRowContext(ctx, `SELECT updated_at FROM binaries WHERE id = $1`, binaryID).Scan(&firstUpdatedAt); err != nil {
		t.Fatalf("query initial updated_at: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	sameBinaryID, err := store.UpsertBinary(ctx, record)
	if err != nil {
		t.Fatalf("noop upsert binary: %v", err)
	}
	if sameBinaryID != binaryID {
		t.Fatalf("expected same binary id %d, got %d", binaryID, sameBinaryID)
	}

	var noopUpdatedAt time.Time
	if err := store.DB().QueryRowContext(ctx, `SELECT updated_at FROM binaries WHERE id = $1`, binaryID).Scan(&noopUpdatedAt); err != nil {
		t.Fatalf("query noop updated_at: %v", err)
	}
	if !noopUpdatedAt.Equal(firstUpdatedAt) {
		t.Fatalf("expected noop upsert to preserve updated_at %s, got %s", firstUpdatedAt.UTC(), noopUpdatedAt.UTC())
	}

	time.Sleep(20 * time.Millisecond)

	changed := record
	changed.MatchConfidence = 0.99
	changed.MatchStatus = "very_strong"
	if _, err := store.UpsertBinary(ctx, changed); err != nil {
		t.Fatalf("changed upsert binary: %v", err)
	}

	var changedUpdatedAt time.Time
	var changedStatus string
	var changedConfidence float64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT updated_at, match_status, match_confidence
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(&changedUpdatedAt, &changedStatus, &changedConfidence); err != nil {
		t.Fatalf("query changed binary row: %v", err)
	}
	if !changedUpdatedAt.After(noopUpdatedAt) {
		t.Fatalf("expected changed upsert to advance updated_at beyond %s, got %s", noopUpdatedAt.UTC(), changedUpdatedAt.UTC())
	}
	if changedStatus != "very_strong" || changedConfidence != 0.99 {
		t.Fatalf("expected changed upsert to persist stronger match, got status=%q confidence=%f", changedStatus, changedConfidence)
	}
}

func TestUpsertBinaryDoesNotDowngradeIdentityWithLowerConfidence(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.binary.upsert.identity.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	record := BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "strong-source",
		ReleaseFamilyKey:  "strong-family",
		FileSetKey:        "strong-fileset",
		FileFamilyKey:     "strong-file-family",
		IdentityStrength:  "strong",
		IdentityReason:    "strong evidence",
		SubjectSetToken:   "strong-token",
		SubjectSetKind:    "subject",
		FamilyKind:        "readable_title",
		BaseStem:          "strong-stem",
		IsMainPayload:     true,
		ReleaseKey:        "strong-family",
		ReleaseName:       "Strong Family",
		BinaryKey:         "stable-identity-binary",
		BinaryName:        "strong.part01.rar",
		FileName:          "strong.part01.rar",
		FileIndex:         1,
		ExpectedFileCount: 2,
		TotalParts:        10,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	}
	binaryID, err := store.UpsertBinary(ctx, record)
	if err != nil {
		t.Fatalf("seed strong binary: %v", err)
	}

	downgrade := record
	downgrade.SourceReleaseKey = "weak-source"
	downgrade.ReleaseFamilyKey = "weak-family"
	downgrade.FileSetKey = "weak-fileset"
	downgrade.FileFamilyKey = "weak-file-family"
	downgrade.IdentityStrength = "weak"
	downgrade.IdentityReason = "weak evidence"
	downgrade.SubjectSetToken = "weak-token"
	downgrade.FamilyKind = "contextual_obfuscated"
	downgrade.BaseStem = "weak-stem"
	downgrade.ReleaseKey = "weak-family"
	downgrade.ReleaseName = "Weak Family"
	downgrade.BinaryName = "weak.bin"
	downgrade.FileName = "weak.bin"
	downgrade.FileIndex = 99
	downgrade.ExpectedFileCount = 3
	downgrade.TotalParts = 12
	downgrade.MatchConfidence = 0.50
	downgrade.MatchStatus = "probable"
	if _, err := store.UpsertBinary(ctx, downgrade); err != nil {
		t.Fatalf("upsert lower-confidence binary: %v", err)
	}

	var got struct {
		releaseFamilyKey string
		releaseName      string
		fileName         string
		fileIndex        int
		expectedCount    int
		totalParts       int
		matchConfidence  float64
		matchStatus      string
	}
	if err := store.DB().QueryRowContext(ctx, `
		SELECT release_family_key, release_name, file_name, file_index,
		       expected_file_count, total_parts, match_confidence, match_status
		FROM binaries
		WHERE id = $1`, binaryID,
	).Scan(
		&got.releaseFamilyKey,
		&got.releaseName,
		&got.fileName,
		&got.fileIndex,
		&got.expectedCount,
		&got.totalParts,
		&got.matchConfidence,
		&got.matchStatus,
	); err != nil {
		t.Fatalf("query binary after lower-confidence upsert: %v", err)
	}
	if got.releaseFamilyKey != "strong-family" || got.releaseName != "Strong Family" || got.fileName != "strong.part01.rar" || got.fileIndex != 1 {
		t.Fatalf("expected strong identity to be retained, got family=%q release=%q file=%q index=%d", got.releaseFamilyKey, got.releaseName, got.fileName, got.fileIndex)
	}
	if got.expectedCount != 3 || got.totalParts != 12 {
		t.Fatalf("expected monotonic counters to advance, got expected=%d total_parts=%d", got.expectedCount, got.totalParts)
	}
	if got.matchConfidence != 0.95 || got.matchStatus != "matched" {
		t.Fatalf("expected stronger match status to remain, got confidence=%f status=%q", got.matchConfidence, got.matchStatus)
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 1, 0, 0, 1, 1, true, 12345, NOW(), 'fragment_only', 0, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', $3, $3, $3, $3, 1, 1, 1, 0, 1, true, 12345, NOW(), 'actionable', 100, NOW() - INTERVAL '1 minute', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, incompleteFamily, completeFamily,
	); err != nil {
		t.Fatalf("seed release summary queue rows: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 2, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != completeFamily {
		t.Fatalf("expected complete family %q, got %q", completeFamily, candidates[0].ReleaseFamilyKey)
	}
	if candidates[1].ReleaseFamilyKey != incompleteFamily {
		t.Fatalf("expected fragment-only family %q second for cooldown, got %q", incompleteFamily, candidates[1].ReleaseFamilyKey)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id IN ($1, $2)`, incompleteBinaryID, completeBinaryID); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key IN ($2, $3)`, newsgroupID, incompleteFamily, completeFamily,
	); err != nil {
		t.Fatalf("cleanup release summaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestRecoveredFileSetReleaseCandidatesBridgeGroupsAndAckUnderlyingSummaries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupA := fmt.Sprintf("alt.test.recovered.groupa.%d", time.Now().UnixNano())
	groupB := fmt.Sprintf("alt.test.recovered.groupb.%d", time.Now().UnixNano())
	groupAID, err := store.EnsureNewsgroup(ctx, groupA)
	if err != nil {
		t.Fatalf("ensure group A: %v", err)
	}
	groupBID, err := store.EnsureNewsgroup(ctx, groupB)
	if err != nil {
		t.Fatalf("ensure group B: %v", err)
	}

	fileSetKey := fmt.Sprintf("recovered-file-set-%d", time.Now().UnixNano())
	familyA := fileSetKey + "-family-a"
	familyB := fileSetKey + "-family-b"
	baseA := fileSetKey + "-base-a"
	baseB := fileSetKey + "-base-b"

	makeRecoveredBinary := func(groupID int64, familyKey, baseStem, binaryKey, fileName string, fileIndex int) int64 {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       groupID,
			SourceReleaseKey:  familyKey,
			ReleaseFamilyKey:  familyKey,
			FileSetKey:        fileSetKey,
			FileFamilyKey:     fileSetKey + "::" + fileName,
			IdentityStrength:  "recovered_yenc",
			IdentityReason:    "yenc_header",
			FamilyKind:        "file_name",
			BaseStem:          baseStem,
			IsMainPayload:     true,
			ReleaseKey:        familyKey,
			ReleaseName:       familyKey,
			BinaryKey:         binaryKey,
			BinaryName:        fileName,
			FileName:          fileName,
			FileIndex:         fileIndex,
			ExpectedFileCount: 2,
			TotalParts:        10,
			MatchConfidence:   0.97,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert recovered binary %s: %v", binaryKey, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE binaries
			SET observed_parts = total_parts,
			    total_bytes = 12345,
			    posted_at = NOW() - INTERVAL '30 minutes',
			    recovered_source = 'yenc_header',
			    updated_at = NOW()
			WHERE id = $1`, binaryID,
		); err != nil {
			t.Fatalf("update recovered binary %s: %v", binaryKey, err)
		}
		return binaryID
	}

	binaryA := makeRecoveredBinary(groupAID, familyA, baseA, fileSetKey+"::a", "movie.part01.rar", 1)
	binaryB := makeRecoveredBinary(groupBID, familyB, baseB, fileSetKey+"::b", "movie.part02.rar", 2)

	keys := []releaseFamilySummaryKey{
		{ProviderID: 1, NewsgroupID: groupAID, KeyKind: "release_family", FamilyKey: familyA},
		{ProviderID: 1, NewsgroupID: groupAID, KeyKind: "base_stem", FamilyKey: strings.ToLower(baseA)},
		{ProviderID: 1, NewsgroupID: groupBID, KeyKind: "release_family", FamilyKey: familyB},
		{ProviderID: 1, NewsgroupID: groupBID, KeyKind: "base_stem", FamilyKey: strings.ToLower(baseB)},
	}
	if err := markReleaseFamiliesDirtyBatch(ctx, store.DB(), keys); err != nil {
		t.Fatalf("queue release summary refresh: %v", err)
	}
	if _, err := store.RefreshQueuedReleaseFamilySummaries(ctx, len(keys)); err != nil {
		t.Fatalf("refresh queued release summaries: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(candidates))
	}
	candidate := candidates[0]
	if candidate.KeyKind != ReleaseCandidateKeyKindRecoveredFileSet {
		t.Fatalf("expected recovered-file-set candidate, got %+v", candidate)
	}
	expectedRepresentativeGroup := groupAID
	if groupBID < expectedRepresentativeGroup {
		expectedRepresentativeGroup = groupBID
	}
	if candidate.NewsgroupID != expectedRepresentativeGroup {
		t.Fatalf("expected representative newsgroup %d, got %d", expectedRepresentativeGroup, candidate.NewsgroupID)
	}
	if candidate.ReleaseFamilyKey != fileSetKey {
		t.Fatalf("expected file set key %q, got %q", fileSetKey, candidate.ReleaseFamilyKey)
	}

	binaries, err := store.ListBinariesForReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, candidate.ReleaseFamilyKey)
	if err != nil {
		t.Fatalf("list binaries for recovered-file-set candidate: %v", err)
	}
	if len(binaries) != 2 {
		t.Fatalf("expected both groups' binaries, got %d", len(binaries))
	}

	if err := store.AckReleaseCandidate(ctx, candidate.ProviderID, candidate.NewsgroupID, candidate.KeyKind, candidate.ReleaseFamilyKey); err != nil {
		t.Fatalf("ack recovered-file-set candidate: %v", err)
	}

	var pending int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_readiness_summaries s
		LEFT JOIN release_family_readiness_acks a
		  ON a.provider_id = s.provider_id
		 AND a.newsgroup_id = s.newsgroup_id
		 AND a.key_kind = s.key_kind
		 AND a.family_key = s.family_key
			WHERE s.provider_id = 1
			  AND newsgroup_id IN ($1, $2)
			  AND family_key IN ($3, $4, $5, $6)
			  AND s.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch')`,
		groupAID, groupBID, familyA, familyB, strings.ToLower(baseA), strings.ToLower(baseB),
	).Scan(&pending); err != nil {
		t.Fatalf("count pending summaries after ack: %v", err)
	}
	if pending != 0 {
		t.Fatalf("expected recovered ack to process underlying group summaries, got pending=%d", pending)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id IN ($1, $2)`, binaryA, binaryB); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
			DELETE FROM release_family_readiness_summaries
			WHERE provider_id = 1 AND newsgroup_id IN ($1, $2) AND family_key IN ($3, $4, $5, $6)`,
		groupAID, groupBID, familyA, familyB, strings.ToLower(baseA), strings.ToLower(baseB),
	); err != nil {
		t.Fatalf("cleanup release summaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id IN ($1, $2)`, groupAID, groupBID); err != nil {
		t.Fatalf("cleanup newsgroups: %v", err)
	}
}

func TestListBinariesForReleaseCandidateReadsBinaryV2Projections(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.binaryfanout.v2.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "fanout-v2-source",
		ReleaseFamilyKey:  "fanout-v2-family",
		ReleaseKey:        "fanout-v2-family",
		ReleaseName:       "Fanout V2 Family",
		BinaryKey:         "fanout-v2-binary",
		BinaryName:        "fanout.v2.rar",
		FileName:          "fanout.v2.rar",
		ExpectedFileCount: 3,
		TotalParts:        7,
		MatchConfidence:   0.88,
		MatchStatus:       "strong",
		FamilyKind:        "archive_stem",
		BaseStem:          "fanout.v2",
		IsMainPayload:     true,
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET release_name = 'LEGACY FANOUT POISON',
		    binary_name = 'legacy.poison.bin',
		    file_name = 'legacy.poison.bin',
		    expected_file_count = 99,
		    family_kind = 'legacy_poison',
		    match_confidence = 0.01
		WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("poison legacy binary row: %v", err)
	}

	binaries, err := store.ListBinariesForReleaseCandidate(ctx, 1, newsgroupID, ReleaseCandidateKeyKindReleaseFamily, "fanout-v2-family")
	if err != nil {
		t.Fatalf("list binaries for release candidate: %v", err)
	}
	if len(binaries) != 1 {
		t.Fatalf("expected one binary, got %d", len(binaries))
	}
	got := binaries[0]
	if got.ReleaseName != "Fanout V2 Family" ||
		got.BinaryName != "fanout.v2.rar" ||
		got.FileName != "fanout.v2.rar" ||
		got.ExpectedFileCount != 3 ||
		got.FamilyKind != "archive_stem" ||
		got.MatchConfidence != 0.88 {
		t.Fatalf("expected fan-out to use v2 projection values, got %+v", got)
	}
}

func TestAckReleaseCandidateStoresAckWithoutMutatingSummaryRow(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.ack.table.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	family := fmt.Sprintf("ack-family-%d", time.Now().UnixNano())
	updatedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	processedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES ($1, $2, 'release_family', $3, '', '', '', 1, 1, 1, 0, 1, true, 100, NOW(), 'actionable', 100, $4, $5)`,
		1, newsgroupID, family, updatedAt, processedAt,
	); err != nil {
		t.Fatalf("seed release summary row: %v", err)
	}

	if err := store.AckReleaseCandidate(ctx, 1, newsgroupID, "release_family", family); err != nil {
		t.Fatalf("ack release candidate: %v", err)
	}

	var summaryProcessedAt time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT processed_at
		FROM release_family_readiness_summaries
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = $2`,
		newsgroupID, family,
	).Scan(&summaryProcessedAt); err != nil {
		t.Fatalf("query summary processed_at: %v", err)
	}
	if !summaryProcessedAt.Equal(processedAt) {
		t.Fatalf("expected summary processed_at unchanged at %s, got %s", processedAt, summaryProcessedAt)
	}

	var ackProcessedAt time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT processed_at
		FROM release_family_readiness_acks
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = $2`,
		newsgroupID, family,
	).Scan(&ackProcessedAt); err != nil {
		t.Fatalf("query ack processed_at: %v", err)
	}
	if !ackProcessedAt.Equal(updatedAt) {
		t.Fatalf("expected ack processed_at %s, got %s", updatedAt, ackProcessedAt)
	}
}

func TestCatalogReleaseNewsgroupsAndArticlesPreserveMultiGroupProvenance(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupA := fmt.Sprintf("alt.test.catalog.groupa.%d", time.Now().UnixNano())
	groupB := fmt.Sprintf("alt.test.catalog.groupb.%d", time.Now().UnixNano())
	groupAID, err := store.EnsureNewsgroup(ctx, groupA)
	if err != nil {
		t.Fatalf("ensure group A: %v", err)
	}
	groupBID, err := store.EnsureNewsgroup(ctx, groupB)
	if err != nil {
		t.Fatalf("ensure group B: %v", err)
	}

	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ReleaseID:         fmt.Sprintf("rel-%d", time.Now().UnixNano()),
		GUID:              fmt.Sprintf("guid-%d", time.Now().UnixNano()),
		ProviderID:        1,
		SourceReleaseKey:  "catalog-multigroup",
		ReleaseFamilyKey:  "catalog-multigroup",
		ReleaseKey:        "catalog-multigroup",
		GroupName:         "release-group-catalog-multigroup",
		Title:             "Catalog Multigroup Release",
		SearchTitle:       "catalog multigroup release",
		Category:          "Other/Misc",
		Classification:    "other",
		PostedAt:          ptrTime(time.Now().UTC()),
		IdentityStatus:    "identified",
		AvailabilityTier:  "good",
		MediaQualityTier:  "unknown",
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	})
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       groupAID,
		SourceReleaseKey:  "catalog-multigroup",
		ReleaseFamilyKey:  "catalog-multigroup",
		FileFamilyKey:     "catalog-multigroup::movie",
		FamilyKind:        "file_name",
		BaseStem:          "catalog-multigroup",
		IsMainPayload:     true,
		ReleaseKey:        "catalog-multigroup",
		ReleaseName:       "Catalog Multigroup Release",
		BinaryKey:         fmt.Sprintf("catalog-multigroup::binary-%d", time.Now().UnixNano()),
		BinaryName:        "movie.part01.rar",
		FileName:          "movie.part01.rar",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        2,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if _, err := store.InsertArticleHeaders(ctx, 1, groupAID, []ArticleHeader{{
		ArticleNumber: 1001,
		MessageID:     "<catalog-a@test>",
		Subject:       `"movie.part01.rar" yEnc (1/2)`,
		Poster:        "poster-a",
		DateUTC:       ptrTime(time.Now().UTC()),
		Bytes:         111,
	}}); err != nil {
		t.Fatalf("insert article one: %v", err)
	}
	if _, err := store.InsertArticleHeaders(ctx, 1, groupBID, []ArticleHeader{{
		ArticleNumber: 2002,
		MessageID:     "<catalog-b@test>",
		Subject:       `"movie.part01.rar" yEnc (2/2)`,
		Poster:        "poster-b",
		DateUTC:       ptrTime(time.Now().UTC()),
		Bytes:         222,
	}}); err != nil {
		t.Fatalf("insert article two: %v", err)
	}

	var articleOneID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		groupAID, "<catalog-a@test>",
	).Scan(&articleOneID); err != nil {
		t.Fatalf("load article one id: %v", err)
	}
	var articleTwoID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		groupBID, "<catalog-b@test>",
	).Scan(&articleTwoID); err != nil {
		t.Fatalf("load article two id: %v", err)
	}
	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{
		{BinaryID: binaryID, ArticleHeaderID: articleOneID, MessageID: "<catalog-a@test>", PartNumber: 1, TotalParts: 2, SegmentBytes: 111, FileName: "movie.part01.rar"},
		{BinaryID: binaryID, ArticleHeaderID: articleTwoID, MessageID: "<catalog-b@test>", PartNumber: 2, TotalParts: 2, SegmentBytes: 222, FileName: "movie.part01.rar"},
	}); err != nil {
		t.Fatalf("upsert binary parts: %v", err)
	}
	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "movie.part01.rar",
		SizeBytes: 333,
		FileIndex: 1,
		Articles: []ReleaseFileArticleRecord{
			{ArticleHeaderID: articleOneID, PartNumber: 1},
			{ArticleHeaderID: articleTwoID, PartNumber: 2},
		},
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}
	if err := store.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{groupBID, groupAID}); err != nil {
		t.Fatalf("replace release newsgroups: %v", err)
	}

	groups, err := store.ListCatalogReleaseNewsgroups(ctx, releaseID)
	if err != nil {
		t.Fatalf("list catalog release newsgroups: %v", err)
	}
	if len(groups) != 2 || groups[0] != groupA || groups[1] != groupB {
		t.Fatalf("unexpected catalog release groups: got %v want [%s %s]", groups, groupA, groupB)
	}

	files, err := store.ListCatalogReleaseFiles(ctx, releaseID)
	if err != nil {
		t.Fatalf("list catalog release files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one catalog release file, got %d", len(files))
	}
	articles, err := store.ListCatalogReleaseFileArticles(ctx, files[0].ID)
	if err != nil {
		t.Fatalf("list catalog release file articles: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected two article refs, got %d", len(articles))
	}
	if articles[0].MessageID != "<catalog-a@test>" || articles[1].MessageID != "<catalog-b@test>" {
		t.Fatalf("unexpected article refs: %+v", articles)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM release_files WHERE release_id = $1`, releaseID); err != nil {
		t.Fatalf("cleanup release files: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binary_parts WHERE binary_id = $1`, binaryID); err != nil {
		t.Fatalf("cleanup binary parts: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM article_headers WHERE id IN ($1, $2)`, articleOneID, articleTwoID); err != nil {
		t.Fatalf("cleanup article headers: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("cleanup binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM releases WHERE release_id = $1`, releaseID); err != nil {
		t.Fatalf("cleanup release: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id IN ($1, $2)`, groupAID, groupBID); err != nil {
		t.Fatalf("cleanup newsgroups: %v", err)
	}
}

func TestInsertArticleHeadersTracksCrosspostPopularity(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	sourceA := fmt.Sprintf("alt.test.crosspost.sourcea.%d", time.Now().UnixNano())
	sourceB := fmt.Sprintf("alt.test.crosspost.sourceb.%d", time.Now().UnixNano())
	popularTarget := fmt.Sprintf("alt.test.crosspost.popular.%d", time.Now().UnixNano())
	extraTarget := fmt.Sprintf("alt.test.crosspost.extra.%d", time.Now().UnixNano())
	sourceAID, err := store.EnsureNewsgroup(ctx, sourceA)
	if err != nil {
		t.Fatalf("ensure source A: %v", err)
	}
	sourceBID, err := store.EnsureNewsgroup(ctx, sourceB)
	if err != nil {
		t.Fatalf("ensure source B: %v", err)
	}

	if _, err := store.InsertArticleHeaders(ctx, 1, sourceAID, []ArticleHeader{{
		ArticleNumber: 101,
		MessageID:     "<crosspost-1@test>",
		Subject:       "Crosspost One",
		Poster:        "poster@test",
		Xref:          fmt.Sprintf("Xref: news.example.com %s:101 %s:201 %s:301 %s:201", sourceA, popularTarget, extraTarget, popularTarget),
	}}); err != nil {
		t.Fatalf("insert source A headers: %v", err)
	}
	if _, err := store.InsertArticleHeaders(ctx, 1, sourceBID, []ArticleHeader{{
		ArticleNumber: 102,
		MessageID:     "<crosspost-1@test>",
		Subject:       "Crosspost One Copy",
		Poster:        "poster@test",
		Xref:          fmt.Sprintf("Xref: news.example.com %s:102 %s:202 %s:302", sourceB, popularTarget, extraTarget),
	}}); err != nil {
		t.Fatalf("insert source B headers: %v", err)
	}
	if out, err := store.RefreshCrosspostPopularity(ctx, 1000); err != nil {
		t.Fatalf("refresh crosspost popularity: %v", err)
	} else if out == nil || out.GroupsRefreshed < 2 {
		t.Fatalf("expected refreshed crosspost groups, got %+v", out)
	}

	items, err := store.GetIndexerCrosspostNewsgroupPopularity(ctx, 20)
	if err != nil {
		t.Fatalf("get crosspost popularity: %v", err)
	}
	byGroup := make(map[string]IndexerCrosspostPopularityItem, len(items))
	for _, item := range items {
		byGroup[item.GroupName] = item
	}

	popular, ok := byGroup[popularTarget]
	if !ok {
		t.Fatalf("expected %s in popularity report, got %+v", popularTarget, items)
	}
	if popular.ObservedArticleCount != 2 {
		t.Fatalf("expected observed article count 2, got %+v", popular)
	}
	if popular.DistinctMessageCount != 1 {
		t.Fatalf("expected distinct message count 1, got %+v", popular)
	}
	if popular.DistinctSourceGroupCount != 2 {
		t.Fatalf("expected distinct source group count 2, got %+v", popular)
	}
	if popular.LastSeenAt == nil {
		t.Fatalf("expected last_seen_at for %+v", popular)
	}

	extra, ok := byGroup[extraTarget]
	if !ok {
		t.Fatalf("expected %s in popularity report, got %+v", extraTarget, items)
	}
	if extra.ObservedArticleCount != 2 || extra.DistinctMessageCount != 1 || extra.DistinctSourceGroupCount != 2 {
		t.Fatalf("unexpected extra-target aggregation: %+v", extra)
	}

	var firstWatermark int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT last_refreshed_article_header_id
		FROM article_header_crosspost_group_summary
		WHERE observed_group_name = $1`, popularTarget).Scan(&firstWatermark); err != nil {
		t.Fatalf("load first crosspost watermark: %v", err)
	}
	if firstWatermark <= 0 {
		t.Fatalf("expected positive crosspost watermark, got %d", firstWatermark)
	}
	if out, err := store.RefreshCrosspostPopularity(ctx, 1000); err != nil {
		t.Fatalf("refresh crosspost popularity with no deltas: %v", err)
	} else if out == nil || out.GroupsRefreshed != 0 {
		t.Fatalf("expected no refreshed groups without new deltas, got %+v", out)
	}
	var secondWatermark int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT last_refreshed_article_header_id
		FROM article_header_crosspost_group_summary
		WHERE observed_group_name = $1`, popularTarget).Scan(&secondWatermark); err != nil {
		t.Fatalf("load second crosspost watermark: %v", err)
	}
	if secondWatermark != firstWatermark {
		t.Fatalf("expected stable crosspost watermark without deltas, first=%d second=%d", firstWatermark, secondWatermark)
	}
}

func TestPersistReleaseSnapshotSeedsFilesGroupsAndNZBCache(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupA := fmt.Sprintf("alt.test.snapshot.groupa.%d", time.Now().UnixNano())
	groupB := fmt.Sprintf("alt.test.snapshot.groupb.%d", time.Now().UnixNano())
	groupAID, err := store.EnsureNewsgroup(ctx, groupA)
	if err != nil {
		t.Fatalf("ensure group A: %v", err)
	}
	groupBID, err := store.EnsureNewsgroup(ctx, groupB)
	if err != nil {
		t.Fatalf("ensure group B: %v", err)
	}

	now := time.Now().UTC()
	releaseID, err := store.PersistReleaseSnapshot(ctx, ReleaseRecord{
		ReleaseID:         fmt.Sprintf("rel-snapshot-%d", now.UnixNano()),
		GUID:              fmt.Sprintf("guid-snapshot-%d", now.UnixNano()),
		ProviderID:        1,
		SourceReleaseKey:  "snapshot-release",
		ReleaseFamilyKey:  "snapshot-release",
		ReleaseKey:        "snapshot-release",
		GroupName:         "release-group-snapshot",
		Title:             "Snapshot Release",
		SearchTitle:       "snapshot release",
		Category:          "Other/Misc",
		Classification:    "other",
		PostedAt:          &now,
		IdentityStatus:    "identified",
		AvailabilityTier:  "good",
		MediaQualityTier:  "unknown",
		MetadataUpdatedAt: &now,
	}, []ReleaseFileRecord{{
		FileName:  "snapshot.part01.rar",
		SizeBytes: 1234,
		FileIndex: 1,
	}}, []int64{groupBID, groupAID})
	if err != nil {
		t.Fatalf("persist release snapshot: %v", err)
	}

	files, err := store.ListCatalogReleaseFiles(ctx, releaseID)
	if err != nil {
		t.Fatalf("list snapshot files: %v", err)
	}
	if len(files) != 1 || files[0].FileName != "snapshot.part01.rar" {
		t.Fatalf("unexpected files: %+v", files)
	}

	groups, err := store.ListCatalogReleaseNewsgroups(ctx, releaseID)
	if err != nil {
		t.Fatalf("list snapshot groups: %v", err)
	}
	if len(groups) != 2 || groups[0] != groupA || groups[1] != groupB {
		t.Fatalf("unexpected groups: %v", groups)
	}

	var generationStatus string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT generation_status
		FROM nzb_cache
		WHERE release_id = $1`,
		releaseID,
	).Scan(&generationStatus); err != nil {
		t.Fatalf("load snapshot nzb cache: %v", err)
	}
	if generationStatus != "pending" {
		t.Fatalf("unexpected generation status: %q", generationStatus)
	}
}

func TestListReleaseCandidatesPrefersExpectedFileCountEvidenceWithinFormableFamilies(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.expected.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	makeCompleteBinary := func(family string, expected int) int64 {
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
			TotalParts:        10,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", family, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE binaries
			SET observed_parts = total_parts, total_bytes = 12345, updated_at = NOW()
			WHERE id = $1`, binaryID,
		); err != nil {
			t.Fatalf("update binary %s stats: %v", family, err)
		}
		return binaryID
	}

	withExpected := fmt.Sprintf("queue-expected-%d", time.Now().UnixNano())
	withoutExpected := fmt.Sprintf("queue-no-expected-%d", time.Now().UnixNano())
	withExpectedBinaryID := makeCompleteBinary(withExpected, 3)
	withoutExpectedBinaryID := makeCompleteBinary(withoutExpected, 0)

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 1, 1, 1, 0, 3, true, 12345, NOW(), 'actionable', 100, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', $3, $3, $3, $3, 1, 1, 1, 0, 0, false, 12345, NOW(), 'actionable', 0, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, withExpected, withoutExpected,
	); err != nil {
		t.Fatalf("seed expected-file summary rows: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 2, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != withExpected {
		t.Fatalf("expected expected-file-count family %q first, got %q", withExpected, candidates[0].ReleaseFamilyKey)
	}
	if candidates[1].ReleaseFamilyKey != withoutExpected {
		t.Fatalf("expected no-expected-file-count family %q second, got %q", withoutExpected, candidates[1].ReleaseFamilyKey)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id IN ($1, $2)`, withExpectedBinaryID, withoutExpectedBinaryID); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key IN ($2, $3)`, newsgroupID, withExpected, withoutExpected,
	); err != nil {
		t.Fatalf("cleanup release summaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesRanksWeakSingleBehindFormableFamilies(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.weak-single.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	actionableFamily := fmt.Sprintf("queue-actionable-%d", time.Now().UnixNano())
	weakFamily := fmt.Sprintf("queue-weak-%d", time.Now().UnixNano())

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 3, 3, 3, 0, 3, true, 12345, NOW(), 'readable_title', 'movie.2026.mkv', 0.95, 'actionable', 100, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', $3, $3, $3, $3, 1, 1, 1, 0, 0, false, 12345, NOW(), 'contextual_obfuscated', 'abc123def456.bin', 0.92, 'weak_single_binary', 0, NOW() - INTERVAL '3 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    dominant_family_kind = EXCLUDED.dominant_family_kind,
		    dominant_file_name = EXCLUDED.dominant_file_name,
		    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, actionableFamily, weakFamily,
	); err != nil {
		t.Fatalf("seed weak-single summary rows: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 2, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != actionableFamily {
		t.Fatalf("expected actionable family %q first, got %q", actionableFamily, candidates[0].ReleaseFamilyKey)
	}
	if candidates[1].ReleaseFamilyKey != weakFamily {
		t.Fatalf("expected weak single family %q second, got %q", weakFamily, candidates[1].ReleaseFamilyKey)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key IN ($2, $3)`, newsgroupID, actionableFamily, weakFamily,
	); err != nil {
		t.Fatalf("cleanup release summaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesQueueWindowPrefersActionableFamiliesOverOlderWeakSingles(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.queue-window.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	actionableFamily := fmt.Sprintf("queue-window-actionable-%d", time.Now().UnixNano())
	weakFamilyA := fmt.Sprintf("queue-window-weak-a-%d", time.Now().UnixNano())
	weakFamilyB := fmt.Sprintf("queue-window-weak-b-%d", time.Now().UnixNano())

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 1, 1, 1, 0, 0, false, 12345, NOW(), 'contextual_obfuscated', 'deadbeef.bin', 0.70, 'weak_single_binary', 0, NOW() - INTERVAL '5 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', $3, $3, $3, $3, 1, 1, 1, 0, 0, false, 12345, NOW(), 'contextual_obfuscated', 'feedface.bin', 0.70, 'weak_single_binary', 0, NOW() - INTERVAL '4 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', $4, $4, $4, $4, 6, 6, 6, 0, 6, true, 54321, NOW(), 'archive_stem', 'movie.part01.rar', 0.98, 'actionable', 100, NOW() - INTERVAL '1 minute', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    dominant_family_kind = EXCLUDED.dominant_family_kind,
		    dominant_file_name = EXCLUDED.dominant_file_name,
		    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`,
		newsgroupID, weakFamilyA, weakFamilyB, actionableFamily,
	); err != nil {
		t.Fatalf("seed queue-window summary rows: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 80})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != actionableFamily {
		t.Fatalf("expected actionable family %q first, got %q", actionableFamily, candidates[0].ReleaseFamilyKey)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key IN ($2, $3, $4)`, newsgroupID, weakFamilyA, weakFamilyB, actionableFamily,
	); err != nil {
		t.Fatalf("cleanup release summaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesSkipsRecoverPendingYEncFamilies(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.recover-pending.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	pendingFamily := fmt.Sprintf("pending-yenc-family-%d", time.Now().UnixNano())
	pendingBaseStem := fmt.Sprintf("pending-yenc-%d", time.Now().UnixNano())
	actionableFamily := fmt.Sprintf("actionable-family-%d", time.Now().UnixNano())

	pendingBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		ReleaseFamilyKey:  pendingFamily,
		FileFamilyKey:     pendingFamily + "::part",
		FamilyKind:        "opaque_set",
		BaseStem:          pendingBaseStem,
		IsMainPayload:     true,
		ReleaseKey:        pendingFamily,
		ReleaseName:       "Pending YEnc Family",
		BinaryKey:         fmt.Sprintf("%s::binary", pendingFamily),
		BinaryName:        "deadbeefcafebabe.bin",
		FileName:          "deadbeefcafebabe.bin",
		ExpectedFileCount: 2,
		TotalParts:        1,
		MatchConfidence:   0.56,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert pending binary: %v", err)
	}

	postedAt := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
		ArticleNumber: 1,
		MessageID:     fmt.Sprintf("<recover-pending-%d@test>", time.Now().UnixNano()),
		Subject:       `Pending Release [1/2] - "payload.dat" yEnc (1/1)`,
		Poster:        "pending@test",
		DateUTC:       &postedAt,
		Bytes:         512,
		Lines:         10,
	}}); err != nil {
		t.Fatalf("insert pending article header: %v", err)
	}

	var articleHeaderID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM article_headers
		WHERE newsgroup_id = $1
		ORDER BY id DESC
		LIMIT 1`, newsgroupID,
	).Scan(&articleHeaderID); err != nil {
		t.Fatalf("query pending article header: %v", err)
	}

	if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
		BinaryID:        pendingBinaryID,
		ArticleHeaderID: articleHeaderID,
		MessageID:       fmt.Sprintf("<recover-pending-part-%d@test>", time.Now().UnixNano()),
		PartNumber:      1,
		TotalParts:      1,
		SegmentBytes:    512,
		FileName:        "payload.dat",
	}); err != nil {
		t.Fatalf("upsert pending binary part: %v", err)
	}

	actionableBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		ReleaseFamilyKey:  actionableFamily,
		FileFamilyKey:     actionableFamily + "::part",
		FamilyKind:        "archive_stem",
		IsMainPayload:     true,
		ReleaseKey:        actionableFamily,
		ReleaseName:       "Actionable Family",
		BinaryKey:         fmt.Sprintf("%s::binary", actionableFamily),
		BinaryName:        "movie.part01.rar",
		FileName:          "movie.part01.rar",
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.98,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert actionable binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET observed_parts = total_parts,
		    total_bytes = 4096,
		    posted_at = NOW(),
		    updated_at = NOW()
		WHERE id IN ($1, $2)`,
		pendingBinaryID,
		actionableBinaryID,
	); err != nil {
		t.Fatalf("update binary readiness state: %v", err)
	}

	keys := []releaseFamilySummaryKey{
		{ProviderID: 1, NewsgroupID: newsgroupID, KeyKind: "release_family", FamilyKey: pendingFamily},
		{ProviderID: 1, NewsgroupID: newsgroupID, KeyKind: "base_stem", FamilyKey: strings.ToLower(pendingBaseStem)},
		{ProviderID: 1, NewsgroupID: newsgroupID, KeyKind: "release_family", FamilyKey: actionableFamily},
	}
	if err := markReleaseFamiliesDirtyBatch(ctx, store.DB(), keys); err != nil {
		t.Fatalf("queue release summary refresh: %v", err)
	}
	if _, err := store.RefreshQueuedReleaseFamilySummaries(ctx, len(keys)); err != nil {
		t.Fatalf("refresh queued release summaries: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 5, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected only actionable candidates after recover-pending filter, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != actionableFamily {
		t.Fatalf("expected actionable family %q, got %q", actionableFamily, candidates[0].ReleaseFamilyKey)
	}
	for _, candidate := range candidates {
		if candidate.ReleaseFamilyKey == pendingFamily || candidate.ReleaseFamilyKey == strings.ToLower(pendingBaseStem) {
			t.Fatalf("unexpected recover-pending candidate returned: kind=%s family=%q", candidate.KeyKind, candidate.ReleaseFamilyKey)
		}
	}
}

func TestBackfillYEncRecoveryWorkItemsQueuesRecoverableStructuredSubjects(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.yenc-recovery.opaque.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	opaqueBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "opaque-yenc-family",
		FileFamilyKey:    "opaque-yenc-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "opaque-yenc-family",
		IsMainPayload:    true,
		ReleaseKey:       "opaque-yenc-family",
		ReleaseName:      "Opaque YEnc Family",
		BinaryKey:        "opaque-yenc-family::binary",
		BinaryName:       "a8f7c1d2e3b4c5d6f7a8b9c0d1e2f3a4.bin",
		FileName:         "a8f7c1d2e3b4c5d6f7a8b9c0d1e2f3a4.bin",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert opaque binary: %v", err)
	}
	placeholderBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "placeholder-yenc-family",
		FileFamilyKey:    "placeholder-yenc-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "placeholder-yenc-family",
		IsMainPayload:    true,
		ReleaseKey:       "placeholder-yenc-family",
		ReleaseName:      "Placeholder YEnc Family",
		BinaryKey:        "placeholder-yenc-family::binary",
		BinaryName:       "payload.dat",
		FileName:         "payload.dat",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert placeholder binary: %v", err)
	}
	readableBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "readable-yenc-family",
		FileFamilyKey:    "readable-yenc-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "readable-yenc-family",
		IsMainPayload:    true,
		ReleaseKey:       "readable-yenc-family",
		ReleaseName:      "Readable YEnc Family",
		BinaryKey:        "readable-yenc-family::binary",
		BinaryName:       "movie.part01.rar",
		FileName:         "movie.part01.rar",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert readable binary: %v", err)
	}

	now := time.Now().UTC()
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 1101,
			MessageID:     "<opaque-yenc@test>",
			Subject:       `"a8f7c1d2e3b4c5d6f7a8b9c0d1e2f3a4.bin" yEnc (1/1)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         111,
			Lines:         11,
		},
		{
			ArticleNumber: 1102,
			MessageID:     "<placeholder-yenc@test>",
			Subject:       `"payload.dat" yEnc (1/1)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         123,
			Lines:         12,
		},
		{
			ArticleNumber: 1103,
			MessageID:     "<readable-yenc@test>",
			Subject:       `"movie.part01.rar" yEnc (1/1)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         222,
			Lines:         22,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	var opaqueArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<opaque-yenc@test>",
	).Scan(&opaqueArticleID); err != nil {
		t.Fatalf("load opaque article id: %v", err)
	}
	var placeholderArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<placeholder-yenc@test>",
	).Scan(&placeholderArticleID); err != nil {
		t.Fatalf("load placeholder article id: %v", err)
	}
	var readableArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<readable-yenc@test>",
	).Scan(&readableArticleID); err != nil {
		t.Fatalf("load readable article id: %v", err)
	}

	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{
		{
			BinaryID:        opaqueBinaryID,
			ArticleHeaderID: opaqueArticleID,
			MessageID:       "<opaque-yenc@test>",
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    111,
			FileName:        "a8f7c1d2e3b4c5d6f7a8b9c0d1e2f3a4.bin",
		},
		{
			BinaryID:        placeholderBinaryID,
			ArticleHeaderID: placeholderArticleID,
			MessageID:       "<placeholder-yenc@test>",
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    123,
			FileName:        "payload.dat",
		},
		{
			BinaryID:        readableBinaryID,
			ArticleHeaderID: readableArticleID,
			MessageID:       "<readable-yenc@test>",
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    222,
			FileName:        "movie.part01.rar",
		},
	}); err != nil {
		t.Fatalf("upsert binary parts: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', 'opaque-yenc-family', 'opaque-yenc-family', 'opaque-yenc-family', 'Opaque YEnc Family', 1, 1, 1, 0, 0, false, 111, NOW(), 'opaque_set', 'a8f7c1d2e3b4c5d6f7a8b9c0d1e2f3a4.bin', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', 'placeholder-yenc-family', 'placeholder-yenc-family', 'placeholder-yenc-family', 'Placeholder YEnc Family', 1, 1, 1, 0, 0, false, 123, NOW(), 'opaque_set', 'payload.dat', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', 'readable-yenc-family', 'readable-yenc-family', 'readable-yenc-family', 'Readable YEnc Family', 1, 1, 1, 0, 0, false, 222, NOW(), 'opaque_set', 'movie.part01.rar', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    dominant_family_kind = EXCLUDED.dominant_family_kind,
		    dominant_file_name = EXCLUDED.dominant_file_name,
		    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`,
		newsgroupID,
	); err != nil {
		t.Fatalf("seed readiness summaries: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET family_kind = 'plain',
		    is_main_payload = false,
		    recovered_source = 'yenc_header',
		    release_family_key = '',
		    identity_strength = 'strong',
		    expected_file_count = 0,
		    expected_archive_file_count = 0,
		    total_parts = 0
		WHERE id IN ($1, $2)`,
		opaqueBinaryID,
		placeholderBinaryID,
	); err != nil {
		t.Fatalf("poison legacy binary yenc fields: %v", err)
	}

	upserted, retired, err := store.BackfillYEncRecoveryWorkItems(ctx, 10)
	if err != nil {
		t.Fatalf("backfill yenc recovery work items: %v", err)
	}
	if upserted != 2 {
		t.Fatalf("expected 2 upserted work items, got %d retired=%d", upserted, retired)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT binary_id
		FROM yenc_recovery_work_items
		WHERE newsgroup_id = $1
		ORDER BY binary_id`,
		newsgroupID,
	)
	if err != nil {
		t.Fatalf("query queued yenc work items: %v", err)
	}
	defer rows.Close()

	queued := make([]int64, 0, 2)
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			t.Fatalf("scan queued yenc work item: %v", err)
		}
		queued = append(queued, binaryID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate queued yenc work items: %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("expected 2 queued binaries, got %v", queued)
	}
	if queued[0] != opaqueBinaryID || queued[1] != placeholderBinaryID {
		t.Fatalf("expected opaque and placeholder binaries to queue, got %v", queued)
	}
}

func TestListYEncRecoveryCandidatesSeedsWhenWorkTableIsNonEmpty(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.yenc-recovery.seed-refresh.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	eligibleBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "seed-refresh-family",
		FileFamilyKey:    "seed-refresh-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "seed-refresh-family",
		IsMainPayload:    true,
		ReleaseKey:       "seed-refresh-family",
		ReleaseName:      "Seed Refresh Family",
		BinaryKey:        "seed-refresh-family::binary",
		BinaryName:       "deadbeefcafebabe1234567890abcdef.bin",
		FileName:         "deadbeefcafebabe1234567890abcdef.bin",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert eligible binary: %v", err)
	}
	otherBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "seed-refresh-other",
		FileFamilyKey:    "seed-refresh-other::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "seed-refresh-other",
		IsMainPayload:    true,
		ReleaseKey:       "seed-refresh-other",
		ReleaseName:      "Seed Refresh Other",
		BinaryKey:        "seed-refresh-other::binary",
		BinaryName:       "movie.part01.rar",
		FileName:         "movie.part01.rar",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert other binary: %v", err)
	}

	now := time.Now().UTC()
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 1201,
			MessageID:     "<seed-refresh-eligible@test>",
			Subject:       `"deadbeefcafebabe1234567890abcdef.bin" yEnc (1/1)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         111,
			Lines:         11,
		},
		{
			ArticleNumber: 1202,
			MessageID:     "<seed-refresh-other@test>",
			Subject:       `"movie.part01.rar" yEnc (1/1)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         222,
			Lines:         22,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	var eligibleArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<seed-refresh-eligible@test>",
	).Scan(&eligibleArticleID); err != nil {
		t.Fatalf("load eligible article id: %v", err)
	}
	var otherArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<seed-refresh-other@test>",
	).Scan(&otherArticleID); err != nil {
		t.Fatalf("load other article id: %v", err)
	}

	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{
		{
			BinaryID:        eligibleBinaryID,
			ArticleHeaderID: eligibleArticleID,
			MessageID:       "<seed-refresh-eligible@test>",
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    111,
			FileName:        "deadbeefcafebabe1234567890abcdef.bin",
		},
		{
			BinaryID:        otherBinaryID,
			ArticleHeaderID: otherArticleID,
			MessageID:       "<seed-refresh-other@test>",
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    222,
			FileName:        "movie.part01.rar",
		},
	}); err != nil {
		t.Fatalf("upsert binary parts: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', 'seed-refresh-family', 'seed-refresh-family', 'seed-refresh-family', 'Seed Refresh Family', 1, 1, 1, 0, 0, false, 111, NOW(), 'opaque_set', 'deadbeefcafebabe1234567890abcdef.bin', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', 'seed-refresh-other', 'seed-refresh-other', 'seed-refresh-other', 'Seed Refresh Other', 1, 1, 1, 0, 0, false, 222, NOW(), 'opaque_set', 'movie.part01.rar', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    dominant_family_kind = EXCLUDED.dominant_family_kind,
		    dominant_file_name = EXCLUDED.dominant_file_name,
		    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`,
		newsgroupID,
	); err != nil {
		t.Fatalf("seed readiness summaries: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO yenc_recovery_work_items (
			binary_id,
			article_header_id,
			provider_id,
			newsgroup_id,
			message_id,
			status,
			ready_at,
			priority_rank,
			missing_count,
			current_binary_key,
			current_release_family_key,
			current_base_stem,
			current_readiness_bucket,
			structured_identity_binary_matched,
			updated_at
		) VALUES (
			$1, $2, 1, $3, '<seed-refresh-other@test>', 'done', NOW(), 2, 0,
			'seed-refresh-other::binary', 'seed-refresh-other', 'seed-refresh-other',
			'weak_single_binary', false, NOW()
		)`,
		otherBinaryID, otherArticleID, newsgroupID,
	); err != nil {
		t.Fatalf("seed existing yenc work item: %v", err)
	}

	if _, err := store.ListYEncRecoveryCandidates(ctx, 10); err != nil {
		t.Fatalf("list yenc recovery candidates: %v", err)
	}

	var status string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT status
		FROM yenc_recovery_work_items
		WHERE binary_id = $1`,
		eligibleBinaryID,
	).Scan(&status); err != nil {
		t.Fatalf("load eligible work item: %v", err)
	}
	if status != "ready" {
		t.Fatalf("expected eligible binary to be queued as ready, got %q", status)
	}
}

func TestListYEncRecoveryCandidatesRoundRobinsAcrossGroups(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupA := fmt.Sprintf("alt.test.yenc-recovery.fair.a.%d", time.Now().UnixNano())
	groupB := fmt.Sprintf("alt.test.yenc-recovery.fair.b.%d", time.Now().UnixNano())
	groupAID, err := store.EnsureNewsgroup(ctx, groupA)
	if err != nil {
		t.Fatalf("ensure group A: %v", err)
	}
	groupBID, err := store.EnsureNewsgroup(ctx, groupB)
	if err != nil {
		t.Fatalf("ensure group B: %v", err)
	}

	makeBinary := func(groupID int64, family, fileName string) int64 {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:       1,
			NewsgroupID:      groupID,
			ReleaseFamilyKey: family,
			FileFamilyKey:    family + "::part",
			FamilyKind:       "opaque_set",
			BaseStem:         family,
			IsMainPayload:    true,
			ReleaseKey:       family,
			ReleaseName:      family,
			BinaryKey:        family + "::binary",
			BinaryName:       fileName,
			FileName:         fileName,
			TotalParts:       1,
			MatchConfidence:  0.56,
			MatchStatus:      "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", family, err)
		}
		return binaryID
	}

	binaryA1 := makeBinary(groupAID, "fair-a-1", "fair-a-1.bin")
	binaryA2 := makeBinary(groupAID, "fair-a-2", "fair-a-2.bin")
	binaryB1 := makeBinary(groupBID, "fair-b-1", "fair-b-1.bin")

	now := time.Now().UTC()
	insertHeader := func(groupID int64, articleNumber int64, messageID, subject string) int64 {
		if _, err := store.InsertArticleHeaders(ctx, 1, groupID, []ArticleHeader{{
			ArticleNumber: articleNumber,
			MessageID:     messageID,
			Subject:       subject,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         100,
			Lines:         10,
		}}); err != nil {
			t.Fatalf("insert article header %s: %v", messageID, err)
		}
		var articleID int64
		if err := store.DB().QueryRowContext(ctx, `
			SELECT id FROM article_headers
			WHERE newsgroup_id = $1 AND message_id = $2`,
			groupID, messageID,
		).Scan(&articleID); err != nil {
			t.Fatalf("load article id %s: %v", messageID, err)
		}
		return articleID
	}

	articleA1 := insertHeader(groupAID, 1301, "<fair-a-1@test>", `"fair-a-1.bin" yEnc (1/1)`)
	articleA2 := insertHeader(groupAID, 1302, "<fair-a-2@test>", `"fair-a-2.bin" yEnc (1/1)`)
	articleB1 := insertHeader(groupBID, 1401, "<fair-b-1@test>", `"fair-b-1.bin" yEnc (1/1)`)

	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{
		{BinaryID: binaryA1, ArticleHeaderID: articleA1, MessageID: "<fair-a-1@test>", PartNumber: 1, TotalParts: 1, SegmentBytes: 100, FileName: "fair-a-1.bin"},
		{BinaryID: binaryA2, ArticleHeaderID: articleA2, MessageID: "<fair-a-2@test>", PartNumber: 1, TotalParts: 1, SegmentBytes: 100, FileName: "fair-a-2.bin"},
		{BinaryID: binaryB1, ArticleHeaderID: articleB1, MessageID: "<fair-b-1@test>", PartNumber: 1, TotalParts: 1, SegmentBytes: 100, FileName: "fair-b-1.bin"},
	}); err != nil {
		t.Fatalf("upsert binary parts: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO yenc_recovery_work_items (
			binary_id,
			article_header_id,
			provider_id,
			newsgroup_id,
			message_id,
			status,
			ready_at,
			priority_rank,
			missing_count,
			current_binary_key,
			current_release_family_key,
			current_base_stem,
			current_readiness_bucket,
			structured_identity_binary_matched,
			updated_at
		) VALUES
			($1, $2, 1, $3, '<fair-a-1@test>', 'ready', NOW(), 2, 0, 'fair-a-1::binary', 'fair-a-1', 'fair-a-1', 'weak_single_binary', false, NOW()),
			($4, $5, 1, $3, '<fair-a-2@test>', 'ready', NOW(), 2, 0, 'fair-a-2::binary', 'fair-a-2', 'fair-a-2', 'weak_single_binary', false, NOW() - INTERVAL '1 minute'),
			($6, $7, 1, $8, '<fair-b-1@test>', 'ready', NOW(), 2, 0, 'fair-b-1::binary', 'fair-b-1', 'fair-b-1', 'weak_single_binary', false, NOW() - INTERVAL '2 minutes')`,
		binaryA1, articleA1, groupAID,
		binaryA2, articleA2,
		binaryB1, articleB1, groupBID,
	); err != nil {
		t.Fatalf("seed yenc recovery work items: %v", err)
	}

	candidates, err := store.ListYEncRecoveryCandidates(ctx, 3)
	if err != nil {
		t.Fatalf("list yenc recovery candidates: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	if candidates[0].BinaryID != binaryA1 {
		t.Fatalf("expected first candidate %d, got %d", binaryA1, candidates[0].BinaryID)
	}
	if candidates[1].BinaryID != binaryB1 {
		t.Fatalf("expected second candidate %d from other group, got %d", binaryB1, candidates[1].BinaryID)
	}
	if candidates[2].BinaryID != binaryA2 {
		t.Fatalf("expected third candidate %d, got %d", binaryA2, candidates[2].BinaryID)
	}

	var runningCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM yenc_recovery_work_items
		WHERE binary_id IN ($1, $2, $3)
		  AND status = 'running'
		  AND lease_owner = 'recover_yenc'
		  AND lease_expires_at > NOW()`,
		binaryA1, binaryA2, binaryB1,
	).Scan(&runningCount); err != nil {
		t.Fatalf("count running yenc work items: %v", err)
	}
	if runningCount != 3 {
		t.Fatalf("expected 3 claimed running work items, got %d", runningCount)
	}

	leasedAgain, err := store.ListYEncRecoveryCandidates(ctx, 3)
	if err != nil {
		t.Fatalf("list yenc recovery candidates after claim: %v", err)
	}
	if len(leasedAgain) != 0 {
		t.Fatalf("expected claimed candidates to be skipped, got %+v", leasedAgain)
	}

	if err := store.RecordYEncRecoveryTransientFailure(ctx, articleA1); err != nil {
		t.Fatalf("record transient failure: %v", err)
	}
	var releasedStatus, leaseOwner string
	var releasedReadyAfter time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT status, lease_owner, ready_at
		FROM yenc_recovery_work_items
		WHERE binary_id = $1`,
		binaryA1,
	).Scan(&releasedStatus, &leaseOwner, &releasedReadyAfter); err != nil {
		t.Fatalf("load released yenc work item: %v", err)
	}
	if releasedStatus != "ready" || leaseOwner != "" || !releasedReadyAfter.After(time.Now().UTC()) {
		t.Fatalf("expected transient failure to release with future backoff, got status=%q lease=%q ready_at=%s", releasedStatus, leaseOwner, releasedReadyAfter)
	}
}

func TestListYEncRecoveryCandidatesAllowsReadyWorkItemsWithoutPayload(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.yenc-recovery.stale-ready.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	makeBinary := func(family, fileName string) (int64, int64) {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:       1,
			NewsgroupID:      newsgroupID,
			ReleaseFamilyKey: family,
			FileFamilyKey:    family + "::part",
			FamilyKind:       "opaque_set",
			BaseStem:         family,
			IsMainPayload:    true,
			ReleaseKey:       family,
			ReleaseName:      family,
			BinaryKey:        family + "::binary",
			BinaryName:       fileName,
			FileName:         fileName,
			TotalParts:       1,
			MatchConfidence:  0.56,
			MatchStatus:      "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", family, err)
		}
		now := time.Now().UTC()
		messageID := fmt.Sprintf("<%s@test>", family)
		if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
			ArticleNumber: time.Now().UnixNano() % 1000000,
			MessageID:     messageID,
			Subject:       fmt.Sprintf(`"%s" yEnc (1/1)`, fileName),
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         100,
			Lines:         10,
		}}); err != nil {
			t.Fatalf("insert article header %s: %v", family, err)
		}
		var articleID int64
		if err := store.DB().QueryRowContext(ctx, `
			SELECT id FROM article_headers
			WHERE newsgroup_id = $1 AND message_id = $2`,
			newsgroupID, messageID,
		).Scan(&articleID); err != nil {
			t.Fatalf("load article id %s: %v", family, err)
		}
		if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{{
			BinaryID:        binaryID,
			ArticleHeaderID: articleID,
			MessageID:       messageID,
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    100,
			FileName:        fileName,
		}}); err != nil {
			t.Fatalf("upsert binary part %s: %v", family, err)
		}
		return binaryID, articleID
	}

	validBinaryID, validArticleID := makeBinary("stale-ready-valid", "stale-ready-valid.bin")
	staleBinaryID, staleArticleID := makeBinary("stale-ready-stale", "stale-ready-stale.bin")

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM article_header_ingest_payloads
		WHERE article_header_id = $1`,
		staleArticleID,
	); err != nil {
		t.Fatalf("delete stale payload: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO yenc_recovery_work_items (
			binary_id,
			article_header_id,
			provider_id,
			newsgroup_id,
			message_id,
			status,
			ready_at,
			priority_rank,
			missing_count,
			current_binary_key,
			current_release_family_key,
			current_base_stem,
			current_readiness_bucket,
			structured_identity_binary_matched,
			updated_at
		) VALUES
			($1, $2, 1, $3, '<stale-ready-valid@test>', 'ready', NOW(), 2, 0, 'stale-ready-valid::binary', 'stale-ready-valid', 'stale-ready-valid', 'weak_single_binary', false, NOW()),
			($4, $5, 1, $3, '<stale-ready-stale@test>', 'ready', NOW(), 2, 0, 'stale-ready-stale::binary', 'stale-ready-stale', 'stale-ready-stale', 'weak_single_binary', false, NOW())`,
		validBinaryID, validArticleID, newsgroupID,
		staleBinaryID, staleArticleID,
	); err != nil {
		t.Fatalf("seed yenc recovery work items: %v", err)
	}

	candidates, err := store.ListYEncRecoveryCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("list yenc recovery candidates: %v", err)
	}
	gotIDs := map[int64]bool{}
	for _, candidate := range candidates {
		gotIDs[candidate.BinaryID] = true
	}
	if len(candidates) != 2 || !gotIDs[validBinaryID] || !gotIDs[staleBinaryID] {
		t.Fatalf("expected both payload-backed and payloadless candidates, got %+v", candidates)
	}

	var payloadlessStatus string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT status
		FROM yenc_recovery_work_items
		WHERE binary_id = $1`,
		staleBinaryID,
	).Scan(&payloadlessStatus); err != nil {
		t.Fatalf("load payloadless status: %v", err)
	}
	if payloadlessStatus != "ready" {
		t.Fatalf("expected payloadless work item to remain ready, got %q", payloadlessStatus)
	}
}

func TestListYEncRecoveryCandidatesRetiresBlankMessageWorkItems(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.yenc-recovery.blank-message.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "blank-message-family",
		FileFamilyKey:    "blank-message-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "blank-message-family",
		IsMainPayload:    true,
		ReleaseKey:       "blank-message-family",
		ReleaseName:      "Blank Message Family",
		BinaryKey:        "blank-message-family::binary",
		BinaryName:       "blank-message-family.bin",
		FileName:         "blank-message-family.bin",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	now := time.Now().UTC()
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
		ArticleNumber: 91001,
		MessageID:     "<blank-message-family@test>",
		Subject:       `"blank-message-family.bin" yEnc (1/1)`,
		Poster:        "poster@test",
		DateUTC:       &now,
		Bytes:         100,
		Lines:         10,
	}}); err != nil {
		t.Fatalf("insert article header: %v", err)
	}
	var articleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = '<blank-message-family@test>'`,
		newsgroupID,
	).Scan(&articleID); err != nil {
		t.Fatalf("load article id: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO yenc_recovery_work_items (
			binary_id,
			article_header_id,
			provider_id,
			newsgroup_id,
			message_id,
			status,
			ready_at,
			priority_rank,
			updated_at
		) VALUES ($1, $2, 1, $3, '', 'ready', NOW(), 2, NOW())`,
		binaryID, articleID, newsgroupID,
	); err != nil {
		t.Fatalf("seed blank-message work item: %v", err)
	}
	candidates, err := store.ListYEncRecoveryCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("list yenc recovery candidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates for blank-message work item, got %+v", candidates)
	}
	var status string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM yenc_recovery_work_items WHERE binary_id = $1`, binaryID).Scan(&status); err != nil {
		t.Fatalf("load work item status: %v", err)
	}
	if status != "stale" {
		t.Fatalf("expected blank-message work item to be stale, got %q", status)
	}
}

func TestRecordYEncRecoveryNoopBacksOffReadyWorkItem(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.yenc-recovery.noop-backoff.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "noop-backoff-family",
		FileFamilyKey:    "noop-backoff-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "noop-backoff-family",
		IsMainPayload:    true,
		ReleaseKey:       "noop-backoff-family",
		ReleaseName:      "Noop Backoff Family",
		BinaryKey:        "noop-backoff-family::binary",
		BinaryName:       "deadbeefdeadbeefdeadbeefdeadbeef.bin",
		FileName:         "deadbeefdeadbeefdeadbeefdeadbeef.bin",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
		IdentityStrength: "weak",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	now := time.Now().UTC()
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
		ArticleNumber: 1601,
		MessageID:     "<noop-backoff@test>",
		Subject:       `"deadbeefdeadbeefdeadbeefdeadbeef.bin" yEnc (1/1)`,
		Poster:        "poster@test",
		DateUTC:       &now,
		Bytes:         100,
		Lines:         10,
	}}); err != nil {
		t.Fatalf("insert article header: %v", err)
	}

	var articleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<noop-backoff@test>",
	).Scan(&articleID); err != nil {
		t.Fatalf("load article id: %v", err)
	}

	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{{
		BinaryID:        binaryID,
		ArticleHeaderID: articleID,
		MessageID:       "<noop-backoff@test>",
		PartNumber:      1,
		TotalParts:      1,
		SegmentBytes:    100,
		FileName:        "deadbeefdeadbeefdeadbeefdeadbeef.bin",
	}}); err != nil {
		t.Fatalf("upsert binary part: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', 'noop-backoff-family', 'noop-backoff-family', 'noop-backoff-family', 'Noop Backoff Family', 1, 1, 1, 0, 0, false, 100, NOW(), 'opaque_set', 'deadbeefdeadbeefdeadbeefdeadbeef.bin', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO NOTHING`,
		newsgroupID,
	); err != nil {
		t.Fatalf("seed readiness summary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO yenc_recovery_work_items (
			binary_id,
			article_header_id,
			provider_id,
			newsgroup_id,
			message_id,
			status,
			ready_at,
			priority_rank,
			missing_count,
			current_binary_key,
			current_release_family_key,
			current_base_stem,
			current_readiness_bucket,
			structured_identity_binary_matched,
			updated_at
		) VALUES (
			$1, $2, 1, $3, '<noop-backoff@test>', 'ready', NOW(), 2, 0,
			'noop-backoff-family::binary', 'noop-backoff-family', 'noop-backoff-family', 'weak_single_binary', false, NOW()
		)`,
		binaryID, articleID, newsgroupID,
	); err != nil {
		t.Fatalf("seed yenc recovery work item: %v", err)
	}

	if err := store.RecordYEncRecoveryNoop(ctx, articleID); err != nil {
		t.Fatalf("record yenc recovery noop: %v", err)
	}

	candidates, err := store.ListYEncRecoveryCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("list yenc recovery candidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected noop-backed-off candidate to be withheld, got %d", len(candidates))
	}

	var missingCount int
	var retryAfter time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COALESCE(p.yenc_recovery_missing_count, 0), p.yenc_recovery_retry_after
		FROM article_header_ingest_payloads p
		WHERE p.article_header_id = $1`,
		articleID,
	).Scan(&missingCount, &retryAfter); err != nil {
		t.Fatalf("load yenc payload retry state: %v", err)
	}
	if missingCount != 1 {
		t.Fatalf("expected noop missing count to increment to 1, got %d", missingCount)
	}
	if !retryAfter.After(time.Now().UTC().Add(10 * time.Minute)) {
		t.Fatalf("expected noop retry_after in the future, got %s", retryAfter.UTC())
	}
}

func TestBackfillYEncRecoveryWorkItemsPrioritizesIndexedAndMultiFileRecoverables(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.yenc-recovery.priority.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	highPriorityBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		ReleaseFamilyKey:  "priority-high-family",
		FileFamilyKey:     "priority-high-family::part",
		FamilyKind:        "opaque_set",
		BaseStem:          "priority-high-family",
		IsMainPayload:     true,
		ReleaseKey:        "priority-high-family",
		ReleaseName:       "Priority High Family",
		BinaryKey:         "priority-high-family::binary",
		BinaryName:        "priority-high.bin",
		FileName:          "priority-high.bin",
		TotalParts:        2,
		FileIndex:         3,
		ExpectedFileCount: 12,
		MatchConfidence:   0.56,
		MatchStatus:       "matched",
		IdentityStrength:  "weak",
	})
	if err != nil {
		t.Fatalf("upsert high priority binary: %v", err)
	}

	lowPriorityBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "priority-low-family",
		FileFamilyKey:    "priority-low-family::part",
		FamilyKind:       "opaque_set",
		BaseStem:         "priority-low-family",
		IsMainPayload:    true,
		ReleaseKey:       "priority-low-family",
		ReleaseName:      "Priority Low Family",
		BinaryKey:        "priority-low-family::binary",
		BinaryName:       "deadbeefcafebabe1234567890abcdef.bin",
		FileName:         "deadbeefcafebabe1234567890abcdef.bin",
		TotalParts:       1,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
		IdentityStrength: "weak",
	})
	if err != nil {
		t.Fatalf("upsert low priority binary: %v", err)
	}

	now := time.Now().UTC()
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 1501,
			MessageID:     "<priority-high@test>",
			Subject:       `"priority-high.bin" yEnc (03/12)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         111,
			Lines:         11,
		},
		{
			ArticleNumber: 1502,
			MessageID:     "<priority-low@test>",
			Subject:       `"deadbeefcafebabe1234567890abcdef.bin" yEnc (1/1)`,
			Poster:        "poster@test",
			DateUTC:       &now,
			Bytes:         222,
			Lines:         22,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	var highArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<priority-high@test>",
	).Scan(&highArticleID); err != nil {
		t.Fatalf("load high priority article id: %v", err)
	}
	var lowArticleID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id FROM article_headers
		WHERE newsgroup_id = $1 AND message_id = $2`,
		newsgroupID, "<priority-low@test>",
	).Scan(&lowArticleID); err != nil {
		t.Fatalf("load low priority article id: %v", err)
	}

	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{
		{
			BinaryID:        highPriorityBinaryID,
			ArticleHeaderID: highArticleID,
			MessageID:       "<priority-high@test>",
			PartNumber:      1,
			TotalParts:      2,
			SegmentBytes:    111,
			FileName:        "priority-high.bin",
		},
		{
			BinaryID:        lowPriorityBinaryID,
			ArticleHeaderID: lowArticleID,
			MessageID:       "<priority-low@test>",
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    222,
			FileName:        "deadbeefcafebabe1234567890abcdef.bin",
		},
	}); err != nil {
		t.Fatalf("upsert binary parts: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', 'priority-high-family', 'priority-high-family', 'priority-high-family', 'Priority High Family', 1, 0, 0, 1, 12, true, 111, NOW(), 'opaque_set', 'priority-high.bin', 0.56, 'fragment_only', 0, NOW(), TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', 'priority-low-family', 'priority-low-family', 'priority-low-family', 'Priority Low Family', 1, 1, 1, 0, 0, false, 222, NOW(), 'opaque_set', 'deadbeefcafebabe1234567890abcdef.bin', 0.56, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    dominant_family_kind = EXCLUDED.dominant_family_kind,
		    dominant_file_name = EXCLUDED.dominant_file_name,
		    dominant_match_confidence = EXCLUDED.dominant_match_confidence,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`,
		newsgroupID,
	); err != nil {
		t.Fatalf("seed readiness summaries: %v", err)
	}

	upserted, retired, err := store.BackfillYEncRecoveryWorkItems(ctx, 10)
	if err != nil {
		t.Fatalf("backfill yenc recovery work items: %v", err)
	}
	if upserted != 2 {
		t.Fatalf("expected 2 upserted work items, got %d retired=%d", upserted, retired)
	}

	candidates, err := store.ListYEncRecoveryCandidates(ctx, 2)
	if err != nil {
		t.Fatalf("list yenc recovery candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].BinaryID != highPriorityBinaryID {
		t.Fatalf("expected high-priority binary %d first, got %d", highPriorityBinaryID, candidates[0].BinaryID)
	}

	var highRank, lowRank int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT priority_rank
		FROM yenc_recovery_work_items
		WHERE binary_id = $1`,
		highPriorityBinaryID,
	).Scan(&highRank); err != nil {
		t.Fatalf("load high priority rank: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `
		SELECT priority_rank
		FROM yenc_recovery_work_items
		WHERE binary_id = $1`,
		lowPriorityBinaryID,
	).Scan(&lowRank); err != nil {
		t.Fatalf("load low priority rank: %v", err)
	}
	if highRank >= lowRank {
		t.Fatalf("expected high-priority rank lower than low-priority rank, got high=%d low=%d", highRank, lowRank)
	}
}

func TestListReleaseCandidatesPrefersBaseStemWhenReadinessIsEqual(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.base-stem-priority.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	releaseFamilyKey := fmt.Sprintf("queue-release-family-%d", time.Now().UnixNano())
	baseStemKey := fmt.Sprintf("queue-base-stem-%d", time.Now().UnixNano())

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 4, 4, 4, 0, 12, true, 12345, NOW(), 'actionable', 100, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'base_stem', $3, $3, $3, $3, 4, 4, 4, 0, 12, true, 12345, NOW(), 'actionable', 100, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, releaseFamilyKey, baseStemKey,
	); err != nil {
		t.Fatalf("seed base-stem priority rows: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 2, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].KeyKind != "base_stem" {
		t.Fatalf("expected base_stem candidate first, got %s", candidates[0].KeyKind)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key IN ($2, $3)`, newsgroupID, releaseFamilyKey, baseStemKey,
	); err != nil {
		t.Fatalf("cleanup release summaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesDerivesWeakSingleBucketFromDominantBinary(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.weak-derive.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	family := fmt.Sprintf("queue-derived-weak-%d", time.Now().UnixNano())
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		SourceReleaseKey: family,
		ReleaseFamilyKey: family,
		FileFamilyKey:    family + "::file",
		FamilyKind:       "contextual_obfuscated",
		IsMainPayload:    true,
		ReleaseKey:       family,
		ReleaseName:      family,
		BinaryKey:        fmt.Sprintf("%s::%d", family, time.Now().UnixNano()),
		BinaryName:       "04601b416a624006a3ef2df4717c6ede.bin",
		FileName:         "04601b416a624006a3ef2df4717c6ede.bin",
		TotalParts:       10,
		MatchConfidence:  0.56,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET observed_parts = total_parts, total_bytes = 12345, updated_at = NOW()
		WHERE id = $1`, binaryID,
	); err != nil {
		t.Fatalf("update binary stats: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 1, 1, 1, 0, 0, false, 12345, NOW(), 'actionable', 0, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, family,
	); err != nil {
		t.Fatalf("seed derived weak-single summary row: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReadinessBucket != releaseReadinessWeakSingle {
		t.Fatalf("expected derived readiness bucket %q, got %q", releaseReadinessWeakSingle, candidates[0].ReadinessBucket)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("cleanup binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, family,
	); err != nil {
		t.Fatalf("cleanup release summary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesDerivesFragmentBucketForNumericOpaqueFamily(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.numeric-opaque.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	family := fmt.Sprintf("80894690-n-yuo-%d", time.Now().UnixNano())
	for idx, name := range []string{
		"0q2503mv5r4sp5gxg64bs9uqdzn0nq4k",
		"e81q5yfbwtk46b9n5szq5mdk740cl1v1",
	} {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			SourceReleaseKey:  family,
			ReleaseFamilyKey:  family,
			FileFamilyKey:     fmt.Sprintf("%s::%d", family, idx+1),
			FamilyKind:        "readable_title",
			IsMainPayload:     true,
			ReleaseKey:        family,
			ReleaseName:       "80894690 n YuO",
			BinaryKey:         fmt.Sprintf("%s::%d::%d", family, idx+1, time.Now().UnixNano()),
			BinaryName:        fmt.Sprintf(`80894690-n-YuO [%d/2] - "%s" yEnc (1/1)`, idx+1, name),
			FileName:          name,
			FileIndex:         idx + 1,
			ExpectedFileCount: 2,
			TotalParts:        1,
			MatchConfidence:   1.0,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary: %v", err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE binaries
			SET observed_parts = total_parts, total_bytes = 1000, updated_at = NOW()
			WHERE id = $1`, binaryID,
		); err != nil {
			t.Fatalf("update binary stats: %v", err)
		}
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, '80894690 n YuO', 2, 2, 2, 0, 2, true, 2000, NOW(), 'actionable', 100, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET release_name = EXCLUDED.release_name,
		    binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, family,
	); err != nil {
		t.Fatalf("seed numeric opaque summary row: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReadinessBucket != releaseReadinessFragmentOnly {
		t.Fatalf("expected derived readiness bucket %q, got %q", releaseReadinessFragmentOnly, candidates[0].ReadinessBucket)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE release_family_key = $1`, family); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, family,
	); err != nil {
		t.Fatalf("cleanup release summary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesDerivesPreferBaseStemBucketFromContextualArchiveEvidence(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.prefer-base-stem.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	family := fmt.Sprintf("queue-prefer-base-stem-%d", time.Now().UnixNano())
	for i := 1; i <= 3; i++ {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			SourceReleaseKey:  family,
			ReleaseFamilyKey:  family,
			FileFamilyKey:     fmt.Sprintf("%s::file-%d", family, i),
			FamilyKind:        "contextual_obfuscated",
			BaseStem:          "opaque.release.7z",
			IsMainPayload:     true,
			ReleaseKey:        family,
			ReleaseName:       family,
			BinaryKey:         fmt.Sprintf("%s::%d", family, i),
			BinaryName:        fmt.Sprintf("opaque.release.7z.%03d", i),
			FileName:          fmt.Sprintf("opaque.release.7z.%03d", i),
			FileIndex:         i,
			ExpectedFileCount: 20,
			TotalParts:        10,
			MatchConfidence:   0.72,
			MatchStatus:       "probable",
		})
		if err != nil {
			t.Fatalf("upsert binary %d: %v", i, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE binaries
			SET observed_parts = total_parts, total_bytes = 12345, updated_at = NOW()
			WHERE id = $1`, binaryID,
		); err != nil {
			t.Fatalf("update binary %d stats: %v", i, err)
		}
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 3, 3, 3, 0, 20, true, 12345, NOW(), 'actionable', 15, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, family,
	); err != nil {
		t.Fatalf("seed prefer-base-stem summary row: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReadinessBucket != releaseReadinessPreferBaseStem {
		t.Fatalf("expected derived readiness bucket %q, got %q", releaseReadinessPreferBaseStem, candidates[0].ReadinessBucket)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM binaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND release_family_key = $2`, newsgroupID, family,
	); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, family,
	); err != nil {
		t.Fatalf("cleanup release summary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesDerivesOvergroupedContextualBucketFromUniqueStemMatrix(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.overgrouped.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	family := fmt.Sprintf("queue-overgrouped-%d", time.Now().UnixNano())
	for i := 1; i <= 24; i++ {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			SourceReleaseKey:  family,
			ReleaseFamilyKey:  family,
			FileFamilyKey:     fmt.Sprintf("%s::file-%d", family, i),
			FamilyKind:        "contextual_obfuscated",
			BaseStem:          fmt.Sprintf("opaque-file-%03d", i),
			IsMainPayload:     true,
			ReleaseKey:        family,
			ReleaseName:       family,
			BinaryKey:         fmt.Sprintf("%s::%d", family, i),
			BinaryName:        fmt.Sprintf("opaque-file-%03d.bin", i),
			FileName:          fmt.Sprintf("opaque-file-%03d.bin", i),
			FileIndex:         i,
			ExpectedFileCount: 8,
			TotalParts:        10,
			MatchConfidence:   0.61,
			MatchStatus:       "probable",
		})
		if err != nil {
			t.Fatalf("upsert binary %d: %v", i, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE binaries
			SET observed_parts = total_parts, total_bytes = 12345, updated_at = NOW()
			WHERE id = $1`, binaryID,
		); err != nil {
			t.Fatalf("update binary %d stats: %v", i, err)
		}
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 24, 24, 24, 0, 8, true, 12345, NOW(), 'actionable', 100, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET binary_count = EXCLUDED.binary_count,
		    complete_binary_count = EXCLUDED.complete_binary_count,
		    complete_main_payload_binary_count = EXCLUDED.complete_main_payload_binary_count,
		    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
		    expected_file_count = EXCLUDED.expected_file_count,
		    has_expected_file_count = EXCLUDED.has_expected_file_count,
		    total_bytes = EXCLUDED.total_bytes,
		    earliest_posted_at = EXCLUDED.earliest_posted_at,
		    readiness_bucket = EXCLUDED.readiness_bucket,
		    expected_file_coverage_pct = EXCLUDED.expected_file_coverage_pct,
		    updated_at = EXCLUDED.updated_at,
		    processed_at = EXCLUDED.processed_at`, newsgroupID, family,
	); err != nil {
		t.Fatalf("seed overgrouped summary row: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReadinessBucket != releaseReadinessOvergrouped {
		t.Fatalf("expected derived readiness bucket %q, got %q", releaseReadinessOvergrouped, candidates[0].ReadinessBucket)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM binaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND release_family_key = $2`, newsgroupID, family,
	); err != nil {
		t.Fatalf("cleanup binaries: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, family,
	); err != nil {
		t.Fatalf("cleanup release summary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestListReleaseCandidatesKeepsZeroBinaryFamiliesEligibleForStaleCleanup(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.stale.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	staleFamily := fmt.Sprintf("queue-stale-%d", time.Now().UnixNano())
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES (1, $1, 'release_family', $2, '', '', '', 0, 0, 0, 0, 0, false, 0, NULL, 'stale_cleanup_only', 0, NOW() - INTERVAL '5 minutes', TIMESTAMPTZ 'epoch')`, newsgroupID, staleFamily,
	); err != nil {
		t.Fatalf("insert stale cleanup family: %v", err)
	}

	candidates, err := store.ListReleaseCandidates(ctx, 1, ReleaseCandidateSelectionOptions{MinExpectedFileCoveragePct: 90})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ReleaseFamilyKey != staleFamily {
		t.Fatalf("expected stale cleanup family %q, got %q", staleFamily, candidates[0].ReleaseFamilyKey)
	}
	if candidates[0].BinaryCount != 0 {
		t.Fatalf("expected zero-binary stale cleanup candidate, got binary_count=%d", candidates[0].BinaryCount)
	}

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = $2`, newsgroupID, staleFamily,
	); err != nil {
		t.Fatalf("cleanup stale summary family: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
		t.Fatalf("cleanup newsgroup: %v", err)
	}
}

func TestRefreshBinaryStatsRequeuesFamilyAfterDirtyRowWasAcked(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.requeue.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
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
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	now := time.Date(2026, 4, 21, 18, 0, 0, 0, time.UTC)
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 100,
			MessageID:     fmt.Sprintf("<requeue-%d@test>", time.Now().UnixNano()),
			Subject:       `Requeue Test [1/1] - "requeue.test.r00" yEnc (1/10)`,
			Poster:        "poster-requeue@example.com",
			DateUTC:       &now,
			Bytes:         500,
			Lines:         10,
		},
	}); err != nil {
		t.Fatalf("insert article header: %v", err)
	}

	var articleHeaderID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM article_headers
		WHERE newsgroup_id = $1
		ORDER BY id DESC
		LIMIT 1`, newsgroupID,
	).Scan(&articleHeaderID); err != nil {
		t.Fatalf("query article header: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		SourceReleaseKey: "requeue-family",
		ReleaseFamilyKey: "requeue-family",
		FileFamilyKey:    "requeue-family::file",
		FamilyKind:       "readable_title",
		IsMainPayload:    true,
		ReleaseKey:       "requeue-family",
		ReleaseName:      "Requeue Family",
		BinaryKey:        fmt.Sprintf("requeue-family::%d", time.Now().UnixNano()),
		BinaryName:       "requeue.test.r00",
		FileName:         "requeue.test.r00",
		TotalParts:       10,
		MatchConfidence:  0.95,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
		BinaryID:        binaryID,
		ArticleHeaderID: articleHeaderID,
		MessageID:       fmt.Sprintf("<requeue-part-%d@test>", time.Now().UnixNano()),
		PartNumber:      1,
		TotalParts:      10,
		SegmentBytes:    500,
		FileName:        "requeue.test.r00",
	}); err != nil {
		t.Fatalf("upsert binary part: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_acks (
			provider_id, newsgroup_id, key_kind, family_key, processed_at, updated_at
		)
		SELECT provider_id, newsgroup_id, key_kind, family_key, updated_at, NOW()
		FROM release_family_readiness_summaries
		WHERE provider_id = 1 AND newsgroup_id = $1 AND family_key = 'requeue-family'
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET processed_at = EXCLUDED.processed_at,
		    updated_at = NOW()`, newsgroupID,
	); err != nil {
		t.Fatalf("ack summary queue row: %v", err)
	}

	if err := store.RefreshBinaryStats(ctx, binaryID); err != nil {
		t.Fatalf("refresh binary stats: %v", err)
	}

	var dirtyCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_readiness_summaries s
		LEFT JOIN release_family_readiness_acks a
		  ON a.provider_id = s.provider_id
		 AND a.newsgroup_id = s.newsgroup_id
		 AND a.key_kind = s.key_kind
		 AND a.family_key = s.family_key
		WHERE s.provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'requeue-family'
		  AND s.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch')`, newsgroupID,
	).Scan(&dirtyCount); err != nil {
		t.Fatalf("query requeued summary family: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("expected family to be requeued after refresh, got %d rows", dirtyCount)
	}
}

func TestRefreshBinaryStatsDeferredSummaryRefreshMarksFamilyDirtyWithoutInlineRecompute(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.refresh.defer.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "deferred-refresh-source",
		ReleaseFamilyKey:  "deferred-refresh-family",
		ReleaseKey:        "deferred-refresh-family",
		ReleaseName:       "Deferred Refresh Family",
		BinaryKey:         "deferred-refresh-binary",
		BinaryName:        "deferred.refresh.rar",
		FileName:          "deferred.refresh.rar",
		ExpectedFileCount: 3,
		TotalParts:        2,
		MatchConfidence:   0.97,
		MatchStatus:       "strong",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	postedAt := time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC)
	for idx := 0; idx < 2; idx++ {
		articleNumber := int64(800 + idx)
		messageID := fmt.Sprintf("<deferred-refresh-%d-%d@test>", time.Now().UnixNano(), idx)
		inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
			ArticleNumber: articleNumber,
			MessageID:     messageID,
			Subject:       `Deferred Refresh - "deferred.refresh.rar" yEnc (1/2)`,
			Poster:        "poster-deferred-refresh@example.com",
			DateUTC:       &postedAt,
			Bytes:         1024,
			Lines:         12,
		}})
		if err != nil {
			t.Fatalf("insert article header %d: %v", idx, err)
		}
		if inserted != 1 {
			t.Fatalf("expected 1 inserted article header, got %d", inserted)
		}

		var articleHeaderID int64
		if err := store.DB().QueryRowContext(ctx, `
			SELECT id
			FROM article_headers
			WHERE newsgroup_id = $1 AND article_number = $2`, newsgroupID, articleNumber,
		).Scan(&articleHeaderID); err != nil {
			t.Fatalf("lookup article header %d: %v", idx, err)
		}

		if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: articleHeaderID,
			MessageID:       messageID,
			PartNumber:      idx + 1,
			TotalParts:      2,
			SegmentBytes:    1024,
			FileName:        "deferred.refresh.rar",
		}); err != nil {
			t.Fatalf("upsert binary part %d: %v", idx, err)
		}
	}

	deferredCtx := WithDeferredReleaseFamilySummaryRefresh(ctx)
	if err := store.RefreshBinaryStats(deferredCtx, binaryID); err != nil {
		t.Fatalf("refresh binary stats with deferred summary refresh: %v", err)
	}

	var (
		summaryCount int
		queueCount   int
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_readiness_summaries
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'deferred-refresh-family'`, newsgroupID,
	).Scan(&summaryCount); err != nil {
		t.Fatalf("count deferred refresh summary rows: %v", err)
	}
	if summaryCount != 0 {
		t.Fatalf("expected deferred refresh to avoid inline summary writes, got %d rows", summaryCount)
	}
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_summary_refresh_queue
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'deferred-refresh-family'`, newsgroupID,
	).Scan(&queueCount); err != nil {
		t.Fatalf("count deferred refresh queue rows: %v", err)
	}
	if queueCount != 1 {
		t.Fatalf("expected deferred refresh to enqueue one family key, got %d", queueCount)
	}
}

func TestRefreshQueuedReleaseFamilySummariesRecomputesDeferredFamily(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.queue.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "queued-refresh-source",
		ReleaseFamilyKey:  "queued-refresh-family",
		ReleaseKey:        "queued-refresh-family",
		ReleaseName:       "Queued Refresh Family",
		BinaryKey:         "queued-refresh-binary",
		BinaryName:        "queued.refresh.rar",
		FileName:          "queued.refresh.rar",
		ExpectedFileCount: 1,
		TotalParts:        2,
		MatchConfidence:   0.97,
		MatchStatus:       "strong",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	postedAt := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	for idx := 0; idx < 2; idx++ {
		articleNumber := int64(1800 + idx)
		messageID := fmt.Sprintf("<queued-refresh-%d-%d@test>", time.Now().UnixNano(), idx)
		inserted, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{{
			ArticleNumber: articleNumber,
			MessageID:     messageID,
			Subject:       `Queued Refresh - "queued.refresh.rar" yEnc (1/2)`,
			Poster:        "poster-queued-refresh@example.com",
			DateUTC:       &postedAt,
			Bytes:         1024,
			Lines:         12,
		}})
		if err != nil {
			t.Fatalf("insert article header %d: %v", idx, err)
		}
		if inserted != 1 {
			t.Fatalf("expected 1 inserted article header, got %d", inserted)
		}

		var articleHeaderID int64
		if err := store.DB().QueryRowContext(ctx, `
			SELECT id
			FROM article_headers
			WHERE newsgroup_id = $1 AND article_number = $2`, newsgroupID, articleNumber,
		).Scan(&articleHeaderID); err != nil {
			t.Fatalf("lookup article header %d: %v", idx, err)
		}

		if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: articleHeaderID,
			MessageID:       messageID,
			PartNumber:      idx + 1,
			TotalParts:      2,
			SegmentBytes:    1024,
			FileName:        "queued.refresh.rar",
		}); err != nil {
			t.Fatalf("upsert binary part %d: %v", idx, err)
		}
	}

	deferredCtx := WithDeferredReleaseFamilySummaryRefresh(ctx)
	if err := store.RefreshBinaryStats(deferredCtx, binaryID); err != nil {
		t.Fatalf("refresh binary stats with deferred summary refresh: %v", err)
	}

	refreshed, err := store.RefreshQueuedReleaseFamilySummaries(ctx, 100)
	if err != nil {
		t.Fatalf("refresh queued release family summaries: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("expected 1 refreshed summary key, got %d", refreshed)
	}

	var (
		binaryCount int
		readiness   string
		queueCount  int
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT binary_count, readiness_bucket
		FROM release_family_readiness_summaries
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'queued-refresh-family'`, newsgroupID,
	).Scan(&binaryCount, &readiness); err != nil {
		t.Fatalf("query refreshed summary row: %v", err)
	}
	if binaryCount != 1 {
		t.Fatalf("expected refreshed summary binary_count=1, got %d", binaryCount)
	}
	if readiness != releaseReadinessActionable {
		t.Fatalf("expected actionable readiness after queued refresh, got %q", readiness)
	}
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_summary_refresh_queue
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'queued-refresh-family'`, newsgroupID,
	).Scan(&queueCount); err != nil {
		t.Fatalf("query refresh queue row: %v", err)
	}
	if queueCount != 0 {
		t.Fatalf("expected queued refresh row to be drained, got %d", queueCount)
	}
}

func TestRefreshQueuedReleaseFamilySummariesReadsBinaryV2Projections(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.v2.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  "v2-summary-source",
		ReleaseFamilyKey:  "v2-summary-family",
		ReleaseKey:        "v2-summary-family",
		ReleaseName:       "V2 Summary Family",
		BinaryKey:         "v2-summary-binary",
		BinaryName:        "v2.summary.rar",
		FileName:          "v2.summary.rar",
		ExpectedFileCount: 2,
		TotalParts:        4,
		MatchConfidence:   0.91,
		MatchStatus:       "strong",
		FamilyKind:        "archive_stem",
		IsMainPayload:     true,
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET release_name = 'LEGACY POISON',
		    expected_file_count = 99,
		    total_bytes = 999999,
		    match_confidence = 0.01,
		    family_kind = 'legacy_poison'
		WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("poison legacy binary row: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_summary_refresh_queue (
			provider_id, newsgroup_id, key_kind, family_key, queued_at
		)
		VALUES (1, $1, 'release_family', 'v2-summary-family', NOW())
		ON CONFLICT DO NOTHING`, newsgroupID); err != nil {
		t.Fatalf("enqueue summary refresh: %v", err)
	}

	refreshed, err := store.RefreshQueuedReleaseFamilySummaries(ctx, 100)
	if err != nil {
		t.Fatalf("refresh queued release family summaries: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("expected 1 refreshed summary key, got %d", refreshed)
	}

	var (
		releaseName             string
		expectedFileCount       int
		dominantFamilyKind      string
		dominantMatchConfidence float64
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			release_name,
			expected_file_count,
			dominant_family_kind,
			dominant_match_confidence
		FROM release_family_readiness_summaries
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND key_kind = 'release_family'
		  AND family_key = 'v2-summary-family'`, newsgroupID,
	).Scan(&releaseName, &expectedFileCount, &dominantFamilyKind, &dominantMatchConfidence); err != nil {
		t.Fatalf("query refreshed summary: %v", err)
	}
	if releaseName != "V2 Summary Family" || expectedFileCount != 2 || dominantFamilyKind != "archive_stem" || dominantMatchConfidence != 0.91 {
		t.Fatalf("expected summary to use v2 projection values, got release_name=%q expected=%d family_kind=%q confidence=%v",
			releaseName,
			expectedFileCount,
			dominantFamilyKind,
			dominantMatchConfidence,
		)
	}
}

func TestRefreshQueuedReleaseFamilySummariesPrioritizesNonWeakResidue(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.priority.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	weakFamily := "priority-weak-family"
	actionableFamily := "priority-actionable-family"
	missingFamily := "priority-missing-family"

	if _, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: actionableFamily,
		SourceReleaseKey: actionableFamily,
		ReleaseKey:       actionableFamily,
		ReleaseName:      actionableFamily,
		BinaryKey:        actionableFamily + "::binary",
		BinaryName:       "priority.actionable.part01.rar",
		FileName:         "priority.actionable.part01.rar",
		FamilyKind:       "archive_stem",
		IsMainPayload:    true,
		TotalParts:       2,
		MatchConfidence:  0.97,
		MatchStatus:      "matched",
	}); err != nil {
		t.Fatalf("upsert actionable binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET observed_parts = total_parts,
		    expected_file_count = 2,
		    updated_at = NOW()
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		  AND release_family_key = $2`,
		newsgroupID,
		actionableFamily,
	); err != nil {
		t.Fatalf("complete actionable binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, expected_archive_file_count, has_expected_file_count, has_expected_archive_file_count,
			total_bytes, earliest_posted_at, dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, recover_pending, expected_file_coverage_pct, archive_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', $2, $2, $2, $2, 1, 1, 1, 0, 0, 0, false, false, 100, NOW(), 'contextual_obfuscated', 'priority-weak.bin', 0.70, 'weak_single_binary', false, 0, 0, NOW() - INTERVAL '3 minutes', TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', $3, $3, $3, $3, 1, 1, 1, 0, 2, 0, true, false, 200, NOW(), 'archive_stem', 'priority.actionable.part01.rar', 0.97, 'actionable', false, 50, 0, NOW() - INTERVAL '2 minutes', TIMESTAMPTZ 'epoch')`,
		newsgroupID,
		weakFamily,
		actionableFamily,
	); err != nil {
		t.Fatalf("insert readiness rows: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_summary_refresh_queue (
			provider_id, newsgroup_id, key_kind, family_key, queued_at
		)
		VALUES
			(1, $1, 'release_family', $2, NOW() - INTERVAL '3 minutes'),
			(1, $1, 'release_family', $3, NOW() - INTERVAL '2 minutes'),
			(1, $1, 'release_family', $4, NOW() - INTERVAL '1 minute')`,
		newsgroupID,
		weakFamily,
		actionableFamily,
		missingFamily,
	); err != nil {
		t.Fatalf("insert refresh queue rows: %v", err)
	}

	refreshed, err := store.RefreshQueuedReleaseFamilySummaries(ctx, 1)
	if err != nil {
		t.Fatalf("refresh queued release family summaries: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("expected 1 refreshed summary key, got %d", refreshed)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT family_key
		FROM release_family_summary_refresh_queue
		WHERE provider_id = 1
		  AND newsgroup_id = $1
		ORDER BY family_key`,
		newsgroupID,
	)
	if err != nil {
		t.Fatalf("query remaining refresh queue: %v", err)
	}
	defer rows.Close()

	var remaining []string
	for rows.Next() {
		var familyKey string
		if err := rows.Scan(&familyKey); err != nil {
			t.Fatalf("scan remaining refresh queue: %v", err)
		}
		remaining = append(remaining, familyKey)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate remaining refresh queue: %v", err)
	}

	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining queued keys, got %v", remaining)
	}
	for _, familyKey := range remaining {
		if familyKey == missingFamily {
			t.Fatalf("expected missing-summary family to be dequeued first, remaining=%v", remaining)
		}
	}
}

func TestRefreshQueuedReleaseFamilySummariesFillsHotBatchAcrossPriorityBranches(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.fill.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	actionableFamily := "fill-actionable-family"
	missingOne := "fill-missing-family-one"
	missingTwo := "fill-missing-family-two"

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, expected_archive_file_count, has_expected_file_count, has_expected_archive_file_count,
			total_bytes, earliest_posted_at, dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, recover_pending, expected_file_coverage_pct, archive_file_coverage_pct, updated_at, processed_at
		)
		VALUES (
			1, $1, 'release_family', $2,
			$2, $2, $2,
			1, 1, 1, 0,
			1, 0, true, false,
			100, NOW(), 'archive_stem', 'fill.actionable.rar', 0.97,
			'actionable', false, 100, 0, NOW() - INTERVAL '1 minute', TIMESTAMPTZ 'epoch'
		)`,
		newsgroupID,
		actionableFamily,
	); err != nil {
		t.Fatalf("insert actionable readiness row: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_summary_refresh_queue (
			provider_id, newsgroup_id, key_kind, family_key, queued_at
		)
		VALUES
			(1, $1, 'release_family', $2, NOW() - INTERVAL '3 minutes'),
			(1, $1, 'release_family', $3, NOW() - INTERVAL '2 minutes'),
			(1, $1, 'release_family', $4, NOW() - INTERVAL '1 minute')`,
		newsgroupID,
		actionableFamily,
		missingOne,
		missingTwo,
	); err != nil {
		t.Fatalf("insert refresh queue rows: %v", err)
	}

	refreshed, err := store.RefreshQueuedReleaseFamilySummaries(ctx, 3)
	if err != nil {
		t.Fatalf("refresh queued release family summaries: %v", err)
	}
	if refreshed != 3 {
		t.Fatalf("expected hot refresh to fill batch across branches, got %d", refreshed)
	}

	var queueCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_summary_refresh_queue
		WHERE provider_id = 1
		  AND newsgroup_id = $1`,
		newsgroupID,
	).Scan(&queueCount); err != nil {
		t.Fatalf("query refresh queue count: %v", err)
	}
	if queueCount != 0 {
		t.Fatalf("expected all queued hot keys to be drained, got %d", queueCount)
	}
}

func TestRefreshBinaryStatsUpdatesReleaseFamilySummary(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.refresh.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
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
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	family := fmt.Sprintf("summary-refresh-family-%d", time.Now().UnixNano())
	baseStem := fmt.Sprintf("summary.refresh.%d", time.Now().UnixNano())
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  family,
		ReleaseFamilyKey:  family,
		FileFamilyKey:     family + "::file",
		FamilyKind:        "archive_stem",
		BaseStem:          baseStem,
		IsMainPayload:     true,
		ReleaseKey:        family,
		ReleaseName:       "Summary Refresh Family",
		BinaryKey:         fmt.Sprintf("%s::%d", family, time.Now().UnixNano()),
		BinaryName:        "summary.refresh.r00",
		FileName:          "summary.refresh.r00",
		ExpectedFileCount: 3,
		TotalParts:        3,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	now := time.Date(2026, 4, 22, 19, 0, 0, 0, time.UTC)
	headers := []ArticleHeader{
		{
			ArticleNumber: 201,
			MessageID:     fmt.Sprintf("<summary-refresh-1-%d@test>", time.Now().UnixNano()),
			Subject:       `Summary Refresh [1/1] - "summary.refresh.r00" yEnc (1/3)`,
			Poster:        "poster-summary-refresh@example.com",
			DateUTC:       &now,
			Bytes:         600,
			Lines:         10,
		},
		{
			ArticleNumber: 202,
			MessageID:     fmt.Sprintf("<summary-refresh-2-%d@test>", time.Now().UnixNano()),
			Subject:       `Summary Refresh [1/1] - "summary.refresh.r00" yEnc (2/3)`,
			Poster:        "poster-summary-refresh@example.com",
			DateUTC:       &now,
			Bytes:         700,
			Lines:         11,
		},
		{
			ArticleNumber: 203,
			MessageID:     fmt.Sprintf("<summary-refresh-3-%d@test>", time.Now().UnixNano()),
			Subject:       `Summary Refresh [1/1] - "summary.refresh.r00" yEnc (3/3)`,
			Poster:        "poster-summary-refresh@example.com",
			DateUTC:       &now,
			Bytes:         800,
			Lines:         12,
		},
	}
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, headers); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT id
		FROM article_headers
		WHERE newsgroup_id = $1
		ORDER BY article_number`, newsgroupID,
	)
	if err != nil {
		t.Fatalf("query article headers: %v", err)
	}
	defer rows.Close()

	partNumber := 1
	for rows.Next() {
		var articleHeaderID int64
		if err := rows.Scan(&articleHeaderID); err != nil {
			t.Fatalf("scan article header id: %v", err)
		}
		if err := store.UpsertBinaryPart(ctx, BinaryPartRecord{
			BinaryID:        binaryID,
			ArticleHeaderID: articleHeaderID,
			MessageID:       fmt.Sprintf("<summary-refresh-part-%d@test>", partNumber),
			PartNumber:      partNumber,
			TotalParts:      3,
			SegmentBytes:    int64(500 + partNumber),
			FileName:        "summary.refresh.r00",
		}); err != nil {
			t.Fatalf("upsert binary part %d: %v", partNumber, err)
		}
		partNumber++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate article header ids: %v", err)
	}

	if err := store.RefreshBinaryStats(ctx, binaryID); err != nil {
		t.Fatalf("refresh binary stats: %v", err)
	}

	for _, keyKind := range []string{"release_family", "base_stem"} {
		var (
			binaryCount                    int
			completeBinaryCount            int
			completeMainPayloadBinaryCount int
			incompleteBinaryCount          int
			expectedFileCount              int
			expectedFileCoveragePct        float64
			hasExpectedFileCount           bool
			readinessBucket                string
		)
		familyKey := family
		if keyKind == "base_stem" {
			familyKey = strings.ToLower(baseStem)
		}
		if err := store.DB().QueryRowContext(ctx, `
			SELECT
				binary_count,
				complete_binary_count,
				complete_main_payload_binary_count,
				incomplete_binary_count,
				expected_file_count,
				expected_file_coverage_pct,
				has_expected_file_count,
				readiness_bucket
			FROM release_family_readiness_summaries
			WHERE provider_id = 1
			  AND newsgroup_id = $1
			  AND key_kind = $2
			  AND family_key = $3`,
			newsgroupID,
			keyKind,
			familyKey,
		).Scan(
			&binaryCount,
			&completeBinaryCount,
			&completeMainPayloadBinaryCount,
			&incompleteBinaryCount,
			&expectedFileCount,
			&expectedFileCoveragePct,
			&hasExpectedFileCount,
			&readinessBucket,
		); err != nil {
			t.Fatalf("query summary row for %s: %v", keyKind, err)
		}
		if binaryCount != 1 || completeBinaryCount != 1 || incompleteBinaryCount != 0 {
			t.Fatalf("unexpected summary counts for %s: binary=%d complete=%d incomplete=%d", keyKind, binaryCount, completeBinaryCount, incompleteBinaryCount)
		}
		if !hasExpectedFileCount {
			t.Fatalf("expected expected-file-count evidence for %s summary", keyKind)
		}
		if completeMainPayloadBinaryCount != 1 || expectedFileCount != 3 || expectedFileCoveragePct < 33 || expectedFileCoveragePct > 34 {
			t.Fatalf("unexpected expected-file coverage stats for %s: complete_main=%d expected=%d coverage=%v", keyKind, completeMainPayloadBinaryCount, expectedFileCount, expectedFileCoveragePct)
		}
		if readinessBucket != releaseReadinessActionable {
			t.Fatalf("expected actionable readiness for %s summary, got %q", keyKind, readinessBucket)
		}
	}
}

func TestUpsertBinaryRefreshesOldAndNewReleaseFamilySummariesWhenFamilyChanges(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.release.summary.move.%d", time.Now().UnixNano())
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

	oldFamily := fmt.Sprintf("summary-move-old-%d", time.Now().UnixNano())
	newFamily := fmt.Sprintf("summary-move-new-%d", time.Now().UnixNano())
	oldBaseStem := fmt.Sprintf("summary.move.old.%d", time.Now().UnixNano())
	newBaseStem := fmt.Sprintf("summary.move.new.%d", time.Now().UnixNano())
	binaryKey := fmt.Sprintf("summary-move::%d", time.Now().UnixNano())

	if _, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  oldFamily,
		ReleaseFamilyKey:  oldFamily,
		FileFamilyKey:     oldFamily + "::file",
		FamilyKind:        "archive_stem",
		BaseStem:          oldBaseStem,
		IsMainPayload:     true,
		ReleaseKey:        oldFamily,
		ReleaseName:       "Summary Move Old",
		BinaryKey:         binaryKey,
		BinaryName:        "summary.move.r00",
		FileName:          "summary.move.r00",
		ExpectedFileCount: 2,
		TotalParts:        10,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	}); err != nil {
		t.Fatalf("upsert binary in old family: %v", err)
	}

	if _, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		SourceReleaseKey:  newFamily,
		ReleaseFamilyKey:  newFamily,
		FileFamilyKey:     newFamily + "::file",
		FamilyKind:        "archive_stem",
		BaseStem:          newBaseStem,
		IsMainPayload:     true,
		ReleaseKey:        newFamily,
		ReleaseName:       "Summary Move New",
		BinaryKey:         binaryKey,
		BinaryName:        "summary.move.r00",
		FileName:          "summary.move.r00",
		ExpectedFileCount: 2,
		TotalParts:        10,
		MatchConfidence:   0.99,
		MatchStatus:       "matched",
	}); err != nil {
		t.Fatalf("upsert binary in new family: %v", err)
	}

	for _, row := range []struct {
		keyKind   string
		familyKey string
		wantCount int
	}{
		{keyKind: "release_family", familyKey: oldFamily, wantCount: 0},
		{keyKind: "base_stem", familyKey: strings.ToLower(oldBaseStem), wantCount: 0},
		{keyKind: "release_family", familyKey: newFamily, wantCount: 1},
		{keyKind: "base_stem", familyKey: strings.ToLower(newBaseStem), wantCount: 1},
	} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM release_family_readiness_summaries
			WHERE provider_id = 1
			  AND newsgroup_id = $1
			  AND key_kind = $2
			  AND family_key = $3`,
			newsgroupID,
			row.keyKind,
			row.familyKey,
		).Scan(&count); err != nil {
			t.Fatalf("query summary count for %s/%s: %v", row.keyKind, row.familyKey, err)
		}
		if count != row.wantCount {
			t.Fatalf("expected %d summary rows for %s/%s, got %d", row.wantCount, row.keyKind, row.familyKey, count)
		}
	}

	for _, row := range []struct {
		keyKind   string
		familyKey string
	}{
		{keyKind: "release_family", familyKey: oldFamily},
		{keyKind: "base_stem", familyKey: strings.ToLower(oldBaseStem)},
		{keyKind: "release_family", familyKey: newFamily},
		{keyKind: "base_stem", familyKey: strings.ToLower(newBaseStem)},
	} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM release_family_readiness_summaries s
			LEFT JOIN release_family_readiness_acks a
			  ON a.provider_id = s.provider_id
			 AND a.newsgroup_id = s.newsgroup_id
			 AND a.key_kind = s.key_kind
			 AND a.family_key = s.family_key
			WHERE s.provider_id = 1
			  AND newsgroup_id = $1
			  AND key_kind = $2
			  AND family_key = $3
			  AND s.updated_at > COALESCE(a.processed_at, TIMESTAMPTZ 'epoch')`,
			newsgroupID,
			row.keyKind,
			row.familyKey,
		).Scan(&count); err != nil {
			t.Fatalf("query summary queue count for %s/%s: %v", row.keyKind, row.familyKey, err)
		}
		if count != 1 {
			t.Fatalf("expected pending summary row for %s/%s after family move, got %d", row.keyKind, row.familyKey, count)
		}
	}
}

func TestSortReleaseFamilySummaryKeysUsesStableLockOrder(t *testing.T) {
	keys := []releaseFamilySummaryKey{
		{ProviderID: 2, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "b"},
		{ProviderID: 1, NewsgroupID: 2, KeyKind: "release_family", FamilyKey: "a"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "z"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "a"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "b"},
	}

	sortReleaseFamilySummaryKeys(keys)

	want := []releaseFamilySummaryKey{
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "a"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "b"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "z"},
		{ProviderID: 1, NewsgroupID: 2, KeyKind: "release_family", FamilyKey: "a"},
		{ProviderID: 2, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "b"},
	}

	if len(keys) != len(want) {
		t.Fatalf("unexpected key count: got %d want %d", len(keys), len(want))
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("unexpected key at %d: got %+v want %+v", i, keys[i], want[i])
		}
	}
}

func TestSortReleaseCandidateAcksUsesStableLockOrder(t *testing.T) {
	acks := []ReleaseCandidateAck{
		{ProviderID: 2, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "b"},
		{ProviderID: 1, NewsgroupID: 2, KeyKind: "release_family", FamilyKey: "a"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "z"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "a"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "b"},
	}

	sortReleaseCandidateAcks(acks)

	want := []ReleaseCandidateAck{
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "a"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "base_stem", FamilyKey: "b"},
		{ProviderID: 1, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "z"},
		{ProviderID: 1, NewsgroupID: 2, KeyKind: "release_family", FamilyKey: "a"},
		{ProviderID: 2, NewsgroupID: 1, KeyKind: "release_family", FamilyKey: "b"},
	}

	if len(acks) != len(want) {
		t.Fatalf("unexpected ack count: got %d want %d", len(acks), len(want))
	}
	for i := range want {
		if acks[i] != want[i] {
			t.Fatalf("unexpected ack at %d: got %+v want %+v", i, acks[i], want[i])
		}
	}
}

func TestUpsertBinaryPartsDeduplicatesConflictingBatchRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.binary.parts.dedupe.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
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
		if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, newsgroupID); err != nil {
			t.Fatalf("cleanup newsgroup: %v", err)
		}
	})

	now := time.Now().UTC()
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 1,
			MessageID:     fmt.Sprintf("<dedupe-a-%d@test>", time.Now().UnixNano()),
			Subject:       `"dedupe.part" yEnc (1/5)`,
			Poster:        "dedupe-a@example.com",
			DateUTC:       &now,
			Bytes:         100,
		},
		{
			ArticleNumber: 2,
			MessageID:     fmt.Sprintf("<dedupe-b-%d@test>", time.Now().UnixNano()),
			Subject:       `"dedupe.part" yEnc (1/5)`,
			Poster:        "dedupe-b@example.com",
			DateUTC:       &now,
			Bytes:         200,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT id, message_id
		FROM article_headers
		WHERE newsgroup_id = $1
		ORDER BY article_number`, newsgroupID)
	if err != nil {
		t.Fatalf("query article headers: %v", err)
	}
	defer rows.Close()

	type articleRow struct {
		id        int64
		messageID string
	}
	var articles []articleRow
	for rows.Next() {
		var row articleRow
		if err := rows.Scan(&row.id, &row.messageID); err != nil {
			t.Fatalf("scan article header: %v", err)
		}
		articles = append(articles, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate article headers: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 article headers, got %d", len(articles))
	}

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		SourceReleaseKey: "dedupe-family",
		ReleaseFamilyKey: "dedupe-family",
		FileFamilyKey:    "dedupe-family::file",
		FamilyKind:       "archive_stem",
		IsMainPayload:    true,
		ReleaseKey:       "dedupe-family",
		ReleaseName:      "Dedupe Family",
		BinaryKey:        fmt.Sprintf("dedupe-binary::%d", time.Now().UnixNano()),
		BinaryName:       "dedupe.part",
		FileName:         "dedupe.part",
		TotalParts:       5,
		MatchConfidence:  0.90,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.UpsertBinaryParts(ctx, []BinaryPartRecord{
		{
			BinaryID:        binaryID,
			ArticleHeaderID: articles[0].id,
			MessageID:       articles[0].messageID,
			PartNumber:      1,
			TotalParts:      5,
			SegmentBytes:    100,
			FileName:        "dedupe.part",
		},
		{
			BinaryID:        binaryID,
			ArticleHeaderID: articles[1].id,
			MessageID:       articles[1].messageID,
			PartNumber:      1,
			TotalParts:      5,
			SegmentBytes:    200,
			FileName:        "dedupe.part",
		},
	}); err != nil {
		t.Fatalf("upsert binary parts batch with duplicate part number: %v", err)
	}

	var (
		partCount          int
		winningHeaderID    int64
		winningSegmentSize int64
		assembledCount     int
	)
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(article_header_id), 0), COALESCE(MAX(segment_bytes), 0)
		FROM binary_parts
		WHERE binary_id = $1`, binaryID,
	).Scan(&partCount, &winningHeaderID, &winningSegmentSize); err != nil {
		t.Fatalf("query binary parts: %v", err)
	}
	if partCount != 1 {
		t.Fatalf("expected 1 deduped binary part row, got %d", partCount)
	}
	if winningHeaderID != articles[1].id {
		t.Fatalf("expected newer/larger duplicate to win, got article_header_id %d want %d", winningHeaderID, articles[1].id)
	}
	if winningSegmentSize != 200 {
		t.Fatalf("expected winning segment size 200, got %d", winningSegmentSize)
	}

	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_headers
		WHERE newsgroup_id = $1
		  AND assembled_at IS NOT NULL`, newsgroupID,
	).Scan(&assembledCount); err != nil {
		t.Fatalf("query assembled header count: %v", err)
	}
	if assembledCount != 2 {
		t.Fatalf("expected both duplicate headers to be marked assembled, got %d", assembledCount)
	}
}

func TestUniqueSortedArticleHeaderIDs(t *testing.T) {
	got := uniqueSortedArticleHeaderIDs([]BinaryPartRecord{
		{ArticleHeaderID: 42},
		{ArticleHeaderID: 7},
		{ArticleHeaderID: 42},
		{ArticleHeaderID: 0},
		{ArticleHeaderID: 19},
		{ArticleHeaderID: 7},
	})

	want := []int64{7, 19, 42}
	if len(got) != len(want) {
		t.Fatalf("unexpected id count: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected id at %d: got %d want %d", i, got[i], want[i])
		}
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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

func TestListUnassembledArticleHeadersPrefersNearCompleteMainPayloadMatches(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.assemble.completion.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	posterName := fmt.Sprintf("poster-completion-%d@example.com", time.Now().UnixNano())
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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

	nearCompleteFile := "release.good.part09.rar"
	lowProgressFile := "release.good.part10.rar"
	completeAuxFile := "release.sample.nfo"
	baseFamily := fmt.Sprintf("completion-priority-%d", time.Now().UnixNano())

	nearCompleteID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseFamily + "-near",
		ReleaseFamilyKey:  baseFamily + "-near",
		FileFamilyKey:     baseFamily + "-near::file",
		FamilyKind:        "archive_stem",
		BaseStem:          "release.good",
		IsMainPayload:     true,
		ReleaseKey:        baseFamily + "-near",
		ReleaseName:       "Near Complete Release",
		BinaryKey:         baseFamily + "-near::binary",
		BinaryName:        nearCompleteFile,
		FileName:          nearCompleteFile,
		FileIndex:         9,
		ExpectedFileCount: 10,
		TotalParts:        20,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert near-complete binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 18 WHERE id = $1`, nearCompleteID); err != nil {
		t.Fatalf("seed near-complete observed parts: %v", err)
	}

	lowProgressID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseFamily + "-low",
		ReleaseFamilyKey:  baseFamily + "-low",
		FileFamilyKey:     baseFamily + "-low::file",
		FamilyKind:        "archive_stem",
		BaseStem:          "release.good",
		IsMainPayload:     true,
		ReleaseKey:        baseFamily + "-low",
		ReleaseName:       "Low Progress Release",
		BinaryKey:         baseFamily + "-low::binary",
		BinaryName:        lowProgressFile,
		FileName:          lowProgressFile,
		FileIndex:         10,
		ExpectedFileCount: 10,
		TotalParts:        20,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert low-progress binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 3 WHERE id = $1`, lowProgressID); err != nil {
		t.Fatalf("seed low-progress observed parts: %v", err)
	}

	completeAuxID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseFamily + "-aux",
		ReleaseFamilyKey:  baseFamily + "-aux",
		FileFamilyKey:     baseFamily + "-aux::file",
		FamilyKind:        "auxiliary",
		BaseStem:          "release.good",
		IsAuxiliary:       true,
		ReleaseKey:        baseFamily + "-aux",
		ReleaseName:       "Complete Auxiliary Release",
		BinaryKey:         baseFamily + "-aux::binary",
		BinaryName:        completeAuxFile,
		FileName:          completeAuxFile,
		FileIndex:         0,
		ExpectedFileCount: 1,
		TotalParts:        5,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert complete auxiliary binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = total_parts WHERE id = $1`, completeAuxID); err != nil {
		t.Fatalf("seed complete auxiliary observed parts: %v", err)
	}

	baseTime := time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC)
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, []ArticleHeader{
		{
			ArticleNumber: 201,
			MessageID:     fmt.Sprintf("<near-complete-%d@test>", time.Now().UnixNano()),
			Subject:       `Near Complete [9/10] - "release.good.part09.rar" yEnc (19/20)`,
			Poster:        posterName,
			DateUTC:       &baseTime,
			Bytes:         4096,
			Lines:         40,
		},
		{
			ArticleNumber: 202,
			MessageID:     fmt.Sprintf("<low-progress-%d@test>", time.Now().UnixNano()),
			Subject:       `Low Progress [10/10] - "release.good.part10.rar" yEnc (4/20)`,
			Poster:        posterName,
			DateUTC:       func() *time.Time { t := baseTime.Add(time.Minute); return &t }(),
			Bytes:         4096,
			Lines:         40,
		},
		{
			ArticleNumber: 203,
			MessageID:     fmt.Sprintf("<complete-aux-%d@test>", time.Now().UnixNano()),
			Subject:       `Complete Aux [1/1] - "release.sample.nfo" yEnc (5/5)`,
			Poster:        posterName,
			DateUTC:       func() *time.Time { t := baseTime.Add(2 * time.Minute); return &t }(),
			Bytes:         512,
			Lines:         5,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	got, err := store.ListUnassembledArticleHeaders(ctx, 3)
	if err != nil {
		t.Fatalf("list unassembled article headers: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(got))
	}
	if got[0].FileName != nearCompleteFile {
		t.Fatalf("expected near-complete main payload first, got %q", got[0].FileName)
	}
	if got[1].FileName != lowProgressFile {
		t.Fatalf("expected lower-progress main payload second, got %q", got[1].FileName)
	}
	if got[2].FileName != completeAuxFile {
		t.Fatalf("expected already-complete auxiliary match after incomplete payloads, got %q", got[2].FileName)
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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

func TestListBinaryInspectionCandidatesInspectPAR2PrefersManifestOverVolumes(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.par2.manifest.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-par2-manifest-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("par2-manifest-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	files := []string{
		"example.par2",
		"example.vol00+01.par2",
		"example.vol02+03.par2",
	}
	for idx, name := range files {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			PosterID:          posterID,
			SourceReleaseKey:  baseKey,
			ReleaseFamilyKey:  baseKey,
			FileFamilyKey:     baseKey + "::par2",
			FamilyKind:        "par2",
			BaseStem:          "example",
			IsAuxiliary:       true,
			IsMainPayload:     false,
			ReleaseKey:        baseKey,
			ReleaseName:       "Example PAR2",
			BinaryKey:         fmt.Sprintf("%s::%d", baseKey, idx+1),
			BinaryName:        name,
			FileName:          name,
			FileIndex:         idx + 1,
			ExpectedFileCount: len(files),
			TotalParts:        1,
			PostedAt:          &now,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", name, err)
		}
		if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 1 WHERE id = $1`, binaryID); err != nil {
			t.Fatalf("seed observed_parts for %s: %v", name, err)
		}
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_par2", 20)
	if err != nil {
		t.Fatalf("list inspect par2 candidates: %v", err)
	}

	filtered := make([]BinaryInspectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.GroupName == baseKey {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) != 1 {
		t.Fatalf("expected one par2 candidate for manifest set, got %d: %+v", len(filtered), filtered)
	}
	if filtered[0].FileName != "example.par2" {
		t.Fatalf("expected manifest par2 candidate, got %+v", filtered[0])
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-archive-retryrow-%d@example.com", time.Now().UnixNano()))
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

func TestListBinaryInspectionCandidatesInspectArchiveRetriesCompletedMissingArticlesDetail(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.missingarticles.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-archive-missingarticles-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-missingarticles-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive Missing Articles Test",
		SourceTitle:             "Archive.Missing.Articles.Test",
		SearchTitle:             "archive missing articles test",
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
		BaseStem:          "archive.missing.articles",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Archive Missing Articles Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "archive.missing.articles.7z.001",
		FileName:          "archive.missing.articles.7z.001",
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
		FileName:  "archive.missing.articles.7z.001",
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
		Summary:         map[string]any{"probe_error_detail": "release file 922 has no articles", "probe_skip_reason": "not_archive_or_unsupported"},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete binary inspection: %v", err)
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
		t.Fatalf("expected missing-articles archive candidate to be retried")
	}
}

func TestListBinaryInspectionCandidatesInspectArchiveRetriesCompletedMetadataOnlyWithoutEntries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.archive.metadataonly.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-archive-metadataonly-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("archive-metadataonly-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Archive Metadata Only Retry Test",
		SourceTitle:             "Archive.Metadata.Only.Retry.Test",
		SearchTitle:             "archive metadata only retry test",
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
		BaseStem:          "archive.metadata.only",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Archive Metadata Only Retry Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "archive.metadata.only.7z.001",
		FileName:          "archive.metadata.only.7z.001",
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
		FileName:  "archive.metadata.only.7z.001",
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
		Summary:         map[string]any{"probe_strategy": "metadata_only", "archive_entries": []any{}},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete archive inspection: %v", err)
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
		t.Fatalf("expected metadata-only archive candidate without entries to be retried")
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
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-archive-recoverable-%d@example.com", time.Now().UnixNano()))
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
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
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

func TestListBinaryInspectionCandidatesInspectPAR2RerunsWhenTargetsMissing(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.par2.retry.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-par2-retry-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("par2-rerun-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::par2",
		FamilyKind:        "par2",
		BaseStem:          "example",
		IsAuxiliary:       true,
		IsMainPayload:     false,
		ReleaseKey:        baseKey,
		ReleaseName:       "Example PAR2 Retry",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "example.par2",
		FileName:          "example.par2",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 1 WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("seed observed_parts: %v", err)
	}

	if err := store.ReplaceBinaryPAR2Sets(ctx, binaryID, []BinaryPAR2SetRecord{{
		BinaryID:    binaryID,
		SetName:     "example.par2",
		BaseName:    "example.par2",
		SignatureOK: true,
	}}); err != nil {
		t.Fatalf("seed par2 set: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_par2",
		BinaryID:        binaryID,
		Status:          "completed",
		Summary:         map[string]any{"has_par2": true},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete par2 inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_par2", 20)
	if err != nil {
		t.Fatalf("list inspect par2 candidates: %v", err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected inspect_par2 candidate to rerun when targets are missing")
	}
}

func TestListBinaryInspectionCandidatesInspectPAR2SkipsCompletedMissingArticleProbe(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.par2.missingarticle.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-par2-missingarticle-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("par2-missingarticle-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::par2",
		FamilyKind:        "par2",
		BaseStem:          "example",
		IsAuxiliary:       true,
		IsMainPayload:     false,
		ReleaseKey:        baseKey,
		ReleaseName:       "Example PAR2 Missing Article",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "example.par2",
		FileName:          "example.par2",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		PostedAt:          &now,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 1 WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("seed observed_parts: %v", err)
	}

	if err := store.ReplaceBinaryPAR2Sets(ctx, binaryID, []BinaryPAR2SetRecord{{
		BinaryID:    binaryID,
		SetName:     "example.par2",
		BaseName:    "example.par2",
		SignatureOK: true,
	}}); err != nil {
		t.Fatalf("seed par2 set: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_par2",
		BinaryID:        binaryID,
		Status:          "completed",
		Summary:         map[string]any{"has_par2": true, "probe_skip_reason": "prefix_sample_failed", "probe_error_detail": "fetch article <abc@example>: article not found (430)"},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete par2 inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_par2", 20)
	if err != nil {
		t.Fatalf("list inspect par2 candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			t.Fatalf("did not expect completed missing-article probe to rerun, got %+v", candidate)
		}
	}
}

func TestListBinaryInspectionCandidatesInspectPAR2SkipsCompletedZeroTargetVolumeSets(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.par2.zero-targets.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-par2-zero-targets-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("par2-zero-targets-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::par2",
		FamilyKind:        "par2",
		BaseStem:          "example",
		IsAuxiliary:       true,
		IsMainPayload:     false,
		ReleaseKey:        baseKey,
		ReleaseName:       "Example PAR2 Zero Targets",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "example.vol00+01.par2",
		FileName:          "example.vol00+01.par2",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		PostedAt:          &now,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 1 WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("seed observed_parts: %v", err)
	}

	if err := store.ReplaceBinaryPAR2Sets(ctx, binaryID, []BinaryPAR2SetRecord{{
		BinaryID:     binaryID,
		SetName:      "example.vol00+01.par2",
		BaseName:     "example.par2",
		IsVolume:     true,
		VolumeNumber: 0,
		SignatureOK:  true,
	}}); err != nil {
		t.Fatalf("seed par2 set: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_par2",
		BinaryID:        binaryID,
		Status:          "completed",
		Summary:         map[string]any{"has_par2": true, "target_count": 0},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete par2 inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_par2", 20)
	if err != nil {
		t.Fatalf("list inspect par2 candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			t.Fatalf("did not expect completed zero-target volume candidate to rerun, got %+v", candidate)
		}
	}
}

func TestListBinaryInspectionCandidatesInspectDiscoveryIncludesStandaloneOpaqueBinary(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.discovery.standalone.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-discovery-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("discovery-standalone-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		PosterID:         posterID,
		SourceReleaseKey: baseKey,
		ReleaseFamilyKey: baseKey,
		FileFamilyKey:    baseKey + "::file",
		FamilyKind:       "opaque_set",
		BaseStem:         baseKey,
		IsMainPayload:    true,
		IsAuxiliary:      false,
		ReleaseKey:       baseKey,
		ReleaseName:      "Standalone Discovery",
		BinaryKey:        baseKey + "::binary",
		BinaryName:       "standalone-discovery.bin",
		FileName:         "standalone-discovery.bin",
		FileIndex:        1,
		TotalParts:       1,
		PostedAt:         &now,
		MatchConfidence:  0.82,
		MatchStatus:      "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_discovery", 20)
	if err != nil {
		t.Fatalf("list inspect discovery candidates: %v", err)
	}

	found := false
	for _, candidate := range candidates {
		if candidate.BinaryID != binaryID {
			continue
		}
		found = true
		if candidate.ReleaseID != "" {
			t.Fatalf("expected standalone discovery candidate to have empty release id, got %+v", candidate)
		}
		if candidate.FileName != "standalone-discovery.bin" {
			t.Fatalf("expected standalone file name, got %+v", candidate)
		}
	}
	if !found {
		t.Fatalf("expected standalone opaque binary to be discoverable, got %d candidates", len(candidates))
	}
}

func TestListBinaryInspectionCandidatesInspectMediaToleratesScalarArchiveEntries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("inspectmediascalar%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, nil)
	newsgroupID, err := store.EnsureNewsgroup(ctx, record.GroupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-media-scalar-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}
	baseKey := fmt.Sprintf("inspect-media-scalar-%d", time.Now().UnixNano())

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  baseKey,
		ReleaseFamilyKey:  baseKey,
		FileFamilyKey:     baseKey + "::archive",
		FamilyKind:        "archive_stem",
		BaseStem:          "media.scalar",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Media Scalar Summary Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "media.scalar.7z.001",
		FileName:          "media.scalar.7z.001",
		FileIndex:         1,
		ExpectedFileCount: 1,
		TotalParts:        1,
		PostedAt:          record.PostedAt,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "media.scalar.7z.001",
		SizeBytes: 716800,
		FileIndex: 1,
		PostedAt:  record.PostedAt,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName: "inspect_archive",
		BinaryID:  binaryID,
		ReleaseID: releaseID,
		Status:    "completed",
		Summary:   map[string]any{"archive_entries": "not-an-array"},
	}); err != nil {
		t.Fatalf("complete archive inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_media", 20)
	if err != nil {
		t.Fatalf("list inspect media candidates with scalar archive_entries: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.BinaryID == binaryID {
			t.Fatalf("did not expect archive-only media candidate when archive_entries is malformed scalar: %+v", candidate)
		}
	}
}

func TestListBinaryInspectionCandidatesInspectPAR2SkipsVolumesWhenManifestAlreadyHasTargets(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.inspect.par2.targets.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-par2-targets-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("par2-targets-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	files := []string{
		"example.par2",
		"example.vol00+01.par2",
		"example.vol02+03.par2",
	}
	ids := make([]int64, 0, len(files))
	for idx, name := range files {
		binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
			ProviderID:        1,
			NewsgroupID:       newsgroupID,
			PosterID:          posterID,
			SourceReleaseKey:  baseKey,
			ReleaseFamilyKey:  baseKey,
			FileFamilyKey:     baseKey + "::par2",
			FamilyKind:        "par2",
			BaseStem:          "example",
			IsAuxiliary:       true,
			IsMainPayload:     false,
			ReleaseKey:        baseKey,
			ReleaseName:       "Example PAR2 Targets",
			BinaryKey:         fmt.Sprintf("%s::%d", baseKey, idx+1),
			BinaryName:        name,
			FileName:          name,
			FileIndex:         idx + 1,
			ExpectedFileCount: len(files),
			TotalParts:        1,
			PostedAt:          &now,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
		if err != nil {
			t.Fatalf("upsert binary %s: %v", name, err)
		}
		if _, err := store.DB().ExecContext(ctx, `UPDATE binaries SET observed_parts = 1 WHERE id = $1`, binaryID); err != nil {
			t.Fatalf("seed observed_parts for %s: %v", name, err)
		}
		ids = append(ids, binaryID)
	}

	manifestID := ids[0]
	if err := store.ReplaceBinaryPAR2Sets(ctx, manifestID, []BinaryPAR2SetRecord{{
		BinaryID:    manifestID,
		SetName:     "example.par2",
		BaseName:    "example.par2",
		SignatureOK: true,
	}}); err != nil {
		t.Fatalf("seed manifest par2 set: %v", err)
	}
	if err := store.ReplaceBinaryPAR2Targets(ctx, manifestID, []BinaryPAR2TargetRecord{{
		BinaryID: manifestID,
		FileName: "target.part01.rar",
		FileSize: 123456,
	}}); err != nil {
		t.Fatalf("seed manifest par2 targets: %v", err)
	}
	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName:       "inspect_par2",
		BinaryID:        manifestID,
		Status:          "completed",
		Summary:         map[string]any{"has_par2": true, "target_count": 1},
		SourceUpdatedAt: &now,
	}); err != nil {
		t.Fatalf("complete manifest par2 inspection: %v", err)
	}

	candidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_par2", 20)
	if err != nil {
		t.Fatalf("list inspect par2 candidates: %v", err)
	}

	for _, candidate := range candidates {
		if candidate.GroupName != baseKey {
			continue
		}
		t.Fatalf("expected no inspect_par2 rerun for manifest-covered set, got %+v", candidate)
	}
}

func TestRefreshIndexerDashboardStatsPersistsCachedCounts(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	before, err := store.RefreshIndexerDashboardStats(ctx)
	if err != nil {
		t.Fatalf("refresh baseline dashboard stats: %v", err)
	}
	beforeByKey := make(map[string]IndexerDashboardStat, len(before.Items))
	for _, item := range before.Items {
		beforeByKey[item.Key] = item
	}

	groupName := fmt.Sprintf("alt.test.dashboard.stats.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterName := fmt.Sprintf("poster-dashboard-stats-%d@example.com", time.Now().UnixNano())
	posterID, err := ensureTestPoster(t, store, ctx, posterName)
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	baseKey := fmt.Sprintf("dashboard-stats-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	releaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:              1,
		SourceReleaseKey:        baseKey,
		ReleaseFamilyKey:        baseKey,
		ReleaseKey:              baseKey,
		GroupName:               groupName,
		Title:                   "Dashboard Stats Test",
		SourceTitle:             "Dashboard.Stats.Test",
		SearchTitle:             "dashboard stats test",
		Category:                "usenet",
		Classification:          "video",
		Poster:                  posterName,
		FileCount:               1,
		ExpectedFileCount:       1,
		CompletionPct:           100,
		MatchConfidence:         0.95,
		IdentityStatus:          "identified",
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
		FileFamilyKey:     baseKey + "::video",
		FamilyKind:        "file_name",
		BaseStem:          "dashboard.stats",
		IsMainPayload:     true,
		ReleaseKey:        baseKey,
		ReleaseName:       "Dashboard Stats Test",
		BinaryKey:         baseKey + "::binary",
		BinaryName:        "dashboard.stats.sample.mkv",
		FileName:          "dashboard.stats.sample.mkv",
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
		FileName:  "dashboard.stats.sample.mkv",
		SizeBytes: 734003200,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_ready_candidates (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count,
			expected_file_count, expected_archive_file_count,
			has_expected_file_count, has_expected_archive_file_count,
			expected_file_coverage_pct, archive_file_coverage_pct,
			total_bytes, earliest_posted_at, ready_reason, updated_at
		)
		VALUES ($1, $2, 'release_family', $3, $3, $3, $4, 1, 1, 1, 1, 0, true, false, 100, 0, 734003200, NOW(), 'actionable', NOW())
		ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
		SET updated_at = NOW(),
		    ready_reason = 'actionable'`,
		1, newsgroupID, baseKey, "Dashboard Stats Test",
	); err != nil {
		t.Fatalf("insert ready release candidate: %v", err)
	}

	after, err := store.RefreshIndexerDashboardStats(ctx)
	if err != nil {
		t.Fatalf("refresh updated dashboard stats: %v", err)
	}
	afterByKey := make(map[string]IndexerDashboardStat, len(after.Items))
	for _, item := range after.Items {
		afterByKey[item.Key] = item
	}

	mediaBefore := beforeByKey["pending_inspect_media_binaries"].Value
	mediaAfter, ok := afterByKey["pending_inspect_media_binaries"]
	if !ok {
		t.Fatalf("missing pending_inspect_media_binaries stat")
	}
	if !mediaAfter.Available || mediaAfter.UpdatedAt == nil {
		t.Fatalf("expected pending_inspect_media_binaries to be cached, got %#v", mediaAfter)
	}
	if mediaAfter.Value < mediaBefore+1 {
		t.Fatalf("expected media backlog to increase by at least 1, before=%d after=%d", mediaBefore, mediaAfter.Value)
	}

	releaseBefore := beforeByKey["pending_release_candidate_families"].Value
	releaseAfter, ok := afterByKey["pending_release_candidate_families"]
	if !ok {
		t.Fatalf("missing pending_release_candidate_families stat")
	}
	if !releaseAfter.Available || releaseAfter.UpdatedAt == nil {
		t.Fatalf("expected pending_release_candidate_families to be cached, got %#v", releaseAfter)
	}
	if !releaseAfter.Exact {
		t.Fatalf("expected pending_release_candidate_families to remain exact")
	}
	if releaseAfter.Value < releaseBefore+1 {
		t.Fatalf("expected release backlog to increase by at least 1, before=%d after=%d", releaseBefore, releaseAfter.Value)
	}
	releaseSummaryRefreshBefore := beforeByKey["pending_release_summary_refresh_summaries"].Value
	releaseSummaryRefreshAfter, ok := afterByKey["pending_release_summary_refresh_summaries"]
	if !ok {
		t.Fatalf("missing pending_release_summary_refresh_summaries stat")
	}
	if !releaseSummaryRefreshAfter.Available || releaseSummaryRefreshAfter.UpdatedAt == nil {
		t.Fatalf("expected pending_release_summary_refresh_summaries to be cached, got %#v", releaseSummaryRefreshAfter)
	}
	if !releaseSummaryRefreshAfter.Exact {
		t.Fatalf("expected pending_release_summary_refresh_summaries to remain exact")
	}
	if releaseSummaryRefreshAfter.Value < releaseSummaryRefreshBefore+1 {
		t.Fatalf("expected release summary refresh backlog to increase by at least 1, before=%d after=%d", releaseSummaryRefreshBefore, releaseSummaryRefreshAfter.Value)
	}

	for _, key := range []string{
		"unassembled_headers",
		"pending_release_summary_refresh_summaries",
		"pending_release_candidate_families",
		"pending_yenc_recovery_binaries",
		"pending_inspect_discovery_binaries",
		"pending_inspect_par2_binaries",
		"pending_inspect_nfo_binaries",
		"pending_inspect_archive_binaries",
		"pending_inspect_password_binaries",
		"pending_inspect_media_binaries",
	} {
		stat, ok := afterByKey[key]
		if !ok || !stat.Available || stat.UpdatedAt == nil {
			t.Fatalf("expected %s stat to be cached, got %#v", key, stat)
		}
	}
	for _, key := range []string{
		"unassembled_headers",
		"pending_inspect_discovery_binaries",
		"pending_inspect_nfo_binaries",
		"pending_inspect_archive_binaries",
		"pending_inspect_password_binaries",
		"pending_inspect_media_binaries",
	} {
		if stat := afterByKey[key]; stat.Exact {
			t.Fatalf("expected %s to be marked as estimated, got %#v", key, stat)
		}
	}
	for _, key := range []string{
		"pending_release_summary_refresh_summaries",
		"pending_inspect_par2_binaries",
	} {
		if stat := afterByKey[key]; !stat.Exact {
			t.Fatalf("expected %s to be marked as exact, got %#v", key, stat)
		}
	}
	if stat := afterByKey["pending_yenc_recovery_binaries"]; stat.Exact {
		t.Fatalf("expected pending_yenc_recovery_binaries to be marked as estimated, got %#v", stat)
	}

	for _, key := range []string{
		"payload_rows",
		"payload_bytes",
		"payload_dead_tuples",
		"grouping_evidence_rows",
		"grouping_evidence_bytes",
		"grouping_evidence_dead_tuples",
		"readiness_rows",
		"readiness_bytes",
		"readiness_dead_tuples",
	} {
		if stat, ok := afterByKey[key]; ok {
			t.Fatalf("did not expect storage diagnostic stat %s in dashboard backlog, got %#v", key, stat)
		}
	}
}

func TestGetIndexerStageThroughputIncludesScrapeBurstMetrics(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	startedAt := time.Now().UTC().Add(-30 * time.Minute)
	finishedAt := startedAt.Add(2 * time.Minute)
	metrics := `{"articles_inserted":12000,"workers_used":8,"groups_scheduled":8,"ranges_fetched":8}`
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO indexer_stage_runs (
			stage_name,
			trigger_kind,
			status,
			claimed_by,
			started_at,
			heartbeat_at,
			finished_at,
			error_text,
			metrics_json
		) VALUES (
			'scrape_latest',
			'scheduled',
			'completed',
			'test-owner',
			$1,
			$1,
			$2,
			'',
			$3::jsonb
		)`,
		startedAt,
		finishedAt,
		metrics,
	); err != nil {
		t.Fatalf("insert scrape stage run: %v", err)
	}

	throughput, err := store.GetIndexerStageThroughput(ctx)
	if err != nil {
		t.Fatalf("get indexer stage throughput: %v", err)
	}

	var scrapeLatest *IndexerStageThroughputItem
	for i := range throughput.Items {
		if throughput.Items[i].StageName == "scrape_latest" {
			scrapeLatest = &throughput.Items[i]
			break
		}
	}
	if scrapeLatest == nil {
		t.Fatal("expected scrape_latest throughput item")
	}
	if len(scrapeLatest.Windows) == 0 {
		t.Fatal("expected scrape_latest throughput windows")
	}
	window := scrapeLatest.Windows[0]
	if window.MaxWorkersUsed != 8 || window.MaxGroupsScheduled != 8 || window.MaxRangesFetched != 8 {
		t.Fatalf("expected scrape burst maxima to round-trip, got %+v", window)
	}
	if window.AvgWorkersUsed != 8 || window.AvgGroupsScheduled != 8 || window.AvgRangesFetched != 8 {
		t.Fatalf("expected scrape burst averages to round-trip, got %+v", window)
	}
}

func TestGetIndexerOverviewCountsArchivedNZBsSeparatelyFromLiveReadyNZBs(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("overviewarchived%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, nil)
	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{
		{
			FileName:  fmt.Sprintf("%s.mkv", token),
			SizeBytes: 1024,
			FileIndex: 1,
			PostedAt:  record.PostedAt,
		},
	}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}
	groupID, err := store.EnsureNewsgroup(ctx, fmt.Sprintf("alt.test.overview.archived.%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	if err := store.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{groupID}); err != nil {
		t.Fatalf("replace release newsgroups: %v", err)
	}
	if err := store.UpsertNZBCache(ctx, releaseID, "ready", "hash-"+token, ""); err != nil {
		t.Fatalf("upsert nzb cache: %v", err)
	}
	if err := store.MarkReleaseArchiveStored(ctx, ReleaseArchiveStoredRecord{
		ReleaseID:         releaseID,
		ArchiveStore:      "indexer_archive",
		ObjectStoreKind:   "fs",
		ObjectKey:         fmt.Sprintf("releases/1/%s/test.nzb", releaseID),
		ContentHashSHA256: fmt.Sprintf("hash-%s", token),
		ObjectSizeBytes:   1024,
		ContentEncoding:   "identity",
		SourceModule:      "usenet_index",
	}); err != nil {
		t.Fatalf("mark release archive stored: %v", err)
	}

	overview, err := store.GetIndexerOverview(ctx)
	if err != nil {
		t.Fatalf("get indexer overview: %v", err)
	}
	if overview.ArchivedNZBCount < 1 {
		t.Fatalf("expected archived nzb count to include seeded release, got %+v", overview)
	}

	if _, err := store.PurgeArchivedReleaseSources(ctx, releaseID); err != nil {
		t.Fatalf("purge archived release sources: %v", err)
	}

	overview, err = store.GetIndexerOverview(ctx)
	if err != nil {
		t.Fatalf("get indexer overview after purge: %v", err)
	}
	if overview.ArchivedNZBCount < 1 {
		t.Fatalf("expected archived nzb count to persist after purge, got %+v", overview)
	}
}

func TestCatalogReleaseFileReadsTolerateNullBinaryID(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("nullbinary%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, nil)
	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{
		{
			FileName:  fmt.Sprintf("%s.mkv", token),
			SizeBytes: 1024,
			FileIndex: 1,
			PostedAt:  record.PostedAt,
		},
	}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	var fileID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM release_catalog_files WHERE release_id = $1 LIMIT 1`, releaseID).Scan(&fileID); err != nil {
		t.Fatalf("lookup release catalog file id: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE release_files
		SET binary_id = NULL
		WHERE release_id = $1`, releaseID,
	); err != nil {
		t.Fatalf("null release file binary id: %v", err)
	}

	files, err := store.ListCatalogReleaseFiles(ctx, releaseID)
	if err != nil {
		t.Fatalf("list catalog release files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].BinaryID != 0 {
		t.Fatalf("expected zero binary id after null scan, got %d", files[0].BinaryID)
	}

	articles, err := store.ListCatalogReleaseFileArticles(ctx, fileID)
	if err != nil {
		t.Fatalf("list catalog release file articles: %v", err)
	}
	if len(articles) != 0 {
		t.Fatalf("expected 0 articles for null binary id, got %d", len(articles))
	}
}

func TestCompleteBinaryInspectionToleratesDeletedBinaryWithExistingRow(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      1,
		SourceReleaseKey: "finish-stale-source",
		ReleaseFamilyKey: "finish-stale-family",
		ReleaseKey:       "finish-stale-family",
		ReleaseName:      "Finish Stale Family",
		BinaryKey:        "finish-stale-binary",
		BinaryName:       "finish-stale-binary",
		FileName:         "finish-stale.mkv",
		FileIndex:        1,
		TotalParts:       2,
		MatchConfidence:  0.9,
		MatchStatus:      "identified",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if err := store.StartBinaryInspection(ctx, "inspect_media", binaryID, "", nil); err != nil {
		t.Fatalf("start binary inspection: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("delete binary: %v", err)
	}

	if err := store.CompleteBinaryInspection(ctx, BinaryInspectionRecord{
		StageName: "inspect_media",
		BinaryID:  binaryID,
		Status:    "completed",
		Summary: map[string]any{
			"probe_mode": "heuristic",
		},
	}); err != nil {
		t.Fatalf("complete binary inspection after delete: %v", err)
	}

	var status string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT status
		FROM binary_inspections
		WHERE stage_name = 'inspect_media' AND binary_id = $1`, binaryID).Scan(&status); err != nil {
		t.Fatalf("read completed inspection row: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected completed inspection status, got %q", status)
	}
}

func TestReplaceInspectionArtifactsToleratesDeletedBinary(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	binaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      1,
		SourceReleaseKey: "artifact-stale-source",
		ReleaseFamilyKey: "artifact-stale-family",
		ReleaseKey:       "artifact-stale-family",
		ReleaseName:      "Artifact Stale Family",
		BinaryKey:        "artifact-stale-binary",
		BinaryName:       "artifact-stale-binary",
		FileName:         "artifact-stale.mkv",
		FileIndex:        1,
		TotalParts:       2,
		MatchConfidence:  0.9,
		MatchStatus:      "identified",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM binaries WHERE id = $1`, binaryID); err != nil {
		t.Fatalf("delete binary: %v", err)
	}

	if err := store.ReplaceBinaryInspectionArtifacts(ctx, "inspect_media", binaryID, []BinaryInspectionArtifactRecord{{
		ArtifactRole: "preview",
		ArtifactName: "screen0001.jpg",
		ArtifactPath: "Screens/screen0001.jpg",
		MIMEType:     "image/jpeg",
		SourceKind:   "archive_image",
	}}); err != nil {
		t.Fatalf("replace binary inspection artifacts after delete: %v", err)
	}

	if err := store.ReplaceBinaryMediaStreams(ctx, binaryID, []BinaryMediaStreamRecord{{
		StreamIndex: 0,
		StreamType:  "video",
		CodecName:   "h264",
	}}); err != nil {
		t.Fatalf("replace binary media streams after delete: %v", err)
	}

	if err := store.ReplaceBinaryArchiveEntries(ctx, binaryID, []BinaryArchiveEntryRecord{{
		EntryName: "Screens/screen0001.jpg",
		MediaType: "image/jpeg",
	}}); err != nil {
		t.Fatalf("replace binary archive entries after delete: %v", err)
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
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-releasefiles-%d@example.com", now.UnixNano()))
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

func TestDeleteAuxiliaryOnlySiblingReleasesKeepsPrimaryRelease(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.aux.sibling.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	posterID, err := ensureTestPoster(t, store, ctx, fmt.Sprintf("poster-aux-sibling-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure poster: %v", err)
	}

	now := time.Now().UTC()
	mainReleaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:        1,
		ReleaseKey:        fmt.Sprintf("main-release-%d", now.UnixNano()),
		GroupName:         groupName,
		Title:             "Main Release Test",
		SourceTitle:       "Main.Release.Test",
		TitleSource:       "source",
		SearchTitle:       "main release test",
		Category:          "usenet",
		Classification:    "archive",
		Poster:            "poster-main",
		PostedAt:          &now,
		FileCount:         2,
		ExpectedFileCount: 2,
		CompletionPct:     100,
		MatchConfidence:   0.9,
		IdentityStatus:    "identified",
		AvailabilityScore: 100,
		AvailabilityTier:  "excellent",
		MetadataUpdatedAt: &now,
	})
	if err != nil {
		t.Fatalf("upsert main release: %v", err)
	}
	auxReleaseID, err := store.UpsertRelease(ctx, ReleaseRecord{
		ProviderID:        1,
		ReleaseKey:        fmt.Sprintf("aux-release-%d", now.UnixNano()),
		GroupName:         groupName + ".aux",
		Title:             "Aux Release Test",
		SourceTitle:       "Aux.Release.Test",
		TitleSource:       "source",
		SearchTitle:       "aux release test",
		Category:          "usenet",
		Classification:    "archive",
		Poster:            "poster-aux",
		PostedAt:          &now,
		FileCount:         1,
		ExpectedFileCount: 2,
		CompletionPct:     100,
		MatchConfidence:   0.9,
		IdentityStatus:    "identified",
		AvailabilityScore: 100,
		AvailabilityTier:  "excellent",
		MetadataUpdatedAt: &now,
	})
	if err != nil {
		t.Fatalf("upsert aux release: %v", err)
	}

	mainBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  "example release 2026",
		ReleaseFamilyKey:  "example release 2026",
		FileFamilyKey:     "example release 2026",
		FamilyKind:        "readable_title",
		BaseStem:          "example release 2026",
		IsMainPayload:     true,
		ReleaseKey:        "example release 2026",
		ReleaseName:       "Example.Release.2026",
		BinaryKey:         "example release 2026::main",
		BinaryName:        "Example.Release.2026.part1.rar",
		FileName:          "Example.Release.2026.part1.rar",
		FileIndex:         1,
		ExpectedFileCount: 2,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert main binary: %v", err)
	}
	auxBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		PosterID:          posterID,
		SourceReleaseKey:  "example release 2026",
		ReleaseFamilyKey:  "example release 2026 par2",
		FileFamilyKey:     "example release 2026 par2",
		FamilyKind:        "readable_title",
		BaseStem:          "example release 2026",
		IsAuxiliary:       true,
		IsMainPayload:     false,
		ReleaseKey:        "example release 2026 par2",
		ReleaseName:       "Example.Release.2026",
		BinaryKey:         "example release 2026::par2",
		BinaryName:        "Example.Release.2026.par2",
		FileName:          "Example.Release.2026.par2",
		FileIndex:         1,
		ExpectedFileCount: 2,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
	})
	if err != nil {
		t.Fatalf("upsert aux binary: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, mainReleaseID, []ReleaseFileRecord{{
		BinaryID:  mainBinaryID,
		FileName:  "Example.Release.2026.part1.rar",
		SizeBytes: 1000,
		FileIndex: 1,
	}}); err != nil {
		t.Fatalf("replace main release files: %v", err)
	}
	if err := store.ReplaceReleaseFiles(ctx, auxReleaseID, []ReleaseFileRecord{{
		BinaryID:  auxBinaryID,
		FileName:  "Example.Release.2026.par2",
		SizeBytes: 100,
		FileIndex: 1,
		IsPars:    true,
	}}); err != nil {
		t.Fatalf("replace aux release files: %v", err)
	}

	if err := store.DeleteAuxiliaryOnlySiblingReleases(ctx, 1, newsgroupID, "example release 2026", []string{mainReleaseID}); err != nil {
		t.Fatalf("delete auxiliary-only sibling releases: %v", err)
	}

	var mainCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE release_id = $1`, mainReleaseID).Scan(&mainCount); err != nil {
		t.Fatalf("count main release: %v", err)
	}
	if mainCount != 1 {
		t.Fatalf("expected main release to remain, got %d rows", mainCount)
	}
	var auxCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE release_id = $1`, auxReleaseID).Scan(&auxCount); err != nil {
		t.Fatalf("count aux release: %v", err)
	}
	if auxCount != 0 {
		t.Fatalf("expected auxiliary-only sibling release to be deleted, got %d rows", auxCount)
	}
}

func TestRunIndexerMaintenancePurgesArticleHeaderPayloadsByStagedRetention(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.maintenance.payloads.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	headers := []ArticleHeader{
		{ArticleNumber: 1001, MessageID: "<payload-1001@test>", Subject: `KeepFast [1/1] - "keepfast.rar" yEnc (1/10)`, Poster: "poster@test", Xref: groupName + ":1001"},
		{ArticleNumber: 1002, MessageID: "<payload-1002@test>", Subject: `NeedsStructuredRecovery placeholder`, Poster: "poster@test", Xref: groupName + ":1002"},
		{ArticleNumber: 1003, MessageID: "<payload-1003@test>", Subject: `DropMissingIdentity placeholder`, Poster: "poster@test", Xref: groupName + ":1003"},
		{ArticleNumber: 1004, MessageID: "<payload-1004@test>", Subject: `RetryLater [1/1] - "retrylater.rar" yEnc (1/10)`, Poster: "poster@test", Xref: groupName + ":1004"},
		{ArticleNumber: 1005, MessageID: "<payload-1005@test>", Subject: `FreshAssembled [1/1] - "freshassembled.rar" yEnc (1/10)`, Poster: "poster@test", Xref: groupName + ":1005"},
		{ArticleNumber: 1006, MessageID: "<payload-1006@test>", Subject: `PendingUnassembled [1/1] - "pendingunassembled.rar" yEnc (1/10)`, Poster: "poster@test", Xref: groupName + ":1006"},
	}
	if _, err := store.InsertArticleHeaders(ctx, 1, newsgroupID, headers); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE article_headers
		SET assembled_at = CASE article_number
			WHEN 1001 THEN NOW() - INTERVAL '2 hours'
			WHEN 1002 THEN NOW() - INTERVAL '2 hours'
			WHEN 1003 THEN NOW() - INTERVAL '25 hours'
			WHEN 1004 THEN NOW() - INTERVAL '25 hours'
			WHEN 1005 THEN NOW() - INTERVAL '30 minutes'
			ELSE assembled_at
		END
		WHERE newsgroup_id = $1`, newsgroupID,
	); err != nil {
		t.Fatalf("update assembled_at: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE article_header_ingest_payloads p
		SET yenc_recovery_missing_count = 2,
		    yenc_recovery_last_missing_at = NOW() - INTERVAL '2 hours',
		    yenc_recovery_retry_after = NOW() + INTERVAL '2 hours'
		FROM article_headers ah
		WHERE ah.id = p.article_header_id
		  AND ah.newsgroup_id = $1
		  AND ah.article_number = 1004`, newsgroupID,
	); err != nil {
		t.Fatalf("update payload retry state: %v", err)
	}

	out, err := store.RunIndexerMaintenance(ctx)
	if err != nil {
		t.Fatalf("run indexer maintenance: %v", err)
	}
	if out == nil {
		t.Fatalf("expected maintenance result")
	}
	if out.PurgedHeaderPayloads != 3 {
		t.Fatalf("expected 3 purged payload rows, got %+v", out)
	}

	type payloadState struct {
		articleNumber int64
		hasPayload    bool
	}
	rows, err := store.DB().QueryContext(ctx, `
		SELECT ah.article_number, (p.article_header_id IS NOT NULL) AS has_payload
		FROM article_headers ah
		LEFT JOIN article_header_ingest_payloads p ON p.article_header_id = ah.id
		WHERE ah.newsgroup_id = $1
		ORDER BY ah.article_number`, newsgroupID)
	if err != nil {
		t.Fatalf("query payload states: %v", err)
	}
	defer rows.Close()

	got := make(map[int64]bool, 6)
	for rows.Next() {
		var item payloadState
		if err := rows.Scan(&item.articleNumber, &item.hasPayload); err != nil {
			t.Fatalf("scan payload state: %v", err)
		}
		got[item.articleNumber] = item.hasPayload
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate payload states: %v", err)
	}

	want := map[int64]bool{
		1001: false,
		1002: true,
		1003: false,
		1004: false,
		1005: true,
		1006: true,
	}
	for articleNumber, hasPayload := range want {
		if got[articleNumber] != hasPayload {
			t.Fatalf("unexpected payload retention for article %d: got=%t want=%t", articleNumber, got[articleNumber], hasPayload)
		}
	}
}

func TestRunIndexerMaintenancePurgesNonPendingReadinessResidue(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.maintenance.readiness.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	if _, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:       1,
		NewsgroupID:      newsgroupID,
		ReleaseFamilyKey: "keep-weak-family",
		FileFamilyKey:    "keep-weak-family::part",
		FamilyKind:       "contextual_obfuscated",
		BaseStem:         "keep-weak-family",
		IsMainPayload:    true,
		ReleaseKey:       "keep-weak-family",
		ReleaseName:      "Keep Weak Family",
		BinaryKey:        "keep-weak-family::binary",
		BinaryName:       "keep-weak-family.bin",
		FileName:         "keep-weak-family.bin",
		TotalParts:       1,
		MatchConfidence:  0.70,
		MatchStatus:      "matched",
	}); err != nil {
		t.Fatalf("upsert weak-family keeper binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', 'drop-weak-family', '', '', '', 1, 1, 1, 0, 0, false, 100, NOW() - INTERVAL '25 hours', 'contextual_obfuscated', 'drop-weak-family.bin', 0.70, 'weak_single_binary', 0, NOW() - INTERVAL '25 hours', NOW() - INTERVAL '25 hours'),
			(1, $1, 'release_family', 'keep-weak-family', '', '', '', 1, 1, 1, 0, 0, false, 100, NOW() - INTERVAL '25 hours', 'contextual_obfuscated', 'keep-weak-family.bin', 0.70, 'weak_single_binary', 0, NOW() - INTERVAL '25 hours', NOW() - INTERVAL '25 hours'),
			(1, $1, 'release_family', 'pending-weak-family', '', '', '', 1, 1, 1, 0, 0, false, 100, NOW() - INTERVAL '25 hours', 'contextual_obfuscated', 'pending-weak-family.bin', 0.70, 'weak_single_binary', 0, NOW(), TIMESTAMPTZ 'epoch'),
			(1, $1, 'release_family', 'drop-fragment-family', '', '', '', 2, 1, 1, 1, 0, false, 200, NOW() - INTERVAL '25 hours', '', '', 0, 'fragment_only', 0, NOW() - INTERVAL '25 hours', NOW() - INTERVAL '25 hours'),
			(1, $1, 'release_family', 'drop-stale-family', '', '', '', 0, 0, 0, 0, 0, false, 0, NULL, '', '', 0, 'stale_cleanup_only', 0, NOW() - INTERVAL '25 hours', NOW() - INTERVAL '25 hours'),
			(1, $1, 'base_stem', 'drop-base-stem', '', '', '', 2, 1, 1, 1, 3, true, 200, NOW() - INTERVAL '7 hours', '', '', 0, 'prefer_base_stem', 0, NOW() - INTERVAL '7 hours', NOW() - INTERVAL '7 hours')`,
		newsgroupID,
	); err != nil {
		t.Fatalf("insert readiness rows: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_acks (
			provider_id, newsgroup_id, key_kind, family_key, processed_at, updated_at
		)
		VALUES
			(1, $1, 'release_family', 'drop-weak-family', NOW() - INTERVAL '25 hours', NOW()),
			(1, $1, 'release_family', 'keep-weak-family', NOW() - INTERVAL '25 hours', NOW()),
			(1, $1, 'release_family', 'drop-fragment-family', NOW() - INTERVAL '25 hours', NOW()),
			(1, $1, 'release_family', 'drop-stale-family', NOW() - INTERVAL '25 hours', NOW()),
			(1, $1, 'base_stem', 'drop-base-stem', NOW() - INTERVAL '7 hours', NOW())`,
		newsgroupID,
	); err != nil {
		t.Fatalf("insert readiness acks: %v", err)
	}

	out, err := store.RunIndexerMaintenance(ctx)
	if err != nil {
		t.Fatalf("run indexer maintenance: %v", err)
	}
	if out == nil {
		t.Fatalf("expected maintenance result")
	}
	if out.PurgedReadinessSummaries != 4 {
		t.Fatalf("expected 4 purged readiness summaries, got %+v", out)
	}

	rows, err := store.DB().QueryContext(ctx, `
		SELECT family_key
		FROM release_family_readiness_summaries
		WHERE newsgroup_id = $1
		ORDER BY family_key`, newsgroupID)
	if err != nil {
		t.Fatalf("query remaining readiness rows: %v", err)
	}
	defer rows.Close()

	var remaining []string
	for rows.Next() {
		var familyKey string
		if err := rows.Scan(&familyKey); err != nil {
			t.Fatalf("scan remaining readiness row: %v", err)
		}
		remaining = append(remaining, familyKey)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate remaining readiness rows: %v", err)
	}

	want := []string{"keep-weak-family", "pending-weak-family"}
	if len(remaining) != len(want) {
		t.Fatalf("unexpected remaining readiness rows: got=%v want=%v", remaining, want)
	}
	for i := range want {
		if remaining[i] != want[i] {
			t.Fatalf("unexpected remaining readiness row at %d: got=%q want=%q", i, remaining[i], want[i])
		}
	}
}

func TestRunIndexerMaintenanceDefersReadinessCleanupWhenRefreshBacklogExists(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.maintenance.refreshbacklog.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_summaries (
			provider_id, newsgroup_id, key_kind, family_key,
			source_release_key, release_key, release_name,
			binary_count, complete_binary_count, complete_main_payload_binary_count, incomplete_binary_count,
			expected_file_count, has_expected_file_count, total_bytes, earliest_posted_at,
			dominant_family_kind, dominant_file_name, dominant_match_confidence,
			readiness_bucket, expected_file_coverage_pct, updated_at, processed_at
		)
		VALUES
			(1, $1, 'release_family', 'queued-family', '', '', '', 1, 1, 1, 0, 0, false, 100, NOW() - INTERVAL '25 hours', 'contextual_obfuscated', 'queued-family.bin', 0.70, 'weak_single_binary', 0, NOW() - INTERVAL '25 hours', NOW() - INTERVAL '25 hours')`,
		newsgroupID,
	); err != nil {
		t.Fatalf("insert readiness row: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_readiness_acks (
			provider_id, newsgroup_id, key_kind, family_key, processed_at, updated_at
		)
		VALUES
			(1, $1, 'release_family', 'queued-family', NOW() - INTERVAL '25 hours', NOW())`,
		newsgroupID,
	); err != nil {
		t.Fatalf("insert readiness ack: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_family_summary_refresh_queue (
			provider_id, newsgroup_id, key_kind, family_key, queued_at
		)
		VALUES (1, $1, 'release_family', 'queued-family', NOW())`,
		newsgroupID,
	); err != nil {
		t.Fatalf("insert refresh queue row: %v", err)
	}

	out, err := store.RunIndexerMaintenance(ctx)
	if err != nil {
		t.Fatalf("run indexer maintenance: %v", err)
	}
	if out == nil {
		t.Fatalf("expected maintenance result")
	}
	if !out.SkippedReadinessCleanup {
		t.Fatalf("expected readiness cleanup to be skipped, got %+v", out)
	}
	if out.RefreshQueueBacklog != 1 {
		t.Fatalf("expected refresh queue backlog 1, got %+v", out)
	}
	if out.PurgedReadinessSummaries != 0 {
		t.Fatalf("expected no readiness summaries purged when refresh backlog exists, got %+v", out)
	}

	var remaining int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_family_readiness_summaries
		WHERE newsgroup_id = $1
		  AND family_key = 'queued-family'`,
		newsgroupID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count remaining readiness row: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected queued readiness row to remain, got %d", remaining)
	}
}

func TestRunIndexerMaintenancePurgesLegacyStableGroupingEvidence(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupName := fmt.Sprintf("alt.test.maintenance.groupingevidence.%d", time.Now().UnixNano())
	newsgroupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}

	stableBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		ReleaseFamilyKey:  "stable-family",
		ReleaseKey:        "stable-family",
		BinaryKey:         fmt.Sprintf("stable-binary-%d", time.Now().UnixNano()),
		BinaryName:        "stable.release.mkv",
		FileName:          "stable.release.mkv",
		IdentityStrength:  "strong",
		FamilyKind:        "readable_title",
		IsMainPayload:     true,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.95,
		MatchStatus:       "matched",
		GroupingEvidence: map[string]any{
			"summary": map[string]any{
				"kind":          "readable_title",
				"status":        "matched",
				"fallback_used": false,
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert stable binary: %v", err)
	}

	weakBinaryID, err := store.UpsertBinary(ctx, BinaryRecord{
		ProviderID:        1,
		NewsgroupID:       newsgroupID,
		ReleaseFamilyKey:  "weak-family",
		ReleaseKey:        "weak-family",
		BinaryKey:         fmt.Sprintf("weak-binary-%d", time.Now().UnixNano()),
		BinaryName:        "weak.bin",
		FileName:          "weak.bin",
		IdentityStrength:  "weak",
		FamilyKind:        "contextual_obfuscated",
		IsMainPayload:     true,
		ExpectedFileCount: 1,
		TotalParts:        1,
		MatchConfidence:   0.70,
		MatchStatus:       "probable",
		GroupingEvidence: map[string]any{
			"summary": map[string]any{
				"kind":          "contextual_obfuscated",
				"status":        "probable",
				"fallback_used": true,
			},
			"fallback": map[string]any{
				"used": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert weak binary: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binaries
		SET grouping_evidence_json = '{}'::jsonb
		WHERE id = $1`, stableBinaryID,
	); err != nil {
		t.Fatalf("clear stable inline summary: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO binary_grouping_evidence (
			binary_id,
			evidence_source,
			evidence_version,
			payload_json,
			updated_at
		)
		VALUES
			(
				$1,
				'matcher',
				'v1',
				'{"summary":{"kind":"readable_title","status":"matched","fallback_used":false}}'::jsonb,
				NOW() - INTERVAL '25 hours'
			),
			(
				$2,
				'matcher',
				'v1',
				'{"summary":{"kind":"contextual_obfuscated","status":"probable","fallback_used":true},"fallback":{"used":true}}'::jsonb,
				NOW() - INTERVAL '25 hours'
			)
		ON CONFLICT (binary_id) DO UPDATE
		SET payload_json = EXCLUDED.payload_json,
		    updated_at = EXCLUDED.updated_at`, stableBinaryID, weakBinaryID,
	); err != nil {
		t.Fatalf("seed legacy grouping evidence rows: %v", err)
	}

	out, err := store.RunIndexerMaintenance(ctx)
	if err != nil {
		t.Fatalf("run indexer maintenance: %v", err)
	}
	if out == nil {
		t.Fatalf("expected maintenance result")
	}
	if out.PurgedGroupingEvidence != 1 {
		t.Fatalf("expected 1 purged grouping evidence row, got %+v", out)
	}

	var stableSummaryKind string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT grouping_summary_kind
		FROM binaries
		WHERE id = $1`, stableBinaryID,
	).Scan(&stableSummaryKind); err != nil {
		t.Fatalf("query stable scalar evidence: %v", err)
	}
	if stableSummaryKind != "readable_title" {
		t.Fatalf("expected stable scalar summary to be backfilled, got %q", stableSummaryKind)
	}

	var stableSideCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_grouping_evidence
		WHERE binary_id = $1`, stableBinaryID,
	).Scan(&stableSideCount); err != nil {
		t.Fatalf("count stable side evidence: %v", err)
	}
	if stableSideCount != 0 {
		t.Fatalf("expected stable side-table evidence to be purged, got %d rows", stableSideCount)
	}

	var weakSideCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_grouping_evidence
		WHERE binary_id = $1`, weakBinaryID,
	).Scan(&weakSideCount); err != nil {
		t.Fatalf("count weak side evidence: %v", err)
	}
	if weakSideCount != 1 {
		t.Fatalf("expected weak side-table evidence to remain, got %d rows", weakSideCount)
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

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
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

func TestPublicIndexerReleaseDetailUsesPermanentCatalogFilesAfterPurge(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("archivedetail%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.RuntimeSeconds = 3600
		in.PrimaryResolution = "1080p"
		in.PrimaryVideoCodec = "x265"
		in.PrimaryAudioCodec = "dts"
		in.SubtitleLanguages = []string{"en", "es"}
		in.PasswordState = "passworded_known"
	})

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{
		{
			FileName:  fmt.Sprintf("%s.mkv", token),
			SizeBytes: 1_400_000_000,
			FileIndex: 1,
			PostedAt:  record.PostedAt,
		},
		{
			FileName:  fmt.Sprintf("%s.par2", token),
			SizeBytes: 128_000,
			FileIndex: 2,
			IsPars:    true,
			PostedAt:  record.PostedAt,
		},
	}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	groupID, err := store.EnsureNewsgroup(ctx, fmt.Sprintf("alt.test.archive.detail.%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	if err := store.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{groupID}); err != nil {
		t.Fatalf("replace release newsgroups: %v", err)
	}
	if err := store.UpsertNZBCache(ctx, releaseID, "ready", "hash-"+token, ""); err != nil {
		t.Fatalf("upsert nzb cache: %v", err)
	}

	if err := store.MarkReleaseArchiveStored(ctx, ReleaseArchiveStoredRecord{
		ReleaseID:         releaseID,
		ArchiveStore:      "indexer_archive",
		ObjectStoreKind:   "fs",
		ObjectKey:         fmt.Sprintf("releases/1/%s/test.nzb", releaseID),
		ContentHashSHA256: fmt.Sprintf("hash-%s", token),
		ObjectSizeBytes:   2048,
		ContentEncoding:   "identity",
		SourceModule:      "usenet_index",
	}); err != nil {
		t.Fatalf("mark release archive stored: %v", err)
	}

	if _, err := store.PurgeArchivedReleaseSources(ctx, releaseID); err != nil {
		t.Fatalf("purge archived release sources: %v", err)
	}

	for _, table := range []string{"release_files", "release_newsgroups", "nzb_cache", "release_archive_lineage_binaries", "release_archive_lineage_article_headers"} {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE release_id = $1", table)
		if err := store.DB().QueryRowContext(ctx, query, releaseID).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected %s rows to be purged, got %d", table, count)
		}
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get archived public detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected archived detail for %s", releaseID)
	}
	if len(detail.Files) != 2 {
		t.Fatalf("expected 2 archived catalog files, got %d", len(detail.Files))
	}
	if detail.Files[0].FileName != fmt.Sprintf("%s.mkv", token) {
		t.Fatalf("unexpected first archived file: %+v", detail.Files[0])
	}
	if detail.Media.RuntimeSeconds != 3600 || detail.Media.PrimaryVideoCodec != "x265" {
		t.Fatalf("expected archived media snapshot to survive purge, got %+v", detail.Media)
	}
	if len(detail.Media.SubtitleLanguages) != 2 {
		t.Fatalf("expected subtitle snapshot to survive purge, got %+v", detail.Media.SubtitleLanguages)
	}
	if detail.Release.PasswordState != "passworded_known" {
		t.Fatalf("expected stable archived password state, got %+v", detail.Release)
	}

	var catalogCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM release_catalog_files WHERE release_id = $1`, releaseID).Scan(&catalogCount); err != nil {
		t.Fatalf("count release catalog files: %v", err)
	}
	if catalogCount != 2 {
		t.Fatalf("expected 2 retained release catalog files, got %d", catalogCount)
	}
}

func TestIndexerReleaseDetailUsesPermanentCatalogFilesAfterPurge(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("admincatalog%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.RuntimeSeconds = 1800
	})
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE releases
		SET external_year = 2024,
		    external_media_type = 'tv'
		WHERE release_id = $1`, releaseID); err != nil {
		t.Fatalf("seed external release metadata: %v", err)
	}

	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{
		{
			FileName:  fmt.Sprintf("%s.mkv", token),
			SizeBytes: 900_000_000,
			FileIndex: 1,
			Subject:   "subject-one",
			Poster:    "poster-one",
			PostedAt:  record.PostedAt,
		},
		{
			FileName:  fmt.Sprintf("%s.sfv", token),
			SizeBytes: 1024,
			FileIndex: 2,
			Subject:   "subject-two",
			Poster:    "poster-two",
			PostedAt:  record.PostedAt,
		},
	}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}

	groupID, err := store.EnsureNewsgroup(ctx, fmt.Sprintf("alt.test.admin.catalog.%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	if err := store.ReplaceReleaseNewsgroups(ctx, releaseID, []int64{groupID}); err != nil {
		t.Fatalf("replace release newsgroups: %v", err)
	}
	if err := store.UpsertNZBCache(ctx, releaseID, "ready", "hash-"+token, ""); err != nil {
		t.Fatalf("upsert nzb cache: %v", err)
	}
	if err := store.MarkReleaseArchiveStored(ctx, ReleaseArchiveStoredRecord{
		ReleaseID:         releaseID,
		ArchiveStore:      "indexer_archive",
		ObjectStoreKind:   "fs",
		ObjectKey:         fmt.Sprintf("releases/1/%s/test.nzb", releaseID),
		ContentHashSHA256: fmt.Sprintf("hash-%s", token),
		ObjectSizeBytes:   4096,
		ContentEncoding:   "identity",
		SourceModule:      "usenet_index",
	}); err != nil {
		t.Fatalf("mark release archive stored: %v", err)
	}
	if _, err := store.PurgeArchivedReleaseSources(ctx, releaseID); err != nil {
		t.Fatalf("purge archived release sources: %v", err)
	}

	detail, err := store.GetIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get indexer release detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected detail for %s", releaseID)
	}
	if detail.Release.RuntimeSeconds != 1800 || detail.Release.ExternalYear != 2024 || detail.Release.ExternalMediaType != "tv" {
		t.Fatalf("expected retained release metadata, got %+v", detail.Release)
	}
	if len(detail.Files) != 2 {
		t.Fatalf("expected 2 retained file summaries, got %d", len(detail.Files))
	}
	if detail.Files[0].FileID != 0 || detail.Files[0].BinaryID != 0 {
		t.Fatalf("expected purged retained file summary to have no live file/binary ids, got %+v", detail.Files[0])
	}
	if detail.Files[0].Subject == "" || detail.Files[0].Poster == "" {
		t.Fatalf("expected retained file subject/poster metadata, got %+v", detail.Files[0])
	}
}

func TestClaimReleasePurgeCandidatesRequiresDurableCatalogAndCompletedMediaInspect(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("purgeclaim%d", time.Now().UnixNano())
	releaseID, record := seedVisibilityTestRelease(t, store, token, nil)
	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  777001,
		FileName:  fmt.Sprintf("%s.mkv", token),
		SizeBytes: 1_024,
		FileIndex: 1,
		PostedAt:  record.PostedAt,
	}}); err != nil {
		t.Fatalf("replace release files: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO binaries (
			id, provider_id, newsgroup_id, source_release_key, release_family_key, release_key,
			family_kind, base_stem, is_auxiliary, release_name, binary_name, file_name,
			posted_at, total_parts, observed_parts, total_bytes, match_confidence, match_status,
			created_at, updated_at
		) VALUES (
			$1, 1, 1, $2, $2, $2,
			'release_family', $2, FALSE, $2, $2, $2,
			NOW(), 1, 1, 1024, 1.0, 'exact',
			NOW(), NOW()
		)
		ON CONFLICT (id) DO NOTHING`, int64(777001), token,
	); err != nil {
		t.Fatalf("seed binary: %v", err)
	}
	if err := store.MarkReleaseArchiveStored(ctx, ReleaseArchiveStoredRecord{
		ReleaseID:         releaseID,
		ArchiveStore:      "indexer_archive",
		ObjectStoreKind:   "fs",
		ObjectKey:         fmt.Sprintf("releases/1/%s/test.nzb", releaseID),
		ContentHashSHA256: fmt.Sprintf("hash-%s", token),
		ObjectSizeBytes:   2048,
		ContentEncoding:   "identity",
		SourceModule:      "usenet_index",
	}); err != nil {
		t.Fatalf("mark release archive stored: %v", err)
	}

	candidates, err := store.ClaimReleasePurgeCandidates(ctx, 10, DefaultReleaseReadyPolicy())
	if err != nil {
		t.Fatalf("claim purge candidates without inspection: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no purge candidates before inspect_media completion, got %+v", candidates)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO binary_inspections (stage_name, binary_id, release_id, status, finished_at, created_at, updated_at)
		VALUES ('inspect_media', $1, $2, 'completed', NOW(), NOW(), NOW())`,
		int64(777001), releaseID,
	); err != nil {
		t.Fatalf("seed media inspection: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM release_catalog_files WHERE release_id = $1`, releaseID); err != nil {
		t.Fatalf("delete release catalog files: %v", err)
	}

	candidates, err = store.ClaimReleasePurgeCandidates(ctx, 10, DefaultReleaseReadyPolicy())
	if err != nil {
		t.Fatalf("claim purge candidates without catalog files: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no purge candidates without durable catalog files, got %+v", candidates)
	}
}

func TestPurgeArchivedReleaseSourcesPreservesSharedBinaryLineage(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("sharedpurge%d", time.Now().UnixNano())
	releaseOne, recordOne := seedVisibilityTestRelease(t, store, token+"a", nil)
	releaseTwo, recordTwo := seedVisibilityTestRelease(t, store, token+"b", nil)

	const sharedBinaryID int64 = 888001
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO binaries (
			id, provider_id, newsgroup_id, source_release_key, release_family_key, release_key,
			family_kind, base_stem, is_auxiliary, release_name, binary_name, file_name,
			posted_at, total_parts, observed_parts, total_bytes, match_confidence, match_status,
			created_at, updated_at
		) VALUES (
			$1, 1, 1, $2, $2, $2,
			'release_family', $2, FALSE, $2, $2, $2,
			NOW(), 1, 1, 4096, 1.0, 'exact',
			NOW(), NOW()
		)
		ON CONFLICT (id) DO NOTHING`, sharedBinaryID, token,
	); err != nil {
		t.Fatalf("seed shared binary: %v", err)
	}

	for _, item := range []struct {
		releaseID string
		postedAt  *time.Time
	}{
		{releaseID: releaseOne, postedAt: recordOne.PostedAt},
		{releaseID: releaseTwo, postedAt: recordTwo.PostedAt},
	} {
		if err := store.ReplaceReleaseFiles(ctx, item.releaseID, []ReleaseFileRecord{{
			BinaryID:  sharedBinaryID,
			FileName:  fmt.Sprintf("%s.mkv", token),
			SizeBytes: 4_096,
			FileIndex: 1,
			PostedAt:  item.postedAt,
		}}); err != nil {
			t.Fatalf("replace release files %s: %v", item.releaseID, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO binary_inspections (stage_name, binary_id, release_id, status, finished_at, created_at, updated_at)
			VALUES ('inspect_media', $1, $2, 'completed', NOW(), NOW(), NOW())
			ON CONFLICT DO NOTHING`, sharedBinaryID, item.releaseID,
		); err != nil {
			t.Fatalf("seed media inspection %s: %v", item.releaseID, err)
		}
	}

	if err := store.MarkReleaseArchiveStored(ctx, ReleaseArchiveStoredRecord{
		ReleaseID:         releaseOne,
		ArchiveStore:      "indexer_archive",
		ObjectStoreKind:   "fs",
		ObjectKey:         fmt.Sprintf("releases/1/%s/test.nzb", releaseOne),
		ContentHashSHA256: fmt.Sprintf("hash-%s-a", token),
		ObjectSizeBytes:   2048,
		ContentEncoding:   "identity",
		SourceModule:      "usenet_index",
	}); err != nil {
		t.Fatalf("mark release one archive stored: %v", err)
	}

	result, err := store.PurgeArchivedReleaseSources(ctx, releaseOne)
	if err != nil {
		t.Fatalf("purge archived release one: %v", err)
	}
	if result.SkippedSharedBinaryRows != 1 {
		t.Fatalf("expected 1 skipped shared binary row, got %+v", result)
	}
	if result.DeletedRowsByTable["binaries"] != 0 {
		t.Fatalf("expected shared binary to be preserved, got %+v", result.DeletedRowsByTable)
	}

	var binaryCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM binaries WHERE id = $1`, sharedBinaryID).Scan(&binaryCount); err != nil {
		t.Fatalf("count shared binary: %v", err)
	}
	if binaryCount != 1 {
		t.Fatalf("expected shared binary to remain, got %d", binaryCount)
	}

	var releaseTwoFiles int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM release_files WHERE release_id = $1`, releaseTwo).Scan(&releaseTwoFiles); err != nil {
		t.Fatalf("count release two files: %v", err)
	}
	if releaseTwoFiles != 1 {
		t.Fatalf("expected other active release to retain live file linkage, got %d", releaseTwoFiles)
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseOne)
	if err != nil {
		t.Fatalf("get purged release one detail: %v", err)
	}
	if detail == nil || len(detail.Files) != 1 {
		t.Fatalf("expected durable purged detail for release one, got %+v", detail)
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

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
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

func TestPublicIndexerReleaseVisibilityRequiresReadyReleaseHeuristics(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("publicreadyheuristics%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.CompletionPct = 99.9
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("list public releases with incomplete completion: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected incomplete release to be hidden, got total=%d items=%d", total, len(items))
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public incomplete detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected incomplete release detail to be hidden, got %+v", detail)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE releases
		SET completion_pct = 100
		WHERE release_id = $1`, releaseID); err != nil {
		t.Fatalf("promote release to ready completion: %v", err)
	}

	items, total, err = store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("list public releases with ready completion: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected ready release to be visible, got total=%d items=%d", total, len(items))
	}

	detail, err = store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public ready detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected ready release detail to be visible")
	}
}

func TestPublicIndexerReleaseVisibilityAllowsUncategorizedReleaseWhenOtherwiseReady(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("uncategorizedhide%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.CategoryID = newsnab.OtherMisc
		in.Category = newsnab.DisplayName(newsnab.OtherMisc)
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("list public uncategorized releases: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected uncategorized release to be visible, got total=%d items=%d", total, len(items))
	}
	if items[0].ReleaseID != releaseID {
		t.Fatalf("expected release %s, got %+v", releaseID, items[0])
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public uncategorized detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected uncategorized detail to be visible")
	}
}

func TestPublicIndexerReleaseBrowseUsesNormalizedCategoryIDs(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	movieToken := fmt.Sprintf("moviebrowse%d", time.Now().UnixNano())
	tvToken := fmt.Sprintf("tvbrowse%d", time.Now().UnixNano())

	_, _ = seedVisibilityTestRelease(t, store, movieToken, func(in *ReleaseRecord) {
		in.CategoryID = newsnab.MoviesHD
		in.Category = newsnab.DisplayName(newsnab.MoviesHD)
	})
	_, _ = seedVisibilityTestRelease(t, store, tvToken, func(in *ReleaseRecord) {
		in.Title = fmt.Sprintf("Public Visible %s S01E01 2026 1080p WEB-DL x265-GRP", tvToken)
		in.SourceTitle = fmt.Sprintf("Public.Visible.%s.S01E01.2026.1080p.WEB-DL.x265-GRP", tvToken)
		in.SearchTitle = strings.ToLower(fmt.Sprintf("public visible %s s01e01 2026 1080p web dl x265 grp", tvToken))
		in.CategoryID = newsnab.TVHD
		in.Category = newsnab.DisplayName(newsnab.TVHD)
		in.Classification = "tv"
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{
		Query:          "browse",
		BrowseCategory: "movies",
		Limit:          50,
	})
	if err != nil {
		t.Fatalf("list public movie browse releases: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected one movie browse release, got total=%d items=%d", total, len(items))
	}
	if items[0].CategoryID != newsnab.MoviesHD {
		t.Fatalf("expected movie category id %d, got %+v", newsnab.MoviesHD, items[0])
	}

	items, total, err = store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{
		Query:             "browse",
		BrowseCategory:    "tv",
		BrowseSubcategory: "hd",
		Limit:             50,
	})
	if err != nil {
		t.Fatalf("list public tv browse releases: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected one tv browse release, got total=%d items=%d", total, len(items))
	}
	if items[0].CategoryID != newsnab.TVHD {
		t.Fatalf("expected tv category id %d, got %+v", newsnab.TVHD, items[0])
	}
}

func TestPublicIndexerReleaseVisibilitySuppressesUnreadableMiscRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("miscobfuscated%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.Title = "JG1OxwlKTzcG8lfrY2t2H90vAFmf37O9 vol12+03"
		in.SourceTitle = in.Title
		in.SearchTitle = strings.ToLower(strings.ReplaceAll(in.Title, "+", " "))
		in.CategoryID = newsnab.OtherMisc
		in.Category = newsnab.DisplayName(newsnab.OtherMisc)
		in.TitleSource = "source"
		in.DeobfuscatedTitle = ""
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: "JG1OxwlKTzcG8lfrY2t2H90vAFmf37O9", Limit: 50})
	if err != nil {
		t.Fatalf("list public misc releases: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected unreadable misc release to be hidden, got total=%d items=%d", total, len(items))
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public misc detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected unreadable misc detail to be hidden, got %+v", detail)
	}
}

func TestPublicIndexerReleaseVisibilitySuppressesProbableOpaquePartRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("probableopaque%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.Title = "etaXMJOIXr7TY6ZRru6mio0bN part3"
		in.SourceTitle = in.Title
		in.SearchTitle = strings.ToLower(in.Title)
		in.CategoryID = newsnab.OtherMisc
		in.Category = newsnab.DisplayName(newsnab.OtherMisc)
		in.Classification = "archive"
		in.IdentityStatus = "probable"
		in.MatchConfidence = 0.898
		in.HasPAR2 = false
		in.HasNFO = false
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: "etaXMJOIXr7TY6ZRru6mio0bN", Limit: 50})
	if err != nil {
		t.Fatalf("list public probable opaque releases: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected probable opaque part release to be hidden, got total=%d items=%d", total, len(items))
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public probable opaque detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected probable opaque detail to be hidden, got %+v", detail)
	}
}

func TestPublicIndexerReleaseVisibilitySuppressesPasswordedUnknownRows(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("passwordunknown%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, func(in *ReleaseRecord) {
		in.Title = "Useful App Suite 2026 x64 Incl Keygen"
		in.SourceTitle = in.Title
		in.SearchTitle = strings.ToLower(in.Title)
		in.CategoryID = newsnab.OtherMisc
		in.Category = newsnab.DisplayName(newsnab.OtherMisc)
		in.Classification = "archive"
		in.IdentityStatus = "identified"
		in.MatchConfidence = 0.96
		in.Passworded = true
		in.PasswordedUnknown = true
		in.PasswordState = "passworded_unknown"
		in.Encrypted = true
	})

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: "Useful App Suite", Limit: 50})
	if err != nil {
		t.Fatalf("list public passworded unknown releases: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("expected passworded_unknown release to be hidden, got total=%d items=%d", total, len(items))
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public passworded unknown detail: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected passworded_unknown detail to be hidden, got %+v", detail)
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

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
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

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
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

	items, total, err := store.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{Query: token, Limit: 50, Offset: 0})
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

func TestPublicIndexerReleaseDetailIncludesPreviewMetadata(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := fmt.Sprintf("publicpreview%d", time.Now().UnixNano())
	releaseID, _ := seedVisibilityTestRelease(t, store, token, nil)

	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO release_archive_state (
			release_id,
			archive_status,
			preview_object_key,
			preview_content_type,
			preview_source_kind,
			preview_updated_at,
			updated_at
		) VALUES ($1, 'purge_pending', 'releases/1/`+token+`/preview.jpg', 'image/jpeg', 'archive_image', NOW(), NOW())
		ON CONFLICT (release_id) DO UPDATE
		SET preview_object_key = EXCLUDED.preview_object_key,
		    preview_content_type = EXCLUDED.preview_content_type,
		    preview_source_kind = EXCLUDED.preview_source_kind,
		    preview_updated_at = EXCLUDED.preview_updated_at,
		    updated_at = NOW()`,
		releaseID,
	); err != nil {
		t.Fatalf("seed release archive preview state: %v", err)
	}

	detail, err := store.GetPublicIndexerReleaseDetail(ctx, releaseID)
	if err != nil {
		t.Fatalf("get public release detail: %v", err)
	}
	if detail == nil {
		t.Fatalf("expected detail for %s", releaseID)
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
		CategoryID:              newsnab.MoviesHD,
		Category:                newsnab.DisplayName(newsnab.MoviesHD),
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
		CategoryID:              newsnab.MoviesHD,
		Category:                newsnab.DisplayName(newsnab.MoviesHD),
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
