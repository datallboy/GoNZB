package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/segmentio/ksuid"
	_ "modernc.org/sqlite"
)

type State string

const (
	StateDiscovered             State = "discovered"
	StateReserved               State = "reserved"
	StateTransferring           State = "transferring"
	StateTransferred            State = "transferred"
	StatePosting                State = "posting"
	StateNZBReady               State = "nzb_ready"
	StateSubmitting             State = "submitting"
	StateComplete               State = "complete"
	StateFailed                 State = "failed"
	StateReconciliationRequired State = "reconciliation_required"
)

type Job struct {
	ID               string
	TorrentHash      string
	ReleaseName      string
	SourceTracker    string
	SourcePath       string
	WorkspacePath    string
	InputPath        string
	SourceSize       int64
	BytesTransferred int64
	State            State
	RetryFrom        State
	RetryCount       int
	LastError        string
	PestoPID         int
	PestoExitCode    *int
	PestoStartedAt   *time.Time
	PestoCompletedAt *time.Time
	NZBPath          string
	ArchivePassword  string
	GoNZBReleaseID   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

type CreateInput struct {
	JobID         string
	TorrentHash   string
	ReleaseName   string
	SourceTracker string
	SourcePath    string
	WorkspacePath string
	InputPath     string
	SourceSize    int64
}

type Store struct {
	db *sql.DB
}

func Open(filename string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return nil, fmt.Errorf("create worker state directory: %w", err)
	}
	stateFile, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create worker state database: %w", err)
	}
	if err := stateFile.Close(); err != nil {
		return nil, fmt.Errorf("close worker state database: %w", err)
	}
	if err := os.Chmod(filename, 0o600); err != nil {
		return nil, fmt.Errorf("secure worker state database: %w", err)
	}
	db, err := sql.Open("sqlite", filename+"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open worker state: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS worker_jobs (
  job_id TEXT PRIMARY KEY,
  torrent_hash TEXT NOT NULL UNIQUE,
  release_name TEXT NOT NULL,
  source_tracker TEXT NOT NULL DEFAULT '',
  source_path TEXT NOT NULL,
  workspace_path TEXT NOT NULL,
  input_path TEXT NOT NULL,
  source_size INTEGER NOT NULL,
  bytes_transferred INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL,
  retry_from TEXT NOT NULL DEFAULT '',
  retry_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  pesto_pid INTEGER NOT NULL DEFAULT 0,
  pesto_exit_code INTEGER,
  pesto_started_at DATETIME,
  pesto_completed_at DATETIME,
  nzb_path TEXT NOT NULL DEFAULT '',
  archive_password TEXT NOT NULL DEFAULT '',
  gonzb_release_id TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_worker_jobs_state_updated ON worker_jobs(state, updated_at);
CREATE TRIGGER IF NOT EXISTS trg_worker_jobs_updated_at
AFTER UPDATE ON worker_jobs BEGIN
  UPDATE worker_jobs SET updated_at = CURRENT_TIMESTAMP WHERE job_id = OLD.job_id;
END;`)
	if err != nil {
		return fmt.Errorf("migrate worker state: %w", err)
	}
	return nil
}

func (s *Store) Reserve(ctx context.Context, in CreateInput) (*Job, bool, error) {
	id := in.JobID
	if id == "" {
		id = ksuid.New().String()
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO worker_jobs (
 job_id, torrent_hash, release_name, source_tracker, source_path,
 workspace_path, input_path, source_size, state
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(torrent_hash) DO NOTHING`,
		id, in.TorrentHash, in.ReleaseName, in.SourceTracker, in.SourcePath,
		in.WorkspacePath, in.InputPath, in.SourceSize, StateReserved)
	if err != nil {
		return nil, false, fmt.Errorf("reserve worker job: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("read reservation result: %w", err)
	}
	job, err := s.ByHash(ctx, in.TorrentHash)
	return job, rows == 1, err
}

func (s *Store) ByHash(ctx context.Context, hash string) (*Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, selectJob+" WHERE torrent_hash = ?", hash))
}

func (s *Store) ByID(ctx context.Context, id string) (*Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, selectJob+" WHERE job_id = ?", id))
}

func (s *Store) NextRunnable(ctx context.Context) (*Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, selectJob+`
 WHERE state IN (?, ?, ?, ?, ?, ?)
 ORDER BY created_at LIMIT 1`, StateReserved, StateTransferring, StateTransferred,
		StateNZBReady, StateSubmitting, StateFailed))
}

func (s *Store) SetState(ctx context.Context, id string, state State) error {
	result, err := s.db.ExecContext(ctx, `UPDATE worker_jobs SET state = ?, last_error = '' WHERE job_id = ?`, state, id)
	return checkedUpdate(result, err, "set worker job state")
}

