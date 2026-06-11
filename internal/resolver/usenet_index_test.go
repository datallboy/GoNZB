package resolver

import (
	"context"
	"encoding/xml"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type nzbDoc struct {
	Files []struct {
		Groups []string `xml:"groups>group"`
	} `xml:"file"`
}

func TestBuildNZBUsesPerFileNewsgroupWhenAvailable(t *testing.T) {
	resolver := &usenetIndexResolver{
		catalog: &fakeUsenetIndexCatalog{
			fileArticles: map[int64][]pgindex.CatalogArticleRef{
				1: {{MessageID: "<a@x>", Bytes: 10, PartNumber: 1}},
				2: {{MessageID: "<b@x>", Bytes: 10, PartNumber: 1}},
			},
		},
	}

	payload, err := resolver.buildNZB(context.Background(), &domain.Release{
		ID:          "rel-1",
		Title:       "Example",
		Poster:      "poster",
		PublishDate: time.Unix(1700000000, 0).UTC(),
	}, []pgindex.CatalogReleaseFile{
		{ID: 1, FileName: "file-a.rar", GroupName: "alt.test.a"},
		{ID: 2, FileName: "file-b.rar", GroupName: "alt.test.b"},
	}, []string{"alt.test.a", "alt.test.b"})
	if err != nil {
		t.Fatalf("build nzb: %v", err)
	}

	var doc nzbDoc
	if err := xml.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("unmarshal nzb: %v", err)
	}
	if len(doc.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(doc.Files))
	}
	if len(doc.Files[0].Groups) != 1 || doc.Files[0].Groups[0] != "alt.test.a" {
		t.Fatalf("expected first file to stay on alt.test.a, got %+v", doc.Files[0].Groups)
	}
	if len(doc.Files[1].Groups) != 1 || doc.Files[1].Groups[0] != "alt.test.b" {
		t.Fatalf("expected second file to stay on alt.test.b, got %+v", doc.Files[1].Groups)
	}
}

type fakeUsenetIndexCatalog struct {
	fileArticles map[int64][]pgindex.CatalogArticleRef
}

func (f *fakeUsenetIndexCatalog) GetCatalogReleaseByID(context.Context, string) (*domain.Release, error) {
	return nil, nil
}

func (f *fakeUsenetIndexCatalog) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return nil, nil
}

func (f *fakeUsenetIndexCatalog) ListCatalogReleaseFileArticles(_ context.Context, releaseFileID int64) ([]pgindex.CatalogArticleRef, error) {
	return f.fileArticles[releaseFileID], nil
}

func (f *fakeUsenetIndexCatalog) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return nil, nil
}

func (f *fakeUsenetIndexCatalog) GetReleaseArchiveState(context.Context, string) (*pgindex.ReleaseArchiveState, error) {
	return nil, nil
}
