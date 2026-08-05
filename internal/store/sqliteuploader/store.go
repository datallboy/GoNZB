package sqliteuploader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/uploader"
	_ "modernc.org/sqlite"
)

const (
	moduleName            = "uploader"
	expectedSchemaVersion = 4
)

type Store struct {
	db       *sql.DB
	blobRoot string
}

func NewStore(dbPath, blobRoot string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("uploader SQLite path is required")
	}
	if strings.TrimSpace(blobRoot) == "" {
		return nil, fmt.Errorf("uploader blob root is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("create uploader database directory: %w", err)
	}
	if err := os.MkdirAll(blobRoot, 0700); err != nil {
		return nil, fmt.Errorf("create uploader blob directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open uploader SQLite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect uploader SQLite: %w", err)
	}
	store := &Store{db: db, blobRoot: filepath.Clean(blobRoot)}
	if err := store.RunMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate uploader SQLite: %w", err)
	}
	if err := store.ValidateSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := restrictSQLitePermissions(dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("uploader store is not initialized")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.db.PingContext(pingCtx)
}

func (s *Store) ValidateSchema(ctx context.Context) error {
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM module_schema_version WHERE module_name = ?`, moduleName).Scan(&version)
	if err != nil {
		return fmt.Errorf("read uploader schema version: %w", err)
	}
	if version != expectedSchemaVersion {
		return fmt.Errorf("uploader schema version mismatch: expected %d, got %d", expectedSchemaVersion, version)
	}
	return nil
}

func (s *Store) CreateSubmission(ctx context.Context, submission uploader.Submission, nzbBytes []byte, artifacts []uploader.Artifact) (uploader.CreateResult, error) {
	if existing, err := s.GetSubmissionBySHA256(ctx, submission.NZBSHA256); err == nil {
		return uploader.CreateResult{Submission: existing, Created: false}, nil
	} else if !errors.Is(err, uploader.ErrNotFound) {
		return uploader.CreateResult{}, err
	}
	if err := s.checkCreateKeys(ctx, submission); err != nil {
		return uploader.CreateResult{}, err
	}

	blobKey := filepath.ToSlash(filepath.Join(submission.ID, "original.nzb"))
	if err := s.writeBlobAtomically(blobKey, nzbBytes); err != nil {
		return uploader.CreateResult{}, err
	}
	submission.NZBBlobKey = blobKey
	writtenBlobKeys := []string{blobKey}
	cleanupBlobs := func() {
		for _, key := range writtenBlobKeys {
			s.removeBlob(key)
		}
	}
	for i := range artifacts {
		artifacts[i].SubmissionID = submission.ID
		artifacts[i].BlobKey = filepath.ToSlash(filepath.Join(submission.ID, artifacts[i].BlobKey))
		if err := s.writeBlobAtomically(artifacts[i].BlobKey, artifacts[i].Payload); err != nil {
			cleanupBlobs()
			return uploader.CreateResult{}, err
		}
		writtenBlobKeys = append(writtenBlobKeys, artifacts[i].BlobKey)
	}

	groupsJSON, err := json.Marshal(submission.Groups)
	if err != nil {
		cleanupBlobs()
		return uploader.CreateResult{}, err
	}
	filesJSON, err := json.Marshal(submission.Files)
	if err != nil {
		cleanupBlobs()
		return uploader.CreateResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		cleanupBlobs()
		return uploader.CreateResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO uploader_submissions (
			id, state, release_id, title, normalized_title, category_id, category,
			size_bytes, posted_at, poster, groups_json, file_count, segment_count,
			password, has_password, has_par2, has_nfo, obfuscated_subjects, encrypted_names,
			imdb_id, tmdb_id, tvdb_id, year, resolution, media_source, video_codec, audio_codec,
			nzb_sha256, nzb_blob_key, idempotency_key, intake_kind,
			provenance_tool, provenance_version, provenance_external_id,
			original_filename, submitted_by, reviewer, review_note, files_json,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		submission.ID, submission.State, submission.ReleaseID, submission.Title, submission.NormalizedTitle,
		submission.CategoryID, submission.Category, submission.SizeBytes, formatTime(submission.PostedAt), submission.Poster,
		string(groupsJSON), submission.FileCount, submission.SegmentCount, submission.Password, submission.HasPassword,
		submission.HasPAR2, submission.HasNFO, submission.ObfuscatedSubjects, submission.EncryptedNames,
		submission.IMDBID, submission.TMDBID, submission.TVDBID, submission.Year, submission.Resolution,
		submission.MediaSource, submission.VideoCodec, submission.AudioCodec, submission.NZBSHA256, submission.NZBBlobKey,
		submission.IdempotencyKey, submission.IntakeKind, submission.ProvenanceTool, submission.ProvenanceVersion,
		submission.ProvenanceExternalID, submission.OriginalFilename, submission.SubmittedBy, submission.Reviewer,
		submission.ReviewNote, string(filesJSON), formatTime(submission.CreatedAt), formatTime(submission.UpdatedAt),
	)
	if err != nil {
		cleanupBlobs()
		if existing, getErr := s.GetSubmissionBySHA256(ctx, submission.NZBSHA256); getErr == nil {
			return uploader.CreateResult{Submission: existing, Created: false}, nil
		}
		if isUniqueConstraint(err) {
			return uploader.CreateResult{}, uploader.ErrConflict
		}
		return uploader.CreateResult{}, fmt.Errorf("insert uploader submission: %w", err)
	}
	for i := range artifacts {
		artifacts[i].Payload = nil
		_, err := tx.ExecContext(ctx, `
			INSERT INTO uploader_artifacts (
				id, submission_id, kind, original_filename, label, declared_media_type,
				detected_media_type, size_bytes, sha256, display_order, blob_key, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifacts[i].ID, submission.ID,
			artifacts[i].Kind, artifacts[i].OriginalFilename, artifacts[i].Label, artifacts[i].DeclaredMediaType,
			artifacts[i].DetectedMediaType, artifacts[i].SizeBytes, artifacts[i].SHA256,
			artifacts[i].DisplayOrder, artifacts[i].BlobKey, formatTime(artifacts[i].CreatedAt))
		if err != nil {
			cleanupBlobs()
			return uploader.CreateResult{}, fmt.Errorf("insert uploader artifact: %w", err)
		}
	}
	if err := insertEvent(ctx, tx, submission.ID, "intake", submission.SubmittedBy, "", submission.State, ""); err != nil {
		cleanupBlobs()
		return uploader.CreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		cleanupBlobs()
		return uploader.CreateResult{}, err
	}
	created, err := s.GetSubmission(ctx, submission.ID)
	if err != nil {
		return uploader.CreateResult{}, err
	}
	return uploader.CreateResult{Submission: created, Created: true}, nil
}

