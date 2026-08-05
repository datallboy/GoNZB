package sqliteuploader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/datallboy/gonzb/internal/uploader"
)

const artifactSelect = `
	SELECT id, submission_id, kind, original_filename, label, declared_media_type,
		detected_media_type, size_bytes, sha256, display_order, blob_key, created_at
	FROM uploader_artifacts`

func (s *Store) listArtifacts(ctx context.Context, submissionID string) ([]uploader.Artifact, error) {
	rows, err := s.db.QueryContext(ctx, artifactSelect+` WHERE submission_id = ? ORDER BY display_order, id`, strings.TrimSpace(submissionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]uploader.Artifact, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) OpenArtifact(ctx context.Context, submissionID, artifactID string) (*uploader.Artifact, io.ReadCloser, error) {
	item, err := scanArtifact(s.db.QueryRowContext(ctx, artifactSelect+` WHERE submission_id = ? AND id = ?`, strings.TrimSpace(submissionID), strings.TrimSpace(artifactID)))
	if err != nil {
		return nil, nil, err
	}
	path, err := s.blobPath(item.BlobKey)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil, uploader.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return item, file, nil
}

func scanArtifact(row rowScanner) (*uploader.Artifact, error) {
	var item uploader.Artifact
	var createdAt string
	err := row.Scan(&item.ID, &item.SubmissionID, &item.Kind, &item.OriginalFilename, &item.Label,
		&item.DeclaredMediaType, &item.DetectedMediaType, &item.SizeBytes, &item.SHA256,
		&item.DisplayOrder, &item.BlobKey, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, uploader.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan uploader artifact: %w", err)
	}
	item.CreatedAt, err = parseTime(createdAt)
	return &item, err
}
