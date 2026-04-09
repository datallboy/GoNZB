package archive

import (
	"context"
	"testing"
	"time"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceAppliesArchivePasswordStateWithoutTouchingMediaFields(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeArchiveRepository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        41,
			ReleaseID:       "rel-archive",
			ReleaseTitle:    "Locked.Release.2026 password:open-sesame",
			FileName:        "locked.release.2026.7z.001",
			SourceUpdatedAt: &now,
		}},
		files: []pgindex.CatalogReleaseFile{{
			ID:        501,
			BinaryID:  41,
			FileName:  "locked.release.2026.7z.001",
			SizeBytes: 2048,
		}},
	}

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), nil, nil, testArchiveLogger{}, inspectpkg.Options{})
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.passwordCandidates) != 1 {
		t.Fatalf("expected one password candidate, got %d", len(repo.passwordCandidates))
	}
	if repo.passwordCandidates[0].PasswordValue != "open-sesame" {
		t.Fatalf("expected extracted password open-sesame, got %+v", repo.passwordCandidates[0])
	}
	if len(repo.archiveEntries) != 1 || len(repo.archiveEntries[0]) != 0 {
		t.Fatalf("expected empty archive entry replacement, got %+v", repo.archiveEntries)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected one release update, got %d", len(repo.releaseUpdates))
	}

	update := repo.releaseUpdates[0]
	if !boolValue(update.Encrypted) || !boolValue(update.Passworded) || !boolValue(update.PasswordedUnknown) {
		t.Fatalf("expected encrypted unresolved password state, got %+v", update)
	}
	if boolValue(update.PasswordedKnown) {
		t.Fatalf("expected no known password flag, got %+v", update)
	}
	if update.PasswordState != "passworded_unknown" {
		t.Fatalf("expected passworded_unknown state, got %q", update.PasswordState)
	}
	if intValue(update.ArchiveCount) != 1 {
		t.Fatalf("expected archive_count 1, got %+v", update.ArchiveCount)
	}
	if update.PrimaryResolution != "" || update.PrimaryVideoCodec != "" || update.PrimaryAudioCodec != "" {
		t.Fatalf("expected archive stage to leave media fields empty, got %+v", update)
	}
	if update.HasPAR2 != nil || update.HasNFO != nil || update.VideoCount != nil || update.AudioCount != nil {
		t.Fatalf("expected archive stage to avoid unrelated fields, got %+v", update)
	}
}

type fakeArchiveRepository struct {
	candidates         []pgindex.BinaryInspectionCandidate
	files              []pgindex.CatalogReleaseFile
	completed          []pgindex.BinaryInspectionRecord
	artifacts          [][]pgindex.BinaryInspectionArtifactRecord
	archiveEntries     [][]pgindex.BinaryArchiveEntryRecord
	passwordCandidates []pgindex.ReleasePasswordCandidateRecord
	releaseUpdates     []pgindex.ReleaseInspectionUpdate
}

func (f *fakeArchiveRepository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakeArchiveRepository) StartBinaryInspection(context.Context, string, int64, string, *time.Time) error {
	return nil
}

func (f *fakeArchiveRepository) CompleteBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.completed = append(f.completed, in)
	return nil
}

func (f *fakeArchiveRepository) FailBinaryInspection(context.Context, pgindex.BinaryInspectionRecord) error {
	return nil
}

func (f *fakeArchiveRepository) ReplaceBinaryInspectionArtifacts(_ context.Context, _ string, _ int64, rows []pgindex.BinaryInspectionArtifactRecord) error {
	f.artifacts = append(f.artifacts, rows)
	return nil
}

func (f *fakeArchiveRepository) ReplaceBinaryArchiveEntries(_ context.Context, _ int64, rows []pgindex.BinaryArchiveEntryRecord) error {
	f.archiveEntries = append(f.archiveEntries, rows)
	return nil
}

func (f *fakeArchiveRepository) UpsertReleasePasswordCandidate(_ context.Context, in pgindex.ReleasePasswordCandidateRecord) (int64, error) {
	f.passwordCandidates = append(f.passwordCandidates, in)
	return int64(len(f.passwordCandidates)), nil
}

func (f *fakeArchiveRepository) ApplyReleaseInspectionUpdate(_ context.Context, in pgindex.ReleaseInspectionUpdate) error {
	f.releaseUpdates = append(f.releaseUpdates, in)
	return nil
}

func (f *fakeArchiveRepository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return f.files, nil
}

func (f *fakeArchiveRepository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return nil, nil
}

func (f *fakeArchiveRepository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type testArchiveLogger struct{}

func (testArchiveLogger) Debug(string, ...interface{}) {}
func (testArchiveLogger) Info(string, ...interface{})  {}
func (testArchiveLogger) Warn(string, ...interface{})  {}
func (testArchiveLogger) Error(string, ...interface{}) {}

func boolValue(v *bool) bool {
	return v != nil && *v
}

func intValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
