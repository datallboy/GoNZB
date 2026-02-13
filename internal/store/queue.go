package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

func (s *PersistentStore) SaveQueueItem(ctx context.Context, item *domain.QueueItem) error {
	query := `
		INSERT INTO queue_items (id, release_id, status, out_dir, error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			error = excluded.error,
			out_dir = excluded.out_dir`

	_, err := s.db.ExecContext(ctx, query,
		item.ID, item.ReleaseID, item.Status, item.OutDir, item.Error,
	)
	return err
}

// GetQueueItems returns all items in the queue, ordered by creation date.
func (s *PersistentStore) GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	query := `
		SELECT 
			q.id, q.release_id, q.status, q.out_dir, q.error, q.created_at,
			r.id, r.file_hash, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed
		FROM queue_items q
		JOIN releases r ON q.release_id = r.id
		ORDER BY q.created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query queue items: %w", err)
	}
	defer rows.Close()

	var items []*domain.QueueItem
	for rows.Next() {
		var qi queueItemDBO
		var rel releaseDBO

		err := rows.Scan(
			&qi.ID, &qi.ReleaseID, &qi.Status, &qi.OutDir, &qi.Error, &qi.CreatedAt,
			&rel.ID, &rel.FileHash, &rel.Title, &rel.Size, &rel.Password, &rel.GUID,
			&rel.Source, &rel.DownloadURL, &rel.PublishDate, &rel.Category, &rel.RedirectAllowed,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue row: %w", err)
		}

		// Mapping DBOs to Domain via our helper methods
		domainRelease := rel.ToDomain()
		items = append(items, qi.ToDomain(domainRelease))
	}

	return items, nil
}

// GetQueueItem fetches a single job by its ID, fully hydrated with its Release.
func (s *PersistentStore) GetQueueItem(ctx context.Context, id string) (*domain.QueueItem, error) {
	query := `
		SELECT 
			q.id, q.release_id, q.status, q.out_dir, q.error, q.created_at,
			r.id, r.file_hash, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed
		FROM queue_items q
		JOIN releases r ON q.release_id = r.id
		WHERE q.id = ? LIMIT 1`

	var qi queueItemDBO
	var rel releaseDBO

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&qi.ID, &qi.ReleaseID, &qi.Status, &qi.OutDir, &qi.Error, &qi.CreatedAt,
		&rel.ID, &rel.FileHash, &rel.Title, &rel.Size, &rel.Password, &rel.GUID,
		&rel.Source, &rel.DownloadURL, &rel.PublishDate, &rel.Category, &rel.RedirectAllowed,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get queue item %s: %w", id, err)
	}

	return qi.ToDomain(rel.ToDomain()), nil
}

// GetActiveQueueItems returns all jobs that are not in a terminal state (Completed/Failed).
func (s *PersistentStore) GetActiveQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	query := `
		SELECT 
			q.id, q.release_id, q.status, q.out_dir, q.error, q.created_at,
			r.id, r.file_hash, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed
		FROM queue_items q
		JOIN releases r ON q.release_id = r.id
		WHERE q.status NOT IN ('Completed', 'Failed')
		ORDER BY q.created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active queue: %w", err)
	}
	defer rows.Close()

	var items []*domain.QueueItem
	for rows.Next() {
		var qi queueItemDBO
		var rel releaseDBO

		err := rows.Scan(
			&qi.ID, &qi.ReleaseID, &qi.Status, &qi.OutDir, &qi.Error, &qi.CreatedAt,
			&rel.ID, &rel.FileHash, &rel.Title, &rel.Size, &rel.Password, &rel.GUID,
			&rel.Source, &rel.DownloadURL, &rel.PublishDate, &rel.Category, &rel.RedirectAllowed,
		)
		if err != nil {
			return nil, err
		}

		items = append(items, qi.ToDomain(rel.ToDomain()))
	}
	return items, nil
}

func (s *PersistentStore) ResetStuckQueueItems(ctx context.Context, newStatus domain.JobStatus, oldStatuses ...domain.JobStatus) error {
	if len(oldStatuses) == 0 {
		return nil
	}

	// Build the "IN (?, ?, ?)" part of the query
	placeholders := make([]string, len(oldStatuses))
	args := make([]interface{}, len(oldStatuses)+1)
	args[0] = string(newStatus)

	for i, status := range oldStatuses {
		placeholders[i] = "?"
		args[i+1] = string(status)
	}

	query := fmt.Sprintf(
		"UPDATE queue_items SET status = ?, error = 'Unexpected shutdown' WHERE status IN (%s)",
		strings.Join(placeholders, ","),
	)

	_, err := s.db.Exec(query, args...)
	return err
}
