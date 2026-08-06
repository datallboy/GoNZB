package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/uploader"
)

const (
	uploaderCatalogProviderKey  = "local_uploader"
	uploaderCatalogProviderName = "Local NZB Uploader"
	uploaderCatalogSourceKind   = "uploader"
	uploaderCatalogFileBatch    = 500
)

// PublishUploaderSubmission projects one approved completed-NZB submission
// into the terminal release catalog. It deliberately does not create scrape,
// binary, inspection, or release-formation records.
func (s *Store) PublishUploaderSubmission(ctx context.Context, submission uploader.Submission) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if submission.State != uploader.StateApproved {
		return fmt.Errorf("uploader submission %s is not approved", submission.ID)
	}
	if strings.TrimSpace(submission.ID) == "" || strings.TrimSpace(submission.ReleaseID) == "" {
		return fmt.Errorf("uploader submission and release ids are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin uploader catalog publication: %w", err)
	}
	defer rollbackTx(tx)

	if err := publishUploaderSubmissionTx(ctx, tx, submission); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit uploader catalog publication: %w", err)
	}
	return nil
}

// WithdrawUploaderSubmission removes only the terminal uploader projection.
// The authoritative uploader submission and its audit history remain intact.
func (s *Store) WithdrawUploaderSubmission(ctx context.Context, releaseID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM releases
		WHERE release_id = $1
		  AND source_kind = $2`, releaseID, uploaderCatalogSourceKind); err != nil {
		return fmt.Errorf("withdraw uploader catalog release %s: %w", releaseID, err)
	}
	return nil
}

// ReconcileUploaderSubmissions republishes all approved submissions and then
// removes projections that no longer have an approved authoritative row.
func (s *Store) ReconcileUploaderSubmissions(ctx context.Context, submissions []uploader.Submission) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	approved := make(map[string]struct{}, len(submissions))
	for _, submission := range submissions {
		if submission.State != uploader.StateApproved {
			continue
		}
		if err := s.PublishUploaderSubmission(ctx, submission); err != nil {
			return err
		}
		approved[strings.TrimSpace(submission.ReleaseID)] = struct{}{}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT release_id
		FROM releases
		WHERE source_kind = $1`, uploaderCatalogSourceKind)
	if err != nil {
		return fmt.Errorf("list uploader catalog projections: %w", err)
	}
	stale := make([]string, 0)
	for rows.Next() {
		var releaseID string
		if err := rows.Scan(&releaseID); err != nil {
			rows.Close()
			return fmt.Errorf("scan uploader catalog projection: %w", err)
		}
		if _, ok := approved[releaseID]; !ok {
			stale = append(stale, releaseID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate uploader catalog projections: %w", err)
	}
	rows.Close()

	for _, releaseID := range stale {
		if err := s.WithdrawUploaderSubmission(ctx, releaseID); err != nil {
			return err
		}
	}
	return nil
}

func publishUploaderSubmissionTx(ctx context.Context, tx *sql.Tx, submission uploader.Submission) error {
	var providerID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO usenet_providers (provider_key, display_name)
		VALUES ($1, $2)
		ON CONFLICT (provider_key) DO UPDATE
		SET display_name = EXCLUDED.display_name
		RETURNING id`, uploaderCatalogProviderKey, uploaderCatalogProviderName).Scan(&providerID); err != nil {
		return fmt.Errorf("ensure uploader catalog provider: %w", err)
	}

	passwordState := "not_passworded"
	if submission.HasPassword {
		passwordState = "password_known"
	}
	searchTitle := strings.TrimSpace(submission.NormalizedTitle)
	if searchTitle == "" {
		searchTitle = uploader.NormalizeTitle(submission.Title)
	}
	parFileCount := uploaderPARFileCount(submission)
	groupName := "uploader:" + strings.TrimSpace(submission.ID)

	var releaseID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO releases (
			release_id, guid, provider_id, source_release_key, release_family_key,
			release_key, group_name, title, source_title, deobfuscated_title,
			matched_media_title, title_source, title_confidence, search_title,
			category_id, category, classification, poster, size_bytes, posted_at,
			file_count, expected_file_count, expected_archive_file_count,
			par_file_count, completion_pct, match_confidence, identity_status,
			passworded, passworded_known, passworded_unknown, password_state,
			encrypted, has_par2, has_nfo, availability_score, availability_tier,
			media_quality_score, media_quality_tier, identity_confidence_score,
			primary_resolution, primary_video_codec, primary_audio_codec,
			metadata_updated_at, tmdb_id, tvdb_id, external_year, source_kind,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
			$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,
			$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,
			$48,$49
		)
		ON CONFLICT (release_id) DO UPDATE SET
			guid = EXCLUDED.guid,
			provider_id = EXCLUDED.provider_id,
			source_release_key = EXCLUDED.source_release_key,
			release_family_key = EXCLUDED.release_family_key,
			release_key = EXCLUDED.release_key,
			group_name = EXCLUDED.group_name,
			title = EXCLUDED.title,
			source_title = EXCLUDED.source_title,
			deobfuscated_title = EXCLUDED.deobfuscated_title,
			matched_media_title = EXCLUDED.matched_media_title,
			title_source = EXCLUDED.title_source,
			title_confidence = EXCLUDED.title_confidence,
			search_title = EXCLUDED.search_title,
			category_id = EXCLUDED.category_id,
			category = EXCLUDED.category,
			classification = EXCLUDED.classification,
			poster = EXCLUDED.poster,
			size_bytes = EXCLUDED.size_bytes,
			posted_at = EXCLUDED.posted_at,
			file_count = EXCLUDED.file_count,
			expected_file_count = EXCLUDED.expected_file_count,
			expected_archive_file_count = EXCLUDED.expected_archive_file_count,
			par_file_count = EXCLUDED.par_file_count,
			completion_pct = EXCLUDED.completion_pct,
			match_confidence = EXCLUDED.match_confidence,
			identity_status = EXCLUDED.identity_status,
			passworded = EXCLUDED.passworded,
			passworded_known = EXCLUDED.passworded_known,
			passworded_unknown = EXCLUDED.passworded_unknown,
			password_state = EXCLUDED.password_state,
			encrypted = EXCLUDED.encrypted,
			has_par2 = EXCLUDED.has_par2,
			has_nfo = EXCLUDED.has_nfo,
			availability_score = EXCLUDED.availability_score,
			availability_tier = EXCLUDED.availability_tier,
			media_quality_score = EXCLUDED.media_quality_score,
			media_quality_tier = EXCLUDED.media_quality_tier,
			identity_confidence_score = EXCLUDED.identity_confidence_score,
			primary_resolution = EXCLUDED.primary_resolution,
			primary_video_codec = EXCLUDED.primary_video_codec,
			primary_audio_codec = EXCLUDED.primary_audio_codec,
			metadata_updated_at = EXCLUDED.metadata_updated_at,
			tmdb_id = EXCLUDED.tmdb_id,
			tvdb_id = EXCLUDED.tvdb_id,
			external_year = EXCLUDED.external_year,
			source_kind = EXCLUDED.source_kind,
			updated_at = EXCLUDED.updated_at
		WHERE releases.source_kind = $47
		RETURNING release_id`,
		submission.ReleaseID,
		submission.ReleaseID,
		providerID,
		submission.ID,
		submission.ID,
		submission.ID,
		groupName,
		strings.TrimSpace(submission.Title),
		strings.TrimSpace(submission.Title),
		strings.TrimSpace(submission.Title),
		"",
		"uploader_review",
		1.0,
		searchTitle,
		submission.CategoryID,
		strings.TrimSpace(submission.Category),
		"",
		strings.TrimSpace(submission.Poster),
		submission.SizeBytes,
		submission.PostedAt.UTC(),
		submission.FileCount,
		submission.FileCount,
		0,
		parFileCount,
		100.0,
		1.0,
		"identified",
		submission.HasPassword,
		submission.HasPassword,
		false,
		passwordState,
		submission.EncryptedNames,
		submission.HasPAR2,
		submission.HasNFO,
		0.0,
		"unverified",
		0.0,
		"unknown",
		1.0,
		strings.TrimSpace(submission.Resolution),
		strings.TrimSpace(submission.VideoCodec),
		strings.TrimSpace(submission.AudioCodec),
		submission.UpdatedAt.UTC(),
		submission.TMDBID,
		submission.TVDBID,
		submission.Year,
		uploaderCatalogSourceKind,
		submission.CreatedAt.UTC(),
		submission.UpdatedAt.UTC(),
	).Scan(&releaseID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("release id %s belongs to a non-uploader catalog source", submission.ReleaseID)
		}
		return fmt.Errorf("upsert uploader catalog release %s: %w", submission.ReleaseID, err)
	}

	if imdbID := strings.TrimSpace(submission.IMDBID); imdbID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_overrides (release_id, imdb_id_override, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (release_id) DO UPDATE
			SET imdb_id_override = CASE
				WHEN BTRIM(COALESCE(release_overrides.imdb_id_override, '')) = ''
				THEN EXCLUDED.imdb_id_override
				ELSE release_overrides.imdb_id_override
			END,
			updated_at = NOW()`, releaseID, imdbID); err != nil {
			return fmt.Errorf("project uploader IMDb id for %s: %w", releaseID, err)
		}
	}

	if err := replaceUploaderCatalogFilesTx(ctx, tx, releaseID, submission); err != nil {
		return err
	}
	newsgroupIDs, err := ensureUploaderNewsgroupsTx(ctx, tx, submission.Groups)
	if err != nil {
		return err
	}
	if err := replaceReleaseNewsgroupsInRunner(ctx, tx, releaseID, newsgroupIDs); err != nil {
		return err
	}
	return nil
}

func replaceUploaderCatalogFilesTx(ctx context.Context, tx *sql.Tx, releaseID string, submission uploader.Submission) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM release_catalog_files WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("clear uploader catalog files for %s: %w", releaseID, err)
	}
	seenNames := make(map[string]int, len(submission.Files))
	for start := 0; start < len(submission.Files); start += uploaderCatalogFileBatch {
		end := min(start+uploaderCatalogFileBatch, len(submission.Files))
		args := make([]any, 0, (end-start)*14)
		values := make([]string, 0, end-start)
		for index := start; index < end; index++ {
			file := submission.Files[index]
			name := uploaderCatalogFileName(file.Name, index, seenNames)
			postedAt := file.PostedAt
			if postedAt.IsZero() {
				postedAt = submission.PostedAt
			}
			base := len(args)
			values = append(values, fmt.Sprintf(
				"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7,
				base+8, base+9, base+10, base+11, base+12, base+13, base+14,
			))
			args = append(args,
				releaseID,
				name,
				file.SizeBytes,
				index,
				strings.HasSuffix(strings.ToLower(strings.TrimSpace(file.Name)), ".par2"),
				strings.TrimSpace(file.Subject),
				strings.TrimSpace(file.Poster),
				postedAt.UTC(),
				file.SegmentCount,
				file.SegmentCount,
				file.SegmentCount,
				1.0,
				"uploader_nzb",
				submission.UpdatedAt.UTC(),
			)
		}
		if len(args) > postgresBindParameterSoftLimit {
			return fmt.Errorf("uploader catalog file insert chunk has %d bind parameters", len(args))
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_catalog_files (
				release_id, file_name, size_bytes, file_index, is_pars, subject,
				poster, posted_at, article_count, total_parts, observed_parts,
				match_confidence, match_status, updated_at
			) VALUES `+strings.Join(values, ","), args...); err != nil {
			return fmt.Errorf("insert uploader catalog files for %s: %w", releaseID, err)
		}
	}
	return nil
}

func ensureUploaderNewsgroupsTx(ctx context.Context, tx *sql.Tx, groups []string) ([]int64, error) {
	seen := make(map[string]struct{}, len(groups))
	ids := make([]int64, 0, len(groups))
	for _, raw := range groups {
		group := strings.TrimSpace(raw)
		if group == "" {
			continue
		}
		key := strings.ToLower(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO newsgroups (group_name)
			VALUES ($1)
			ON CONFLICT (group_name) DO UPDATE
			SET group_name = EXCLUDED.group_name
			RETURNING id`, group).Scan(&id); err != nil {
			return nil, fmt.Errorf("ensure uploader newsgroup %q: %w", group, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func uploaderCatalogFileName(raw string, index int, seen map[string]int) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = fmt.Sprintf("file-%d", index+1)
	}
	key := strings.ToLower(name)
	seen[key]++
	if seen[key] > 1 {
		return fmt.Sprintf("%s [%d]", name, seen[key])
	}
	return name
}

func uploaderPARFileCount(submission uploader.Submission) int {
	count := 0
	for _, file := range submission.Files {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(file.Name)), ".par2") {
			count++
		}
	}
	return count
}
