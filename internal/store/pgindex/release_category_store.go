package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
)

func (s *Store) refreshReleaseCategory(ctx context.Context, releaseID string) error {
	return refreshReleaseCategoryRunner(ctx, s.db, releaseID)
}

func (s *Store) refreshReleaseCategoryTx(ctx context.Context, tx *sql.Tx, releaseID string) error {
	return refreshReleaseCategoryRunner(ctx, tx, releaseID)
}

func refreshReleaseCategoryRunner(ctx context.Context, runner sqlExecQueryRower, releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	var (
		attrs       newsnab.ReleaseAttributes
		chosenCat   sql.NullString
		chosenGenre sql.NullString
	)
	if err := runner.QueryRowContext(ctx, `
		SELECT
			COALESCE(r.classification, ''),
			COALESCE(r.external_media_type, ''),
			COALESCE(r.tmdb_id, 0),
			COALESCE(r.tvdb_id, 0),
			COALESCE(r.season_number, 0),
			COALESCE(r.episode_number, 0),
			COALESCE(r.primary_resolution, ''),
			COALESCE(r.primary_audio_codec, ''),
			COALESCE(r.title, ''),
			COALESCE(r.source_title, ''),
			COALESCE(r.deobfuscated_title, ''),
			COALESCE(r.matched_media_title, ''),
			COALESCE(r.title_source, ''),
			pe.category,
			pe.genre
		FROM releases r
		LEFT JOIN release_predb_matches rpm
		  ON rpm.release_id = r.release_id
		 AND rpm.chosen = TRUE
		LEFT JOIN predb_entries pe
		  ON pe.id = rpm.predb_entry_id
		WHERE r.release_id = $1`,
		releaseID,
	).Scan(
		&attrs.Classification,
		&attrs.ExternalMediaType,
		&attrs.TMDBID,
		&attrs.TVDBID,
		&attrs.SeasonNumber,
		&attrs.EpisodeNumber,
		&attrs.PrimaryResolution,
		&attrs.PrimaryAudioCodec,
		&attrs.Title,
		&attrs.SourceTitle,
		&attrs.DeobfuscatedTitle,
		&attrs.MatchedMediaTitle,
		&attrs.TitleSource,
		&chosenCat,
		&chosenGenre,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("load release category inputs %s: %w", releaseID, err)
	}

	if chosenCat.Valid {
		attrs.PredbCategory = chosenCat.String
	}
	if chosenGenre.Valid {
		attrs.PredbGenre = chosenGenre.String
	}
	resolved := newsnab.ResolveReleaseCategory(attrs)
	_, err := runner.ExecContext(ctx, `
		UPDATE releases
		SET category_id = $2,
		    category = $3,
		    updated_at = NOW()
		WHERE release_id = $1`,
		releaseID,
		resolved.ID,
		resolved.Name,
	)
	if err != nil {
		return fmt.Errorf("update release category %s: %w", releaseID, err)
	}
	return nil
}
