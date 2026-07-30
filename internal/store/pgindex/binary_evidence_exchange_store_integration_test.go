package pgindex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/evidence"
)

func TestYEncEvidencePersistencePrefersLocalAndQuarantinesConflicts(t *testing.T) {
	store := openPostgresTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	messageID := "<evidence-part@example>"

	if accepted, quarantined, err := store.UpsertYEncHeaderEvidence(ctx, []YEncEvidenceRecord{{
		SourcePostedAt: now, MessageID: messageID, FileName: "movie.mkv",
		PartNumber: 1, TotalParts: 10, FileSize: 1000,
		Provenance: "local_body", AcceptanceState: "accepted",
	}}); err != nil || accepted != 1 || quarantined != 0 {
		t.Fatalf("persist local evidence accepted=%d quarantined=%d err=%v", accepted, quarantined, err)
	}
	if accepted, quarantined, err := store.UpsertYEncHeaderEvidence(ctx, []YEncEvidenceRecord{{
		SourcePostedAt: now, MessageID: messageID, FileName: "movie.mkv",
		PartNumber: 1, TotalParts: 10, FileSize: 1000,
		Provenance: "peer", SourcePoolID: "pool.test", SourceNodeID: "node.peer",
		SourceBundleID: "bundle.one", AcceptanceState: "accepted",
	}}); err != nil || accepted != 1 || quarantined != 0 {
		t.Fatalf("persist peer evidence accepted=%d quarantined=%d err=%v", accepted, quarantined, err)
	}

	items, err := store.FindAcceptedYEncEvidence(ctx, []string{messageID}, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Provenance != "local_body" || items[0].FileName != "movie.mkv" {
		t.Fatalf("expected local evidence precedence, got %+v", items)
	}

	if accepted, quarantined, err := store.UpsertYEncHeaderEvidence(ctx, []YEncEvidenceRecord{{
		SourcePostedAt: now, MessageID: messageID, FileName: "conflict.mkv",
		PartNumber: 2, TotalParts: 10, FileSize: 1000,
		Provenance: "peer", SourcePoolID: "pool.test", SourceNodeID: "node.peer",
		SourceBundleID: "bundle.two", AcceptanceState: "accepted",
	}}); err != nil || accepted != 0 || quarantined != 1 {
		t.Fatalf("expected peer conflict quarantine accepted=%d quarantined=%d err=%v", accepted, quarantined, err)
	}

	peerFirstID := "<peer-first-evidence@example>"
	if accepted, quarantined, err := store.UpsertYEncHeaderEvidence(ctx, []YEncEvidenceRecord{{
		SourcePostedAt: now, MessageID: peerFirstID, FileName: "peer-name.mkv",
		PartNumber: 1, TotalParts: 20, FileSize: 2000,
		Provenance: "peer", SourcePoolID: "pool.test", SourceNodeID: "node.peer",
		SourceBundleID: "bundle.three", AcceptanceState: "accepted",
	}}); err != nil || accepted != 1 || quarantined != 0 {
		t.Fatalf("persist peer-first evidence accepted=%d quarantined=%d err=%v", accepted, quarantined, err)
	}
	if accepted, quarantined, err := store.UpsertYEncHeaderEvidence(ctx, []YEncEvidenceRecord{{
		SourcePostedAt: now, MessageID: peerFirstID, FileName: "local-name.mkv",
		PartNumber: 1, TotalParts: 20, FileSize: 2000,
		Provenance: "local_body", AcceptanceState: "accepted",
	}}); err != nil || accepted != 1 || quarantined != 0 {
		t.Fatalf("local evidence should supersede peer accepted=%d quarantined=%d err=%v", accepted, quarantined, err)
	}
	items, err = store.FindAcceptedYEncEvidence(ctx, []string{peerFirstID}, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Provenance != "local_body" || items[0].FileName != "local-name.mkv" {
		t.Fatalf("expected later local evidence to supersede peer, got %+v", items)
	}
}

func TestPeerSegmentsCompleteEffectiveBinaryWithoutArticleHeaderWrites(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	groupName := fmt.Sprintf("alt.test.evidence.local.%d", time.Now().UnixNano())
	peerGroup := fmt.Sprintf("alt.test.evidence.peer.%d", time.Now().UnixNano())
	groupID, err := store.EnsureNewsgroup(ctx, groupName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureNewsgroup(ctx, peerGroup); err != nil {
		t.Fatal(err)
	}
	binaryID, err := upsertTestBinary(t, store, ctx, BinaryRecord{
		ProviderID: 1, NewsgroupID: groupID,
		SourceReleaseKey: "peer completion", ReleaseFamilyKey: "peer completion",
		FileFamilyKey: "peer-completion::file", FamilyKind: "file_name",
		BaseStem: "peer completion", IsMainPayload: true,
		ReleaseKey: "peer completion", ReleaseName: "Peer Completion",
		BinaryKey:  fmt.Sprintf("peer-completion-%d", time.Now().UnixNano()),
		BinaryName: "peer-completion.mkv", FileName: "peer-completion.mkv",
		FileIndex: 1, ExpectedFileCount: 1, TotalParts: 2,
		PostedAt: &now, MatchConfidence: 0.95, MatchStatus: "matched",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertArticleHeaders(ctx, 1, groupID, []ArticleHeader{{
		ArticleNumber: 100, MessageID: "<effective-local@example>",
		Subject: `"peer-completion.mkv" yEnc (1/2)`, Poster: "local-poster",
		DateUTC: &now, Bytes: 100,
	}}); err != nil {
		t.Fatal(err)
	}
	var articleID int64
	var sourcePostedAt time.Time
	if err := store.DB().QueryRowContext(ctx, `
		SELECT id, source_posted_at
		FROM article_headers
		WHERE message_id = '<effective-local@example>'`).Scan(&articleID, &sourcePostedAt); err != nil {
		t.Fatal(err)
	}
	if err := upsertTestBinaryPart(t, store, ctx, BinaryPartRecord{
		BinaryID: binaryID, ArticleHeaderID: articleID, SourcePostedAt: sourcePostedAt,
		MessageID: "<effective-local@example>", PartNumber: 1, TotalParts: 2,
		SegmentBytes: 100, FileName: "peer-completion.mkv",
	}); err != nil {
		t.Fatal(err)
	}
	var headersBefore int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM article_headers`).Scan(&headersBefore); err != nil {
		t.Fatal(err)
	}
	imported, conflicts, err := store.ImportPeerSegments(
		ctx, binaryID, "pool.test", "node.peer", "bundle.peer",
		[]evidence.Segment{{
			PartNumber: 2, TotalParts: 2, MessageID: "<effective-peer@example>",
			Bytes: 200, PostedAt: now.Add(time.Second).Format(time.RFC3339),
			SourcePostedAt: now.Format(time.RFC3339), Groups: []string{peerGroup},
			FileName: "peer-completion.mkv",
		}},
	)
	if err != nil || imported != 1 || conflicts != 0 {
		t.Fatalf("import peer segment imported=%d conflicts=%d err=%v", imported, conflicts, err)
	}
	var observed, total int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT observed_parts, total_parts
		FROM binary_observation_stats
		WHERE binary_id = $1
		ORDER BY updated_at DESC
		LIMIT 1`, binaryID).Scan(&observed, &total); err != nil {
		t.Fatal(err)
	}
	if observed != 2 || total != 2 {
		t.Fatalf("effective completion got %d/%d, want 2/2", observed, total)
	}
	articles, err := store.ListCatalogBinaryArticles(ctx, binaryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 2 || articles[0].GroupName != groupName || articles[1].GroupName != peerGroup {
		t.Fatalf("unexpected effective catalog articles: %+v", articles)
	}
	var headersAfter int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM article_headers`).Scan(&headersAfter); err != nil {
		t.Fatal(err)
	}
	if headersAfter != headersBefore {
		t.Fatalf("peer import wrote article_headers: before=%d after=%d", headersBefore, headersAfter)
	}
	if _, err := store.RefreshBinaryExchangeIdentities(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var contentIdentities int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM binary_exchange_identities
		WHERE binary_id = $1 AND scheme = 'content_v1'`, binaryID).Scan(&contentIdentities); err != nil {
		t.Fatal(err)
	}
	if contentIdentities != 1 {
		t.Fatalf("expected one content identity, got %d", contentIdentities)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO binary_exchange_identities
			(binary_id, scheme, match_id, identity_json, confidence)
		VALUES ($1, 'yenc_v1', 'yenc_v1:test', '{"scheme":"yenc_v1"}', 0.99)
		ON CONFLICT (binary_id, scheme) DO NOTHING`, binaryID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE binary_observation_stats
		SET total_parts = 3, observed_parts = 2, updated_at = NOW()
		WHERE binary_id = $1`, binaryID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SeedBinaryEvidenceRepairWork(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var repairScheme string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT scheme
		FROM binary_evidence_repair_work_items
		WHERE binary_id = $1`, binaryID).Scan(&repairScheme); err != nil {
		t.Fatal(err)
	}
	if repairScheme != "yenc_v1" {
		t.Fatalf("expected strongest yenc repair identity, got %q", repairScheme)
	}
}
