package pgindex

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/uploader"
)

func TestUploaderCatalogProjectionAppearsInPublicAndAdminReleases(t *testing.T) {
	store := openPostgresTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	submission := uploader.Submission{
		ID:              "uploader-submission-projection",
		State:           uploader.StateApproved,
		ReleaseID:       "uploader-release-projection",
		Title:           "Catalog Projection Fixture",
		NormalizedTitle: "catalog projection fixture",
		CategoryID:      8010,
		Category:        "Misc > Other",
		SizeBytes:       64,
		PostedAt:        now.Add(-time.Hour),
		Poster:          "fixture@example.invalid",
		Groups:          []string{"alt.test.gonzb"},
		FileCount:       1,
		SegmentCount:    1,
		IMDBID:          "tt1234567",
		CreatedAt:       now.Add(-time.Minute),
		UpdatedAt:       now,
		Files: []nzb.FileFacts{{
			Name:         "projection.bin",
			Subject:      "projection.bin yEnc",
			Poster:       "fixture@example.invalid",
			PostedAt:     now.Add(-time.Hour),
			Groups:       []string{"alt.test.gonzb"},
			SizeBytes:    64,
			SegmentCount: 1,
		}},
	}

	if err := store.PublishUploaderSubmission(t.Context(), submission); err != nil {
		t.Fatal(err)
	}
	public, total, err := store.ListPublicIndexerReleases(t.Context(), PublicIndexerReleaseListParams{
		Query: "Projection", Limit: 10, ReadyPolicy: DefaultReleaseReadyPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(public) != 1 || public[0].ReleaseID != submission.ReleaseID || public[0].SourceKind != uploaderCatalogSourceKind {
		t.Fatalf("unexpected public projection: total=%d items=%+v", total, public)
	}
	publicDetail, err := store.GetPublicIndexerReleaseDetail(t.Context(), submission.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if publicDetail == nil || len(publicDetail.Files) != 1 || publicDetail.Files[0].ObservedParts != 1 {
		t.Fatalf("unexpected public detail: %+v", publicDetail)
	}

	admin, total, err := store.ListIndexerReleases(t.Context(), AdminIndexerReleaseListParams{Query: "Projection", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(admin) != 1 || admin[0].SourceKind != uploaderCatalogSourceKind || !admin[0].PublicVisible || admin[0].NZBGenerationStatus != "ready" {
		t.Fatalf("unexpected admin projection: total=%d items=%+v", total, admin)
	}
	adminDetail, err := store.GetIndexerReleaseDetail(t.Context(), submission.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if adminDetail == nil || len(adminDetail.Files) != 1 || adminDetail.Release.PayloadCompletionState != "complete" {
		t.Fatalf("unexpected admin detail: %+v", adminDetail)
	}

	if err := store.ReconcileUploaderSubmissions(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	removed, err := store.GetIndexerReleaseDetail(t.Context(), submission.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if removed != nil {
		t.Fatalf("stale uploader projection remained after reconciliation: %+v", removed)
	}
}
