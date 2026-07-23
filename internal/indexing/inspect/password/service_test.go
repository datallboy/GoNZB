package password

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
				FileName:        "sample.rar",
				SourceUpdatedAt: &now,
				ArchiveSummaryJSON: mustJSON(t, map[string]any{
					"encrypted": true,
				}),
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

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), fakeFetcher{}, fakeCommandRunner{verifyOK: true}, testLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.updatedCandidateStatuses) != 1 {
		t.Fatalf("expected 1 password candidate status update, got %d", len(repo.updatedCandidateStatuses))
	}
	if repo.updatedCandidateStatuses[0].status != "verified" {
		t.Fatalf("expected verified status, got %q", repo.updatedCandidateStatuses[0].status)
	}
	if repo.releaseUpdates[0].PreferredPasswordID == nil || *repo.releaseUpdates[0].PreferredPasswordID != 10 {
		t.Fatalf("expected preferred password id 10, got %#v", repo.releaseUpdates[0].PreferredPasswordID)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected 1 release inspection update, got %d", len(repo.releaseUpdates))
	}
	if repo.releaseUpdates[0].PasswordState != "password_known" {
		t.Fatalf("expected password_known, got %q", repo.releaseUpdates[0].PasswordState)
	}
	if len(repo.completedInspections) != 1 {
		t.Fatalf("expected completed inspection, got %d", len(repo.completedInspections))
	}
	if len(repo.artifactRows) != 1 {
		t.Fatalf("expected one artifact row, got %+v", repo.artifactRows)
	}
	if repo.artifactRows[0].ArtifactPath != "" {
		t.Fatalf("expected no transient artifact path, got %+v", repo.artifactRows[0])
	}
}

func TestRunOnceSkipsNonEncryptedArchiveCandidates(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{
		binaryCandidates: []pgindex.BinaryInspectionCandidate{
			{
				BinaryID:        8,
				ReleaseID:       "rel-2",
				ReleaseTitle:    "Show.S01E02",
				FileName:        "sample.rar",
				SourceUpdatedAt: &now,
				ArchiveSummaryJSON: mustJSON(t, map[string]any{
					"encrypted": false,
				}),
			},
		},
	}

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), fakeFetcher{}, fakeCommandRunner{}, testLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.completedInspections) != 0 {
		t.Fatalf("expected no completed inspections, got %d", len(repo.completedInspections))
	}
	if len(repo.releaseUpdates) != 0 {
		t.Fatalf("expected no release updates, got %d", len(repo.releaseUpdates))
	}
	if len(repo.updatedCandidateStatuses) != 0 {
		t.Fatalf("expected no password candidate status updates, got %d", len(repo.updatedCandidateStatuses))
	}
}

func TestRunOnceMarksEncryptedReleaseUnknownWhenNoPasswordCandidatesExist(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{
		binaryCandidates: []pgindex.BinaryInspectionCandidate{
			{
				BinaryID:        7,
				ReleaseID:       "rel-unknown",
				ReleaseTitle:    "Locked.Release.2026",
				FileName:        "locked.rar",
				SourceUpdatedAt: &now,
				ArchiveSummaryJSON: mustJSON(t, map[string]any{
					"encrypted": true,
				}),
			},
		},
	}

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), fakeFetcher{}, fakeCommandRunner{}, testLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.updatedCandidateStatuses) != 0 {
		t.Fatalf("expected no candidate status updates, got %d", len(repo.updatedCandidateStatuses))
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected 1 release update, got %d", len(repo.releaseUpdates))
	}
	update := repo.releaseUpdates[0]
	if !boolPtrValue(update.Passworded) || boolPtrValue(update.PasswordedKnown) || !boolPtrValue(update.PasswordedUnknown) {
		t.Fatalf("expected password flags true/false/true, got %+v", update)
	}
	if update.PasswordState != "password_unknown" {
		t.Fatalf("expected password_unknown state, got %q", update.PasswordState)
	}
	if update.PreferredPasswordID != nil {
		t.Fatalf("expected no preferred password id, got %#v", update.PreferredPasswordID)
	}
}

func TestRunOnceRejectsFalsePositivePasswordHintAndKeepsUnknownState(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{
		binaryCandidates: []pgindex.BinaryInspectionCandidate{
			{
				BinaryID:        7,
				ReleaseID:       "rel-false-positive",
				ReleaseTitle:    "Release password:not-it",
				FileName:        "locked.rar",
				SourceUpdatedAt: &now,
				ArchiveSummaryJSON: mustJSON(t, map[string]any{
					"encrypted": true,
				}),
			},
		},
		passwordCandidates: []pgindex.PasswordVerificationCandidate{
			{
				ID:            21,
				ReleaseID:     "rel-false-positive",
				BinaryID:      7,
				PasswordValue: "not-it",
				SourceRef:     "title-hint",
				Title:         "Release password:not-it",
			},
		},
	}

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), fakeFetcher{}, fakeCommandRunner{}, testLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.updatedCandidateStatuses) != 1 {
		t.Fatalf("expected 1 password candidate status update, got %d", len(repo.updatedCandidateStatuses))
	}
	if repo.updatedCandidateStatuses[0].status != "rejected" {
		t.Fatalf("expected rejected status, got %q", repo.updatedCandidateStatuses[0].status)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected 1 release update, got %d", len(repo.releaseUpdates))
	}
	update := repo.releaseUpdates[0]
	if !boolPtrValue(update.Passworded) || boolPtrValue(update.PasswordedKnown) || !boolPtrValue(update.PasswordedUnknown) {
		t.Fatalf("expected password flags true/false/true, got %+v", update)
	}
	if update.PasswordState != "password_unknown" {
		t.Fatalf("expected password_unknown state, got %q", update.PasswordState)
	}
}

