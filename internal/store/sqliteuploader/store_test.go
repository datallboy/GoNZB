package sqliteuploader

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/uploader"
)

const testNZB = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><head><meta type="name">Synthetic.Release</meta></head><file poster="fixture@example.invalid" date="1700000000" subject="&quot;synthetic.bin&quot; yEnc"><groups><group>alt.test.gonzb</group></groups><segments><segment bytes="128" number="1">segment-1@example.invalid</segment></segments></file></nzb>`

func TestStoreSubmissionLifecycleAndDeduplication(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatalf("new uploader store: %v", err)
	}
	defer store.Close()
	service := uploader.NewService(store, nzb.DefaultLimits())

	input := uploader.SubmitInput{
		NZBBytes:         []byte(testNZB),
		OriginalFilename: "synthetic.nzb",
		IntakeKind:       uploader.IntakeHTTP,
		SubmittedBy:      "fixture",
		IdempotencyKey:   "fixture-key",
		Artifacts:        []uploader.ArtifactInput{{Filename: "synthetic.nfo", DeclaredMediaType: "text/plain", Payload: []byte("synthetic fixture")}},
	}
	input.Metadata.Artifacts = []uploader.ArtifactDescriptor{{Filename: "synthetic.nfo", Kind: uploader.ArtifactNFO, Label: "Fixture NFO"}}
	created, err := service.Submit(t.Context(), input)
	if err != nil {
		t.Fatalf("submit NZB: %v", err)
	}
	if !created.Created || created.Submission.State != uploader.StatePendingReview {
		t.Fatalf("unexpected create result: %#v", created)
	}
	if len(created.Submission.Artifacts) != 1 || created.Submission.Artifacts[0].Kind != uploader.ArtifactNFO {
		t.Fatalf("unexpected stored artifacts: %#v", created.Submission.Artifacts)
	}
	artifact, artifactReader, err := service.OpenArtifact(t.Context(), created.Submission.ID, created.Submission.Artifacts[0].ID)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	artifactPayload, err := io.ReadAll(artifactReader)
	_ = artifactReader.Close()
	if err != nil || artifact.OriginalFilename != "synthetic.nfo" || string(artifactPayload) != "synthetic fixture" {
		t.Fatalf("unexpected artifact: item=%#v payload=%q err=%v", artifact, artifactPayload, err)
	}

	duplicate, err := service.Submit(t.Context(), input)
	if err != nil {
		t.Fatalf("retry NZB: %v", err)
	}
	if duplicate.Created || duplicate.Submission.ID != created.Submission.ID {
		t.Fatalf("expected idempotent retry, got %#v", duplicate)
	}

	conflictInput := input
	conflictInput.NZBBytes = []byte(stringsReplaceMessageID(testNZB, "different@example.invalid"))
	if _, err := service.Submit(t.Context(), conflictInput); !errors.Is(err, uploader.ErrConflict) {
		t.Fatalf("expected conflicting idempotency key, got %v", err)
	}

	updatedTitle := "Reviewed Synthetic Release"
	categoryID := 8010
	updated, err := service.Update(t.Context(), created.Submission.ID, uploader.Update{
		Title: &updatedTitle, CategoryID: &categoryID, Actor: "reviewer",
	}, false)
	if err != nil {
		t.Fatalf("update pending submission: %v", err)
	}
	if updated.Title != updatedTitle || updated.NormalizedTitle != "reviewed synthetic release" {
		t.Fatalf("unexpected updated title: %#v", updated)
	}

	approved, err := service.Transition(t.Context(), updated.ID, uploader.StateApproved, "reviewer", "fixture approved", false)
	if err != nil {
		t.Fatalf("approve submission: %v", err)
	}
	if approved.State != uploader.StateApproved || approved.ApprovedAt == nil {
		t.Fatalf("unexpected approved state: %#v", approved)
	}
	if _, err := service.Update(t.Context(), approved.ID, uploader.Update{Title: &updatedTitle}, false); !errors.Is(err, uploader.ErrInvalidTransition) {
		t.Fatalf("expected approved update rejection, got %v", err)
	}

	reader, err := service.OpenNZB(t.Context(), approved.ID, true)
	if err != nil {
		t.Fatalf("open approved NZB: %v", err)
	}
	payload, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(payload) != testNZB {
		t.Fatalf("unexpected stored NZB: err=%v bytes=%d", err, len(payload))
	}

	pending, err := service.Transition(t.Context(), approved.ID, uploader.StatePendingReview, "reviewer", "recheck", false)
	if err != nil || pending.State != uploader.StatePendingReview {
		t.Fatalf("return to pending: item=%#v err=%v", pending, err)
	}
	if _, err := service.OpenNZB(t.Context(), pending.ID, true); !errors.Is(err, uploader.ErrNotFound) {
		t.Fatalf("expected approved-only NZB access rejection, got %v", err)
	}

	events, err := service.Events(t.Context(), pending.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected intake, edit, approve, and return events; got %#v", events)
	}
	if events[0].CreatedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("event timestamp is in the future: %#v", events[0])
	}
}

func TestFederationPublicationLifecycleIsDurablePerPool(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := uploader.NewService(store, nzb.DefaultLimits())
	created, err := service.Submit(t.Context(), uploader.SubmitInput{
		NZBBytes: []byte(testNZB), OriginalFilename: "synthetic.nzb", SubmittedBy: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(t.Context(), created.Submission.ID, uploader.StateApproved, "reviewer", "approved", false); err != nil {
		t.Fatal(err)
	}
	requested, err := store.RequestFederationPublication(t.Context(), created.Submission.ID, "pool.synthetic", "admin")
	if err != nil || requested.State != uploader.PublicationRequested {
		t.Fatalf("request publication: item=%#v err=%v", requested, err)
	}
	published, err := store.CompleteFederationPublication(t.Context(), created.Submission.ID, "pool.synthetic", uploader.PublicationPublished, uploader.PublicationOutcome{
		ReleaseID: "rel_synthetic", ManifestID: "man_synthetic", CardEventID: "evt_card",
		ManifestEventID: "evt_manifest", PublicationStateEventID: "evt_active",
	})
	if err != nil || published.State != uploader.PublicationPublished || published.ReleaseID != "rel_synthetic" {
		t.Fatalf("complete publication: item=%#v err=%v", published, err)
	}
	if _, err := service.Transition(t.Context(), created.Submission.ID, uploader.StatePendingReview, "reviewer", "recheck", false); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListFederationPublications(t.Context(), created.Submission.ID)
	if err != nil || len(items) != 1 || items[0].State != uploader.PublicationWithdrawalRequested {
		t.Fatalf("expected automatic withdrawal request: items=%#v err=%v", items, err)
	}
	withdrawn, err := store.CompleteFederationPublication(t.Context(), created.Submission.ID, "pool.synthetic", uploader.PublicationWithdrawn, uploader.PublicationOutcome{PublicationStateEventID: "evt_withdrawn"})
	if err != nil || withdrawn.State != uploader.PublicationWithdrawn || withdrawn.PublicationStateEventID != "evt_withdrawn" {
		t.Fatalf("complete withdrawal: item=%#v err=%v", withdrawn, err)
	}
}

func stringsReplaceMessageID(raw, replacement string) string {
	const original = "segment-1@example.invalid"
	for index := 0; index+len(original) <= len(raw); index++ {
		if raw[index:index+len(original)] == original {
			return raw[:index] + replacement + raw[index+len(original):]
		}
	}
	return raw
}
