package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestArticleCohortScanCursorAdvancesAndWraps(t *testing.T) {
	store := openPostgresTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Hour)
	end := start.Add(time.Hour)

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin cursor tx: %v", err)
	}
	postedAt, binaryID, err := lockArticleCohortScanCursorInTx(ctx, tx, "test-window", start, end)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("lock new cursor: %v", err)
	}
	if postedAt.Valid || binaryID.Valid {
		_ = tx.Rollback()
		t.Fatalf("new cursor unexpectedly populated: %v %v", postedAt, binaryID)
	}
	lastPostedAt := start.Add(15 * time.Minute)
	if err := advanceArticleCohortScanCursorInTx(
		ctx,
		tx,
		"test-window",
		sql.NullTime{Time: lastPostedAt, Valid: true},
		sql.NullInt64{Int64: 42, Valid: true},
	); err != nil {
		_ = tx.Rollback()
		t.Fatalf("advance cursor: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit cursor advance: %v", err)
	}

	tx, err = store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin cursor reload tx: %v", err)
	}
	postedAt, binaryID, err = lockArticleCohortScanCursorInTx(ctx, tx, "test-window", start, end)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("reload cursor: %v", err)
	}
	if !postedAt.Valid || !postedAt.Time.Equal(lastPostedAt) || !binaryID.Valid || binaryID.Int64 != 42 {
		_ = tx.Rollback()
		t.Fatalf("reloaded cursor = %v/%v, want %s/42", postedAt, binaryID, lastPostedAt)
	}
	if err := advanceArticleCohortScanCursorInTx(ctx, tx, "test-window", sql.NullTime{}, sql.NullInt64{}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("wrap cursor: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit cursor wrap: %v", err)
	}

	var wrapped int64
	var cursorCleared bool
	if err := store.DB().QueryRowContext(ctx, `
		SELECT wrapped_count, cursor_posted_at IS NULL AND cursor_binary_id IS NULL
		FROM indexer_article_cohort_scan_state
		WHERE scan_key = 'test-window'`).Scan(&wrapped, &cursorCleared); err != nil {
		t.Fatalf("load wrapped cursor: %v", err)
	}
	if wrapped != 1 || !cursorCleared {
		t.Fatalf("wrapped cursor = count %d cleared %v, want 1/true", wrapped, cursorCleared)
	}
}

