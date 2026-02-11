package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

// SaveReleaseFiles maps the actual NZB file contents into the database.
func (s *PersistentStore) SaveReleaseFiles(ctx context.Context, releaseID string, files []*domain.DownloadFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, f := range files {
		// 1. Upsert File Record
		var fileID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO release_files (release_id, filename, size, file_index, is_pars, subject, date)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(release_id, filename) DO UPDATE SET size = excluded.size
			RETURNING id`,
			releaseID, f.FileName, f.Size, f.Index, f.IsPars, f.Subject, f.Date,
		).Scan(&fileID)
		if err != nil {
			return err
		}

		// 2. Map Many-to-Many Groups
		for _, groupName := range f.Groups {
			var groupID int64
			err := tx.QueryRowContext(ctx, `
				INSERT INTO groups (name) VALUES (?) 
				ON CONFLICT(name) DO UPDATE SET name=name 
				RETURNING id`, groupName).Scan(&groupID)
			if err != nil {
				return err
			}

			_, err = tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO release_file_groups (release_file_id, group_id)
				VALUES (?, ?)`, fileID, groupID)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GetReleaseFiles retrieves all files and their cross-posted groups for a specific release.
func (s *PersistentStore) GetReleaseFiles(ctx context.Context, releaseID string) ([]*domain.DownloadFile, error) {
	query := `
		SELECT 
			rf.id, rf.release_id, rf.filename, rf.size, rf.file_index, 
			rf.is_pars, rf.subject, rf.date,
			GROUP_CONCAT(g.name) as group_names
		FROM release_files rf
		LEFT JOIN release_file_groups rfg ON rf.id = rfg.release_file_id
		LEFT JOIN groups g ON rfg.group_id = g.id
		WHERE rf.release_id = ?
		GROUP BY rf.id
		ORDER BY rf.file_index ASC`

	rows, err := s.db.QueryContext(ctx, query, releaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to query release files: %w", err)
	}
	defer rows.Close()

	var files []*domain.DownloadFile
	for rows.Next() {
		var f domain.DownloadFile
		var groupConcat sql.NullString

		err := rows.Scan(
			&f.ID, &f.ReleaseID, &f.FileName, &f.Size, &f.Index,
			&f.IsPars, &f.Subject, &f.Date, &groupConcat,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan release file: %w", err)
		}

		// Convert the comma-separated string from GROUP_CONCAT back into a slice
		if groupConcat.Valid && groupConcat.String != "" {
			f.Groups = strings.Split(groupConcat.String, ",")
		} else {
			f.Groups = []string{}
		}

		files = append(files, &f)
	}

	return files, nil
}
