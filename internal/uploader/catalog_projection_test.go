package uploader_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/sqliteuploader"
	"github.com/datallboy/gonzb/internal/uploader"
)

const catalogProjectionNZB = `<?xml version="1.0"?><nzb><file poster="fixture@example.invalid" date="1700000000" subject="projection.bin yEnc"><groups><group>alt.test.gonzb</group></groups><segments><segment bytes="32" number="1">projection@example.invalid</segment></segments></file></nzb>`

func TestApprovalPublishesAndUnapprovalWithdrawsCatalogProjection(t *testing.T) {
	store := openUploaderProjectionTestStore(t)
	projector := &recordingCatalogProjector{}
	service := uploader.NewService(store, nzb.DefaultLimits())
	service.SetCatalogProjector(projector)

	created, err := service.Submit(t.Context(), uploader.SubmitInput{
		NZBBytes:         []byte(catalogProjectionNZB),
		OriginalFilename: "projection.nzb",
		Metadata:         uploader.Metadata{Title: "Catalog Projection Fixture", CategoryID: 8010},
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Transition(t.Context(), created.Submission.ID, uploader.StateApproved, "reviewer", "approved", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projector.published) != 1 || projector.published[0].ReleaseID != approved.ReleaseID {
		t.Fatalf("unexpected publications: %+v", projector.published)
	}

	pending, err := service.Transition(t.Context(), created.Submission.ID, uploader.StatePendingReview, "reviewer", "recheck", false)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != uploader.StatePendingReview || len(projector.withdrawn) != 1 || projector.withdrawn[0] != approved.ReleaseID {
		t.Fatalf("unexpected withdrawal: state=%s releases=%v", pending.State, projector.withdrawn)
	}
}

func TestFailedCatalogPublicationRollsApprovalBackToPending(t *testing.T) {
	store := openUploaderProjectionTestStore(t)
	projector := &recordingCatalogProjector{publishErr: errors.New("catalog unavailable")}
	service := uploader.NewService(store, nzb.DefaultLimits())
	service.SetCatalogProjector(projector)

	created, err := service.Submit(t.Context(), uploader.SubmitInput{
		NZBBytes:         []byte(catalogProjectionNZB),
		OriginalFilename: "projection.nzb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(t.Context(), created.Submission.ID, uploader.StateApproved, "reviewer", "approved", false); err == nil {
		t.Fatal("expected catalog publication failure")
	}
	item, err := store.GetSubmission(t.Context(), created.Submission.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != uploader.StatePendingReview {
		t.Fatalf("submission state = %s, want pending_review", item.State)
	}
}

func TestCatalogReconciliationUsesApprovedAuthoritativeState(t *testing.T) {
	store := openUploaderProjectionTestStore(t)
	service := uploader.NewService(store, nzb.DefaultLimits())
	created, err := service.Submit(t.Context(), uploader.SubmitInput{
		NZBBytes:         []byte(catalogProjectionNZB),
		OriginalFilename: "projection.nzb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionSubmission(t.Context(), created.Submission.ID, uploader.StateApproved, "reviewer", "approved before restart"); err != nil {
		t.Fatal(err)
	}

	projector := &recordingCatalogProjector{}
	service.SetCatalogProjector(projector)
	if err := service.ReconcileCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(projector.reconciled) != 1 || projector.reconciled[0].ReleaseID != created.Submission.ReleaseID {
		t.Fatalf("unexpected reconciliation: %+v", projector.reconciled)
	}
}

func openUploaderProjectionTestStore(t *testing.T) *sqliteuploader.Store {
	t.Helper()
	root := t.TempDir()
	store, err := sqliteuploader.NewStore(filepath.Join(root, "uploader.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type recordingCatalogProjector struct {
	publishErr error
	published  []uploader.Submission
	withdrawn  []string
	reconciled []uploader.Submission
}

func (p *recordingCatalogProjector) PublishUploaderSubmission(_ context.Context, submission uploader.Submission) error {
	if p.publishErr != nil {
		return p.publishErr
	}
	p.published = append(p.published, submission)
	return nil
}

func (p *recordingCatalogProjector) WithdrawUploaderSubmission(_ context.Context, releaseID string) error {
	p.withdrawn = append(p.withdrawn, releaseID)
	return nil
}

func (p *recordingCatalogProjector) ReconcileUploaderSubmissions(_ context.Context, submissions []uploader.Submission) error {
	p.reconciled = append(p.reconciled, submissions...)
	return nil
}
