package sqlitejob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/segmentio/ksuid"
)

// fileSetHashItem is the normalized shape used to hash a queue file set.
type fileSetHashItem struct {
	FileName string   `json:"file_name"`
	Size     int64    `json:"size"`
	Index    int      `json:"index"`
	IsPars   bool     `json:"is_pars"`
	Subject  string   `json:"subject"`
	Date     int64    `json:"date_unix"`
	Poster   string   `json:"poster"`
	Groups   []string `json:"groups"`
}

// buildFileSetHash deterministically hashes a normalized file list.
func buildFileSetHash(files []*domain.DownloadFile) (string, error) {
	items := make([]fileSetHashItem, 0, len(files))

	for _, f := range files {
		groups := append([]string(nil), f.Groups...)
		sort.Strings(groups)

		items = append(items, fileSetHashItem{
			FileName: f.FileName,
			Size:     f.Size,
			Index:    f.Index,
			IsPars:   f.IsPars,
			Subject:  f.Subject,
			Date:     f.Date,
			Poster:   f.Poster,
			Groups:   groups,
		})
	}

	// CHANGED: stable ordering by file index then name.
	sort.Slice(items, func(i, j int) bool {
		if items[i].Index == items[j].Index {
			return items[i].FileName < items[j].FileName
		}
		return items[i].Index < items[j].Index
	})

	b, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("marshal normalized file set: %w", err)
	}

	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) SaveQueueItemFiles(ctx context.Context, queueItemID string, files []*domain.DownloadFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// dedupe by content hash of normalized file metadata.
	contentHash, err := buildFileSetHash(files)
	if err != nil {
		return err
	}

	var fileSetID string
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM queue_file_sets
		WHERE content_hash = ?
		LIMIT 1`, contentHash).Scan(&fileSetID)

	switch {
	case err == nil:
		// existing set found, reuse it
	case err == sql.ErrNoRows:
		// create a new file set and all file_set_items rows.
		fileSetID = ksuid.New().String()

		_, err = tx.ExecContext(ctx, `
			INSERT INTO queue_file_sets (id, content_hash, total_files)
			VALUES (?, ?, ?)`,
			fileSetID, contentHash, len(files),
		)
		if err != nil {
			return fmt.Errorf("insert queue_file_sets row: %w", err)
		}

		for _, f := range files {
			groupsJSON, err := json.Marshal(f.Groups)
			if err != nil {
				return fmt.Errorf("marshal groups_json for %q: %w", f.FileName, err)
			}

			_, err = tx.ExecContext(ctx, `
				INSERT INTO queue_file_set_items (
					file_set_id, file_name, size, file_index, is_pars, subject, date_unix, poster, groups_json
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(file_set_id, file_index) DO UPDATE SET
					file_name = excluded.file_name,
					size = excluded.size,
					is_pars = excluded.is_pars,
					subject = excluded.subject,
					date_unix = excluded.date_unix,
					poster = excluded.poster,
					groups_json = excluded.groups_json`,
				fileSetID,
				f.FileName,
				f.Size,
				f.Index,
				f.IsPars,
				f.Subject,
				f.Date,
				f.Poster,
				string(groupsJSON),
			)
			if err != nil {
				return fmt.Errorf("insert queue_file_set_items row for %q: %w", f.FileName, err)
			}
		}
	default:
		return fmt.Errorf("lookup queue_file_sets by hash: %w", err)
	}

	// link queue item to deduped file set.
	_, err = tx.ExecContext(ctx, `
		UPDATE queue_items
		SET file_set_id = ?
		WHERE id = ?`, fileSetID, queueItemID)
	if err != nil {
		return fmt.Errorf("update queue_items.file_set_id: %w", err)
	}

	return tx.Commit()
}

func (s *Store) GetQueueItemFiles(ctx context.Context, queueItemID string) ([]*domain.DownloadFile, error) {
	var fileSetID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT file_set_id
		FROM queue_items
		WHERE id = ?
		LIMIT 1`, queueItemID).Scan(&fileSetID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup queue item file_set_id: %w", err)
	}

	if !fileSetID.Valid || fileSetID.String == "" {
		return []*domain.DownloadFile{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			file_name,
			size,
			file_index,
			is_pars,
			subject,
			date_unix,
			poster,
			groups_json
		FROM queue_file_set_items
		WHERE file_set_id = ?
		ORDER BY file_index ASC, id ASC`, fileSetID.String)
	if err != nil {
		return nil, fmt.Errorf("query queue_file_set_items: %w", err)
	}
	defer rows.Close()

	files := make([]*domain.DownloadFile, 0)
	for rows.Next() {
		f := &domain.DownloadFile{}
		var subject sql.NullString
		var poster sql.NullString
		var groupsJSON sql.NullString

		if err := rows.Scan(
			&f.ID,
			&f.FileName,
			&f.Size,
			&f.Index,
			&f.IsPars,
			&subject,
			&f.Date,
			&poster,
			&groupsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan queue_file_set_items row: %w", err)
		}

		// compatibility for existing API mapping usage
		f.ReleaseID = queueItemID

		if subject.Valid {
			f.Subject = subject.String
		}
		if poster.Valid {
			f.Poster = poster.String
		}

		if groupsJSON.Valid && groupsJSON.String != "" {
			if err := json.Unmarshal([]byte(groupsJSON.String), &f.Groups); err != nil {
				return nil, fmt.Errorf("unmarshal groups_json for %q: %w", f.FileName, err)
			}
		}
		if f.Groups == nil {
			f.Groups = []string{}
		}

		files = append(files, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queue_file_set_items rows: %w", err)
	}

	return files, nil
}