func (s *Store) MarkTransferStarted(ctx context.Context, id string) error {
	return s.SetState(ctx, id, StateTransferring)
}

func (s *Store) MarkTransferred(ctx context.Context, id string, bytes int64) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, bytes_transferred = ?, last_error = '' WHERE job_id = ?`, StateTransferred, bytes, id)
	return checkedUpdate(result, err, "mark worker transfer complete")
}

func (s *Store) MarkPestoStarted(ctx context.Context, id string, pid int, started time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, pesto_pid = ?, pesto_started_at = ?, pesto_exit_code = NULL,
 pesto_completed_at = NULL, last_error = '' WHERE job_id = ?`, StatePosting, pid, started.UTC(), id)
	return checkedUpdate(result, err, "mark Pesto started")
}

func (s *Store) MarkPestoComplete(ctx context.Context, id, nzbPath, password string, exitCode int, completed time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, nzb_path = ?, archive_password = ?, pesto_exit_code = ?,
 pesto_completed_at = ?, last_error = '' WHERE job_id = ?`, StateNZBReady, nzbPath, password, exitCode, completed.UTC(), id)
	return checkedUpdate(result, err, "mark Pesto complete")
}

func (s *Store) MarkSubmitting(ctx context.Context, id string) error {
	return s.SetState(ctx, id, StateSubmitting)
}

func (s *Store) MarkComplete(ctx context.Context, id, releaseID string, completed time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, gonzb_release_id = ?, completed_at = ?, last_error = '' WHERE job_id = ?`,
		StateComplete, releaseID, completed.UTC(), id)
	return checkedUpdate(result, err, "mark worker job complete")
}

func (s *Store) MarkRetryableFailure(ctx context.Context, id string, retryFrom State, cause error) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, retry_from = ?, retry_count = retry_count + 1, last_error = ? WHERE job_id = ?`,
		StateFailed, retryFrom, errorText(cause), id)
	return checkedUpdate(result, err, "mark retryable worker failure")
}

func (s *Store) MarkReconciliationRequired(ctx context.Context, id string, exitCode *int, cause error) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, pesto_exit_code = ?, pesto_completed_at = ?, last_error = ? WHERE job_id = ?`,
		StateReconciliationRequired, exitCode, time.Now().UTC(), errorText(cause), id)
	return checkedUpdate(result, err, "mark worker reconciliation required")
}

func (s *Store) ReconcileInterruptedPosts(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE worker_jobs SET state = ?, last_error = ? WHERE state = ?`,
		StateReconciliationRequired, "worker restarted while Pesto state was ambiguous", StatePosting)
	if err != nil {
		return 0, fmt.Errorf("reconcile interrupted Pesto jobs: %w", err)
	}
	return result.RowsAffected()
}

const selectJob = `SELECT job_id, torrent_hash, release_name, source_tracker, source_path,
 workspace_path, input_path, source_size, bytes_transferred, state, retry_from, retry_count,
 last_error, pesto_pid, pesto_exit_code, pesto_started_at, pesto_completed_at, nzb_path,
 archive_password, gonzb_release_id, created_at, updated_at, completed_at FROM worker_jobs`

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (*Job, error) {
	var j Job
	var state, retryFrom string
	var exit sql.NullInt64
	var pestoStarted, pestoCompleted, completed sql.NullTime
	err := row.Scan(&j.ID, &j.TorrentHash, &j.ReleaseName, &j.SourceTracker, &j.SourcePath,
		&j.WorkspacePath, &j.InputPath, &j.SourceSize, &j.BytesTransferred, &state, &retryFrom,
		&j.RetryCount, &j.LastError, &j.PestoPID, &exit, &pestoStarted, &pestoCompleted,
		&j.NZBPath, &j.ArchivePassword, &j.GoNZBReleaseID, &j.CreatedAt, &j.UpdatedAt, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read worker job: %w", err)
	}
	j.State, j.RetryFrom = State(state), State(retryFrom)
	if exit.Valid {
		value := int(exit.Int64)
		j.PestoExitCode = &value
	}
	if pestoStarted.Valid {
		j.PestoStartedAt = &pestoStarted.Time
	}
	if pestoCompleted.Valid {
		j.PestoCompletedAt = &pestoCompleted.Time
	}
	if completed.Valid {
		j.CompletedAt = &completed.Time
	}
	return &j, nil
}

func checkedUpdate(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: job not found", action)
	}
	return nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	const max = 4000
	text := err.Error()
	if len(text) > max {
		return text[:max]
	}
	return text
}
