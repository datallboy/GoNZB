package pgindex

import (
	"context"
	"testing"
)

func TestListReleaseEnrichmentCandidatesQueriesAreUnambiguous(t *testing.T) {
	store := openPostgresTestStore(t)

	for _, stage := range []string{
		"enrich_predb",
		"enrich_predb_scene_name_recovery",
		"enrich_predb_metadata_only_fallback",
		"enrich_tmdb",
	} {
		t.Run(stage, func(t *testing.T) {
			if _, err := store.ListReleaseEnrichmentCandidates(context.Background(), stage, 1); err != nil {
				t.Fatalf("list candidates: %v", err)
			}
		})
	}
}
