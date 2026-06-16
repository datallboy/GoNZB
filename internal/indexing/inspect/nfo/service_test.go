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
	if len(repo.artifacts) != 1 || repo.artifacts[0].ArtifactPath != "" {
		t.Fatalf("expected one artifact without transient path, got %+v", repo.artifacts)
	}
	if len(repo.completed) != 1 {
		t.Fatalf("expected one completed inspection, got %+v", repo.completed)
	}
	if _, ok := repo.completed[0].Summary["workspace_path"]; ok {
		t.Fatalf("expected no transient workspace_path in summary, got %+v", repo.completed[0].Summary)
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

func TestRunOnceRecordsBadNFOCandidateAndContinues(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeNFORepository{
		candidates: []pgindex.BinaryInspectionCandidate{
			{
				BinaryID:        61,
				ReleaseID:       "rel-bad-nfo",
				FileName:        "bad.nfo",
				SourceUpdatedAt: &now,
				TotalBytes:      64,
			},
			{
				BinaryID:        62,
				ReleaseID:       "rel-good-nfo",
				FileName:        "good.nfo",
				SourceUpdatedAt: &now,
				TotalBytes:      64,
			},
		},
		files: []pgindex.CatalogReleaseFile{{
			ID:        701,
			BinaryID:  61,
			FileName:  "bad.nfo",
			SizeBytes: 64,
		}, {
			ID:        702,
			BinaryID:  62,
			FileName:  "good.nfo",
			SizeBytes: 64,
		}},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		&sequenceNFOFetcher{
			payloads: []string{
				"not a yenc encoded article",
				yencNFOPayload([]byte("hello"), "good.nfo"),
				yencNFOPayload([]byte("hello"), "good.nfo"),
			},
		},
		testNFOLogger{},
		inspectpkg.Options{MaxBytes: 1024},
	)
	metrics, err := svc.RunOnceWithMetrics(context.Background())
	if err != nil {
		t.Fatalf("run once should continue after bad nfo candidate: %v", err)
	}
	if metrics["processed_count"] != 2 || metrics["failed_count"] != 1 {
		t.Fatalf("unexpected metrics: %+v failed=%+v", metrics, repo.failed)
	}
	if len(repo.failed) != 1 || repo.failed[0].BinaryID != 61 {
		t.Fatalf("expected one failed inspection for bad candidate, got %+v", repo.failed)
	}
	if len(repo.completed) != 1 || repo.completed[0].BinaryID != 62 {
		t.Fatalf("expected good candidate to complete, got %+v", repo.completed)
	}
}

type fakeNFORepository struct {
	candidates         []pgindex.BinaryInspectionCandidate
	files              []pgindex.CatalogReleaseFile
	completed          []pgindex.BinaryInspectionRecord
	failed             []pgindex.BinaryInspectionRecord
	artifacts          []pgindex.BinaryInspectionArtifactRecord
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

func (f *fakeNFORepository) CompleteBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.completed = append(f.completed, in)
	return nil
}

func (f *fakeNFORepository) FailBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.failed = append(f.failed, in)
	return nil
}

func (f *fakeNFORepository) ReplaceBinaryInspectionArtifacts(_ context.Context, _ string, _ int64, rows []pgindex.BinaryInspectionArtifactRecord) error {
	f.artifacts = append(f.artifacts, rows...)
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
	return bytes.NewBufferString(yencNFOPayload(f.body, f.fileName)), nil
}

type sequenceNFOFetcher struct {
	payloads []string
	next     int
}

func (f *sequenceNFOFetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	if f.next >= len(f.payloads) {
		return bytes.NewBufferString(""), nil
	}
	payload := f.payloads[f.next]
	f.next++
	return bytes.NewBufferString(payload), nil
}

func yencNFOPayload(body []byte, fileName string) string {
	return fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=%s\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d pcrc32=00000000\r\n", len(body), fileName, len(body), encodeYEncNFO(body), len(body))
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