func TestRunOnceVerifiesLaterPasswordCandidateAfterRejectingEarlierHint(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{
		binaryCandidates: []pgindex.BinaryInspectionCandidate{
			{
				BinaryID:        7,
				ReleaseID:       "rel-multi",
				ReleaseTitle:    "Show.S01E01 password:wrong-one",
				FileName:        "sample.rar",
				SourceUpdatedAt: &now,
				ArchiveSummaryJSON: mustJSON(t, map[string]any{
					"encrypted": true,
				}),
			},
		},
		passwordCandidates: []pgindex.PasswordVerificationCandidate{
			{
				ID:            31,
				ReleaseID:     "rel-multi",
				BinaryID:      7,
				PasswordValue: "wrong-one",
				SourceRef:     "title-hint",
				Title:         "Show.S01E01 password:wrong-one",
			},
			{
				ID:            32,
				ReleaseID:     "rel-multi",
				BinaryID:      7,
				PasswordValue: "open-sesame",
				SourceRef:     "predb",
				Title:         "Show.S01E01",
			},
		},
	}

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), fakeFetcher{}, fakeCommandRunner{acceptedPassword: "open-sesame"}, testLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.updatedCandidateStatuses) != 2 {
		t.Fatalf("expected 2 password candidate updates, got %d", len(repo.updatedCandidateStatuses))
	}
	if repo.updatedCandidateStatuses[0].status != "rejected" || repo.updatedCandidateStatuses[1].status != "verified" {
		t.Fatalf("expected rejected then verified statuses, got %+v", repo.updatedCandidateStatuses)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected 1 release update, got %d", len(repo.releaseUpdates))
	}
	update := repo.releaseUpdates[0]
	if !boolPtrValue(update.Passworded) || !boolPtrValue(update.PasswordedKnown) || boolPtrValue(update.PasswordedUnknown) {
		t.Fatalf("expected password flags true/true/false, got %+v", update)
	}
	if update.PreferredPasswordID == nil || *update.PreferredPasswordID != 32 {
		t.Fatalf("expected preferred password id 32, got %#v", update.PreferredPasswordID)
	}
	if update.PasswordState != "password_known" {
		t.Fatalf("expected password_known, got %q", update.PasswordState)
	}
}

type fakeRepository struct {
	binaryCandidates         []pgindex.BinaryInspectionCandidate
	passwordCandidates       []pgindex.PasswordVerificationCandidate
	completedInspections     []pgindex.BinaryInspectionRecord
	failedInspections        []pgindex.BinaryInspectionRecord
	artifactRows             []pgindex.BinaryInspectionArtifactRecord
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

func (f *fakeRepository) ListBinaryInspectionCandidatesWithOptions(context.Context, string, int, pgindex.BinaryInspectionCandidateOptions) ([]pgindex.BinaryInspectionCandidate, error) {
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

func (f *fakeRepository) ReplaceBinaryInspectionArtifacts(_ context.Context, _ string, _ int64, rows []pgindex.BinaryInspectionArtifactRecord) error {
	f.artifactRows = append(f.artifactRows, rows...)
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

func (f *fakeRepository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return []pgindex.CatalogReleaseFile{{
		ID:        70,
		BinaryID:  7,
		FileName:  "sample.rar",
		SizeBytes: 4,
	}}, nil
}

func (f *fakeRepository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return []pgindex.CatalogArticleRef{{
		MessageID:  "<msg-1>",
		Bytes:      4,
		PartNumber: 1,
	}}, nil
}

func (f *fakeRepository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type testLogger struct{}

func (testLogger) Debug(string, ...interface{}) {}
func (testLogger) Info(string, ...interface{})  {}
func (testLogger) Warn(string, ...interface{})  {}
func (testLogger) Error(string, ...interface{}) {}

func mustJSON(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
}

type fakeFetcher struct{}

func (fakeFetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	body := []byte("Rar!")
	payload := fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=sample.rar\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d pcrc32=00000000\r\n", len(body), len(body), encodeYEnc(body), len(body))
	return bytes.NewBufferString(payload), nil
}

type fakeCommandRunner struct {
	verifyOK         bool
	acceptedPassword string
}

func (f fakeCommandRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if f.acceptedPassword != "" {
		for _, arg := range args {
			if strings.TrimSpace(arg) == "-p"+f.acceptedPassword {
				return []byte("Path = sample.rar\n"), nil
			}
		}
		return []byte("Wrong password"), fmt.Errorf("wrong password")
	}
	if f.verifyOK {
		return []byte("Path = sample.rar\n"), nil
	}
	return []byte("Wrong password"), fmt.Errorf("wrong password")
}

func (f fakeCommandRunner) RunInput(ctx context.Context, _ io.Reader, name string, args ...string) ([]byte, error) {
	return f.Run(ctx, name, args...)
}

func boolPtrValue(v *bool) bool {
	return v != nil && *v
}

func encodeYEnc(data []byte) string {
	out := make([]byte, 0, len(data))
	for _, b := range data {
		enc := b + 42
		if enc == 0 || enc == '\n' || enc == '\r' || enc == '=' {
			out = append(out, '=')
			enc += 64
		}
		out = append(out, enc)
	}
	return string(out)
}
