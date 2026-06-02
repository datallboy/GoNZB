package releasegenerate

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type testRepo struct {
	candidates []pgindex.ReleaseNZBGenerateCandidate
	policy     pgindex.ReleaseReadyPolicy
	stored     *pgindex.ReleaseArchiveStoredRecord
	failed     map[string]string
}

func (r *testRepo) ListReleaseNZBGenerateCandidates(_ context.Context, _ int, policy pgindex.ReleaseReadyPolicy) ([]pgindex.ReleaseNZBGenerateCandidate, error) {
	r.policy = policy
	return append([]pgindex.ReleaseNZBGenerateCandidate(nil), r.candidates...), nil
}

func (r *testRepo) MarkReleaseArchiveStored(_ context.Context, in pgindex.ReleaseArchiveStoredRecord) error {
	cp := in
	r.stored = &cp
	return nil
}

func (r *testRepo) MarkReleaseArchiveFailed(_ context.Context, releaseID, errText string) error {
	if r.failed == nil {
		r.failed = map[string]string{}
	}
	r.failed[releaseID] = errText
	return nil
}

type testResolver struct {
	fail map[string]error
	seen []string
}

func (r *testResolver) GetNZB(_ context.Context, rel *domain.Release) (io.ReadCloser, error) {
	r.seen = append(r.seen, rel.ID)
	if err := r.fail[rel.ID]; err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("<nzb/>")), nil
}

type testBlobStore struct {
	saved map[string][]byte
}

func (s *testBlobStore) SaveNZBAtomically(key string, data []byte) error {
	if s.saved == nil {
		s.saved = map[string][]byte{}
	}
	s.saved[key] = append([]byte(nil), data...)
	return nil
}

func TestRunOnceWithMetrics(t *testing.T) {
	repo := &testRepo{
		candidates: []pgindex.ReleaseNZBGenerateCandidate{
			{ReleaseID: "rel-1", ProviderID: 7, Title: "One"},
			{ReleaseID: "rel-2", ProviderID: 7, Title: "Two"},
		},
	}
	resolver := &testResolver{
		fail: map[string]error{"rel-2": fmt.Errorf("boom")},
	}
	store := &testBlobStore{}
	svc := NewService(repo, resolver, store, Options{
		BatchSize: 25,
		Policy: pgindex.ReleaseReadyPolicy{
			MinMatchConfidence: 0.8,
			MinCompletionPct:   100,
			MinIdentityStatus:  "identified",
			RequireInspection:  true,
			RequireEnrichment:  true,
		},
	})

	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("RunOnceWithMetrics error = %v", err)
	}
	if got, want := metrics["generate_candidates"], 2; got != want {
		t.Fatalf("generate_candidates = %v want %d", got, want)
	}
	if got, want := metrics["generate_attempted"], 2; got != want {
		t.Fatalf("generate_attempted = %v want %d", got, want)
	}
	if got, want := metrics["archived_count"], 1; got != want {
		t.Fatalf("archived_count = %v want %d", got, want)
	}
	if got, want := metrics["generate_failures"], 1; got != want {
		t.Fatalf("generate_failures = %v want %d", got, want)
	}
	if len(resolver.seen) != 2 {
		t.Fatalf("resolver seen = %v want 2 items", resolver.seen)
	}
	if repo.stored == nil || repo.stored.ObjectKey == "" {
		t.Fatalf("expected archive metadata to be stored, got %+v", repo.stored)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved blobs = %d want 1", len(store.saved))
	}
	if repo.policy.MinIdentityStatus != "identified" || !repo.policy.RequireInspection || !repo.policy.RequireEnrichment {
		t.Fatalf("repo policy = %+v", repo.policy)
	}
}
