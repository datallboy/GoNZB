package store

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/domain"
)

func (s *PersistentStore) SaveQueueEvent(ctx context.Context, ev *domain.QueueItemEvent) error {
	if ev == nil || ev.QueueID == "" {
		return nil
	}

	query := `
		INSERT INTO queue_item_events (queue_item_id, stage, status, message, meta_json)
		VALUES (?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query, ev.QueueID, ev.Stage, ev.Status, ev.Message, ev.MetaJSON)
	return err
}

func (s *PersistentStore) GetQueueEvents(ctx context.Context, queueID string) ([]*domain.QueueItemEvent, error) {
	query := `
		SELECT id, queue_item_id, stage, status, message, meta_json, created_at
		FROM queue_item_events
		WHERE queue_item_id = ?
		ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, queueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query queue events: %w", err)
	}
	defer rows.Close()

	events := make([]*domain.QueueItemEvent, 0)
	for rows.Next() {
		ev := &domain.QueueItemEvent{}
		if err := rows.Scan(&ev.ID, &ev.QueueID, &ev.Stage, &ev.Status, &ev.Message, &ev.MetaJSON, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan queue event: %w", err)
		}
		events = append(events, ev)
	}

	return events, nil
}
