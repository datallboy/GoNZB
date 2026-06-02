package media

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

func TestRunOnceUsesFFProbeFactsAndOnlyUpdatesMediaOutputs(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeMediaRepository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        51,
			ReleaseID:       "rel-media",
			ReleaseTitle:    "Example.Feature.2026",
			FileName:        "example.feature.2026.mkv",
			SourceUpdatedAt: &now,
			TotalBytes:      8,
		}},
		files: []pgindex.CatalogReleaseFile{{
			ID:        601,
			BinaryID:  51,
			FileName:  "example.feature.2026.mkv",
			SizeBytes: 8,
		}},
	}

	ffprobeJSON := `{"format":{"duration":"5400","bit_rate":"9000000","format_name":"matroska","format_long_name":"Matroska","probe_score":100},"streams":[{"index":0,"codec_type":"video","codec_name":"hevc","codec_long_name":"H.265 / HEVC","width":1920,"height":1080,"duration":"5400","bit_rate":"8000000","disposition":{"default":1,"forced":0}},{"index":1,"codec_type":"audio","codec_name":"aac","codec_long_name":"AAC","channels":6,"duration":"5400","bit_rate":"384000","tags":{"language":"eng"},"disposition":{"default":1,"forced":0}},{"index":2,"codec_type":"subtitle","codec_name":"subrip","codec_long_name":"SubRip","duration":"5400","tags":{"language":"spa"},"disposition":{"default":0,"forced":0}}]}`

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		mediaFetcher{body: []byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00, 0x00, 0x00}, fileName: "example.feature.2026.mkv"},
		mediaRunner{output: []byte(ffprobeJSON)},
		nil,
		testMediaLogger{},
		inspectpkg.Options{FFProbePath: "ffprobe", MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.mediaStreams) != 3 {
		t.Fatalf("expected 3 media streams, got %d", len(repo.mediaStreams))
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected one release update, got %d", len(repo.releaseUpdates))
	}

	update := repo.releaseUpdates[0]
	if intValueMedia(update.VideoCount) != 1 || intValueMedia(update.AudioCount) != 1 || intValueMedia(update.RuntimeSeconds) != 5400 {
		t.Fatalf("unexpected media counts/runtime, got %+v", update)
	}
	if update.PrimaryResolution != "1080p" || update.PrimaryVideoCodec != "hevc" || update.PrimaryAudioCodec != "aac" {
		t.Fatalf("unexpected primary media facts, got %+v", update)
	}
	if len(update.SubtitleLanguages) != 1 || update.SubtitleLanguages[0] != "spa" {
		t.Fatalf("expected subtitle language spa, got %+v", update.SubtitleLanguages)
	}
	if update.Passworded != nil || update.PasswordedKnown != nil || update.PasswordedUnknown != nil || update.Encrypted != nil {
		t.Fatalf("expected media stage not to touch password/encryption flags, got %+v", update)
	}
	if len(repo.completed) != 1 || repo.completed[0].Summary["probe_mode"] != "ffprobe_direct" {
		t.Fatalf("expected ffprobe_direct summary, got %+v", repo.completed)
	}
	if _, ok := repo.completed[0].Summary["workspace_path"]; ok {
		t.Fatalf("expected no transient workspace_path in summary, got %+v", repo.completed[0].Summary)
	}
	if len(repo.artifacts) != 1 {
		t.Fatalf("expected one artifact row, got %+v", repo.artifacts)
	}
	if repo.artifacts[0].ArtifactPath != "" {
		t.Fatalf("expected no transient artifact path, got %+v", repo.artifacts[0])
	}
	if _, ok := repo.artifacts[0].Metadata["archive_path"]; ok {
		t.Fatalf("expected no transient archive path in artifact metadata, got %+v", repo.artifacts[0].Metadata)
	}
}

