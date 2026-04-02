package password

import (
	"context"
	"testing"
	"time"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceVerifiesPasswordCandidateAndRollsUpReleaseState(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{
		binaryCandidates: []pgindex.BinaryInspectionCandidate{
			{
				BinaryID:        7,
				ReleaseID:       "rel-1",
				ReleaseTitle:    "Show.S01E01 password:open-sesame",
				SourceUpdatedAt: &now,
			},
		},
		passwordCandidates: []pgindex.PasswordVerificationCandidate{
			{
				ID:            10,
				ReleaseID:     "rel-1",
				BinaryID:      7,
				PasswordValue: "open-sesame",
				SourceRef:     "password:open-sesame",
				Title:         "Show.S01E01 password:open-sesame",
			},
		},
	}

	svc := NewService(repo, testLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.updatedCandidateStatuses) != 1 {
		t.Fatalf("expected 1 password candidate status update, got %d", len(repo.updatedCandidateStatuses))
	}
	if repo.updatedCandidateStatuses[0].status != "verified" {
		t.Fatalf("expected verified status, got %q", repo.updatedCandidateStatuses[0].status)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected 1 release inspection update, got %d", len(repo.releaseUpdates))
	}
	if repo.releaseUpdates[0].PasswordState != "passworded_known" {
		t.Fatalf("expected passworded_known, got %q", repo.releaseUpdates[0].PasswordState)
	}
	if len(repo.completedInspections) != 1 {
		t.Fatalf("expected completed inspection, got %d", len(repo.completedInspections))
	}
}

type fakeRepository struct {
	binaryCandidates         []pgindex.BinaryInspectionCandidate
	passwordCandidates       []pgindex.PasswordVerificationCandidate
	startedInspections       []int64
	completedInspections     []pgindex.BinaryInspectionRecord
	failedInspections        []pgindex.BinaryInspectionRecord
	updatedCandidateStatuses []candidateStatusUpdate
	releaseUpdates           []pgindex.ReleaseInspectionUpdate
}

type candidateStatusUpdate struct {
	id     int64
	status string
}

func (f *fakeRepository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.binaryCandidates, nil
}

func (f *fakeRepository) ListPasswordVerificationCandidates(context.Context, int) ([]pgindex.PasswordVerificationCandidate, error) {
	return f.passwordCandidates, nil
}

func (f *fakeRepository) StartBinaryInspection(context.Context, string, int64, string, *time.Time) error {
	return nil
}

func (f *fakeRepository) CompleteBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.completedInspections = append(f.completedInspections, in)
	return nil
}

func (f *fakeRepository) FailBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.failedInspections = append(f.failedInspections, in)
	return nil
}

func (f *fakeRepository) UpdateReleasePasswordCandidateStatus(_ context.Context, candidateID int64, status string, _ *time.Time, _ string) error {
	f.updatedCandidateStatuses = append(f.updatedCandidateStatuses, candidateStatusUpdate{id: candidateID, status: status})
	return nil
}

func (f *fakeRepository) ApplyReleaseInspectionUpdate(_ context.Context, in pgindex.ReleaseInspectionUpdate) error {
	f.releaseUpdates = append(f.releaseUpdates, in)
	return nil
}

type testLogger struct{}

func (testLogger) Debug(string, ...interface{}) {}
func (testLogger) Info(string, ...interface{})  {}
func (testLogger) Warn(string, ...interface{})  {}
func (testLogger) Error(string, ...interface{}) {}
