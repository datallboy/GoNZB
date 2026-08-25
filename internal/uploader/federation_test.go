package uploader_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/sqliteuploader"
	"github.com/datallboy/gonzb/internal/uploader"
)

type fakeFederationBackend struct {
	candidate releasecard.LocalRelease
	prior     uploader.FederationPublication
}

func (b *fakeFederationBackend) EligiblePools(context.Context) ([]string, error) {
	return []string{"pool.synthetic"}, nil
}

func (b *fakeFederationBackend) Publish(_ context.Context, _ string, candidate releasecard.LocalRelease, prior uploader.FederationPublication) (uploader.PublicationOutcome, error) {
	b.candidate = candidate
	b.prior = prior
	return uploader.PublicationOutcome{
		ReleaseID: "rel_synthetic", ManifestID: "man_synthetic", CardEventID: "evt_card",
		ManifestEventID: "evt_manifest", PublicationStateEventID: "evt_active",
	}, nil
}

func (b *fakeFederationBackend) PublishState(_ context.Context, _ string, publication uploader.FederationPublication, _, _ string) (uploader.PublicationOutcome, error) {
	return uploader.PublicationOutcome{
		ReleaseID: publication.ReleaseID, ManifestID: publication.ManifestID,
		CardEventID: publication.CardEventID, ManifestEventID: publication.ManifestEventID,
		PublicationStateEventID: "evt_withdrawn",
	}, nil
}

func TestFederationServicePublishesValidatedNZBWithoutTorrentInputs(t *testing.T) {
	root := t.TempDir()
	store, err := sqliteuploader.NewStore(filepath.Join(root, "metadata.db"), filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := uploader.NewService(store, nzb.DefaultLimits())
	metadata := uploader.Metadata{Password: "synthetic-secret"}
	created, err := service.Submit(t.Context(), uploader.SubmitInput{
		NZBBytes: []byte(inboxNZB), OriginalFilename: "synthetic.nzb", Metadata: metadata, SubmittedBy: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(t.Context(), created.Submission.ID, uploader.StateApproved, "reviewer", "approved", false); err != nil {
		t.Fatal(err)
	}
	backend := &fakeFederationBackend{}
	federation := uploader.NewFederationService(service, backend)
	items, err := federation.Request(t.Context(), created.Submission.ID, []string{"pool.synthetic"}, "admin")
	if err != nil || len(items) != 1 || items[0].State != uploader.PublicationPublished {
		t.Fatalf("publish: items=%#v err=%v", items, err)
	}
	if backend.candidate.SourceKind != "local_uploader" || backend.candidate.ArchivePassword != "synthetic-secret" || len(backend.candidate.Files) != 1 {
		t.Fatalf("unexpected supplied candidate: %#v", backend.candidate)
	}
	withdrawn, err := federation.Withdraw(t.Context(), created.Submission.ID, "pool.synthetic", "admin", "fixture withdrawal")
	if err != nil || withdrawn.State != uploader.PublicationWithdrawn {
		t.Fatalf("withdraw: item=%#v err=%v", withdrawn, err)
	}
}
