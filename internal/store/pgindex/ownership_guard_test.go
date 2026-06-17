package pgindex

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBinaryStorageV2WritesStayInExplicitOwners(t *testing.T) {
	allowedWriterFiles := map[string]bool{
		"archive_store.go":         true,
		"assembly_store.go":        true,
		"binary_recovery_store.go": true,
		"release_store.go":         true,
		"yenc_recovery_store.go":   true,
	}
	tables := []string{
		"binary_core",
		"binary_observation_stats",
		"binary_identity_current",
		"binary_recovery_current",
		"binary_lifecycle",
		"binary_projection_events",
	}

	patterns := make([]*regexp.Regexp, 0, len(tables))
	for _, table := range tables {
		patterns = append(patterns, regexp.MustCompile(`(?is)\b(insert\s+into|update|delete\s+from)\s+(public\.)?`+regexp.QuoteMeta(table)+`\b`))
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pgindex dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, pattern := range patterns {
			if !pattern.Match(content) {
				continue
			}
			if !allowedWriterFiles[name] {
				t.Fatalf("%s writes a binary storage v2 table outside the explicit owner allowlist; update ownership policy before adding this write", name)
			}
		}
	}
}

func TestLegacyBinaryAnchorIsNotUsedByProductionStoreCode(t *testing.T) {
	pattern := regexp.MustCompile(`(?is)\b(from|join|update|delete\s+from|insert\s+into)\s+(public\.)?binaries\b`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pgindex dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if pattern.Match(content) {
			t.Fatalf("%s accesses retired binaries compatibility rows; use binary_core and stage-owned v2 projections instead", name)
		}
	}
}

func TestLegacyBinaryWritesAreRemovedFromProductionStoreCode(t *testing.T) {
	pattern := regexp.MustCompile(`(?is)\b(insert\s+into|update|delete\s+from)\s+(public\.)?binaries\b`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pgindex dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if pattern.Match(content) {
			t.Fatalf("%s writes retired binaries compatibility rows; split the write into stage-owned v2 tables first", name)
		}
	}
}

func TestScrapeHotPathDoesNotWriteMaterializedDimensions(t *testing.T) {
	forbiddenByFile := map[string][]string{
		"repository.go": {
			"posters",
		},
		"crosspost_store.go": {
			"article_header_crosspost_group_summary",
			"article_header_crosspost_group_messages",
			"article_header_crosspost_group_sources",
		},
	}

	for fileName, tables := range forbiddenByFile {
		content, err := os.ReadFile(filepath.Clean(fileName))
		if err != nil {
			t.Fatalf("read %s: %v", fileName, err)
		}
		for _, table := range tables {
			pattern := regexp.MustCompile(`(?is)\b(insert\s+into|update|delete\s+from)\s+(public\.)?` + regexp.QuoteMeta(table) + `\b`)
			if pattern.Match(content) {
				t.Fatalf("%s writes %s from the scrape hot path; enqueue materialization work instead", fileName, table)
			}
		}
	}
}

func TestPosterDimensionWritesStayInMaterializer(t *testing.T) {
	allowedWriterFiles := map[string]bool{
		"scrape_materializer_store.go": true,
	}
	pattern := regexp.MustCompile(`(?is)\b(insert\s+into|update|delete\s+from)\s+(public\.)?posters\b`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pgindex dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if pattern.Match(content) && !allowedWriterFiles[name] {
			t.Fatalf("%s writes posters outside poster_materialize ownership", name)
		}
	}
}

func TestPosterMaterializerDoesNotWriteScrapePayloads(t *testing.T) {
	content, err := os.ReadFile(filepath.Clean("scrape_materializer_store.go"))
	if err != nil {
		t.Fatalf("read scrape_materializer_store.go: %v", err)
	}
	pattern := regexp.MustCompile(`(?is)\b(update|delete\s+from|insert\s+into)\s+(public\.)?article_header_ingest_payloads\b`)
	if pattern.Match(content) {
		t.Fatalf("poster materializer must not write article_header_ingest_payloads; write article_header_poster_refs instead")
	}
}
