package pgindex

import (
	"context"
	"testing"
	"time"
)

func TestInspectDiscoverySeedBackoffResetsOnUsefulOrChangedWork(t *testing.T) {
	store := &Store{}
	now := time.Now()

	store.recordInspectDiscoverySeedResult(now, 0)
	if !store.shouldBackoffInspectDiscoverySeed(now.Add(30 * time.Second)) {
		t.Fatal("expected empty discovery seed to back off")
	}
	if store.shouldBackoffInspectDiscoverySeed(now.Add(2 * time.Minute)) {
		t.Fatal("expected first discovery seed backoff to expire after one minute")
	}

	store.recordInspectDiscoverySeedResult(now, 1)
	if store.shouldBackoffInspectDiscoverySeed(now) {
		t.Fatal("expected useful discovery seed to clear backoff")
	}

	store.recordInspectDiscoverySeedResult(now, 0)
	store.clearInspectDiscoverySeedBackoff()
	if store.shouldBackoffInspectDiscoverySeed(now) {
		t.Fatal("expected changed binary work to clear discovery seed backoff")
	}
}

func TestInspectReadyQueueDefersDiscoveryAndBacksOffFailures(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	groupID, err := store.EnsureNewsgroup(ctx, "alt.test.inspect-ready-deferral")
	if err != nil {
		t.Fatalf("ensure newsgroup: %v", err)
	}
	postedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := store.InsertArticleHeaders(ctx, testProviderID, groupID, []ArticleHeader{{
		ArticleNumber: 1,
		MessageID:     "<inspect-ready-deferral@test>",
		Subject:       "opaque inspect candidate",
		Poster:        "poster@test",
		DateUTC:       &postedAt,
		Bytes:         1024,
		Lines:         10,
	}}); err != nil {
		t.Fatalf("insert article header: %v", err)
	}
	var articleID int64
	var sourcePostedAt time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id, source_posted_at
		FROM article_headers
		WHERE newsgroup_id = $1
		  AND message_id = '<inspect-ready-deferral@test>'`,
		groupID,
	).Scan(&articleID, &sourcePostedAt); err != nil {
		t.Fatalf("load article header: %v", err)
	}

	binaryID, err := upsertTestBinary(t, store, ctx, BinaryRecord{
		ProviderID:       testProviderID,
		NewsgroupID:      groupID,
		SourceReleaseKey: "inspect-ready-deferral",
		FamilyKind:       "opaque_set",
		IsMainPayload:    true,
		ReleaseKey:       "inspect-ready-deferral",
		ReleaseName:      "inspect-ready-deferral",
		BinaryKey:        "inspect-ready-deferral::opaque",
		BinaryName:       "opaque.bin",
		FileName:         "opaque.bin",
		TotalParts:       1,
		PostedAt:         &sourcePostedAt,
		MatchConfidence:  0.5,
		MatchStatus:      "probable",
		IdentityStrength: "provisional",
		IdentityReason:   "opaque_subject_set",
	})
	if err != nil {
		t.Fatalf("upsert binary: %v", err)
	}
	if err := upsertTestBinaryParts(t, store, ctx, []BinaryPartRecord{{
		BinaryID:        binaryID,
		ArticleHeaderID: articleID,
		MessageID:       "<inspect-ready-deferral@test>",
		PartNumber:      1,
		TotalParts:      1,
		SegmentBytes:    1024,
		FileName:        "opaque.bin",
	}}); err != nil {
		t.Fatalf("upsert binary part: %v", err)
	}
	if err := store.RefreshBinaryStats(ctx, binaryID); err != nil {
		t.Fatalf("refresh binary stats: %v", err)
	}

	res, err := store.DB().ExecContext(ctx, `
		UPDATE yenc_recovery_work_items
		SET status = 'ready', ready_at = NOW(), updated_at = NOW()
		WHERE binary_id = $1
		  AND article_header_id = $2`,
		binaryID,
		articleID,
	)
	if err != nil {
		t.Fatalf("activate yenc work item: %v", err)
	}
	if rows, err := res.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("expected one seeded yenc work item, rows=%d err=%v", rows, err)
	}
	releaseID := "inspect-ready-deferral-release"
	if _, err := store.PersistReleaseSnapshot(ctx, ReleaseRecord{
		ReleaseID:         releaseID,
		GUID:              releaseID,
		ProviderID:        testProviderID,
		SourceReleaseKey:  "inspect-ready-deferral",
		ReleaseFamilyKey:  "inspect-ready-deferral",
		ReleaseKey:        "inspect-ready-deferral",
		GroupName:         "inspect-ready-deferral",
		Title:             "inspect-ready-deferral",
		SearchTitle:       "inspect-ready-deferral",
		FileCount:         1,
		ExpectedFileCount: 1,
		CompletionPct:     100,
		PostedAt:          &postedAt,
		MetadataUpdatedAt: &postedAt,
	}, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "opaque.bin",
		FileIndex: 1,
		PostedAt:  &postedAt,
	}}, []int64{groupID}); err != nil {
		t.Fatalf("persist release with active yenc work: %v", err)
	}
	var readyCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_inspection_ready_queue
		WHERE stage_name = 'inspect_discovery'
		  AND binary_id = $1
		  AND status = 'ready'`,
		binaryID,
	).Scan(&readyCount); err != nil {
		t.Fatalf("count deferred discovery rows: %v", err)
	}
	if readyCount != 0 {
		t.Fatalf("expected active yenc work to defer discovery, got %d ready rows", readyCount)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE yenc_recovery_work_items
		SET status = 'done', updated_at = NOW()
		WHERE binary_id = $1`,
		binaryID,
	); err != nil {
		t.Fatalf("complete yenc work item: %v", err)
	}
	if err := store.ReplaceReleaseFiles(ctx, releaseID, []ReleaseFileRecord{{
		BinaryID:  binaryID,
		FileName:  "opaque.bin",
		FileIndex: 1,
		PostedAt:  &postedAt,
	}}); err != nil {
		t.Fatalf("publish discovery event after yenc: %v", err)
	}
	if err := finishInspectReadyQueueRow(ctx, store.DB(), "inspect_discovery", binaryID, "failed", "temporary fetch failure"); err != nil {
		t.Fatalf("finish failed discovery row: %v", err)
	}

	var status string
	var readyAt time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT status, ready_at
		FROM binary_inspection_ready_queue
		WHERE stage_name = 'inspect_discovery'
		  AND binary_id = $1`,
		binaryID,
	).Scan(&status, &readyAt); err != nil {
		t.Fatalf("load failed discovery queue row: %v", err)
	}
	if status != "ready" || readyAt.Before(time.Now().Add(4*time.Minute)) {
		t.Fatalf("expected failed discovery to have a five-minute retry delay, got status=%q ready_at=%s", status, readyAt)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binary_inspection_ready_queue
		SET ready_at = NOW()
		WHERE stage_name = 'inspect_discovery'
		  AND binary_id = $1`,
		binaryID,
	); err != nil {
		t.Fatalf("make discovery queue row claimable: %v", err)
	}

	lockConn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("acquire refresh lock connection: %v", err)
	}
	defer lockConn.Close()
	lockTx, err := lockConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin refresh lock transaction: %v", err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1))`,
		"gonzb-inspect-discovery-ready-reconcile",
	); err != nil {
		t.Fatalf("hold discovery refresh lock: %v", err)
	}

	claimCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	candidates, err := store.ClaimBinaryInspectionCandidates(claimCtx, BinaryInspectionClaimRequest{
		StageName:     "inspect_discovery",
		Limit:         1,
		Owner:         "inspect-ready-queue-test",
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim already-ready discovery candidate without refresh: %v", err)
	}
	if len(candidates) != 1 || candidates[0].BinaryID != binaryID {
		t.Fatalf("expected existing ready candidate %d, got %+v", binaryID, candidates)
	}
}

func TestInspectionReadyQueueReconcilesMissedReleaseEvent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	releaseID, binaryID := seedNZBGenerateCandidateRelease(
		t,
		store,
		ctx,
		"inspection-reconcile",
		"Inspection.Reconcile.2026.mkv",
		0,
		"video",
	)
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM binary_inspection_ready_queue
		WHERE stage_name = 'inspect_media'
		  AND binary_id = $1`, binaryID); err != nil {
		t.Fatalf("simulate missed inspection event: %v", err)
	}

	result, err := store.RefreshInspectionReadyQueue(ctx, "inspect_media", 100)
	if err != nil {
		t.Fatalf("reconcile inspect_media queue: %v", err)
	}
	if result.ReadyUpserted != 1 {
		t.Fatalf("expected one reconciled media candidate, got %+v", result)
	}

	var queuedReleaseID string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT release_id
		FROM binary_inspection_ready_queue
		WHERE stage_name = 'inspect_media'
		  AND binary_id = $1`, binaryID).Scan(&queuedReleaseID); err != nil {
		t.Fatalf("load reconciled media row: %v", err)
	}
	if queuedReleaseID != releaseID {
		t.Fatalf("expected reconciled release %q, got %q", releaseID, queuedReleaseID)
	}
}