func (s *Store) GetSubmission(ctx context.Context, id string) (*uploader.Submission, error) {
	item, err := scanSubmission(s.db.QueryRowContext(ctx, submissionSelect+` WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	item.Artifacts, err = s.listArtifacts(ctx, item.ID)
	return item, err
}

func (s *Store) GetSubmissionByReleaseID(ctx context.Context, releaseID string) (*uploader.Submission, error) {
	item, err := scanSubmission(s.db.QueryRowContext(ctx, submissionSelect+` WHERE release_id = ?`, releaseID))
	if err != nil {
		return nil, err
	}
	item.Artifacts, err = s.listArtifacts(ctx, item.ID)
	return item, err
}

func (s *Store) GetSubmissionBySHA256(ctx context.Context, hash string) (*uploader.Submission, error) {
	item, err := scanSubmission(s.db.QueryRowContext(ctx, submissionSelect+` WHERE nzb_sha256 = ?`, hash))
	if err != nil {
		return nil, err
	}
	item.Artifacts, err = s.listArtifacts(ctx, item.ID)
	return item, err
}

func (s *Store) ListSubmissions(ctx context.Context, filter uploader.ListFilter) ([]uploader.Submission, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	clauses := []string{"1 = 1"}
	args := make([]any, 0, 6)
	if filter.ApprovedOnly {
		clauses = append(clauses, "state = ?")
		args = append(args, uploader.StateApproved)
	} else if filter.State != "" {
		clauses = append(clauses, "state = ?")
		args = append(args, filter.State)
	}
	if filter.CategoryID > 0 {
		clauses = append(clauses, "category_id = ?")
		args = append(args, filter.CategoryID)
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		clauses = append(clauses, "(normalized_title LIKE ? OR original_filename LIKE ? OR provenance_tool LIKE ?)")
		pattern := "%" + uploader.NormalizeTitle(query) + "%"
		args = append(args, pattern, "%"+query+"%", "%"+query+"%")
	}
	args = append(args, limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, submissionListSelect+` WHERE `+strings.Join(clauses, " AND ")+` ORDER BY posted_at DESC, id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]uploader.Submission, 0)
	for rows.Next() {
		item, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateSubmission(ctx context.Context, id string, update uploader.Update) (*uploader.Submission, error) {
	item, err := s.GetSubmission(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.State != uploader.StatePendingReview {
		return nil, uploader.ErrInvalidTransition
	}
	if update.Title != nil {
		item.Title = strings.TrimSpace(*update.Title)
		item.NormalizedTitle = uploader.NormalizeTitle(item.Title)
	}
	if update.CategoryID != nil {
		item.CategoryID = *update.CategoryID
		item.Category = newsnab.DisplayName(item.CategoryID)
	}
	if update.PostedAt != nil {
		item.PostedAt = update.PostedAt.UTC()
	}
	if update.Password != nil {
		item.Password = *update.Password
		item.HasPassword = item.Password != ""
	}
	if update.IMDBID != nil {
		item.IMDBID = strings.TrimSpace(*update.IMDBID)
	}
	if update.TMDBID != nil {
		item.TMDBID = *update.TMDBID
	}
	if update.TVDBID != nil {
		item.TVDBID = *update.TVDBID
	}
	if update.Year != nil {
		item.Year = *update.Year
	}
	if update.Resolution != nil {
		item.Resolution = strings.TrimSpace(*update.Resolution)
	}
	if update.MediaSource != nil {
		item.MediaSource = strings.TrimSpace(*update.MediaSource)
	}
	if update.VideoCodec != nil {
		item.VideoCodec = strings.TrimSpace(*update.VideoCodec)
	}
	if update.AudioCodec != nil {
		item.AudioCodec = strings.TrimSpace(*update.AudioCodec)
	}
	item.UpdatedAt = time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE uploader_submissions SET
			title = ?, normalized_title = ?, category_id = ?, category = ?, posted_at = ?,
			password = ?, has_password = ?, imdb_id = ?, tmdb_id = ?, tvdb_id = ?, year = ?,
			resolution = ?, media_source = ?, video_codec = ?, audio_codec = ?, updated_at = ?
		WHERE id = ? AND state = ?`,
		item.Title, item.NormalizedTitle, item.CategoryID, item.Category, formatTime(item.PostedAt),
		item.Password, item.HasPassword, item.IMDBID, item.TMDBID, item.TVDBID, item.Year,
		item.Resolution, item.MediaSource, item.VideoCodec, item.AudioCodec, formatTime(item.UpdatedAt),
		id, uploader.StatePendingReview,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, uploader.ErrInvalidTransition
	}
	if err := insertEvent(ctx, tx, id, "metadata_edit", update.Actor, item.State, item.State, boundedNote(update.Note)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetSubmission(ctx, id)
}

func (s *Store) TransitionSubmission(ctx context.Context, id string, next uploader.State, actor, note string) (*uploader.Submission, error) {
	item, err := s.GetSubmission(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.State == next {
		return item, nil
	}
	allowed := (item.State == uploader.StatePendingReview && (next == uploader.StateApproved || next == uploader.StateRejected)) ||
		((item.State == uploader.StateApproved || item.State == uploader.StateRejected) && next == uploader.StatePendingReview)
	if !allowed {
		return nil, uploader.ErrInvalidTransition
	}
	now := time.Now().UTC()
	var reviewedAt, approvedAt, rejectedAt any
	reviewedAt = formatTime(now)
	if next == uploader.StateApproved {
		approvedAt = formatTime(now)
	}
	if next == uploader.StateRejected {
		rejectedAt = formatTime(now)
	}
	if next == uploader.StatePendingReview {
		reviewedAt = nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE uploader_submissions SET state = ?, reviewer = ?, review_note = ?, updated_at = ?,
			reviewed_at = ?, approved_at = ?, rejected_at = ?
		WHERE id = ? AND state = ?`, next, actor, boundedNote(note), formatTime(now), reviewedAt, approvedAt, rejectedAt, id, item.State)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, uploader.ErrInvalidTransition
	}
	eventType := string(next)
	if next == uploader.StatePendingReview {
		eventType = "return_to_pending"
	}
	if err := insertEvent(ctx, tx, id, eventType, actor, item.State, next, boundedNote(note)); err != nil {
		return nil, err
	}
	if next == uploader.StatePendingReview {
		result, err := tx.ExecContext(ctx, `
			UPDATE uploader_federation_publications
			SET state = ?, requested_by = ?, last_error = '', next_attempt_at = ?, updated_at = ?
			WHERE submission_id = ? AND state = ?`, uploader.PublicationWithdrawalRequested,
			actor, formatTime(now), formatTime(now), id, uploader.PublicationPublished)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			if err := insertEvent(ctx, tx, id, "federation_withdrawal_requested", actor, next, next, boundedNote(note)); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetSubmission(ctx, id)
}

func (s *Store) ListEvents(ctx context.Context, submissionID string) ([]uploader.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, submission_id, event_type, actor, prior_state, next_state, note, created_at
		FROM uploader_submission_events WHERE submission_id = ? ORDER BY id`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]uploader.Event, 0)
	for rows.Next() {
		var item uploader.Event
		var created string
		if err := rows.Scan(&item.ID, &item.SubmissionID, &item.EventType, &item.Actor, &item.PriorState, &item.NextState, &item.Note, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) OpenNZB(ctx context.Context, id string) (io.ReadCloser, error) {
	item, err := s.GetSubmission(ctx, id)
	if err != nil {
		return nil, err
	}
	path, err := s.blobPath(item.NZBBlobKey)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, uploader.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return file, nil
}

const submissionSelect = `
	SELECT id, state, release_id, title, normalized_title, category_id, category,
		size_bytes, posted_at, poster, groups_json, file_count, segment_count,
		password, has_password, has_par2, has_nfo, obfuscated_subjects, encrypted_names,
		imdb_id, tmdb_id, tvdb_id, year, resolution, media_source, video_codec, audio_codec,
		nzb_sha256, nzb_blob_key, idempotency_key, intake_kind,
		provenance_tool, provenance_version, provenance_external_id,
		original_filename, submitted_by, reviewer, review_note, files_json,
		created_at, updated_at, reviewed_at, approved_at, rejected_at
	FROM uploader_submissions`

// List queries deliberately do not select the protected password column.
const submissionListSelect = `
	SELECT id, state, release_id, title, normalized_title, category_id, category,
		size_bytes, posted_at, poster, groups_json, file_count, segment_count,
		'' AS password, has_password, has_par2, has_nfo, obfuscated_subjects, encrypted_names,
		imdb_id, tmdb_id, tvdb_id, year, resolution, media_source, video_codec, audio_codec,
		nzb_sha256, nzb_blob_key, idempotency_key, intake_kind,
		provenance_tool, provenance_version, provenance_external_id,
		original_filename, submitted_by, reviewer, review_note, files_json,
		created_at, updated_at, reviewed_at, approved_at, rejected_at
	FROM uploader_submissions`

type rowScanner interface{ Scan(dest ...any) error }

func scanSubmission(row rowScanner) (*uploader.Submission, error) {
	var item uploader.Submission
	var groupsJSON, filesJSON, postedAt, createdAt, updatedAt string
	var reviewedAt, approvedAt, rejectedAt sql.NullString
	err := row.Scan(
		&item.ID, &item.State, &item.ReleaseID, &item.Title, &item.NormalizedTitle, &item.CategoryID, &item.Category,
		&item.SizeBytes, &postedAt, &item.Poster, &groupsJSON, &item.FileCount, &item.SegmentCount,
		&item.Password, &item.HasPassword, &item.HasPAR2, &item.HasNFO, &item.ObfuscatedSubjects, &item.EncryptedNames,
		&item.IMDBID, &item.TMDBID, &item.TVDBID, &item.Year, &item.Resolution, &item.MediaSource, &item.VideoCodec, &item.AudioCodec,
		&item.NZBSHA256, &item.NZBBlobKey, &item.IdempotencyKey, &item.IntakeKind,
		&item.ProvenanceTool, &item.ProvenanceVersion, &item.ProvenanceExternalID,
		&item.OriginalFilename, &item.SubmittedBy, &item.Reviewer, &item.ReviewNote, &filesJSON,
		&createdAt, &updatedAt, &reviewedAt, &approvedAt, &rejectedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, uploader.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(groupsJSON), &item.Groups); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(filesJSON), &item.Files); err != nil {
		return nil, err
	}
	if item.PostedAt, err = parseTime(postedAt); err != nil {
		return nil, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if item.ReviewedAt, err = parseOptionalTime(reviewedAt); err != nil {
		return nil, err
	}
	if item.ApprovedAt, err = parseOptionalTime(approvedAt); err != nil {
		return nil, err
	}
	if item.RejectedAt, err = parseOptionalTime(rejectedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) checkCreateKeys(ctx context.Context, item uploader.Submission) error {
	for query, value := range map[string]string{
		`SELECT nzb_sha256 FROM uploader_submissions WHERE idempotency_key = ?`:                                item.IdempotencyKey,
		`SELECT nzb_sha256 FROM uploader_submissions WHERE provenance_tool = ? AND provenance_external_id = ?`: item.ProvenanceExternalID,
	} {
		if value == "" {
			continue
		}
		var existingHash string
		var err error
		if strings.Contains(query, "provenance_tool") {
			err = s.db.QueryRowContext(ctx, query, item.ProvenanceTool, value).Scan(&existingHash)
		} else {
			err = s.db.QueryRowContext(ctx, query, value).Scan(&existingHash)
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if existingHash != item.NZBSHA256 {
			return uploader.ErrConflict
		}
	}
	return nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, submissionID, eventType, actor string, prior, next uploader.State, note string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO uploader_submission_events (submission_id, event_type, actor, prior_state, next_state, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, submissionID, eventType, actor, prior, next, boundedNote(note), formatTime(time.Now().UTC()))
	return err
}

func (s *Store) writeBlobAtomically(key string, payload []byte) error {
	path, err := s.blobPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nzb-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) blobPath(key string) (string, error) {
	key = filepath.Clean(filepath.FromSlash(key))
	if key == "." || filepath.IsAbs(key) || key == ".." || strings.HasPrefix(key, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid uploader blob key")
	}
	path := filepath.Join(s.blobRoot, key)
	rel, err := filepath.Rel(s.blobRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("uploader blob path escapes root")
	}
	return path, nil
}

func (s *Store) removeBlob(key string) {
	path, err := s.blobPath(key)
	if err != nil {
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(filepath.Dir(path))
}

func restrictSQLitePermissions(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restrict uploader SQLite permissions for %s: %w", candidate, err)
		}
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
func boundedNote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		value = value[:4096]
	}
	return value
}

var _ uploader.Store = (*Store)(nil)
