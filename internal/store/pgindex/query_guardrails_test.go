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

func TestBinaryStatsRefreshUsesBoundedPartAndHeaderLookups(t *testing.T) {
	src := readGuardrailSource(t, "assembly_store.go")
	start := strings.Index(src, "func refreshBinaryStatsIDsInTxForWindow")
	end := strings.Index(src, "func (s *Store) RepairStaleBinaryObservationStats")
	if start < 0 || end <= start {
		t.Fatal("could not locate binary stats refresh query")
	}
	refreshSrc := src[start:end]

	if strings.Contains(refreshSrc, "JOIN binary_effective_parts") {
		t.Fatal("binary stats refresh must not expand binary_effective_parts before filtering requested binary IDs")
	}
	for _, required := range []string{
		"JOIN binary_parts bp",
		"LEFT JOIN LATERAL",
		"ah.source_posted_at = bp.source_posted_at",
		"ah.id = bp.article_header_id",
		"JOIN binary_peer_segments ps",
	} {
		if !strings.Contains(refreshSrc, required) {
			t.Fatalf("binary stats refresh must retain bounded local and peer part hydration; missing %q", required)
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

func TestPosterMaterializationCompletionUsesSourcePostedAt(t *testing.T) {
	src := readGuardrailSource(t, "scrape_materializer_store.go")
	required := []string{
		"WITH completed(source_posted_at, article_header_id)",
		"q.source_posted_at >= $1",
		"q.source_posted_at < $2",
		"q.source_posted_at = completed.source_posted_at",
		"q.article_header_id = completed.article_header_id",
	}
	for _, term := range required {
		if !strings.Contains(src, term) {
			t.Fatalf("poster completion must use the partition key; missing %q", term)
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
		"binary_superseded_sources",
		"yenc_recovery_work_items",
		"article_cohort_candidates",
		"article_cohort_assembly_queue",
		"article_cohort_yenc_queue",
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

func TestActiveStagePartitionProvisioningUsesExactShortTransactions(t *testing.T) {
	src := readGuardrailSource(t, "partition_provision.go")
	if strings.Contains(src, "pgindex_ensure_source_work_partitions") {
		t.Fatalf("partition provisioning must not call pgindex_ensure_source_work_partitions; multi-parent runtime DDL caused relation-lock deadlocks")
	}
	for _, required := range []string{"partitionBundleScrape", "partitionBundleScheduler", "partitionBundleAssemble", "partitionBundleYEnc", "partitionBundleInspect", "partitionBundleRelease"} {
		if !strings.Contains(src, required) {
			t.Fatalf("partition provisioning must define the %s stage bundle", required)
		}
	}
	if !strings.Contains(src, "BeginTx") || !strings.Contains(src, "set_config('lock_timeout'") || !strings.Contains(src, "pgindex_ensure_daily_partition") {
		t.Fatalf("partition provisioning should create one parent/day child in a bounded transaction")
	}
	if !strings.Contains(src, "offline default-rehome workflow") || !strings.Contains(src, "refusing to route rows into default partitions") {
		t.Fatalf("partition provisioning must fail closed when a default contains the source day")
	}
}

func TestDownstreamPartitionedWritersProvisionTheirStageBundles(t *testing.T) {
	cases := map[string][]string{
		"assembly_store.go":                 {"provisionAssemblyPartitionsForBinaryRecords", "provisionAssemblyPartitionsForBinaryPartRecords", "partitionBundleAssemble", "partitionBundleYEnc"},
		"article_cohort_scheduler_store.go": {"provisionSchedulerPartitionsForReadyWork"},
		"inspect_ready_queue_store.go":      {"ensurePartitionBundleForBinaryIDs", "partitionBundleInspect", "binary_inspection_ready_queue_"},
		"inspection_store.go":               {"ensurePartitionBundleForBinaryIDs", "partitionBundleInspect", "binary_inspections_"},
		"release_family_summary_store.go":   {"provisionReleasePartitionsForQueuedWork"},
	}
	for fileName, required := range cases {
		src := readGuardrailSource(t, fileName)
		for _, term := range required {
			if !strings.Contains(src, term) {
				t.Fatalf("%s must provision and fail closed on its stage-owned partitions; missing %q", fileName, term)
			}
		}
	}
}

func TestArticleCohortSchedulerDoesNotProbePartitionCatalogPerCandidate(t *testing.T) {
	src := readGuardrailSource(t, "article_cohort_scheduler_store.go")
	if strings.Contains(src, "to_regclass(") {
		t.Fatalf("article cohort scheduling must rely on its pre-provisioned exact source-day partitions, not call to_regclass per candidate row")
	}
}

func TestInspectDiscoveryDefersToYEncAndBacksOffFailures(t *testing.T) {
	src := readGuardrailSource(t, "inspect_ready_queue_store.go")
	for _, required := range []string{
		"FROM yenc_recovery_work_items wi",
		"wi.status IN ('ready', 'running')",
		"bl.lifecycle_status = 'superseded'",
		"bp.part_number = 1",
		"(5 * time.Minute).Seconds()",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("inspect discovery queue must defer to active yEnc work and back off failures; missing %q", required)
		}
	}
}

func TestReleasePartitionPreflightCanUsePartialFamilyIndexes(t *testing.T) {
	for _, fileName := range []string{"partition_provision.go", "release_family_summary_store.go"} {
		src := readGuardrailSource(t, fileName)
		for _, required := range []string{
			"BTRIM(bic.release_family_key) <> ''",
			"BTRIM(bic.base_stem) <> ''",
		} {
			if !strings.Contains(src, required) {
				t.Fatalf("%s release partition lookup must imply its partial family-index predicate; missing %q", fileName, required)
			}
		}
		if strings.Contains(src, "(r.key_kind = 'release_family' AND bic.release_family_key = r.family_key)") {
			t.Fatalf("%s release partition lookup must not combine family-key indexes behind an OR join", fileName)
		}
	}
}

func TestPartitionDefaultRehomeUsesUTCDayBoundaries(t *testing.T) {
	src := readGuardrailSource(t, "maintenance_tasks_store.go")
	for _, required := range []string{"source_posted_at AT TIME ZONE 'UTC'", "time.ParseInLocation(\"2006-01-02\", dayKey, time.UTC)", "dayStart.Format(time.RFC3339)", "dayEnd.Format(time.RFC3339)"} {
		if !strings.Contains(src, required) {
			t.Fatalf("partition default rehome must use UTC day boundaries; missing %q", required)
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
