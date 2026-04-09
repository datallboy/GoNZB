package nfo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestRunOnceStoresTextEvidenceAndOnlySetsHasNFO(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeNFORepository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        61,
			ReleaseID:       "rel-nfo",
			ReleaseTitle:    "Obfuscated.Release",
			FileName:        "release.nfo",
			SourceUpdatedAt: &now,
			TotalBytes:      64,
		}},
		files: []pgindex.CatalogReleaseFile{{
			ID:        701,
			BinaryID:  61,
			FileName:  "release.nfo",
			SizeBytes: 64,
		}},
	}

	nfoText := "Example.Feature.2026.1080p.BluRay.x265-GRP\nPassword: swordfish\n"
	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		nfoFetcher{body: []byte(nfoText), fileName: "release.nfo"},
		testNFOLogger{},
		inspectpkg.Options{MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.textEvidence) != 1 || repo.textEvidence[0].EvidenceKind != "nfo_text" {
		t.Fatalf("expected one nfo text evidence row, got %+v", repo.textEvidence)
	}
	if len(repo.passwordCandidates) != 1 || repo.passwordCandidates[0].PasswordValue != "swordfish" {
		t.Fatalf("expected one nfo password candidate, got %+v", repo.passwordCandidates)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected one release update, got %d", len(repo.releaseUpdates))
	}

	update := repo.releaseUpdates[0]
	if !boolValueNFO(update.HasNFO) {
		t.Fatalf("expected has_nfo to be true, got %+v", update)
	}
	if update.HasPAR2 != nil || update.Encrypted != nil || update.Passworded != nil || update.VideoCount != nil {
		t.Fatalf("expected nfo stage to avoid unrelated fields, got %+v", update)
	}
}

type fakeNFORepository struct {
	candidates         []pgindex.BinaryInspectionCandidate
	files              []pgindex.CatalogReleaseFile
	textEvidence       []pgindex.BinaryTextEvidenceRecord
	passwordCandidates []pgindex.ReleasePasswordCandidateRecord
	releaseUpdates     []pgindex.ReleaseInspectionUpdate
}

func (f *fakeNFORepository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakeNFORepository) StartBinaryInspection(context.Context, string, int64, string, *time.Time) error {
	return nil
}

func (f *fakeNFORepository) CompleteBinaryInspection(context.Context, pgindex.BinaryInspectionRecord) error {
	return nil
}

func (f *fakeNFORepository) FailBinaryInspection(context.Context, pgindex.BinaryInspectionRecord) error {
	return nil
}

func (f *fakeNFORepository) ReplaceBinaryInspectionArtifacts(context.Context, string, int64, []pgindex.BinaryInspectionArtifactRecord) error {
	return nil
}

func (f *fakeNFORepository) ReplaceBinaryTextEvidence(_ context.Context, _ string, _ int64, rows []pgindex.BinaryTextEvidenceRecord) error {
	f.textEvidence = append(f.textEvidence, rows...)
	return nil
}

func (f *fakeNFORepository) UpsertReleasePasswordCandidate(_ context.Context, in pgindex.ReleasePasswordCandidateRecord) (int64, error) {
	f.passwordCandidates = append(f.passwordCandidates, in)
	return int64(len(f.passwordCandidates)), nil
}

func (f *fakeNFORepository) ApplyReleaseInspectionUpdate(_ context.Context, in pgindex.ReleaseInspectionUpdate) error {
	f.releaseUpdates = append(f.releaseUpdates, in)
	return nil
}

func (f *fakeNFORepository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return f.files, nil
}

func (f *fakeNFORepository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return []pgindex.CatalogArticleRef{{MessageID: "<nfo-1>", Bytes: 64, PartNumber: 1}}, nil
}

func (f *fakeNFORepository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type nfoFetcher struct {
	body     []byte
	fileName string
}

func (f nfoFetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	payload := fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=%s\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d pcrc32=00000000\r\n", len(f.body), f.fileName, len(f.body), encodeYEncNFO(f.body), len(f.body))
	return bytes.NewBufferString(payload), nil
}

type testNFOLogger struct{}

func (testNFOLogger) Debug(string, ...interface{}) {}
func (testNFOLogger) Info(string, ...interface{})  {}
func (testNFOLogger) Warn(string, ...interface{})  {}
func (testNFOLogger) Error(string, ...interface{}) {}

func encodeYEncNFO(data []byte) string {
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

func boolValueNFO(v *bool) bool {
	return v != nil && *v
}
