package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/datallboy/gonzb/internal/domain"
)

func (s *PersistentStore) SaveQueueItem(item *domain.QueueItem) error {

	tasksJSON, err := json.Marshal(item.Tasks)
	if err != nil {
		return fmt.Errorf("failed to encode tasks: %w", err)
	}

	query := `INSERT OR REPLACE INTO queue_items (id, name, password, status, total_bytes, bytes_written, tasks, error) 
              VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.Exec(query,
		item.ID,
		item.Name,
		item.Password,
		item.Status,
		item.TotalBytes,
		item.BytesWritten.Load(),
		tasksJSON,
		item.Error,
	)
	return err
}

func (s *PersistentStore) GetQueueItems() ([]*domain.QueueItem, error) {
	rows, err := s.db.Query("SELECT id, name, password, status, total_bytes, bytes_written, tasks, error FROM queue_items")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*domain.QueueItem
	for rows.Next() {
		item := &domain.QueueItem{}
		var tasksJSON string
		var bytesWritten uint64

		err := rows.Scan(&item.ID, &item.Name, &item.Password, &item.Status, &item.TotalBytes, &bytesWritten, &tasksJSON, &item.Error)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(tasksJSON), &item.Tasks); err != nil {
			// TODO: Log this, but maybe don't kill the whole app
			continue
		}

		item.BytesWritten.Store(bytesWritten) // Atomic store
		items = append(items, item)
	}

	// Sort by KSUID (Chronological)
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})

	return items, nil
}

func (s *PersistentStore) GetQueueItem(id string) (*domain.QueueItem, error) {
	query := `
			SELECT id, name, password, status, total_bytes, bytes_written, tasks, error 
			FROM queue_items 
			WHERE id = ? LIMIT 1`

	row := s.db.QueryRow(query, id)

	item := &domain.QueueItem{}
	var tasksJSON string
	var bytesWritten uint64

	err := row.Scan(&item.ID, &item.Name, &item.Password, &item.Status, &item.TotalBytes, &bytesWritten, &tasksJSON, &item.Error)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Return nil, nil to indicate "Not found"
		}
		return nil, fmt.Errorf("failed to fetch queue item: %w", err)
	}

	if err := json.Unmarshal([]byte(tasksJSON), &item.Tasks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tasks for %s: %w", id, err)
	}

	item.BytesWritten.Store(bytesWritten) // Atomic store

	return item, nil
}

func (s *PersistentStore) GetActiveQueueItems() ([]*domain.QueueItem, error) {
	query := `
		SELECT id, name, password, status, total_bytes, bytes_written, tasks, error 
		FROM queue_items 
		WHERE status NOT IN ('completed', 'failed')
		ORDER BY id ASC`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active queue: %w", err)
	}
	defer rows.Close()

	var items []*domain.QueueItem
	for rows.Next() {
		item := &domain.QueueItem{}
		var tasksJSON string
		var bytesWritten uint64

		err := rows.Scan(
			&item.ID, &item.Name, &item.Password, &item.Status,
			&item.TotalBytes, &bytesWritten, &tasksJSON, &item.Error,
		)
		if err != nil {
			return nil, err
		}

		// Hydrate the tasks from JSON
		if err := json.Unmarshal([]byte(tasksJSON), &item.Tasks); err != nil {
			// Log error but continue to next item
			continue
		}

		item.BytesWritten.Store(bytesWritten)
		items = append(items, item)
	}

	return items, nil

}
