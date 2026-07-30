package evidencerepair

import (
	"context"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/evidenceclient"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type fakeStore struct {
	completed bool
}

func (*fakeStore) RefreshBinaryExchangeIdentities(context.Context, int) (int, error) { return 1, nil }
func (*fakeStore) SeedBinaryEvidenceRepairWork(context.Context, int) (int, error)    { return 1, nil }
func (*fakeStore) ClaimBinaryEvidenceRepairWork(context.Context, string, int, time.Duration) ([]pgindex.BinaryEvidenceRepairCandidate, error) {
	return []pgindex.BinaryEvidenceRepairCandidate{{
		BinaryID: 7, Scheme: "yenc_v1", MatchID: "match",
		MissingParts: []int{2}, Anchors: []string{"<one@example>"},
	}}, nil
}
func (s *fakeStore) CompleteBinaryEvidenceRepairWork(context.Context, int64, bool, string) error {
	s.completed = true
	return nil
}
func (*fakeStore) BinaryEffectiveComplete(context.Context, int64) (bool, error) { return true, nil }

type fakeClient struct{}

func (fakeClient) LookupSegments(context.Context, int64, string, string, []int, []string) (evidenceclient.SegmentLookupResult, error) {
	return evidenceclient.SegmentLookupResult{Imported: 1, PeerRequests: 1}, nil
}

func TestRunOnceCompletesFromPeerSegment(t *testing.T) {
	store := &fakeStore{}
	result, err := New(store, fakeClient{}, "test", 10).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !store.completed || result.Imported != 1 || result.Completed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
