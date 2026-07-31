package pgindex

import (
	"context"
	"testing"
)

func TestFederationStorageReportIncludesProtocolAndEvidenceRelations(t *testing.T) {
	store := openPostgresTestStore(t)
	report, err := store.GetFederationStorageReport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Available || report.DatabaseBytes <= 0 || report.GoNZBNetBytes <= 0 {
		t.Fatalf("unexpected storage report: %+v", report)
	}
	found := map[string]bool{}
	for _, relation := range report.Relations {
		if relation.TotalBytes < 0 || relation.EstimatedRows < 0 {
			t.Fatalf("invalid relation statistics: %+v", relation)
		}
		found[relation.Name] = true
	}
	for _, name := range []string{"federation_events", "resolution_manifests", "yenc_header_evidence"} {
		if !found[name] {
			t.Fatalf("expected relation %s in storage report", name)
		}
	}
}
