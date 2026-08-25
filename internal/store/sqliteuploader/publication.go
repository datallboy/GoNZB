package sqliteuploader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/uploader"
)

const publicationSelect = `
	SELECT submission_id, pool_id, state, release_id, manifest_id, card_event_id,
		manifest_event_id, publication_state_event_id, attempt_count, last_error,
		next_attempt_at, requested_by, created_at, updated_at
	FROM uploader_federation_publications`

func (s *Store) ListFederationPublications(ctx context.Context, submissionID string) ([]uploader.FederationPublication, error) {
	rows, err := s.db.QueryContext(ctx, publicationSelect+` WHERE submission_id = ? ORDER BY pool_id`, strings.TrimSpace(submissionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPublications(rows)
}

func (s *Store) ListDueFederationPublications(ctx context.Context, limit int) ([]uploader.FederationPublication, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, publicationSelect+`
		WHERE state IN (?, ?, ?)
		  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY updated_at, submission_id, pool_id LIMIT ?`,
		uploader.PublicationRequested, uploader.PublicationFailed, uploader.PublicationWithdrawalRequested,
		formatTime(time.Now().UTC()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPublications(rows)
}

func (s *Store) RequestFederationPublication(ctx context.Context, submissionID, poolID, actor string) (*uploader.FederationPublication, error) {
	submissionID, poolID = strings.TrimSpace(submissionID), strings.TrimSpace(poolID)
	item, err := s.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	if item.State != uploader.StateApproved || poolID == "" {
		return nil, uploader.ErrInvalidTransition
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO uploader_federation_publications (
			submission_id, pool_id, state, requested_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (submission_id, pool_id) DO UPDATE SET
			state = CASE WHEN uploader_federation_publications.state = ? THEN ? ELSE uploader_federation_publications.state END,
			requested_by = excluded.requested_by,
			last_error = CASE WHEN uploader_federation_publications.state = ? THEN '' ELSE uploader_federation_publications.last_error END,
			next_attempt_at = CASE WHEN uploader_federation_publications.state = ? THEN excluded.updated_at ELSE uploader_federation_publications.next_attempt_at END,
			updated_at = excluded.updated_at`,
		submissionID, poolID, uploader.PublicationRequested, strings.TrimSpace(actor), formatTime(now), formatTime(now),
		uploader.PublicationWithdrawn, uploader.PublicationRequested,
		uploader.PublicationWithdrawn, uploader.PublicationWithdrawn,
	)
	if err != nil {
		return nil, err
	}
	if err := insertEvent(ctx, tx, submissionID, "federation_publication_requested", actor, item.State, item.State, "pool="+poolID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFederationPublication(ctx, submissionID, poolID)
}

func (s *Store) RequestFederationWithdrawal(ctx context.Context, submissionID, poolID, actor, note string) (*uploader.FederationPublication, error) {
	publication, err := s.getFederationPublication(ctx, submissionID, poolID)
	if err != nil {
		return nil, err
	}
	if publication.State == uploader.PublicationWithdrawn || publication.State == uploader.PublicationWithdrawalRequested {
		return publication, nil
	}
	if publication.State != uploader.PublicationPublished {
		return nil, uploader.ErrInvalidTransition
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE uploader_federation_publications
		SET state = ?, requested_by = ?, last_error = '', next_attempt_at = ?, updated_at = ?
		WHERE submission_id = ? AND pool_id = ? AND state = ?`,
		uploader.PublicationWithdrawalRequested, strings.TrimSpace(actor), formatTime(now), formatTime(now),
		submissionID, poolID, uploader.PublicationPublished); err != nil {
		return nil, err
	}
	if err := insertEvent(ctx, tx, submissionID, "federation_withdrawal_requested", actor, "", "", "pool="+poolID+" "+boundedNote(note)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFederationPublication(ctx, submissionID, poolID)
}

func (s *Store) RequestSubmissionWithdrawals(ctx context.Context, submissionID, actor, note string) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE uploader_federation_publications
		SET state = ?, requested_by = ?, last_error = '', next_attempt_at = ?, updated_at = ?
		WHERE submission_id = ? AND state = ?`, uploader.PublicationWithdrawalRequested,
		strings.TrimSpace(actor), formatTime(now), formatTime(now), strings.TrimSpace(submissionID), uploader.PublicationPublished)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		if err := insertEvent(ctx, tx, submissionID, "federation_withdrawal_requested", actor, "", "", boundedNote(note)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CompleteFederationPublication(ctx context.Context, submissionID, poolID string, state uploader.PublicationState, outcome uploader.PublicationOutcome) (*uploader.FederationPublication, error) {
	if state != uploader.PublicationPublished && state != uploader.PublicationWithdrawn {
		return nil, uploader.ErrInvalidTransition
	}
	now := time.Now().UTC()
	prior, err := s.getFederationPublication(ctx, submissionID, poolID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE uploader_federation_publications SET
			state = ?, release_id = COALESCE(NULLIF(?, ''), release_id),
			manifest_id = COALESCE(NULLIF(?, ''), manifest_id),
			card_event_id = COALESCE(NULLIF(?, ''), card_event_id),
			manifest_event_id = COALESCE(NULLIF(?, ''), manifest_event_id),
			publication_state_event_id = COALESCE(NULLIF(?, ''), publication_state_event_id),
			last_error = '', next_attempt_at = NULL, updated_at = ?
		WHERE submission_id = ? AND pool_id = ?`, state, outcome.ReleaseID, outcome.ManifestID,
		outcome.CardEventID, outcome.ManifestEventID, outcome.PublicationStateEventID,
		formatTime(now), strings.TrimSpace(submissionID), strings.TrimSpace(poolID))
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, uploader.ErrNotFound
	}
	eventType := "federation_published"
	if state == uploader.PublicationWithdrawn {
		eventType = "federation_withdrawn"
	} else if prior.PublicationStateEventID != "" {
		eventType = "federation_restored"
	}
	if err := insertEvent(ctx, tx, submissionID, eventType, prior.RequestedBy, "", "", "pool="+poolID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFederationPublication(ctx, submissionID, poolID)
}

func (s *Store) FailFederationPublication(ctx context.Context, submissionID, poolID string, cause error) (*uploader.FederationPublication, error) {
	item, err := s.getFederationPublication(ctx, submissionID, poolID)
	if err != nil {
		return nil, err
	}
	nextState := uploader.PublicationFailed
	if item.State == uploader.PublicationWithdrawalRequested {
		nextState = uploader.PublicationWithdrawalRequested
	}
	attempt := item.AttemptCount + 1
	delay := time.Minute * time.Duration(1<<min(attempt-1, 6))
	next := time.Now().UTC().Add(delay)
	message := "publication failed"
	if cause != nil {
		message = boundedNote(cause.Error())
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		UPDATE uploader_federation_publications
		SET state = ?, attempt_count = ?, last_error = ?, next_attempt_at = ?, updated_at = ?
		WHERE submission_id = ? AND pool_id = ?`, nextState, attempt, message, formatTime(next),
		formatTime(time.Now().UTC()), submissionID, poolID)
	if err != nil {
		return nil, err
	}
	eventType := "federation_publication_failed"
	if nextState == uploader.PublicationWithdrawalRequested {
		eventType = "federation_withdrawal_failed"
	}
	if err := insertEvent(ctx, tx, submissionID, eventType, item.RequestedBy, "", "", "pool="+poolID+" "+message); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getFederationPublication(ctx, submissionID, poolID)
}

func (s *Store) getFederationPublication(ctx context.Context, submissionID, poolID string) (*uploader.FederationPublication, error) {
	return scanPublication(s.db.QueryRowContext(ctx, publicationSelect+` WHERE submission_id = ? AND pool_id = ?`, strings.TrimSpace(submissionID), strings.TrimSpace(poolID)))
}

func scanPublications(rows *sql.Rows) ([]uploader.FederationPublication, error) {
	items := make([]uploader.FederationPublication, 0)
	for rows.Next() {
		item, err := scanPublication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanPublication(row rowScanner) (*uploader.FederationPublication, error) {
	var item uploader.FederationPublication
	var nextAttempt sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(&item.SubmissionID, &item.PoolID, &item.State, &item.ReleaseID, &item.ManifestID,
		&item.CardEventID, &item.ManifestEventID, &item.PublicationStateEventID, &item.AttemptCount,
		&item.LastError, &nextAttempt, &item.RequestedBy, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, uploader.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan uploader federation publication: %w", err)
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if nextAttempt.Valid {
		value, err := parseTime(nextAttempt.String)
		if err != nil {
			return nil, err
		}
		item.NextAttemptAt = &value
	}
	return &item, nil
}
