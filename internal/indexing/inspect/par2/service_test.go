package par2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
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
		par2Fetcher{body: buildPAR2Sample("target.part01.rar", 123456), fileName: "example.vol03+04.par2"},
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
	if len(repo.par2Targets) != 1 {
		t.Fatalf("expected one par2 target, got %+v", repo.par2Targets)
	}
	if repo.par2Targets[0].FileName != "target.part01.rar" || repo.par2Targets[0].FileSize != 123456 {
		t.Fatalf("unexpected par2 target %+v", repo.par2Targets[0])
	}
	if len(repo.coverageRows) != 1 || repo.coverageRows[0].FileName != "target.part01.rar" {
		t.Fatalf("expected par2 target coverage rows, got %+v", repo.coverageRows)
	}
	if len(repo.completed) != 1 {
		t.Fatalf("expected one completed inspection, got %+v", repo.completed)
	}
	if got := repo.completed[0].Summary["signature_ok"]; got != true {
		t.Fatalf("expected signature_ok summary, got %+v", repo.completed[0].Summary)
	}
	if got := repo.completed[0].Summary["target_count"]; got != 1 {
		t.Fatalf("expected target_count=1, got %+v", repo.completed[0].Summary)
	}
	if got := repo.completed[0].Summary["main_target_count"]; got != 1 {
		t.Fatalf("expected main_target_count=1, got %+v", repo.completed[0].Summary)
	}
	if got := repo.completed[0].Summary["target_coverage_updates"]; got != 3 {
		t.Fatalf("expected target_coverage_updates=3, got %+v", repo.completed[0].Summary)
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

func TestRunOnceInspectsStandalonePAR2BinaryBeforeReleaseFormation(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakePAR2Repository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        73,
			FileName:        "standalone.par2",
			SourceUpdatedAt: &now,
			TotalBytes:      8,
		}},
		standaloneFile: &pgindex.CatalogReleaseFile{
			BinaryID:  73,
			FileName:  "standalone.par2",
			SizeBytes: 8,
			IsPars:    true,
		},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		par2Fetcher{body: buildPAR2Sample("target.part01.rar", 123456), fileName: "standalone.par2"},
		testPAR2Logger{},
		inspectpkg.Options{MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.completed) != 1 {
		t.Fatalf("expected completed standalone inspection, got %+v", repo.completed)
	}
	if len(repo.releaseUpdates) != 0 {
		t.Fatalf("expected no release update before release exists, got %+v", repo.releaseUpdates)
	}
	if len(repo.coverageRows) != 1 {
		t.Fatalf("expected coverage to apply for standalone par2, got %+v", repo.coverageRows)
	}
}

func TestRunOnceFallsBackToFullManifestMaterializationForPlainPAR2(t *testing.T) {
	now := time.Now().UTC()
	full := buildPAR2Sample("target.part01.rar", 123456)
	split := len(full) / 2
	repo := &fakePAR2Repository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        74,
			FileName:        "manifest.par2",
			SourceUpdatedAt: &now,
			TotalBytes:      int64(len(full)),
		}},
		standaloneFile: &pgindex.CatalogReleaseFile{
			BinaryID:  74,
			FileName:  "manifest.par2",
			SizeBytes: int64(len(full)),
			IsPars:    true,
		},
		standaloneArticles: []pgindex.CatalogArticleRef{
			{MessageID: "<manifest-par2-1>", Bytes: int64(split), PartNumber: 1},
			{MessageID: "<manifest-par2-2>", Bytes: int64(len(full) - split), PartNumber: 2},
		},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		par2Fetcher{
			parts: map[string]par2Part{
				"<manifest-par2-1>": {body: full[:split], fileName: "manifest.par2", begin: 1, totalSize: len(full)},
				"<manifest-par2-2>": {body: full[split:], fileName: "manifest.par2", begin: split + 1, totalSize: len(full)},
			},
		},
		testPAR2Logger{},
		inspectpkg.Options{MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.par2Targets) != 1 {
		t.Fatalf("expected one par2 target after full manifest fallback, got %+v", repo.par2Targets)
	}
	if got := repo.completed[0].Summary["full_manifest_fallback"]; got != true {
		t.Fatalf("expected full_manifest_fallback=true, got %+v", repo.completed[0].Summary)
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

func TestPAR2RunBudgetIsBounded(t *testing.T) {
	cases := []struct {
		name        string
		toolTimeout time.Duration
		want        time.Duration
	}{
		{name: "default", toolTimeout: 0, want: 2 * time.Minute},
		{name: "small timeout has floor", toolTimeout: time.Second, want: 30 * time.Second},
		{name: "normal timeout multiplies", toolTimeout: 20 * time.Second, want: 80 * time.Second},
		{name: "large timeout has ceiling", toolTimeout: 10 * time.Minute, want: 2 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := par2RunBudget(inspectpkg.Options{ToolTimeout: tc.toolTimeout}); got != tc.want {
				t.Fatalf("expected run budget %s, got %s", tc.want, got)
			}
		})
	}
}

