package sqlitejob

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

const queueSelectColumns = `
q.id,
q.status,
q.out_dir,
q.error,
q.source_kind,
q.source_release_id,
q.release_title,
q.release_size,
q.release_snapshot_json,
q.payload_mode,     
q.resumable,       
q.created_at,
q.updated_at,
q.started_at_unix,
q.completed_at_unix,
q.download_seconds,
q.postprocess_seconds,
q.avg_bps,
q.downloaded_bytes
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReleaseWithQueue(s rowScanner) (*domain.QueueItem, error) {
	var q queueItemDBO
	if err := s.Scan(
		&q.ID,
		&q.Status,
		&q.OutDir,
		&q.Error,
		&q.SourceKind,
		&q.SourceReleaseID,
		&q.ReleaseTitle,
		&q.ReleaseSize,
		&q.ReleaseSnapshotJSON,
		&q.PayloadMode,
		&q.Resumable,
		&q.CreatedAt,
		&q.UpdatedAt,
		&q.StartedAtUnix,
		&q.CompletedAtUnix,
		&q.DownloadSeconds,
		&q.PostProcessSeconds,
		&q.AvgBps,
		&q.DownloadedBytes,
	); err != nil {
		return nil, err
	}
	return q.ToDomain(), nil
}

func (s *Store) SaveQueueItem(ctx context.Context, item *domain.QueueItem) error {
	startedAtUnix := int64(0)
	if !item.StartedAt.IsZero() {
		startedAtUnix = item.StartedAt.Unix()
	}
	completedAtUnix := int64(0)
	if !item.CompletedAt.IsZero() {
		completedAtUnix = item.CompletedAt.Unix()
	}

	sourceKind := item.SourceKind
	if sourceKind == "" {
		sourceKind = "aggregator"
	}

	sourceReleaseID := item.SourceReleaseID
	if sourceReleaseID == "" {
		sourceReleaseID = item.ReleaseID
	}

	releaseTitle := item.ReleaseTitle
	if releaseTitle == "" && item.Release != nil {
		releaseTitle = item.Release.Title
	}

	releaseSize := item.ReleaseSize
	if releaseSize == 0 && item.Release != nil {
		releaseSize = item.Release.Size
	}

	releaseSnapshotJSON := item.ReleaseSnapshotJSON
	if releaseSnapshotJSON == "" {
		releaseSnapshotJSON = "{}"
	}

	payloadMode := item.PayloadMode
	if payloadMode == "" {
		payloadMode = domain.PayloadModeCached
	}

	resumable := item.Resumable
	if payloadMode == domain.PayloadModeCached && !item.Resumable {
		// keep explicit false if caller set it
		resumable = false
	}
	if payloadMode == domain.PayloadModeCached && item.Resumable {
		resumable = true
	}
	if payloadMode == domain.PayloadModeEphemeral {
		resumable = false
	}

	query := `
	INSERT INTO queue_items (
		id, status, out_dir, error,
		source_kind, source_release_id, release_title, release_size, release_snapshot_json,
		payload_mode, resumable,
		started_at_unix, completed_at_unix, download_seconds, postprocess_seconds, avg_bps, downloaded_bytes
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		status = excluded.status,
		error = excluded.error,
		out_dir = excluded.out_dir,
		source_kind = excluded.source_kind,
		source_release_id = excluded.source_release_id,
		release_title = excluded.release_title,
		release_size = excluded.release_size,
		release_snapshot_json = excluded.release_snapshot_json,
		payload_mode = excluded.payload_mode,
		resumable = excluded.resumable,
		started_at_unix = excluded.started_at_unix,
		completed_at_unix = excluded.completed_at_unix,
		download_seconds = excluded.download_seconds,
		postprocess_seconds = excluded.postprocess_seconds,
		avg_bps = excluded.avg_bps,
		downloaded_bytes = excluded.downloaded_bytes`

	_, err := s.db.ExecContext(ctx, query,
		item.ID, item.Status, item.OutDir, item.Error,
		sourceKind, sourceReleaseID, releaseTitle, releaseSize, releaseSnapshotJSON,
		payloadMode, resumable,
		startedAtUnix, completedAtUnix, item.DownloadSeconds, item.PostProcessSeconds, item.AvgBps, item.DownloadedBytes,
	)
	return err
}

// GetQueueItems returns all items in the queue, ordered by creation date.
func (s *Store) GetQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	query := `
		SELECT ` + queueSelectColumns + `
		FROM queue_items q
		ORDER BY q.created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query queue items: %w", err)
	}
	defer rows.Close()

	var items []*domain.QueueItem
	for rows.Next() {
		item, scanErr := scanReleaseWithQueue(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan queue row: %w", scanErr)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queue rows iteration error: %w", err)
	}

	return items, nil
}

// GetQueueItem fetches a single job by its ID, fully hydrated with its Release.
func (s *Store) GetQueueItem(ctx context.Context, id string) (*domain.QueueItem, error) {
	query := `
		SELECT ` + queueSelectColumns + `
		FROM queue_items q
		WHERE q.id = ? LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, id)
	item, err := scanReleaseWithQueue(row)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get queue item %s: %w", id, err)
	}

	return item, nil
}

// GetActiveQueueItems returns all jobs that are not in a terminal state (Completed/Failed).
func (s *Store) GetActiveQueueItems(ctx context.Context) ([]*domain.QueueItem, error) {
	query := `
		SELECT ` + queueSelectColumns + `
		FROM queue_items q
		WHERE q.status NOT IN ('completed', 'failed')
		ORDER BY q.created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active queue: %w", err)
	}
	defer rows.Close()

	var items []*domain.QueueItem
	for rows.Next() {
		item, scanErr := scanReleaseWithQueue(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("active queue rows iteration error: %w", err)
	}

	return items, nil
}

func (s *Store) DeleteQueueItems(ctx context.Context, ids []string) (int64, error) {
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

func (s *Store) ClearQueueHistory(ctx context.Context, statuses []domain.JobStatus) (int64, error) {
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

func (s *Store) ResetStuckQueueItems(ctx context.Context, newStatus domain.JobStatus, oldStatuses ...domain.JobStatus) error {
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
