package archive

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
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

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), nil, nil, nil, testArchiveLogger{}, inspectpkg.Options{})
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
	if len(repo.artifacts) != 1 || len(repo.artifacts[0]) != 1 {
		t.Fatalf("expected one archive artifact row, got %+v", repo.artifacts)
	}
	if repo.artifacts[0][0].ArtifactPath != "" {
		t.Fatalf("expected no transient artifact path, got %+v", repo.artifacts[0][0])
	}
	if _, ok := repo.artifacts[0][0].Metadata["probe_path"]; ok {
		t.Fatalf("expected no transient probe_path in artifact metadata, got %+v", repo.artifacts[0][0].Metadata)
	}
	if len(repo.completed) != 1 {
		t.Fatalf("expected one completed archive inspection, got %d", len(repo.completed))
	}
	if _, ok := repo.completed[0].Summary["workspace_path"]; ok {
		t.Fatalf("expected no transient workspace_path in summary, got %+v", repo.completed[0].Summary)
	}
	if _, ok := repo.completed[0].Summary["probe_path"]; ok {
		t.Fatalf("expected no transient probe_path in summary, got %+v", repo.completed[0].Summary)
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
	if update.PasswordState != "password_unknown" {
		t.Fatalf("expected password_unknown state, got %q", update.PasswordState)
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

func TestRunOnceDedupesObfuscatedSplitRARCandidates(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeArchiveRepository{
		candidates: []pgindex.BinaryInspectionCandidate{
			{BinaryID: 41, ReleaseID: "rel-archive", FileName: "random.part001.rar", SourceUpdatedAt: &now},
			{BinaryID: 42, ReleaseID: "rel-archive", FileName: "other.part02.rar", SourceUpdatedAt: &now},
			{BinaryID: 43, ReleaseID: "rel-archive", FileName: "third.part03.rar", SourceUpdatedAt: &now},
		},
		files: []pgindex.CatalogReleaseFile{
			{ID: 501, BinaryID: 41, FileName: "random.part001.rar", FileIndex: 1, SizeBytes: 2048},
			{ID: 502, BinaryID: 42, FileName: "other.part02.rar", FileIndex: 2, SizeBytes: 2048},
			{ID: 503, BinaryID: 43, FileName: "third.part03.rar", FileIndex: 3, SizeBytes: 2048},
		},
	}

	svc := NewService(repo, inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir()}), nil, nil, nil, testArchiveLogger{}, inspectpkg.Options{})
	candidates, err := svc.dedupeCandidates(context.Background(), repo.candidates)
	if err != nil {
		t.Fatalf("dedupe candidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected one deduped archive candidate, got %d", len(candidates))
	}
	if candidates[0].BinaryID != 41 {
		t.Fatalf("expected first RAR part representative to win, got %+v", candidates[0])
	}
}

func TestRunOncePersistsPasswordUnknownWhenArchiveProbePromptsForPassword(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeArchiveRepository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:        41,
			ReleaseID:       "rel-password-prompt",
			FileName:        "locked.release.2026.part01.rar",
			SourceUpdatedAt: &now,
		}},
		files: []pgindex.CatalogReleaseFile{{
			ID:        501,
			BinaryID:  41,
			FileName:  "locked.release.2026.part01.rar",
			FileIndex: 1,
			SizeBytes: 2048,
		}},
		articlesByFileID: map[int64][]pgindex.CatalogArticleRef{
			501: {{MessageID: "<part1@test>", PartNumber: 1, Bytes: 4}},
		},
	}

	svc := NewService(
		repo,
		inspectpkg.NewWorkspaceManager(inspectpkg.Options{WorkDir: t.TempDir(), SevenZipPath: "7z"}),
		fakeArchiveFetcher{},
		fakeArchiveCommandRunner{output: "Enter password (will not be echoed):", err: fmt.Errorf("password required")},
		nil,
		testArchiveLogger{},
		inspectpkg.Options{SevenZipPath: "7z"},
	)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if len(repo.completed) != 1 {
		t.Fatalf("expected one completed archive inspection, got %d", len(repo.completed))
	}
	summary := repo.completed[0].Summary
	if got := summary["probe_skip_reason"]; got != "password_required" {
		t.Fatalf("expected password_required skip reason, got %+v", got)
	}
	if got := summary["encrypted"]; got != true {
		t.Fatalf("expected encrypted summary true, got %+v", got)
	}
	if len(repo.releaseUpdates) != 1 {
		t.Fatalf("expected one release update, got %d", len(repo.releaseUpdates))
	}
	update := repo.releaseUpdates[0]
	if !boolValue(update.Encrypted) || !boolValue(update.Passworded) || !boolValue(update.PasswordedUnknown) {
		t.Fatalf("expected encrypted unresolved password update, got %+v", update)
	}
	if update.PasswordState != "password_unknown" {
		t.Fatalf("expected password_unknown state, got %q", update.PasswordState)
	}
}

type fakeArchiveRepository struct {
	candidates         []pgindex.BinaryInspectionCandidate
	files              []pgindex.CatalogReleaseFile
	articlesByFileID   map[int64][]pgindex.CatalogArticleRef
	completed          []pgindex.BinaryInspectionRecord
	artifacts          [][]pgindex.BinaryInspectionArtifactRecord
	archiveEntries     [][]pgindex.BinaryArchiveEntryRecord
	passwordCandidates []pgindex.ReleasePasswordCandidateRecord
	releaseUpdates     []pgindex.ReleaseInspectionUpdate
}

func (f *fakeArchiveRepository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakeArchiveRepository) ClaimBinaryInspectionCandidates(context.Context, pgindex.BinaryInspectionClaimRequest) ([]pgindex.BinaryInspectionCandidate, error) {
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

func (f *fakeArchiveRepository) ListCatalogReleaseFileArticles(_ context.Context, releaseFileID int64) ([]pgindex.CatalogArticleRef, error) {
	return f.articlesByFileID[releaseFileID], nil
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

type fakeArchiveFetcher struct{}

func (fakeArchiveFetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	body := []byte("Rar!")
	crc := crc32.ChecksumIEEE(body)
	payload := fmt.Sprintf("=ybegin part=1 total=1 line=128 size=%d name=sample.part01.rar\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d pcrc32=%08x\r\n", len(body), len(body), encodeArchiveYEnc(body), len(body), crc)
	return bytes.NewBufferString(payload), nil
}

type fakeArchiveCommandRunner struct {
	output  string
	err     error
	runFunc func(context.Context, string, ...string) ([]byte, error)
}

func (f fakeArchiveCommandRunner) Run(ctx context.Context, tool string, args ...string) ([]byte, error) {
	if f.runFunc != nil {
		return f.runFunc(ctx, tool, args...)
	}
	return []byte(f.output), f.err
}

func (f fakeArchiveCommandRunner) RunInput(_ context.Context, _ io.Reader, _ string, _ ...string) ([]byte, error) {
	return []byte(f.output), f.err
}

func encodeArchiveYEnc(data []byte) string {
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