func TestPAR2WorkerCountAllowsTwentyWorkers(t *testing.T) {
	if got := par2WorkerCount(inspectpkg.Options{Concurrency: 20}, 1000); got != 20 {
		t.Fatalf("expected 20 workers, got %d", got)
	}
	if got := par2WorkerCount(inspectpkg.Options{Concurrency: 40}, 1000); got != 32 {
		t.Fatalf("expected safety cap of 32 workers, got %d", got)
	}
}

func TestPAR2ProbeSkipReasonClassifiesOperationalCauses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "article missing on provider",
			err:  errors.New("fetch article <abc@example>: article not found (430)"),
			want: "article_not_found",
		},
		{
			name: "binary missing",
			err:  errors.New("binary 123 not found"),
			want: "binary_not_found",
		},
		{
			name: "yenc header decode",
			err:  errors.New("decode article <abc@example> header: invalid yenc header"),
			want: "yenc_header_decode_failed",
		},
		{
			name: "yenc body decode",
			err:  errors.New("decode article <abc@example> body: invalid escape"),
			want: "yenc_body_decode_failed",
		},
		{
			name: "yenc verify checksum",
			err:  errors.New("decode article <abc@example> verify: checksum mismatch"),
			want: "article_checksum_mismatch",
		},
		{
			name: "unsupported standalone",
			err:  errors.New("standalone binary materialization is not supported"),
			want: "standalone_materialization_unsupported",
		},
		{
			name: "unknown prefix failure",
			err:  errors.New("fetch article <abc@example>: malformed response"),
			want: "prefix_sample_failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := par2ProbeSkipReason(tc.err); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestTrimPAR2ProbeErrorDetailBoundsLogPayload(t *testing.T) {
	detail := trimPAR2ProbeErrorDetail(errors.New(strings.Repeat("x", 300)))
	if len(detail) > 243 {
		t.Fatalf("expected bounded detail, got length %d", len(detail))
	}
	if !strings.HasSuffix(detail, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", detail)
	}
}

func TestParseTargetFilesRejectsImplausibleNamesAndSizes(t *testing.T) {
	sample := append(buildPAR2Sample("valid.part01.rar", 1234), buildPAR2Sample("\x7f\x12\x15\x1e", 1234)...)
	sample = append(sample, buildPAR2Sample("absurd.part02.rar", 1<<60)...)

	targets := parseTargetFiles(sample)
	if len(targets) != 1 {
		t.Fatalf("expected one valid target, got %+v", targets)
	}
	if targets[0].Name != "valid.part01.rar" || targets[0].Size != 1234 {
		t.Fatalf("unexpected valid target %+v", targets[0])
	}
}

