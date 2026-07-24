package discovery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestDiscoveryCompletesTerminalPrefixErrorsWithoutRetrying(t *testing.T) {
	tests := []struct {
		name       string
		articles   []pgindex.CatalogArticleRef
		fetcher    inspectpkg.ArticleFetcher
		skipReason string
	}{
		{
			name:       "articles moved after claim",
			skipReason: "candidate_no_longer_materializable",
		},
		{
			name:       "first available article is not the prefix",
			articles:   []pgindex.CatalogArticleRef{{MessageID: "<part-2>", PartNumber: 1}},
			fetcher:    staticDiscoveryFetcher{body: yencPartTwoBody()},
			skipReason: "prefix_not_available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeDiscoveryRepository{
				candidates: []pgindex.BinaryInspectionCandidate{{
					BinaryID:   42,
					FileName:   "opaque.bin",
					TotalBytes: 2,
				}},
				articles: tt.articles,
			}
			fetcher := tt.fetcher
			if fetcher == nil {
				fetcher = staticDiscoveryFetcher{}
			}
			service := NewService(repo, fetcher, nil, inspectpkg.Options{
				CandidateBatchSize: 1,
				Concurrency:        1,
				MaxBytes:           4096,
			})

			if err := service.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if len(repo.failed) != 0 {
				t.Fatalf("expected terminal sample error not to be retried, got %+v", repo.failed)
			}
			if len(repo.completed) != 1 {
				t.Fatalf("expected one completed skip record, got %+v", repo.completed)
			}
			if got := repo.completed[0].Summary["probe_skip_reason"]; got != tt.skipReason {
				t.Fatalf("probe_skip_reason = %v, want %q", got, tt.skipReason)
			}
		})
	}
}

func TestDiscoveryRetriesTransientFetchErrors(t *testing.T) {
	repo := &fakeDiscoveryRepository{
		candidates: []pgindex.BinaryInspectionCandidate{{
			BinaryID:   43,
			FileName:   "opaque.bin",
			TotalBytes: 2,
		}},
		articles: []pgindex.CatalogArticleRef{{MessageID: "<part-1>", PartNumber: 1}},
	}
	service := NewService(repo, staticDiscoveryFetcher{err: errors.New("temporary NNTP failure")}, nil, inspectpkg.Options{
		CandidateBatchSize: 1,
		Concurrency:        1,
		MaxBytes:           4096,
	})

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(repo.completed) != 0 {
		t.Fatalf("expected transient fetch failure not to complete, got %+v", repo.completed)
	}
	if len(repo.failed) != 1 {
		t.Fatalf("expected one retryable failure, got %+v", repo.failed)
	}
	if got := repo.failed[0].ErrorText; got != "sample opaque binary prefix: fetch article <part-1>: temporary NNTP failure" {
		t.Fatalf("error text = %q", got)
	}
}

type fakeDiscoveryRepository struct {
	candidates []pgindex.BinaryInspectionCandidate
	articles   []pgindex.CatalogArticleRef
	completed  []pgindex.BinaryInspectionRecord
	failed     []pgindex.BinaryInspectionRecord
}

func (f *fakeDiscoveryRepository) ListBinaryInspectionCandidates(context.Context, string, int) ([]pgindex.BinaryInspectionCandidate, error) {
	return f.candidates, nil
}

func (f *fakeDiscoveryRepository) StartBinaryInspection(context.Context, string, int64, string, *time.Time) error {
	return nil
}

func (f *fakeDiscoveryRepository) CompleteBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.completed = append(f.completed, in)
	return nil
}

func (f *fakeDiscoveryRepository) FailBinaryInspection(_ context.Context, in pgindex.BinaryInspectionRecord) error {
	f.failed = append(f.failed, in)
	return nil
}

func (f *fakeDiscoveryRepository) ApplyBinaryRecovery(context.Context, pgindex.BinaryRecoveryRecord) error {
	return nil
}

func (f *fakeDiscoveryRepository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return nil, nil
}

func (f *fakeDiscoveryRepository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return nil, nil
}

func (f *fakeDiscoveryRepository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

func (f *fakeDiscoveryRepository) GetCatalogBinaryFile(_ context.Context, binaryID int64) (*pgindex.CatalogReleaseFile, error) {
	return &pgindex.CatalogReleaseFile{BinaryID: binaryID, FileName: "opaque.bin", SizeBytes: 2}, nil
}

func (f *fakeDiscoveryRepository) ListCatalogBinaryArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return f.articles, nil
}

func (f *fakeDiscoveryRepository) ListCatalogBinaryNewsgroups(context.Context, int64) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type staticDiscoveryFetcher struct {
	body []byte
	err  error
}

func (f staticDiscoveryFetcher) Fetch(context.Context, string, []string) (io.Reader, error) {
	if f.err != nil {
		return nil, f.err
	}
	return bytes.NewReader(f.body), nil
}

func yencPartTwoBody() []byte {
	return []byte("=ybegin part=2 total=2 line=128 size=2 name=opaque.bin\r\n=ypart begin=2 end=2\r\nk\r\n=yend size=1\r\n")
}
