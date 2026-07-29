package pgindex

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestFinishPosterMaterializationRowsUsesPartitionKey(t *testing.T) {
	store := openPostgresTestStore(t)
	ensureDefaultTestProvider(t, store)

	ctx := context.Background()
	today := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	yesterday := today.Add(-24 * time.Hour)
	if err := store.provisionPartitionBundleForDays(ctx, partitionBundleScrape, []time.Time{yesterday}); err != nil {
		t.Fatalf("provision yesterday scrape partitions: %v", err)
	}

	suffix := time.Now().UnixNano()
	if _, err := store.InsertArticleHeaders(ctx, testProviderID, testNewsgroupID, []ArticleHeader{
		{
			ArticleNumber: 81001,
			MessageID:     fmt.Sprintf("<poster-finish-today-%d@test>", suffix),
			Subject:       `"today.bin" yEnc (1/1)`,
			Poster:        "poster-finish-test",
			DateUTC:       &today,
			Bytes:         100,
			Lines:         1,
		},
		{
			ArticleNumber: 81002,
			MessageID:     fmt.Sprintf("<poster-finish-yesterday-%d@test>", suffix),
			Subject:       `"yesterday.bin" yEnc (1/1)`,
			Poster:        "poster-finish-test",
			DateUTC:       &yesterday,
			Bytes:         100,
			Lines:         1,
		},
	}); err != nil {
		t.Fatalf("insert article headers: %v", err)
	}

	var todayID int64
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM article_headers
		WHERE source_posted_at = $1
		  AND article_number = 81001`,
		today,
	).Scan(&todayID); err != nil {
		t.Fatalf("read today article id: %v", err)
	}

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin completion transaction: %v", err)
	}
	if err := finishPosterMaterializationRows(ctx, tx, []claimedPosterMaterializationRow{{
		ArticleHeaderID: todayID,
		SourcePostedAt:  &today,
	}}); err != nil {
		tx.Rollback()
		t.Fatalf("finish poster materialization row: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit completion transaction: %v", err)
	}

	var todayStatus, yesterdayStatus string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT
			MAX(status) FILTER (WHERE article_number = 81001),
			MAX(status) FILTER (WHERE article_number = 81002)
		FROM poster_materialization_queue q
		JOIN article_headers ah
		  ON ah.source_posted_at = q.source_posted_at
		 AND ah.id = q.article_header_id`,
	).Scan(&todayStatus, &yesterdayStatus); err != nil {
		t.Fatalf("read poster queue statuses: %v", err)
	}
	if todayStatus != "done" || yesterdayStatus != "pending" {
		t.Fatalf("unexpected poster queue statuses: today=%q yesterday=%q", todayStatus, yesterdayStatus)
	}
}
