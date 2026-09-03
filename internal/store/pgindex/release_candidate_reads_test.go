package pgindex

import (
	"context"
	"testing"
	"time"
)

func TestListIndexerReleaseCandidatesExposesPipelineState(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for _, candidate := range []struct {
		name       string
		familyKey  string
		releaseKey string
		updatedAt  time.Time
	}{
		{name: "Alpha.Pending.2026-GROUP", familyKey: "alpha family", releaseKey: "alpha-release", updatedAt: now},
		{name: "Beta.Evaluated.2026-GROUP", familyKey: "beta family", releaseKey: "beta-release", updatedAt: now.Add(-2 * time.Minute)},
		{name: "Gamma.Formed.2026-GROUP", familyKey: "gamma family", releaseKey: "gamma-release", updatedAt: now.Add(-time.Minute)},
	} {
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO release_ready_candidates (
				provider_id, newsgroup_id, key_kind, family_key,
				source_release_key, release_key, release_name,
				binary_count, complete_binary_count, complete_main_payload_binary_count,
				expected_file_count, has_expected_file_count, expected_file_coverage_pct,
				total_bytes, earliest_posted_at, ready_reason, updated_at, source_posted_at
			) VALUES ($1, $2, 'release_family', $3, $3, $4, $5, 4, 3, 1, 4, TRUE, 75, 1024, $6, 'actionable', $7, $6)`,
			testProviderID, testNewsgroupID, candidate.familyKey, candidate.releaseKey, candidate.name, now, candidate.updatedAt,
		); err != nil {
			t.Fatalf("insert candidate %s: %v", candidate.name, err)
		}
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO release_ready_candidate_acks (
			provider_id, newsgroup_id, key_kind, family_key, processed_at
		) VALUES ($1, $2, 'release_family', 'beta family', $3)`,
		testProviderID, testNewsgroupID, now,
	); err != nil {
		t.Fatalf("ack evaluated candidate: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO releases (
			release_id, guid, provider_id, release_key, title, group_name,
			source_kind, source_release_key, release_family_key
		) VALUES ('rel-gamma', 'guid-gamma', $1, 'gamma-release', 'Gamma.Formed.2026-GROUP',
			'alt.test.integration-default', 'usenet_index', 'gamma family', 'gamma family')`,
		testProviderID,
	); err != nil {
		t.Fatalf("insert formed release: %v", err)
	}

	items, total, err := store.ListIndexerReleaseCandidates(ctx, IndexerReleaseCandidateListParams{
		Sort:  "name_asc",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("list release candidates: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("candidate counts = total %d rows %d, want 3", total, len(items))
	}
	if items[0].EvaluationState != "pending" || items[0].EvaluationNote != "awaiting_release_stage" {
		t.Fatalf("unexpected pending candidate: %+v", items[0])
	}
	if items[1].EvaluationState != "evaluated" || items[1].EvaluatedAt == nil {
		t.Fatalf("unexpected evaluated candidate: %+v", items[1])
	}
	if items[2].EvaluationState != "formed" || items[2].FormedReleaseID != "rel-gamma" {
		t.Fatalf("unexpected formed candidate: %+v", items[2])
	}

	pending, pendingTotal, err := store.ListIndexerReleaseCandidates(ctx, IndexerReleaseCandidateListParams{
		EvaluationState: "pending",
		Query:           "Alpha",
		Newsgroup:       "integration-default",
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("filter pending release candidates: %v", err)
	}
	if pendingTotal != 1 || len(pending) != 1 || pending[0].ReleaseKey != "alpha-release" {
		t.Fatalf("unexpected pending filter result total=%d items=%+v", pendingTotal, pending)
	}
}