func TestBalancedArticleCohortSchedulerSamplesTargetWindow(t *testing.T) {
	store := openPostgresTestStore(t)
	ensureDefaultTestProvider(t, store)
	ctx := context.Background()
	groupID, err := store.EnsureNewsgroup(ctx, fmt.Sprintf("alt.test.cohort-target.%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ensure target-window group: %v", err)
	}
	postedAt := time.Now().UTC().Truncate(time.Second)
	const cohortSize = 20
	for i := 0; i < cohortSize; i++ {
		name := fmt.Sprintf("opaque-target-%02d-%d.bin", i, postedAt.UnixNano())
		binaryID, err := upsertTestBinary(t, store, ctx, BinaryRecord{
			ProviderID:       testProviderID,
			NewsgroupID:      groupID,
			ReleaseFamilyKey: name,
			FileFamilyKey:    name + "::part",
			IdentityStrength: "provisional",
			IdentityReason:   "opaque_subject_set",
			FamilyKind:       "opaque_set",
			BaseStem:         name,
			IsMainPayload:    true,
			ReleaseKey:       name,
			ReleaseName:      name,
			BinaryKey:        name + "::binary",
			BinaryName:       name,
			FileName:         name,
			TotalParts:       1,
			PostedAt:         &postedAt,
			MatchConfidence:  0.5,
			MatchStatus:      "provisional",
		})
		if err != nil {
			t.Fatalf("upsert target-window binary %d: %v", i, err)
		}
		messageID := fmt.Sprintf("<opaque-target-%02d-%d@test>", i, postedAt.UnixNano())
		if _, err := store.InsertArticleHeaders(ctx, testProviderID, groupID, []ArticleHeader{{
			ArticleNumber: int64(10000 + i),
			MessageID:     messageID,
			Subject:       name,
			Poster:        "cohort@test",
			DateUTC:       &postedAt,
			Bytes:         100,
			Lines:         10,
		}}); err != nil {
			t.Fatalf("insert target-window article %d: %v", i, err)
		}
		var articleID int64
		if err := store.DB().QueryRowContext(ctx, `
			SELECT id
			FROM article_headers
			WHERE provider_id = $1 AND newsgroup_id = $2 AND message_id = $3`,
			testProviderID, groupID, messageID,
		).Scan(&articleID); err != nil {
			t.Fatalf("load target-window article %d: %v", i, err)
		}
		if err := upsertTestBinaryParts(t, store, ctx, []BinaryPartRecord{{
			BinaryID:        binaryID,
			ArticleHeaderID: articleID,
			MessageID:       messageID,
			PartNumber:      1,
			TotalParts:      1,
			SegmentBytes:    100,
			FileName:        name,
		}}); err != nil {
			t.Fatalf("upsert target-window binary part %d: %v", i, err)
		}
	}

	windowStart := postedAt.Add(-time.Minute)
	windowEnd := postedAt.Add(time.Minute)
	result, err := store.RunArticleCohortScheduler(ctx, ArticleCohortSchedulerRequest{
		BatchSize:         100,
		AssemblyQueueMax:  100,
		YEncQueueMax:      100,
		TargetWindowStart: &windowStart,
		TargetWindowEnd:   &windowEnd,
	})
	if err != nil {
		t.Fatalf("run target-window cohort scheduler: %v", err)
	}
	if result.YEncQueued != 16 {
		t.Fatalf("balanced target-window yEnc queued = %d, want 16-sample", result.YEncQueued)
	}
	var decisions, queued int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE recovery_decision = 'sample'),
			(SELECT COUNT(*) FROM article_cohort_yenc_queue WHERE newsgroup_id = $1)
		FROM article_cohort_candidates
		WHERE newsgroup_id = $1`,
		groupID,
	).Scan(&decisions, &queued); err != nil {
		t.Fatalf("load target-window cohort state: %v", err)
	}
	if decisions != 1 || queued != 16 {
		t.Fatalf("target-window cohort state = sample decisions %d queue rows %d, want 1/16", decisions, queued)
	}
}

func TestArticleCohortRecoveryPromotesRepeatedStableEvidence(t *testing.T) {
	store := openPostgresTestStore(t)
	ensureDefaultTestProvider(t, store)
	ctx := context.Background()
	sourcePostedAt := time.Now().UTC()
	cohortKey := "opaque:1:1:stable"
	insertArticleCohortRecoveryFixture(t, store, sourcePostedAt, cohortKey, 3)

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin cohort feedback tx: %v", err)
	}
	feedback := []articleCohortYEncRecoveryFeedback{
		{ArticleHeaderID: 1, StableSignalKey: "release-file-set"},
		{ArticleHeaderID: 2, StableSignalKey: "release-file-set"},
		{ArticleHeaderID: 3, StableSignalKey: "unrelated-random-name"},
	}
	if err := recordArticleCohortYEncRecoveredInTx(ctx, tx, feedback); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record stable cohort feedback: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit stable cohort feedback: %v", err)
	}

	var decision, status string
	var stableCount, doneCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT recovery_decision, status, stable_signal_count, yenc_done_count
		FROM article_cohort_candidates
		WHERE source_posted_at = $1 AND cohort_key = $2`,
		sourcePostedAt, cohortKey,
	).Scan(&decision, &status, &stableCount, &doneCount); err != nil {
		t.Fatalf("load promoted cohort: %v", err)
	}
	if decision != "promoted" || status != "active" || stableCount != 2 || doneCount != 3 {
		t.Fatalf("promoted cohort = decision %q status %q stable %d done %d", decision, status, stableCount, doneCount)
	}
}

