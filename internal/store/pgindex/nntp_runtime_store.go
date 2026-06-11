package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type NNTPRuntimeSnapshot struct {
	PublisherID string
	ModuleName  string
	Scope       string
	Payload     json.RawMessage
	UpdatedAt   time.Time
}

func (s *Store) UpsertNNTPSnapshot(ctx context.Context, publisherID, moduleName, scope string, payload []byte) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if publisherID == "" {
		return fmt.Errorf("publisher id is required")
	}
	if moduleName == "" {
		return fmt.Errorf("module name is required")
	}
	if scope == "" {
		scope = "indexer"
	}
	if !json.Valid(payload) {
		return fmt.Errorf("nntp runtime snapshot payload must be valid json")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer_nntp_runtime_snapshots (
			publisher_id,
			module_name,
			scope,
			payload,
			updated_at
		) VALUES ($1, $2, $3, $4::jsonb, now())
		ON CONFLICT (publisher_id) DO UPDATE
		SET module_name = EXCLUDED.module_name,
			scope = EXCLUDED.scope,
			payload = EXCLUDED.payload,
			updated_at = now()`,
		publisherID,
		moduleName,
		scope,
		string(payload),
	)
	if err != nil {
		return fmt.Errorf("upsert nntp runtime snapshot: %w", err)
	}
	return nil
}

func (s *Store) GetLatestNNTPSnapshot(ctx context.Context, moduleName string) (*NNTPRuntimeSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if moduleName == "" {
		moduleName = "indexer"
	}

	var snapshot NNTPRuntimeSnapshot
	err := s.db.QueryRowContext(ctx, `
		SELECT publisher_id, module_name, scope, payload, updated_at
		FROM indexer_nntp_runtime_snapshots
		WHERE module_name = $1
		ORDER BY updated_at DESC, publisher_id
		LIMIT 1`,
		moduleName,
	).Scan(
		&snapshot.PublisherID,
		&snapshot.ModuleName,
		&snapshot.Scope,
		&snapshot.Payload,
		&snapshot.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest nntp runtime snapshot: %w", err)
	}
	return &snapshot, nil
}

func (s *Store) ListRecentNNTPSnapshots(ctx context.Context, moduleName string, since time.Time) ([]NNTPRuntimeSnapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if moduleName == "" {
		moduleName = "indexer"
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT publisher_id, module_name, scope, payload, updated_at
		FROM indexer_nntp_runtime_snapshots
		WHERE module_name = $1
		  AND updated_at >= $2
		ORDER BY updated_at DESC, publisher_id`,
		moduleName,
		since.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("list recent nntp runtime snapshots: %w", err)
	}
	defer rows.Close()

	items := make([]NNTPRuntimeSnapshot, 0, 8)
	for rows.Next() {
		var item NNTPRuntimeSnapshot
		if err := rows.Scan(&item.PublisherID, &item.ModuleName, &item.Scope, &item.Payload, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan nntp runtime snapshot: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nntp runtime snapshots: %w", err)
	}
	return items, nil
}