type fakePAR2Repository struct {
	candidates         []pgindex.BinaryInspectionCandidate
	files              []pgindex.CatalogReleaseFile
	standaloneArticles []pgindex.CatalogArticleRef
	completed          []pgindex.BinaryInspectionRecord
	failed             []pgindex.BinaryInspectionRecord
	artifacts          []pgindex.BinaryInspectionArtifactRecord
	par2Sets           []pgindex.BinaryPAR2SetRecord
	par2Targets        []pgindex.BinaryPAR2TargetRecord
	coverageRows       []pgindex.BinaryPAR2TargetRecord
	releaseUpdates     []pgindex.ReleaseInspectionUpdate
	standaloneFile     *pgindex.CatalogReleaseFile
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

func (f *fakePAR2Repository) ReplaceBinaryPAR2Targets(_ context.Context, _ int64, rows []pgindex.BinaryPAR2TargetRecord) error {
	f.par2Targets = append(f.par2Targets, rows...)
	return nil
}

func (f *fakePAR2Repository) ApplyBinaryPAR2TargetCoverage(_ context.Context, _ int64, rows []pgindex.BinaryPAR2TargetRecord) (*pgindex.BinaryPAR2TargetCoverageResult, error) {
	f.coverageRows = append(f.coverageRows, rows...)
	return &pgindex.BinaryPAR2TargetCoverageResult{TargetCount: len(rows), MainTargetCount: len(rows), UpdatedBinaryCount: 3}, nil
}

func (f *fakePAR2Repository) ApplyReleaseInspectionUpdate(_ context.Context, in pgindex.ReleaseInspectionUpdate) error {
	f.releaseUpdates = append(f.releaseUpdates, in)
	return nil
}

func (f *fakePAR2Repository) ApplyPAR2InspectionBatch(_ context.Context, rows []pgindex.PAR2InspectionBatchRecord) (*pgindex.PAR2InspectionBatchResult, error) {
	out := &pgindex.PAR2InspectionBatchResult{}
	for _, row := range rows {
		f.artifacts = append(f.artifacts, row.ArtifactRows...)
		f.par2Sets = append(f.par2Sets, row.PAR2SetRows...)
		f.par2Targets = append(f.par2Targets, row.PAR2TargetRows...)
		f.coverageRows = append(f.coverageRows, row.PAR2TargetRows...)
		summary := map[string]any{}
		for k, v := range row.Summary {
			summary[k] = v
		}
		if len(row.PAR2TargetRows) > 0 {
			summary["main_target_count"] = len(row.PAR2TargetRows)
			summary["target_coverage_updates"] = 3
		}
		f.completed = append(f.completed, pgindex.BinaryInspectionRecord{
			StageName:         row.StageName,
			BinaryID:          row.BinaryID,
			ReleaseID:         row.ReleaseID,
			Status:            "completed",
			MaterializedBytes: row.MaterializedBytes,
			ToolProvenance:    row.ToolProvenance,
			Summary:           summary,
			SourceUpdatedAt:   row.SourceUpdatedAt,
		})
		out.FlushedCandidates++
		out.RowsWritten += int64(len(row.ArtifactRows) + len(row.PAR2SetRows) + len(row.PAR2TargetRows) + 1)
	}
	return out, nil
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

func (f *fakePAR2Repository) GetCatalogBinaryFile(context.Context, int64) (*pgindex.CatalogReleaseFile, error) {
	return f.standaloneFile, nil
}

func (f *fakePAR2Repository) ListCatalogBinaryArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	if len(f.standaloneArticles) > 0 {
		return f.standaloneArticles, nil
	}
	return []pgindex.CatalogArticleRef{{MessageID: "<standalone-par2-1>", Bytes: 8, PartNumber: 1}}, nil
}

func (f *fakePAR2Repository) ListCatalogBinaryNewsgroups(context.Context, int64) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type par2Fetcher struct {
	body     []byte
	fileName string
	parts    map[string]par2Part
}

type par2Part struct {
	body      []byte
	fileName  string
	begin     int
	totalSize int
}

func (f par2Fetcher) Fetch(_ context.Context, messageID string, _ []string) (io.Reader, error) {
	if len(f.parts) > 0 {
		part, ok := f.parts[messageID]
		if !ok {
			return nil, fmt.Errorf("missing par2 test part for %s", messageID)
		}
		begin := part.begin
		if begin <= 0 {
			begin = 1
		}
		totalSize := part.totalSize
		if totalSize <= 0 {
			totalSize = len(part.body)
		}
		end := begin + len(part.body) - 1
		payload := fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=%s\r\n=ypart begin=%d end=%d\r\n%s\r\n=yend size=%d pcrc32=00000000\r\n", totalSize, part.fileName, begin, end, encodeYEncPAR2(part.body), len(part.body))
		return bytes.NewBufferString(payload), nil
	}
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

func buildPAR2Sample(fileName string, fileSize uint64) []byte {
	name := append([]byte(fileName), 0)
	for len(name)%4 != 0 {
		name = append(name, 0)
	}
	packetLen := 64 + 56 + len(name)
	packet := make([]byte, packetLen)
	copy(packet[:8], []byte("PAR2\x00PKT"))
	binary.LittleEndian.PutUint64(packet[8:16], uint64(packetLen))
	copy(packet[48:64], []byte("PAR 2.0\x00FileDesc"))
	binary.LittleEndian.PutUint64(packet[64+48:64+56], fileSize)
	copy(packet[64+56:], name)
	return packet
}
