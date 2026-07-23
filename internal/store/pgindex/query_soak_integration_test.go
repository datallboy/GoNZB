package pgindex

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestIndexerQuerySoak exercises the production store paths against enough
// rows for PostgreSQL to choose realistic plans. It is opt-in because it
// creates and removes a five-part, 10,000-header source cohort.
func TestIndexerQuerySoak(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GONZB_QUERY_SOAK")) != "1" {
		t.Skip("set GONZB_QUERY_SOAK=1 and GONZB_TEST_PG_DSN to run the indexer query soak")
	}
	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Fatal("GONZB_TEST_PG_DSN is required for the indexer query soak")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	store, err := NewStore(dsn)
	if err != nil {
		t.Fatalf("open query soak store: %v", err)
	}
	requireDisposableQuerySoakDatabase(t, store)

	token := time.Now().UTC().UnixNano()
	providerID, err := store.EnsureProvider(ctx, fmt.Sprintf("query-soak-%d", token), "Query Soak")
	if err != nil {
		t.Fatalf("ensure query soak provider: %v", err)
	}
	groupID, err := store.EnsureNewsgroup(ctx, fmt.Sprintf("alt.test.query-soak.%d", token))
	if err != nil {
		t.Fatalf("ensure query soak newsgroup: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if strings.TrimSpace(os.Getenv("GONZB_QUERY_SOAK_KEEP_DATA")) != "1" {
			if _, err := store.DB().ExecContext(cleanupCtx, `
				DELETE FROM binary_core
				WHERE provider_id = $1
				  AND newsgroup_id = $2`, providerID, groupID); err != nil {
				t.Errorf("cleanup query soak binaries: %v", err)
			}
			if _, err := store.DB().ExecContext(cleanupCtx, `
				DELETE FROM article_headers
				WHERE provider_id = $1
				  AND newsgroup_id = $2`, providerID, groupID); err != nil {
				t.Errorf("cleanup query soak article headers: %v", err)
			}
			if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM newsgroups WHERE id = $1`, groupID); err != nil {
				t.Errorf("cleanup query soak newsgroup: %v", err)
			}
			if _, err := store.DB().ExecContext(cleanupCtx, `DELETE FROM usenet_providers WHERE id = $1`, providerID); err != nil {
				t.Errorf("cleanup query soak provider: %v", err)
			}
		}
		if err := store.Close(); err != nil {
			t.Errorf("close query soak store: %v", err)
		}
	})

	const (
		binaryCount   = 2000
		partsPerFile  = 5
		headerCount   = binaryCount * partsPerFile
		insertBatch   = 1000
		incompleteMod = 10
		opaqueMod     = 20
	)
	postedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)

	started := time.Now()
	for start := 0; start < headerCount; start += insertBatch {
		end := min(start+insertBatch, headerCount)
		headers := make([]ArticleHeader, 0, end-start)
		for idx := start; idx < end; idx++ {
			binaryOrdinal := idx / partsPerFile
			partNumber := idx%partsPerFile + 1
			fileName := fmt.Sprintf("query.soak.%06d.mkv", binaryOrdinal)
			if binaryOrdinal%opaqueMod == 0 {
				fileName = fmt.Sprintf("query-soak-%d-%06d.bin", token, binaryOrdinal)
			}
			headers = append(headers, ArticleHeader{
				ArticleNumber: int64(idx + 1),
				MessageID:     fmt.Sprintf("<query-soak-%d-%08d@test>", token, idx),
				Subject: fmt.Sprintf(
					`Query Soak %06d [1/1] - "%s" yEnc (%d/%d)`,
					binaryOrdinal,
					fileName,
					partNumber,
					partsPerFile,
				),
				Poster:  "query-soak@example.test",
				DateUTC: &postedAt,
				Bytes:   750_000,
				Lines:   1000,
			})
		}
		if _, err := store.InsertArticleHeaders(ctx, providerID, groupID, headers); err != nil {
			t.Fatalf("insert query soak headers %d-%d: %v", start, end, err)
		}
	}
	t.Logf("inserted %d article headers in %s", headerCount, time.Since(started))

	type headerKey struct {
		id             int64
		sourcePostedAt time.Time
	}
	headerByNumber := make(map[int64]headerKey, headerCount)
	rows, err := store.DB().QueryContext(ctx, `
		SELECT id, source_posted_at, article_number
		FROM article_headers
		WHERE provider_id = $1
		  AND newsgroup_id = $2`, providerID, groupID)
	if err != nil {
		t.Fatalf("load query soak headers: %v", err)
	}
	for rows.Next() {
		var key headerKey
		var articleNumber int64
		if err := rows.Scan(&key.id, &key.sourcePostedAt, &articleNumber); err != nil {
			rows.Close()
			t.Fatalf("scan query soak header: %v", err)
		}
		headerByNumber[articleNumber] = key
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close query soak header rows: %v", err)
	}
	if len(headerByNumber) != headerCount {
		t.Fatalf("loaded %d query soak headers, want %d", len(headerByNumber), headerCount)
	}

	binaries := make([]BinaryRecord, 0, binaryCount)
	for idx := 0; idx < binaryCount; idx++ {
		fileName := fmt.Sprintf("query.soak.%06d.mkv", idx)
		identityStrength := "strong"
		familyKind := "readable"
		if idx%opaqueMod == 0 {
			fileName = fmt.Sprintf("query-soak-%d-%06d.bin", token, idx)
			identityStrength = "weak"
			familyKind = "opaque_set"
		}
		familyKey := fmt.Sprintf("query-soak-family-%d-%06d", token, idx)
		binaries = append(binaries, BinaryRecord{
			ProviderID:        providerID,
			NewsgroupID:       groupID,
			SourceReleaseKey:  familyKey,
			ReleaseFamilyKey:  familyKey,
			FileSetKey:        familyKey,
			FileFamilyKey:     familyKey,
			IdentityStrength:  identityStrength,
			IdentityReason:    "query_soak",
			FamilyKind:        familyKind,
			BaseStem:          familyKey,
			IsMainPayload:     true,
			ReleaseKey:        familyKey,
			ReleaseName:       fmt.Sprintf("Query Soak %06d", idx),
			BinaryKey:         fmt.Sprintf("query-soak-binary-%d-%06d", token, idx),
			BinaryName:        fileName,
			FileName:          fileName,
			FileIndex:         1,
			ExpectedFileCount: 1,
			TotalParts:        partsPerFile,
			PostedAt:          &postedAt,
			MatchConfidence:   0.95,
			MatchStatus:       "matched",
		})
	}
	started = time.Now()
	binaryIDs, err := store.UpsertBinaries(ctx, binaries)
	if err != nil {
		t.Fatalf("upsert query soak binaries: %v", err)
	}
	t.Logf("upserted %d binaries in %s", len(binaryIDs), time.Since(started))

	parts := make([]BinaryPartRecord, 0, headerCount)
	for binaryOrdinal, binaryID := range binaryIDs {
		for partNumber := 1; partNumber <= partsPerFile; partNumber++ {
			if binaryOrdinal%incompleteMod == 0 && partNumber == partsPerFile {
				continue
			}
			articleNumber := int64(binaryOrdinal*partsPerFile + partNumber)
			header := headerByNumber[articleNumber]
			parts = append(parts, BinaryPartRecord{
				BinaryID:        binaryID,
				ArticleHeaderID: header.id,
				SourcePostedAt:  header.sourcePostedAt,
				MessageID:       fmt.Sprintf("<query-soak-%d-%08d@test>", token, articleNumber-1),
				PartNumber:      partNumber,
				TotalParts:      partsPerFile,
				SegmentBytes:    750_000,
				FileName:        binaries[binaryOrdinal].FileName,
			})
		}
	}
	started = time.Now()
	if err := store.UpsertBinaryParts(ctx, parts); err != nil {
		t.Fatalf("upsert query soak binary parts: %v", err)
	}
	if err := store.RefreshBinaryStatsBatch(ctx, binaryIDs); err != nil {
		t.Fatalf("refresh query soak binary stats: %v", err)
	}
	t.Logf("upserted %d binary parts and refreshed stats in %s", len(parts), time.Since(started))

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE article_header_assembly_queue
		SET queued_at = NOW() - INTERVAL '1 minute'
		WHERE provider_id = $1
		  AND newsgroup_id = $2`, providerID, groupID); err != nil {
		t.Fatalf("age query soak assembly queue: %v", err)
	}

	started = time.Now()
	claimed := 0
	for claimed < headerCount {
		items, _, err := store.ClaimAssemblyQueueBatchWithStats(ctx, AssemblyClaimRequest{
			Limit:         insertBatch,
			Owner:         fmt.Sprintf("query-soak-%d", token),
			LeaseDuration: 5 * time.Minute,
			Lane:          AssemblyClaimLaneCombined,
		})
		if err != nil {
			t.Fatalf("claim query soak assembly batch: %v", err)
		}
		if len(items) == 0 {
			break
		}
		claimed += len(items)
	}
	t.Logf("claimed and hydrated %d assembly rows in %s", claimed, time.Since(started))

	started = time.Now()
	refreshed := 0
	for pass := 0; pass < 20; pass++ {
		metrics, err := store.RefreshQueuedReleaseFamilySummariesWithMetrics(ctx, 1000)
		if err != nil {
			t.Fatalf("refresh query soak release summaries: %v", err)
		}
		if metrics.Refreshed <= 0 {
			break
		}
		refreshed += metrics.Refreshed
	}
	candidates, err := store.ListReleaseCandidates(ctx, 1000, ReleaseCandidateSelectionOptions{})
	if err != nil {
		t.Fatalf("list query soak release candidates: %v", err)
	}
	t.Logf("refreshed %d release summaries and selected %d candidates in %s", refreshed, len(candidates), time.Since(started))

	started = time.Now()
	if _, _, err := store.BackfillYEncRecoveryWorkItems(ctx, 5000); err != nil {
		t.Fatalf("backfill query soak yenc work: %v", err)
	}
	yencCandidates, err := store.ListYEncRecoveryCandidates(ctx, 1000)
	if err != nil {
		t.Fatalf("list query soak yenc candidates: %v", err)
	}
	t.Logf("selected %d yenc candidates in %s", len(yencCandidates), time.Since(started))

	started = time.Now()
	if _, err := store.RefreshInspectDiscoveryReadyQueue(ctx, 5000); err != nil {
		t.Fatalf("refresh query soak discovery queue: %v", err)
	}
	inspectionCandidates, err := store.ListBinaryInspectionCandidates(ctx, "inspect_discovery", 1000)
	if err != nil {
		t.Fatalf("list query soak inspection candidates: %v", err)
	}
	if _, err := store.RefreshIndexerDashboardStats(ctx); err != nil {
		t.Fatalf("refresh query soak dashboard stats: %v", err)
	}
	t.Logf("selected %d inspection candidates and refreshed dashboard in %s", len(inspectionCandidates), time.Since(started))
}

