package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

func (s *PersistentStore) SaveQueueItem(ctx context.Context, item *domain.QueueItem) error {
	startedAtUnix := int64(0)
	if !item.StartedAt.IsZero() {
		startedAtUnix = item.StartedAt.Unix()
	}
	completedAtUnix := int64(0)
	if !item.CompletedAt.IsZero() {
		completedAtUnix = item.CompletedAt.Unix()
	}

	query := `
		INSERT INTO queue_items (
			id, release_id, status, out_dir, error,
			started_at_unix, completed_at_unix, download_seconds, postprocess_seconds, avg_bps, downloaded_bytes
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			error = excluded.error,
			out_dir = excluded.out_dir,
			started_at_unix = excluded.started_at_unix,
			completed_at_unix = excluded.completed_at_unix,
			download_seconds = excluded.download_seconds,
			postprocess_seconds = excluded.postprocess_seconds,
			avg_bps = excluded.avg_bps,
			downloaded_bytes = excluded.downloaded_bytes`

	_, err := s.db.ExecContext(ctx, query,
		item.ID, item.ReleaseID, item.Status, item.OutDir, item.Error,
		startedAtUnix, completedAtUnix, item.DownloadSeconds, item.PostProcessSeconds, item.AvgBps, item.DownloadedBytes,
	)
	return err
}

// GetQueueItems returns all items in the queue, ordered by creation date.
func (s *PersistentStore) GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	query := `
		SELECT 
			q.id, q.release_id, q.status, q.out_dir, q.error, q.created_at, q.updated_at,
			q.started_at_unix, q.completed_at_unix, q.download_seconds, q.postprocess_seconds, q.avg_bps, q.downloaded_bytes,
			r.id, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed
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
			&qi.ID, &qi.ReleaseID, &qi.Status, &qi.OutDir, &qi.Error, &qi.CreatedAt, &qi.UpdatedAt,
			&qi.StartedAtUnix, &qi.CompletedAtUnix, &qi.DownloadSeconds, &qi.PostProcessSeconds, &qi.AvgBps, &qi.DownloadedBytes,
			&rel.ID, &rel.Title, &rel.Size, &rel.Password, &rel.GUID,
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
			q.id, q.release_id, q.status, q.out_dir, q.error, q.created_at, q.updated_at,
			q.started_at_unix, q.completed_at_unix, q.download_seconds, q.postprocess_seconds, q.avg_bps, q.downloaded_bytes,
			r.id, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed
		FROM queue_items q
		JOIN releases r ON q.release_id = r.id
		WHERE q.id = ? LIMIT 1`

	var qi queueItemDBO
	var rel releaseDBO

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&qi.ID, &qi.ReleaseID, &qi.Status, &qi.OutDir, &qi.Error, &qi.CreatedAt, &qi.UpdatedAt,
		&qi.StartedAtUnix, &qi.CompletedAtUnix, &qi.DownloadSeconds, &qi.PostProcessSeconds, &qi.AvgBps, &qi.DownloadedBytes,
		&rel.ID, &rel.Title, &rel.Size, &rel.Password, &rel.GUID,
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
			q.id, q.release_id, q.status, q.out_dir, q.error, q.created_at, q.updated_at,
			q.started_at_unix, q.completed_at_unix, q.download_seconds, q.postprocess_seconds, q.avg_bps, q.downloaded_bytes,
			r.id, r.title, r.size, r.password, r.guid, r.source, r.download_url, r.publish_date, r.category, r.redirect_allowed
		FROM queue_items q
		JOIN releases r ON q.release_id = r.id
		WHERE q.status NOT IN ('completed', 'failed')
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
			&qi.ID, &qi.ReleaseID, &qi.Status, &qi.OutDir, &qi.Error, &qi.CreatedAt, &qi.UpdatedAt,
			&qi.StartedAtUnix, &qi.CompletedAtUnix, &qi.DownloadSeconds, &qi.PostProcessSeconds, &qi.AvgBps, &qi.DownloadedBytes,
			&rel.ID, &rel.Title, &rel.Size, &rel.Password, &rel.GUID,
			&rel.Source, &rel.DownloadURL, &rel.PublishDate, &rel.Category, &rel.RedirectAllowed,
		)
		if err != nil {
			return nil, err
		}

		items = append(items, qi.ToDomain(rel.ToDomain()))
	}
	return items, nil
}

func (s *PersistentStore) DeleteQueueItems(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM queue_items WHERE id IN (%s)", strings.Join(placeholders, ","))
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *PersistentStore) ClearQueueHistory(ctx context.Context, statuses []domain.JobStatus) (int64, error) {
	if len(statuses) == 0 {
		statuses = []domain.JobStatus{domain.StatusCompleted, domain.StatusFailed}
	}

	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, st := range statuses {
		placeholders[i] = "?"
		args[i] = string(st)
	}

	query := fmt.Sprintf(
		"DELETE FROM queue_items WHERE status IN (%s)",
		strings.Join(placeholders, ","),
	)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
