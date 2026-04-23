package par2

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

func TestRunOnceCapturesPAR2SetAndOnlySetsHasPAR2(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakePAR2Repository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        71,
			ReleaseID:       "rel-par2",
			FileName:        "example.vol03+04.par2",
			SourceUpdatedAt: &now,
			TotalBytes:      8,
		}},
		files: []pgindex.CatalogReleaseFile{{
			ID:        801,
			BinaryID:  71,
			FileName:  "example.vol03+04.par2",
			SizeBytes: 8,
		}},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		par2Fetcher{body: []byte("PAR2\x00P\x01\x02"), fileName: "example.vol03+04.par2"},
		testPAR2Logger{},
		inspectpkg.Options{MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.par2Sets) != 1 {
		t.Fatalf("expected one par2 set, got %+v", repo.par2Sets)
	}
	row := repo.par2Sets[0]
	if !row.IsVolume || row.VolumeNumber != 3 || row.RecoveryBlocks != 4 || !row.SignatureOK {
		t.Fatalf("unexpected par2 set row %+v", row)
	}
	if row.BaseName != "example.par2" {
		t.Fatalf("expected base name example.par2, got %q", row.BaseName)
	}
	if len(repo.releaseUpdates) != 1 || !boolValuePAR2(repo.releaseUpdates[0].HasPAR2) {
		t.Fatalf("expected has_par2 update, got %+v", repo.releaseUpdates)
	}
	if len(repo.completed) != 1 {
		t.Fatalf("expected one completed inspection, got %+v", repo.completed)
	}
	if got := repo.completed[0].Summary["signature_ok"]; got != true {
		t.Fatalf("expected signature_ok summary, got %+v", repo.completed[0].Summary)
	}
	if len(repo.artifacts) != 1 || repo.artifacts[0].ArtifactRole != "prefix_sample" {
		t.Fatalf("expected one prefix sample artifact, got %+v", repo.artifacts)
	}
	if repo.artifacts[0].ArtifactPath != "" {
		t.Fatalf("expected no transient artifact path, got %+v", repo.artifacts[0])
	}
	if _, ok := repo.completed[0].Summary["workspace_path"]; ok {
		t.Fatalf("expected no transient workspace_path in summary, got %+v", repo.completed[0].Summary)
	}
	update := repo.releaseUpdates[0]
	if update.HasNFO != nil || update.Encrypted != nil || update.Passworded != nil || update.VideoCount != nil {
		t.Fatalf("expected par2 stage to avoid unrelated fields, got %+v", update)
	}
}

func TestRunOnceCompletesDeterministicPAR2SampleFailuresWithoutRetryChurn(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakePAR2Repository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        72,
			ReleaseID:       "rel-par2-missing",
			FileName:        "missing.par2",
			SourceUpdatedAt: &now,
			TotalBytes:      8,
		}},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		par2Fetcher{body: []byte("PAR2\x00P\x01\x02"), fileName: "missing.par2"},
		testPAR2Logger{},
		inspectpkg.Options{MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.failed) != 0 {
		t.Fatalf("expected deterministic input to avoid failed status churn, got %+v", repo.failed)
	}
	if len(repo.completed) != 1 {
		t.Fatalf("expected one completed inspection, got %+v", repo.completed)
	}
	summary := repo.completed[0].Summary
	if summary["probe_skip_reason"] != "release_file_missing" {
		t.Fatalf("expected release_file_missing skip, got %+v", summary)
	}
	if summary["probe_error_detail"] == "" {
		t.Fatalf("expected probe_error_detail, got %+v", summary)
	}
	if _, ok := summary["workspace_path"]; ok {
		t.Fatalf("expected no transient workspace_path in skipped summary, got %+v", summary)
	}
}

type fakePAR2Repository struct {
	candidates     []pgindex.BinaryInspectionCandidate
	files          []pgindex.CatalogReleaseFile
	completed      []pgindex.BinaryInspectionRecord
	failed         []pgindex.BinaryInspectionRecord
	artifacts      []pgindex.BinaryInspectionArtifactRecord
	par2Sets       []pgindex.BinaryPAR2SetRecord
	releaseUpdates []pgindex.ReleaseInspectionUpdate
}

func (f *fakePAR2Repository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakePAR2Repository) StartBinaryInspection(context.Context, string, int64, string, *time.Time) error {
	return nil
}

func (f *fakePAR2Repository) CompleteBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.completed = append(f.completed, in)
	return nil
}

func (f *fakePAR2Repository) FailBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.failed = append(f.failed, in)
	return nil
}

func (f *fakePAR2Repository) ReplaceBinaryInspectionArtifacts(_ context.Context, _ string, _ int64, rows []pgindex.BinaryInspectionArtifactRecord) error {
	f.artifacts = append(f.artifacts, rows...)
	return nil
}

func (f *fakePAR2Repository) ReplaceBinaryPAR2Sets(_ context.Context, _ int64, rows []pgindex.BinaryPAR2SetRecord) error {
	f.par2Sets = append(f.par2Sets, rows...)
	return nil
}

func (f *fakePAR2Repository) ApplyReleaseInspectionUpdate(_ context.Context, in pgindex.ReleaseInspectionUpdate) error {
	f.releaseUpdates = append(f.releaseUpdates, in)
	return nil
}

func (f *fakePAR2Repository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return f.files, nil
}

func (f *fakePAR2Repository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return []pgindex.CatalogArticleRef{{MessageID: "<par2-1>", Bytes: 8, PartNumber: 1}}, nil
}

func (f *fakePAR2Repository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type par2Fetcher struct {
	body     []byte
	fileName string
}

func (f par2Fetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	payload := fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=%s\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d pcrc32=00000000\r\n", len(f.body), f.fileName, len(f.body), encodeYEncPAR2(f.body), len(f.body))
	return bytes.NewBufferString(payload), nil
}

type testPAR2Logger struct{}

func (testPAR2Logger) Debug(string, ...interface{}) {}
func (testPAR2Logger) Info(string, ...interface{})  {}
func (testPAR2Logger) Warn(string, ...interface{})  {}
func (testPAR2Logger) Error(string, ...interface{}) {}

func encodeYEncPAR2(data []byte) string {
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

func boolValuePAR2(v *bool) bool {
	return v != nil && *v
}