func TestIndexerQuerySoakRefreshExisting(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GONZB_QUERY_SOAK_REFRESH_EXISTING")) != "1" {
		t.Skip("set GONZB_QUERY_SOAK_REFRESH_EXISTING=1 and GONZB_TEST_PG_DSN to refresh an existing soak cohort")
	}
	dsn := strings.TrimSpace(os.Getenv("GONZB_TEST_PG_DSN"))
	if dsn == "" {
		t.Fatal("GONZB_TEST_PG_DSN is required for the indexer query soak")
	}
	store, err := NewStore(dsn)
	if err != nil {
		t.Fatalf("open query soak store: %v", err)
	}
	defer store.Close()
	requireDisposableQuerySoakDatabase(t, store)

	rows, err := store.DB().Query(`
		SELECT binary_id
		FROM binary_core
		ORDER BY binary_id
		LIMIT 2000`)
	if err != nil {
		t.Fatalf("list existing query soak binaries: %v", err)
	}
	var binaryIDs []int64
	for rows.Next() {
		var binaryID int64
		if err := rows.Scan(&binaryID); err != nil {
			rows.Close()
			t.Fatalf("scan existing query soak binary: %v", err)
		}
		binaryIDs = append(binaryIDs, binaryID)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close existing query soak binary rows: %v", err)
	}
	if len(binaryIDs) == 0 {
		t.Fatal("existing query soak cohort is empty")
	}

	started := time.Now()
	if err := store.RefreshBinaryStatsBatch(context.Background(), binaryIDs); err != nil {
		t.Fatalf("refresh existing query soak binaries: %v", err)
	}
	t.Logf("refreshed %d existing binaries in %s", len(binaryIDs), time.Since(started))
}

func requireDisposableQuerySoakDatabase(t *testing.T, store *Store) {
	t.Helper()
	var databaseName string
	if err := store.DB().QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("read query soak database name: %v", err)
	}
	normalized := strings.ToLower(strings.TrimSpace(databaseName))
	if !strings.Contains(normalized, "soak") && !strings.Contains(normalized, "test") {
		t.Fatalf("query soak refuses database %q; use a disposable database whose name contains test or soak", databaseName)
	}
}