func TestRunOnceSkipsArchiveProbeWhenArchiveEntryAlreadyHasStrongMediaSignals(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeMediaRepository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:           77,
			ReleaseID:          "rel-archive-media",
			ReleaseTitle:       "Obfuscated.Release",
			FileName:           "archive.7z.001",
			SourceUpdatedAt:    &now,
			TotalBytes:         1024,
			ArchiveSummaryJSON: []byte(`{"archive_entries":["Example.Movie.2026.1080p.BluRay.x265.DTS-HD.mkv"]}`),
		}},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}),
		nil,
		nil,
		nil,
		testMediaLogger{},
		inspectpkg.Options{FFProbePath: "ffprobe", MaxBytes: 1024},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.mediaStreams) != 0 {
		t.Fatalf("expected no media streams when archive probe is skipped, got %d", len(repo.mediaStreams))
	}
	if len(repo.artifacts) != 0 {
		t.Fatalf("expected no artifact rows when archive probe is skipped, got %+v", repo.artifacts)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected one release update, got %d", len(repo.releaseUpdates))
	}

	update := repo.releaseUpdates[0]
	if update.PrimaryResolution != "1080p" || update.PrimaryVideoCodec != "x265" || update.PrimaryAudioCodec != "dts-hd" {
		t.Fatalf("unexpected heuristic archive media facts, got %+v", update)
	}
	if intValueMedia(update.VideoCount) != 1 || intValueMedia(update.AudioCount) != 0 {
		t.Fatalf("unexpected heuristic media counts, got %+v", update)
	}
	if len(repo.completed) != 1 || repo.completed[0].Summary["probe_mode"] != "heuristic_archive_entry" {
		t.Fatalf("expected heuristic_archive_entry summary, got %+v", repo.completed)
	}
	if _, ok := repo.completed[0].Summary["probe_skip_reason"]; ok {
		t.Fatalf("expected no probe_skip_reason on heuristic archive completion, got %+v", repo.completed[0].Summary)
	}
}

type fakeMediaRepository struct {
	candidates     []pgindex.BinaryInspectionCandidate
	files          []pgindex.CatalogReleaseFile
	completed      []pgindex.BinaryInspectionRecord
	artifacts      []pgindex.BinaryInspectionArtifactRecord
	mediaStreams   []pgindex.BinaryMediaStreamRecord
	releaseUpdates []pgindex.ReleaseInspectionUpdate
	previewKey     string
}

func (f *fakeMediaRepository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakeMediaRepository) ClaimBinaryInspectionCandidates(context.Context, pgindex.BinaryInspectionClaimRequest) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakeMediaRepository) StartBinaryInspection(context.Context, string, int64, string, *time.Time) error {
	return nil
}

func (f *fakeMediaRepository) CompleteBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.completed = append(f.completed, in)
	return nil
}

func (f *fakeMediaRepository) FailBinaryInspection(context.Context, pgindex.BinaryInspectionRecord) error {
	return nil
}

func (f *fakeMediaRepository) ReplaceBinaryInspectionArtifacts(_ context.Context, _ string, _ int64, rows []pgindex.BinaryInspectionArtifactRecord) error {
	f.artifacts = append(f.artifacts, rows...)
	return nil
}

func (f *fakeMediaRepository) ReplaceBinaryMediaStreams(_ context.Context, _ int64, rows []pgindex.BinaryMediaStreamRecord) error {
	f.mediaStreams = append(f.mediaStreams, rows...)
	return nil
}

func (f *fakeMediaRepository) ApplyReleaseInspectionUpdate(_ context.Context, in pgindex.ReleaseInspectionUpdate) error {
	f.releaseUpdates = append(f.releaseUpdates, in)
	return nil
}

func (f *fakeMediaRepository) SetReleaseArchivePreview(_ context.Context, _ string, objectKey, _ string, _ string) error {
	f.previewKey = objectKey
	return nil
}

func (f *fakeMediaRepository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return f.files, nil
}

func (f *fakeMediaRepository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return []pgindex.CatalogArticleRef{{MessageID: "<media-1>", Bytes: 8, PartNumber: 1}}, nil
}

func (f *fakeMediaRepository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type mediaFetcher struct {
	body     []byte
	fileName string
}

func (f mediaFetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	payload := fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=%s\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d pcrc32=00000000\r\n", len(f.body), f.fileName, len(f.body), encodeYEncMedia(f.body), len(f.body))
	return bytes.NewBufferString(payload), nil
}

type mediaRunner struct {
	output []byte
}

func (f mediaRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return f.output, nil
}

func (f mediaRunner) RunInput(context.Context, io.Reader, string, ...string) ([]byte, error) {
	return f.output, nil
}

type testMediaLogger struct{}

func (testMediaLogger) Debug(string, ...interface{}) {}
func (testMediaLogger) Info(string, ...interface{})  {}
func (testMediaLogger) Warn(string, ...interface{})  {}
func (testMediaLogger) Error(string, ...interface{}) {}

func encodeYEncMedia(data []byte) string {
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

func intValueMedia(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
