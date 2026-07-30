package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type BodyRequestBudgetSnapshot struct {
	BudgetKey     string    `json:"budget_key"`
	Limit         int64     `json:"limit"`
	Used          int64     `json:"used"`
	Remaining     int64     `json:"remaining"`
	WindowStarted time.Time `json:"window_started_at"`
}

func lockBodyRequestBudgetInTx(ctx context.Context, tx *sql.Tx, budgetKey string, limit int64) (BodyRequestBudgetSnapshot, error) {
	budgetKey = strings.TrimSpace(budgetKey)
	if tx == nil || budgetKey == "" || limit <= 0 {
		return BodyRequestBudgetSnapshot{BudgetKey: budgetKey, Limit: limit}, nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO indexer_body_request_budget_state (budget_key)
		VALUES ($1)
		ON CONFLICT (budget_key) DO NOTHING`, budgetKey); err != nil {
		return BodyRequestBudgetSnapshot{}, fmt.Errorf("ensure body request budget %s: %w", budgetKey, err)
	}

	var snapshot BodyRequestBudgetSnapshot
	snapshot.BudgetKey = budgetKey
	snapshot.Limit = limit
	if err := tx.QueryRowContext(ctx, `
		UPDATE indexer_body_request_budget_state
		SET window_started_at = CASE
				WHEN window_started_at < date_trunc('hour', NOW()) THEN date_trunc('hour', NOW())
				ELSE window_started_at
			END,
		    requests_used = CASE
				WHEN window_started_at < date_trunc('hour', NOW()) THEN 0
				ELSE requests_used
			END,
		    updated_at = NOW()
		WHERE budget_key = $1
		RETURNING window_started_at, requests_used`, budgetKey).Scan(&snapshot.WindowStarted, &snapshot.Used); err != nil {
		return BodyRequestBudgetSnapshot{}, fmt.Errorf("lock body request budget %s: %w", budgetKey, err)
	}
	snapshot.WindowStarted = snapshot.WindowStarted.UTC()
	snapshot.Remaining = limit - snapshot.Used
	if snapshot.Remaining < 0 {
		snapshot.Remaining = 0
	}
	return snapshot, nil
}

func consumeBodyRequestBudgetInTx(ctx context.Context, tx *sql.Tx, budgetKey string, requests int) error {
	if tx == nil || strings.TrimSpace(budgetKey) == "" || requests <= 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE indexer_body_request_budget_state
		SET requests_used = requests_used + $2,
		    updated_at = NOW()
		WHERE budget_key = $1`, strings.TrimSpace(budgetKey), requests); err != nil {
		return fmt.Errorf("consume body request budget %s: %w", budgetKey, err)
	}
	return nil
}

func (s *Store) ReserveBodyRequestBudget(ctx context.Context, budgetKey string, limit int64, requested int) (BodyRequestBudgetSnapshot, int, error) {
	if s == nil || s.db == nil {
		return BodyRequestBudgetSnapshot{}, 0, fmt.Errorf("store is required")
	}
	if requested <= 0 || limit <= 0 {
		return BodyRequestBudgetSnapshot{BudgetKey: strings.TrimSpace(budgetKey), Limit: limit}, 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BodyRequestBudgetSnapshot{}, 0, fmt.Errorf("begin body request budget reservation: %w", err)
	}
	defer rollbackTx(tx)
	snapshot, err := lockBodyRequestBudgetInTx(ctx, tx, budgetKey, limit)
	if err != nil {
		return BodyRequestBudgetSnapshot{}, 0, err
	}
	granted := requested
	if int64(granted) > snapshot.Remaining {
		granted = int(snapshot.Remaining)
	}
	if err := consumeBodyRequestBudgetInTx(ctx, tx, budgetKey, granted); err != nil {
		return BodyRequestBudgetSnapshot{}, 0, err
	}
	snapshot.Used += int64(granted)
	snapshot.Remaining -= int64(granted)
	if err := tx.Commit(); err != nil {
		return BodyRequestBudgetSnapshot{}, 0, fmt.Errorf("commit body request budget reservation: %w", err)
	}
	return snapshot, granted, nil
}
