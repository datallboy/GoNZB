package pgindex

import (
	"context"
	"testing"
)

func TestReserveBodyRequestBudgetEnforcesAndResetsHourlyLimit(t *testing.T) {
	store := openPostgresTestStore(t)
	ctx := context.Background()

	first, granted, err := store.ReserveBodyRequestBudget(ctx, "recover_yenc_balanced", 5, 3)
	if err != nil {
		t.Fatalf("reserve first body budget: %v", err)
	}
	if granted != 3 || first.Used != 3 || first.Remaining != 2 {
		t.Fatalf("first reservation = granted %d snapshot %+v, want 3 used and 2 remaining", granted, first)
	}

	second, granted, err := store.ReserveBodyRequestBudget(ctx, "recover_yenc_balanced", 5, 3)
	if err != nil {
		t.Fatalf("reserve second body budget: %v", err)
	}
	if granted != 2 || second.Used != 5 || second.Remaining != 0 {
		t.Fatalf("second reservation = granted %d snapshot %+v, want capped at 2", granted, second)
	}

	_, granted, err = store.ReserveBodyRequestBudget(ctx, "recover_yenc_balanced", 5, 1)
	if err != nil {
		t.Fatalf("reserve exhausted body budget: %v", err)
	}
	if granted != 0 {
		t.Fatalf("exhausted reservation granted %d, want 0", granted)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE indexer_body_request_budget_state
		SET window_started_at = NOW() - INTERVAL '2 hours'
		WHERE budget_key = 'recover_yenc_balanced'`); err != nil {
		t.Fatalf("age body budget window: %v", err)
	}
	reset, granted, err := store.ReserveBodyRequestBudget(ctx, "recover_yenc_balanced", 5, 1)
	if err != nil {
		t.Fatalf("reserve reset body budget: %v", err)
	}
	if granted != 1 || reset.Used != 1 || reset.Remaining != 4 {
		t.Fatalf("reset reservation = granted %d snapshot %+v, want fresh hourly window", granted, reset)
	}
}
