package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ReleaseOverrideRecord struct {
	ReleaseID              string     `json:"release_id"`
	DisplayTitle           string     `json:"display_title"`
	ClassificationOverride string     `json:"classification_override"`
	TMDBIDOverride         int64      `json:"tmdb_id_override"`
	TVDBIDOverride         int64      `json:"tvdb_id_override"`
	IMDBIDOverride         string     `json:"imdb_id_override"`
	Hidden                 bool       `json:"hidden"`
	Notes                  string     `json:"notes"`
	Tags                   []string   `json:"tags"`
	CreatedAt              *time.Time `json:"created_at,omitempty"`
	UpdatedAt              *time.Time `json:"updated_at,omitempty"`
}

func (s *Store) UpsertReleaseOverride(ctx context.Context, in ReleaseOverrideRecord) error {
	tagsJSON, err := json.Marshal(sanitizeStringSlice(in.Tags))
	if err != nil {
		return fmt.Errorf("marshal release override tags: %w", err)
	}
	return execReleaseMutationTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO release_overrides (
				release_id,
				display_title,
				classification_override,
				tmdb_id_override,
				tvdb_id_override,
				imdb_id_override,
				hidden,
				notes,
				tags_json,
				updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
			ON CONFLICT (release_id) DO UPDATE SET
				display_title = EXCLUDED.display_title,
				classification_override = EXCLUDED.classification_override,
				tmdb_id_override = EXCLUDED.tmdb_id_override,
				tvdb_id_override = EXCLUDED.tvdb_id_override,
				imdb_id_override = EXCLUDED.imdb_id_override,
				hidden = EXCLUDED.hidden,
				notes = EXCLUDED.notes,
				tags_json = EXCLUDED.tags_json,
				updated_at = NOW()`,
			strings.TrimSpace(in.ReleaseID),
			strings.TrimSpace(in.DisplayTitle),
			strings.TrimSpace(in.ClassificationOverride),
			in.TMDBIDOverride,
			in.TVDBIDOverride,
			strings.TrimSpace(in.IMDBIDOverride),
			in.Hidden,
			in.Notes,
			string(tagsJSON),
		)
		if err != nil {
			return fmt.Errorf("upsert release override %s: %w", in.ReleaseID, err)
		}
		return nil
	})
}

func (s *Store) GetReleaseOverride(ctx context.Context, releaseID string) (*ReleaseOverrideRecord, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}
	var (
		item     ReleaseOverrideRecord
		tagsJSON []byte
		created  *time.Time
		updated  *time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT release_id, display_title, classification_override, tmdb_id_override, tvdb_id_override,
		       imdb_id_override, hidden, notes, tags_json, created_at, updated_at
		FROM release_overrides
		WHERE release_id = $1`, releaseID,
	).Scan(
		&item.ReleaseID,
		&item.DisplayTitle,
		&item.ClassificationOverride,
		&item.TMDBIDOverride,
		&item.TVDBIDOverride,
		&item.IMDBIDOverride,
		&item.Hidden,
		&item.Notes,
		&tagsJSON,
		&created,
		&updated,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get release override %s: %w", releaseID, err)
	}
	if len(tagsJSON) > 0 {
		_ = json.Unmarshal(tagsJSON, &item.Tags)
	}
	item.CreatedAt = created
	item.UpdatedAt = updated
	return &item, nil
}
