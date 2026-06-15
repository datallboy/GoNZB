package pgindex

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBinaryStorageV2WritesStayBehindBridge(t *testing.T) {
	allowedWriterFiles := map[string]bool{
		"binary_storage_v2.go": true,
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
				t.Fatalf("%s writes a binary storage v2 table outside the bridge; update ownership policy before adding this write", name)
			}
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
