package sqliteuploader

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) ShouldAttemptInboxPath(ctx context.Context, fingerprint string, modTime time.Time, size int64, now time.Time) (bool, error) {
	var observedMod, nextAttempt string
	var observedSize int64
	err := s.db.QueryRowContext(ctx, `
		SELECT observed_mod_time, observed_size, next_attempt_at
		FROM uploader_inbox_failures WHERE path_fingerprint = ?`, strings.TrimSpace(fingerprint)).Scan(&observedMod, &observedSize, &nextAttempt)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if observedMod != formatTime(modTime.UTC()) || observedSize != size {
		return true, nil
	}
	next, err := parseTime(nextAttempt)
	if err != nil {
		return true, err
	}
	return !next.After(now.UTC()), nil
}

func (s *Store) RecordInboxFailure(ctx context.Context, fingerprint string, modTime time.Time, size int64, code, message string, nextAttempt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO uploader_inbox_failures (
			path_fingerprint, observed_mod_time, observed_size, error_code, safe_message, next_attempt_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (path_fingerprint) DO UPDATE SET
			observed_mod_time = excluded.observed_mod_time,
			observed_size = excluded.observed_size,
			error_code = excluded.error_code,
			safe_message = excluded.safe_message,
			next_attempt_at = excluded.next_attempt_at,
			updated_at = excluded.updated_at`, strings.TrimSpace(fingerprint), formatTime(modTime.UTC()), size,
		strings.TrimSpace(code), boundedNote(message), formatTime(nextAttempt.UTC()), formatTime(time.Now().UTC()))
	return err
}

func (s *Store) ClearInboxFailure(ctx context.Context, fingerprint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM uploader_inbox_failures WHERE path_fingerprint = ?`, strings.TrimSpace(fingerprint))
	return err
}
