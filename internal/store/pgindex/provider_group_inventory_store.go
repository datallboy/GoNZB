package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type IndexerProviderGroupInventoryItem struct {
	ProviderID   string
	ProviderName string
	GroupName    string
	High         int64
	Low          int64
	Status       string
	ScannedAt    string
}

type IndexerProviderGroupInventoryStats struct {
	Count      int
	LatestScan string
}

const providerGroupInventoryInsertBatchSize = 500

func (s *Store) ReplaceIndexerProviderGroupInventory(ctx context.Context, rows []IndexerProviderGroupInventoryItem) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin provider group inventory replace tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM indexer_provider_group_inventory`); err != nil {
		return fmt.Errorf("delete provider group inventory: %w", err)
	}

	for start := 0; start < len(rows); start += providerGroupInventoryInsertBatchSize {
		end := start + providerGroupInventoryInsertBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertProviderGroupInventoryBatch(ctx, tx, rows[start:end]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit provider group inventory replace tx: %w", err)
	}
	return nil
}

func insertProviderGroupInventoryBatch(ctx context.Context, tx *sql.Tx, rows []IndexerProviderGroupInventoryItem) error {
	if len(rows) == 0 {
		return nil
	}
	var query strings.Builder
	args := make([]any, 0, len(rows)*7)
	query.WriteString(`
		INSERT INTO indexer_provider_group_inventory (
			provider_id,
			provider_name,
			group_name,
			high,
			low,
			status,
			scanned_at
		) VALUES `)
	for i, row := range rows {
		if i > 0 {
			query.WriteByte(',')
		}
		base := len(args)
		query.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d::timestamptz)", base+1, base+2, base+3, base+4, base+5, base+6, base+7))
		scannedAt := strings.TrimSpace(row.ScannedAt)
		if scannedAt == "" {
			scannedAt = time.Now().UTC().Format(time.RFC3339)
		}
		args = append(args,
			strings.TrimSpace(row.ProviderID),
			strings.TrimSpace(row.ProviderName),
			strings.TrimSpace(row.GroupName),
			row.High,
			row.Low,
			strings.TrimSpace(row.Status),
			scannedAt,
		)
	}
	query.WriteString(`
		ON CONFLICT (provider_id, group_name) DO UPDATE
		SET provider_name = EXCLUDED.provider_name,
		    high = EXCLUDED.high,
		    low = EXCLUDED.low,
		    status = EXCLUDED.status,
		    scanned_at = EXCLUDED.scanned_at`)
	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("insert provider group inventory batch: %w", err)
	}
	return nil
}

func (s *Store) GetIndexerProviderGroupInventoryStats(ctx context.Context) (IndexerProviderGroupInventoryStats, error) {
	var out IndexerProviderGroupInventoryStats
	if s == nil || s.db == nil {
		return out, fmt.Errorf("pgindex store is not initialized")
	}
	var latest sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(scanned_at)
		FROM indexer_provider_group_inventory`).Scan(&out.Count, &latest); err != nil {
		return out, fmt.Errorf("get provider group inventory stats: %w", err)
	}
	if latest.Valid {
		out.LatestScan = latest.Time.UTC().Format(time.RFC3339)
	}
	return out, nil
}

func (s *Store) ListIndexerProviderGroupInventoryCandidates(ctx context.Context, query string, patternHints []string) ([]IndexerProviderGroupInventoryItem, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	where := make([]string, 0, len(patternHints)+1)
	args := make([]any, 0, len(patternHints)+1)
	if q := strings.ToLower(strings.TrimSpace(query)); q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(lower(group_name) LIKE $%d OR lower(provider_id) LIKE $%d OR lower(provider_name) LIKE $%d)", len(args), len(args), len(args)))
	}
	for _, hint := range patternHints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if hint == "" {
			continue
		}
		args = append(args, "%"+hint+"%")
		where = append(where, fmt.Sprintf("lower(group_name) LIKE $%d", len(args)))
	}
	sqlText := `
		SELECT provider_id, provider_name, group_name, high, low, status, scanned_at
		FROM indexer_provider_group_inventory`
	if len(where) > 0 {
		sqlText += "\nWHERE " + strings.Join(where, " OR ")
	}
	sqlText += "\nORDER BY lower(group_name), provider_id"

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("list provider group inventory candidates: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerProviderGroupInventoryItem, 0)
	for rows.Next() {
		var item IndexerProviderGroupInventoryItem
		var scannedAt time.Time
		if err := rows.Scan(&item.ProviderID, &item.ProviderName, &item.GroupName, &item.High, &item.Low, &item.Status, &scannedAt); err != nil {
			return nil, fmt.Errorf("scan provider group inventory candidate: %w", err)
		}
		item.ScannedAt = scannedAt.UTC().Format(time.RFC3339)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider group inventory candidates: %w", err)
	}
	return out, nil
}
