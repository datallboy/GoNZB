package releasearchive

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceArchivesClaimedRelease(t *testing.T) {
	repo := &archiveRepoStub{
		candidates: []pgindex.ReleaseArchiveCandidate{{
			ReleaseID:  "rel-1",
			ProviderID: 42,
			Title:      "Example.Release",
		}},
	}
	store := &archiveBlobStoreStub{}
	svc := NewService(
		repo,
		archiveResolverStub{payload: []byte("<nzb />")},
		store,
		archiveLoggerStub{},
		Options{BatchSize: 10},
	)

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}

	if got := metrics["archived_count"]; got != 1 {
		t.Fatalf("archived_count=%v want 1", got)
	}
	if repo.stored == nil {
		t.Fatalf("expected archive metadata to be persisted")
	}
	if repo.stored.ObjectKey != "releases/42/rel-1/33936bd10869da15571c725a43add185e1e263ca96ac2ad018c3fd8c4b3edf4c.nzb" {
		t.Fatalf("unexpected object key %q", repo.stored.ObjectKey)
	}
	if string(store.saved["releases/42/rel-1/33936bd10869da15571c725a43add185e1e263ca96ac2ad018c3fd8c4b3edf4c.nzb"]) != "<nzb />" {
		t.Fatalf("expected payload to be written to archive store")
	}
}

type archiveRepoStub struct {
	candidates []pgindex.ReleaseArchiveCandidate
	stored     *pgindex.ReleaseArchiveStoredRecord
	failed     map[string]string
}

func (s *archiveRepoStub) ClaimReleaseArchiveCandidates(context.Context, int, pgindex.ReleaseReadyPolicy) ([]pgindex.ReleaseArchiveCandidate, error) {
	return s.candidates, nil
}

func (s *archiveRepoStub) MarkReleaseArchiveStored(_ context.Context, in pgindex.ReleaseArchiveStoredRecord) error {
	cp := in
	s.stored = &cp
	return nil
}

func (s *archiveRepoStub) MarkReleaseArchiveFailed(_ context.Context, releaseID, errText string) error {
	if s.failed == nil {
		s.failed = map[string]string{}
	}
	s.failed[releaseID] = errText
	return nil
}

type archiveResolverStub struct {
	payload []byte
}

func (s archiveResolverStub) GetNZB(context.Context, *domain.Release) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.payload)), nil
}

type archiveBlobStoreStub struct {
	saved map[string][]byte
}

func (s *archiveBlobStoreStub) SaveNZBAtomically(key string, data []byte) error {
	if s.saved == nil {
		s.saved = map[string][]byte{}
	}
	s.saved[key] = append([]byte(nil), data...)
	return nil
}

type archiveLoggerStub struct{}

func (archiveLoggerStub) Info(string, ...interface{}) {}
func (archiveLoggerStub) Warn(string, ...interface{}) {}
