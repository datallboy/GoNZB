package main

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestBuildNZBModelPreservesFullReleaseGroupSetPerFile(t *testing.T) {
	postedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	groups := []string{"alt.binaries.group.a", "alt.binaries.group.b"}

	fileA := exportFileFromCatalog(pgindex.CatalogReleaseFile{
		ID:        1,
		BinaryID:  101,
		FileName:  "movie.part01.rar",
		Subject:   "movie.part01.rar",
		Poster:    "poster-a",
		PostedAt:  &postedAt,
		SizeBytes: 1000,
		FileIndex: 1,
	}, groups, []pgindex.CatalogArticleRef{
		{MessageID: "<msg-2@test>", Bytes: 200, PartNumber: 2},
		{MessageID: "<msg-1@test>", Bytes: 100, PartNumber: 1},
	})
	fileB := exportFileFromCatalog(pgindex.CatalogReleaseFile{
		ID:        2,
		BinaryID:  102,
		FileName:  "movie.part02.rar",
		Subject:   "",
		Poster:    "poster-b",
		PostedAt:  &postedAt,
		SizeBytes: 2000,
		FileIndex: 2,
	}, groups, []pgindex.CatalogArticleRef{
		{MessageID: "<msg-3@test>", Bytes: 300, PartNumber: 1},
	})

	model := buildNZBModel("Recovered Release", []nzbExportFile{fileA, fileB})
	if len(model.Files) != 2 {
		t.Fatalf("expected 2 nzb files, got %d", len(model.Files))
	}
	for i, file := range model.Files {
		if len(file.Groups) != 2 {
			t.Fatalf("file %d expected full group set, got %v", i, file.Groups)
		}
		if file.Groups[0] != groups[0] || file.Groups[1] != groups[1] {
			t.Fatalf("file %d unexpected groups: got %v want %v", i, file.Groups, groups)
		}
	}
	if model.Files[0].Segments[0] != (nzb.Segment{Number: 2, Bytes: 200, MessageID: "msg-2@test"}) {
		t.Fatalf("unexpected first file segment[0]: %+v", model.Files[0].Segments[0])
	}
	if model.Files[0].Segments[1] != (nzb.Segment{Number: 1, Bytes: 100, MessageID: "msg-1@test"}) {
		t.Fatalf("unexpected first file segment[1]: %+v", model.Files[0].Segments[1])
	}
	if model.Files[1].Subject != "movie.part02.rar" {
		t.Fatalf("expected empty subject to fall back to file name, got %q", model.Files[1].Subject)
	}
}
