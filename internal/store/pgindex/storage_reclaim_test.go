package pgindex

import (
	"reflect"
	"testing"
)

func TestNormalizeIndexerStorageReclaimTablesDefaults(t *testing.T) {
	tables, err := normalizeIndexerStorageReclaimTables(nil)
	if err != nil {
		t.Fatalf("normalize tables: %v", err)
	}
	if !reflect.DeepEqual(tables, indexerStorageReclaimTableOrder) {
		t.Fatalf("expected default order %v, got %v", indexerStorageReclaimTableOrder, tables)
	}
}

func TestNormalizeIndexerStorageReclaimTablesAliasesAndOrdering(t *testing.T) {
	tables, err := normalizeIndexerStorageReclaimTables([]string{
		"payloads",
		"readiness",
		"binary_grouping_evidence",
		"payloads",
	})
	if err != nil {
		t.Fatalf("normalize tables: %v", err)
	}

	expected := []string{
		"release_family_readiness_summaries",
		"binary_grouping_evidence",
		"article_header_ingest_payloads",
	}
	if !reflect.DeepEqual(tables, expected) {
		t.Fatalf("expected ordered tables %v, got %v", expected, tables)
	}
}

func TestNormalizeIndexerStorageReclaimTablesRejectsUnknownTable(t *testing.T) {
	if _, err := normalizeIndexerStorageReclaimTables([]string{"binaries"}); err == nil {
		t.Fatal("expected unknown reclaim table error")
	}
}

func TestIndexerStorageReclaimStatement(t *testing.T) {
	statement, err := indexerStorageReclaimStatement("binary_grouping_evidence", true)
	if err != nil {
		t.Fatalf("build full statement: %v", err)
	}
	if statement != "VACUUM (FULL, ANALYZE) binary_grouping_evidence" {
		t.Fatalf("unexpected full statement %q", statement)
	}

	statement, err = indexerStorageReclaimStatement("binary_grouping_evidence", false)
	if err != nil {
		t.Fatalf("build analyze statement: %v", err)
	}
	if statement != "VACUUM (ANALYZE) binary_grouping_evidence" {
		t.Fatalf("unexpected analyze statement %q", statement)
	}
}
