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
	for _, fileName := range []string{
		"assembly_store.go",
		"yenc_work_item_store.go",
		"yenc_recovery_store.go",
		"catalog_reads.go",
		"inspect_reads.go",
		"inspection_store.go",
		"release_catalog_files.go",
	} {
		src := readGuardrailSource(t, fileName)
		forbidden := []string{
			"JOIN article_headers ah ON ah.id",
			"JOIN article_headers ah\n\t\t\t  ON ah.id",
			"article_header_ingest_payloads p ON p.article_header_id",
			"article_header_ingest_payloads aip ON aip.article_header_id",
			"article_header_poster_refs apr ON apr.article_header_id",
			"JOIN binary_parts bp ON bp.binary_id",
			"JOIN binary_identity_current bic ON bic.binary_id",
			"JOIN binary_observation_stats bos ON bos.binary_id",
			"LEFT JOIN binary_recovery_current brc ON brc.binary_id",
			"LEFT JOIN binary_grouping_evidence bge ON bge.binary_id",
		}
		for _, term := range forbidden {
			if strings.Contains(src, term) {
				t.Fatalf("%s must not contain id-only partitioned source join %q", fileName, term)
			}
		}
	}
}

func TestNativeSourceWorkPartitionTargetsMatchSprintScope(t *testing.T) {
	want := []string{
		"article_headers",
		"article_header_ingest_payloads",
		"article_header_crosspost_groups",
		"article_header_poster_refs",
		"article_header_assembly_queue",
		"poster_materialization_queue",
		"binary_parts",
		"binary_observation_stats",
		"binary_identity_current",
		"binary_recovery_current",
		"binary_lifecycle",
		"binary_completion_keys",
		"binary_grouping_evidence",
		"binary_projection_events",
		"binary_superseded_sources",
		"yenc_recovery_work_items",
		"binary_inspection_ready_queue",
		"binary_inspections",
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_sets",
		"binary_par2_targets",
		"release_family_readiness_summaries",
		"release_ready_candidates",
		"release_recovered_file_set_candidates",
		"release_stage_dirty_families",
	}
	got := nativeSourceWorkPartitionTables()
	if len(got) != len(want) {
		t.Fatalf("partition target count mismatch: got %d want %d: %v", len(got), len(want), got)
	}
	seen := make(map[string]struct{}, len(got))
	for _, table := range got {
		seen[table] = struct{}{}
	}
	for _, table := range want {
		if _, ok := seen[table]; !ok {
			t.Fatalf("partition target list missing %s: %v", table, got)
		}
	}
}

func TestPartitionedWritersUseSourcePostedConflictTargets(t *testing.T) {
	files := []string{
		"assembly_store.go",
		"yenc_recovery_store.go",
		"inspect_ready_queue_store.go",
		"inspection_store.go",
		"release_family_summary_store.go",
	}
	for _, fileName := range files {
		src := readGuardrailSource(t, fileName)
		forbidden := []string{
			"ON CONFLICT (binary_id)",
			"ON CONFLICT (source_binary_id)",
			"ON CONFLICT (stage_name, binary_id)",
			"ON CONFLICT (provider_id, file_set_key)",
			"ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key)",
		}
		for _, term := range forbidden {
			if strings.Contains(src, term) {
				t.Fatalf("%s must not contain partition-incompatible conflict target %q", fileName, term)
			}
		}
	}
}

func TestPartitionedInspectionEvidenceInsertsCarrySourcePostedAt(t *testing.T) {
	src := readGuardrailSource(t, "inspection_store.go")
	tables := []string{
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_sets",
		"binary_par2_targets",
	}
	for _, table := range tables {
		insertAt := strings.Index(src, "INSERT INTO "+table)
		if insertAt < 0 {
			t.Fatalf("inspection_store.go missing insert into %s", table)
		}
		valuesAt := strings.Index(src[insertAt:], "VALUES")
		if valuesAt < 0 {
			t.Fatalf("inspection_store.go insert into %s missing VALUES", table)
		}
		columnList := src[insertAt : insertAt+valuesAt]
		if !strings.Contains(columnList, "source_posted_at") {
			t.Fatalf("inspection_store.go insert into %s must carry source_posted_at", table)
		}
	}
}

func TestPartitionedReleaseWorkInsertsCarrySourcePostedAt(t *testing.T) {
	src := readGuardrailSource(t, "release_family_summary_store.go")
	tables := []string{
		"release_family_readiness_summaries",
		"release_ready_candidates",
		"release_recovered_file_set_candidates",
	}
	for _, table := range tables {
		needle := "INSERT INTO " + table
		searchFrom := 0
		found := 0
		for {
			insertAt := strings.Index(src[searchFrom:], needle)
			if insertAt < 0 {
				break
			}
			found++
			insertAt += searchFrom
			valuesAt := strings.Index(src[insertAt:], "VALUES")
			selectAt := strings.Index(src[insertAt:], "SELECT")
			endAt := valuesAt
			if selectAt >= 0 && (endAt < 0 || selectAt < endAt) {
				endAt = selectAt
			}
			if endAt < 0 {
				t.Fatalf("release_family_summary_store.go insert into %s missing VALUES/SELECT", table)
			}
			columnList := src[insertAt : insertAt+endAt]
			if !strings.Contains(columnList, "source_posted_at") {
				t.Fatalf("release_family_summary_store.go insert into %s must carry source_posted_at", table)
			}
			searchFrom = insertAt + len(needle)
		}
		if found == 0 {
			t.Fatalf("release_family_summary_store.go missing insert into %s", table)
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
