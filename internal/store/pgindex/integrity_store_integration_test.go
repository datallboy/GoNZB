package pgindex

import (
	"context"
	"strings"
	"testing"
)

func TestCriticalIndexerIntegrityChecksPhysicalIndexPartitions(t *testing.T) {
	store := openPostgresTestStore(t)
	ensureDefaultTestProvider(t, store)

	report, err := store.CheckCriticalIndexerIntegrity(context.Background(), true)
	if err != nil {
		t.Fatalf("check critical indexer integrity: %v", err)
	}
	if !report.AmcheckAvailable {
		t.Fatal("expected disposable PostgreSQL test role to support amcheck")
	}
	if report.HasFailures() {
		t.Fatalf("critical indexer integrity failed: %s", report.FailureSummary())
	}

	for _, check := range report.Checks {
		if check.Relation != "public.article_headers_pkey" {
			continue
		}
		if !check.AmcheckRan {
			t.Fatalf("expected physical article_headers index partitions to be checked: %+v", check)
		}
		if !strings.Contains(check.Detail, "physical index partitions") {
			t.Fatalf("expected partition verification detail, got %q", check.Detail)
		}
		return
	}
	t.Fatal("article_headers_pkey was not included in the critical integrity report")
}