func TestArticleCohortRecoveryStopsRandomRecoveredSample(t *testing.T) {
	store := openPostgresTestStore(t)
	ensureDefaultTestProvider(t, store)
	ctx := context.Background()
	sourcePostedAt := time.Now().UTC()
	cohortKey := "opaque:1:1:random"
	const sampleSize = 16
	insertArticleCohortRecoveryFixture(t, store, sourcePostedAt, cohortKey, sampleSize)

	feedback := make([]articleCohortYEncRecoveryFeedback, 0, sampleSize)
	for i := 1; i <= sampleSize; i++ {
		feedback = append(feedback, articleCohortYEncRecoveryFeedback{
			ArticleHeaderID: int64(i),
			StableSignalKey: fmt.Sprintf("random-file-%d", i),
		})
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin cohort feedback tx: %v", err)
	}
	if err := recordArticleCohortYEncRecoveredInTx(ctx, tx, feedback); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record random cohort feedback: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit random cohort feedback: %v", err)
	}

	var decision, status string
	var stableCount, doneCount, decisionArticleCount int
	var settled bool
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			recovery_decision,
			status,
			stable_signal_count,
			yenc_done_count,
			decision_article_count,
			settled_at IS NOT NULL
		FROM article_cohort_candidates
		WHERE source_posted_at = $1 AND cohort_key = $2`,
		sourcePostedAt, cohortKey,
	).Scan(&decision, &status, &stableCount, &doneCount, &decisionArticleCount, &settled); err != nil {
		t.Fatalf("load no-yield cohort: %v", err)
	}
	if decision != "no_yield" || status != "cooldown" || stableCount != 1 ||
		doneCount != sampleSize || decisionArticleCount != sampleSize || !settled {
		t.Fatalf(
			"no-yield cohort = decision %q status %q stable %d done %d decision articles %d settled %v",
			decision, status, stableCount, doneCount, decisionArticleCount, settled,
		)
	}
}

func insertArticleCohortRecoveryFixture(t *testing.T, store *Store, sourcePostedAt time.Time, cohortKey string, count int) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO article_cohort_candidates (
			source_posted_at,
			cohort_key,
			provider_id,
			newsgroup_id,
			cohort_kind,
			status,
			bucket_start,
			bucket_end,
			article_count,
			singleton_count
		)
		VALUES (
			$1::timestamptz,
			$2,
			$3,
			$4,
			'opaque_near_time',
			'active',
			$1::timestamptz,
			$1::timestamptz + INTERVAL '5 minutes',
			$5,
			$5
		)`,
		sourcePostedAt, cohortKey, testProviderID, testNewsgroupID, count,
	); err != nil {
		t.Fatalf("insert article cohort candidate: %v", err)
	}
	for i := 1; i <= count; i++ {
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO article_headers (
				id,
				provider_id,
				newsgroup_id,
				article_number,
				message_id,
				date_utc,
				source_posted_at
			)
			VALUES (
				$1::bigint,
				$2,
				$3,
				$1::bigint,
				'<cohort-' || $1::bigint::text || '>',
				$4,
				$4
			)`,
			i, testProviderID, testNewsgroupID, sourcePostedAt,
		); err != nil {
			t.Fatalf("insert article header %d: %v", i, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO binary_core (
				binary_id,
				provider_id,
				newsgroup_id,
				binary_key,
				source_posted_at
			)
			VALUES ($1::bigint, $2, $3, 'cohort-binary-' || $1::bigint::text, $4)`,
			i, testProviderID, testNewsgroupID, sourcePostedAt,
		); err != nil {
			t.Fatalf("insert binary core %d: %v", i, err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO article_cohort_yenc_queue (
				source_posted_at,
				binary_id,
				article_header_id,
				cohort_key,
				provider_id,
				newsgroup_id,
				cohort_kind,
				status
			)
			VALUES ($1, $2, $2, $3, $4, $5, 'opaque_near_time', 'admitted')`,
			sourcePostedAt, i, cohortKey, testProviderID, testNewsgroupID,
		); err != nil {
			t.Fatalf("insert article cohort queue row %d: %v", i, err)
		}
	}
}
