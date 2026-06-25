package pgindex

import (
	"os"
	"strings"
	"testing"
)

func TestAssembleStoreDoesNotUseArticleHeaderWriteBackState(t *testing.T) {
	src := readGuardrailSource(t, "assembly_store.go")
	forbidden := []string{
		"UPDATE article_headers",
		"article_headers SET",
		"assembled_at",
		"assembly_claimed_until",
	}
	for _, term := range forbidden {
		if strings.Contains(src, term) {
			t.Fatalf("assembly_store.go must not contain %q; assemble state belongs in article_header_assembly_queue", term)
		}
	}
}

func TestYEncRecoveryDoesNotWriteBackToScrapeOwnedSourceTables(t *testing.T) {
	for _, fileName := range []string{"assembly_store.go", "yenc_recovery_store.go", "yenc_work_item_store.go"} {
		src := readGuardrailSource(t, fileName)
		forbidden := []string{
			"UPDATE article_headers",
			"article_headers SET",
			"UPDATE article_header_ingest_payloads",
		}
		for _, term := range forbidden {
			if strings.Contains(src, term) {
				t.Fatalf("%s must not contain %q; yEnc retry/progress state belongs in recovery-owned tables", fileName, term)
			}
		}
	}
}

func TestPartitionedSourceJoinsUseSourcePostedAt(t *testing.T) {
	for _, fileName := range []string{"assembly_store.go", "yenc_work_item_store.go", "yenc_recovery_store.go"} {
		src := readGuardrailSource(t, fileName)
		forbidden := []string{
			"JOIN article_headers ah ON ah.id",
			"JOIN article_headers ah\n\t\t\t  ON ah.id",
			"article_header_ingest_payloads p ON p.article_header_id",
		}
		for _, term := range forbidden {
			if strings.Contains(src, term) {
				t.Fatalf("%s must not contain id-only partitioned source join %q", fileName, term)
			}
		}
	}
}

func readGuardrailSource(t *testing.T, fileName string) string {
	t.Helper()
	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("read %s: %v", fileName, err)
	}
	return string(data)
}
