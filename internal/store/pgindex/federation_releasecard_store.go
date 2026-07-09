package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
)

type FederatedReleaseCardSearchParams struct {
	Query  string
	IMDBID string
	TVDBID int64
	Pools  []string
	Limit  int
}

type FederatedReleaseCardSummary struct {
	ReleaseID          string
	ManifestID         string
	Title              string
	NormalizedTitle    string
	NewznabCategories  []int
	SizeBytes          int64
	PostedAt           *time.Time
	BestScore          float64
	AvailabilityScore  float64
	TrustScore         float64
	Resolvable         bool
	SourceEventID      string
	PoolID             string
	SourceNodeID       string
	ManifestConfidence float64
}

func (s *Store) ListGoNZBNetLocalReleaseCandidates(ctx context.Context, limit int) ([]releasecard.LocalRelease, error) {
	if limit <= 0 {
		limit = 50
	}
	summaries, _, err := s.ListPublicIndexerReleases(ctx, PublicIndexerReleaseListParams{
		Limit:       limit,
		Sort:        "posted_at_desc",
		ReadyPolicy: DefaultReleaseReadyPolicy(),
	})
	if err != nil {
		return nil, err
	}

	out := make([]releasecard.LocalRelease, 0, len(summaries))
	for _, summary := range summaries {
		item, err := s.GetGoNZBNetLocalRelease(ctx, summary.ReleaseID)
		if err != nil {
			return nil, err
		}
		if item.LocalReleaseID == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) GetGoNZBNetLocalRelease(ctx context.Context, releaseID string) (releasecard.LocalRelease, error) {
	detail, err := s.GetPublicIndexerReleaseDetailWithPolicy(ctx, releaseID, DefaultReleaseReadyPolicy())
	if err != nil {
		return releasecard.LocalRelease{}, err
	}
	if detail == nil {
		return releasecard.LocalRelease{}, nil
	}

	groups, err := s.ListCatalogReleaseNewsgroups(ctx, releaseID)
	if err != nil {
		return releasecard.LocalRelease{}, err
	}
	files, err := s.ListCatalogReleaseFiles(ctx, releaseID)
	if err != nil {
		return releasecard.LocalRelease{}, err
	}

	localFiles := make([]releasecard.LocalFile, 0, len(files))
	for _, file := range files {
		articles, err := s.ListCatalogReleaseFileArticles(ctx, file.ID)
		if err != nil {
			return releasecard.LocalRelease{}, err
		}
		segments := make([]releasecard.LocalSegment, 0, len(articles))
		for _, article := range articles {
			segments = append(segments, releasecard.LocalSegment{
				Number:    article.PartNumber,
				Bytes:     article.Bytes,
				MessageID: article.MessageID,
			})
		}
		localFiles = append(localFiles, releasecard.LocalFile{
			ID:           file.ID,
			Name:         file.FileName,
			Subject:      file.Subject,
			Poster:       file.Poster,
			PostedAt:     file.PostedAt,
			SizeBytes:    file.SizeBytes,
			FileIndex:    file.FileIndex,
			IsPars:       file.IsPars,
			ArticleCount: len(segments),
			TotalParts:   len(segments),
			Segments:     segments,
		})
	}

	release := detail.Release
	return releasecard.LocalRelease{
		LocalReleaseID:    release.ReleaseID,
		GUID:              release.GUID,
		Title:             release.Title,
		Category:          release.Category,
		CategoryID:        release.CategoryID,
		Classification:    release.Classification,
		SizeBytes:         release.SizeBytes,
		PostedAt:          release.PostedAt,
		AddedAt:           release.AddedAt,
		FileCount:         release.FileCount,
		CompletionPct:     release.CompletionPct,
		Groups:            groups,
		Files:             localFiles,
		HasPAR2:           release.HasPAR2,
		HasNFO:            release.HasNFO,
		PasswordState:     release.PasswordState,
		Availability:      release.AvailabilityScore,
		TMDBID:            release.TMDBID,
		TVDBID:            release.TVDBID,
		IMDBID:            release.IMDBID,
		ExternalMedia:     release.ExternalMediaType,
		ExternalTitle:     release.ExternalTitle,
		ExternalYear:      release.ExternalYear,
		RuntimeSeconds:    detail.Media.RuntimeSeconds,
		PrimaryResolution: detail.Media.PrimaryResolution,
		PrimaryVideoCodec: detail.Media.PrimaryVideoCodec,
		PrimaryAudioCodec: detail.Media.PrimaryAudioCodec,
	}, nil
}

func (s *Store) UpsertFederatedReleaseCardProjection(ctx context.Context, projection releasecard.Projection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	card := projection.Card
	if strings.TrimSpace(card.ReleaseID) == "" {
		return fmt.Errorf("release_id is required")
	}
	if strings.TrimSpace(projection.EventID) == "" {
		return fmt.Errorf("source event_id is required")
	}
	if strings.TrimSpace(projection.SourceNodeID) == "" {
		return fmt.Errorf("source_node_id is required")
	}
	poolID := strings.TrimSpace(projection.PoolID)
	if poolID == "" {
		poolID = "pool.local"
	}

	bodyJSON, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("marshal release card body: %w", err)
	}
	categoryJSON, err := json.Marshal(card.Category)
	if err != nil {
		return fmt.Errorf("marshal release card categories: %w", err)
	}
	newznabJSON, err := json.Marshal(card.NewznabCategories)
	if err != nil {
		return fmt.Errorf("marshal release card newznab categories: %w", err)
	}
	groupsJSON, err := json.Marshal(card.Groups)
	if err != nil {
		return fmt.Errorf("marshal release card groups: %w", err)
	}
	mediaJSON, err := json.Marshal(card.Media)
	if err != nil {
		return fmt.Errorf("marshal release card media: %w", err)
	}
	qualityJSON, err := json.Marshal(card.Quality)
	if err != nil {
		return fmt.Errorf("marshal release card quality: %w", err)
	}
	flagsJSON, err := json.Marshal(card.Flags)
	if err != nil {
		return fmt.Errorf("marshal release card flags: %w", err)
	}
	resolutionJSON, err := json.Marshal(card.Resolution)
	if err != nil {
		return fmt.Errorf("marshal release card resolution: %w", err)
	}

	postedAt := parseOptionalRFC3339(card.PostedAt)
	expiresAt := parseOptionalRFC3339(card.ExpiresAt)
	resolvable := strings.TrimSpace(card.ManifestID) != ""
	availabilityScore := card.Source.Confidence
	manifestConfidenceScore := 0.0
	if resolvable {
		manifestConfidenceScore = 1.0
	}
	bestScore := availabilityScore
	if manifestConfidenceScore > bestScore {
		bestScore = manifestConfidenceScore
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO federated_release_cards (
			release_id, manifest_id, title, normalized_title, category_json,
			newznab_categories, size_bytes, posted_at, groups_json, file_count,
			segment_count, poster_hash, subject_fingerprint, file_fingerprint,
			media_json, quality_json, flags_json, resolution_json, body_json,
			best_score, availability_score, manifest_confidence_score, trust_score,
			resolvable, status, source_event_id, expires_at, updated_at
		)
		VALUES (
			$1, NULLIF($2, ''), $3, $4, $5::jsonb,
			$6::jsonb, $7, $8, $9::jsonb, $10,
			$11, NULLIF($12, ''), $13, $14,
			$15::jsonb, $16::jsonb, $17::jsonb, $18::jsonb, $19::jsonb,
			$20, $21, $22, $23,
			$24, $25, $26, $27, NOW()
		)
		ON CONFLICT (release_id) DO UPDATE SET
			manifest_id = EXCLUDED.manifest_id,
			title = EXCLUDED.title,
			normalized_title = EXCLUDED.normalized_title,
			category_json = EXCLUDED.category_json,
			newznab_categories = EXCLUDED.newznab_categories,
			size_bytes = EXCLUDED.size_bytes,
			posted_at = EXCLUDED.posted_at,
			groups_json = EXCLUDED.groups_json,
			file_count = EXCLUDED.file_count,
			segment_count = EXCLUDED.segment_count,
			poster_hash = EXCLUDED.poster_hash,
			subject_fingerprint = EXCLUDED.subject_fingerprint,
			file_fingerprint = EXCLUDED.file_fingerprint,
			media_json = EXCLUDED.media_json,
			quality_json = EXCLUDED.quality_json,
			flags_json = EXCLUDED.flags_json,
			resolution_json = EXCLUDED.resolution_json,
			body_json = EXCLUDED.body_json,
			best_score = EXCLUDED.best_score,
			availability_score = EXCLUDED.availability_score,
			manifest_confidence_score = EXCLUDED.manifest_confidence_score,
			trust_score = EXCLUDED.trust_score,
			resolvable = EXCLUDED.resolvable,
			status = EXCLUDED.status,
			source_event_id = EXCLUDED.source_event_id,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()`,
		card.ReleaseID,
		card.ManifestID,
		card.Title,
		card.NormalizedTitle,
		string(categoryJSON),
		string(newznabJSON),
		card.SizeBytes,
		postedAt,
		string(groupsJSON),
		card.FileCount,
		card.SegmentCount,
		card.PosterHash,
		card.SubjectFingerprint,
		card.FileFingerprint,
		string(mediaJSON),
		string(qualityJSON),
		string(flagsJSON),
		string(resolutionJSON),
		string(bodyJSON),
		bestScore,
		availabilityScore,
		manifestConfidenceScore,
		1.0,
		resolvable,
		"accepted",
		projection.EventID,
		expiresAt,
	); err != nil {
		return fmt.Errorf("upsert federated release card: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO federated_release_sources (
			release_id, manifest_id, source_node_id, source_event_id, pool_id,
			trust_score, availability_score, manifest_confidence_score, resolvable,
			last_seen_at
		)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (release_id, source_node_id, pool_id) DO UPDATE SET
			manifest_id = EXCLUDED.manifest_id,
			source_event_id = EXCLUDED.source_event_id,
			trust_score = EXCLUDED.trust_score,
			availability_score = EXCLUDED.availability_score,
			manifest_confidence_score = EXCLUDED.manifest_confidence_score,
			resolvable = EXCLUDED.resolvable,
			last_seen_at = NOW()`,
		card.ReleaseID,
		card.ManifestID,
		projection.SourceNodeID,
		projection.EventID,
		poolID,
		1.0,
		availabilityScore,
		manifestConfidenceScore,
		resolvable,
	); err != nil {
		return fmt.Errorf("upsert federated release source: %w", err)
	}

	if strings.TrimSpace(card.ManifestID) != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO federated_manifest_sources (
				manifest_id, release_id, source_node_id, pool_id, advertised,
				trust_score, updated_at
			)
			VALUES ($1, $2, $3, $4, TRUE, $5, NOW())
			ON CONFLICT (manifest_id, source_node_id, pool_id) DO UPDATE SET
				release_id = EXCLUDED.release_id,
				advertised = TRUE,
				trust_score = EXCLUDED.trust_score,
				updated_at = NOW()`,
			card.ManifestID,
			card.ReleaseID,
			projection.SourceNodeID,
			poolID,
			1.0,
		); err != nil {
			return fmt.Errorf("upsert federated manifest source: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) ListFederationSearchPoolsForPrincipal(ctx context.Context, userID string, roleIDs []string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	roleIDsJSON, err := json.Marshal(normalizeStrings(roleIDs))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT pool_id
		FROM (
			SELECT pool_id
			FROM user_federation_pool_access
			WHERE user_id = NULLIF($1, '')
			  AND can_search = TRUE
			UNION
			SELECT r.pool_id
			FROM role_federation_pool_access r
			WHERE r.can_search = TRUE
			  AND EXISTS (
			    SELECT 1
			    FROM jsonb_array_elements_text($2::jsonb) role_ids(role_id)
			    WHERE role_ids.role_id = r.role_id
			  )
		) pools
		ORDER BY pool_id`, strings.TrimSpace(userID), string(roleIDsJSON))
	if err != nil {
		return nil, fmt.Errorf("list federation search pools: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0, 4)
	for rows.Next() {
		var poolID string
		if err := rows.Scan(&poolID); err != nil {
			return nil, fmt.Errorf("scan federation search pool: %w", err)
		}
		out = append(out, poolID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate federation search pools: %w", err)
	}
	return out, nil
}

func (s *Store) SearchFederatedReleaseCards(ctx context.Context, params FederatedReleaseCardSearchParams) ([]FederatedReleaseCardSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	pools := normalizeStrings(params.Pools)
	if len(pools) == 0 {
		return []FederatedReleaseCardSummary{}, nil
	}
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	poolsJSON, err := json.Marshal(pools)
	if err != nil {
		return nil, err
	}

	clauses := []string{
		"c.status = 'accepted'",
		"(c.expires_at IS NULL OR c.expires_at > NOW())",
		"s.trust_score > 0",
		`EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text($1::jsonb) pools(pool_id)
			WHERE pools.pool_id = s.pool_id
		)`,
	}
	args := []any{string(poolsJSON)}
	arg := 2
	if query := strings.TrimSpace(params.Query); query != "" {
		clauses = append(clauses, fmt.Sprintf("(c.normalized_title ILIKE $%d OR c.title ILIKE $%d)", arg, arg))
		args = append(args, "%"+query+"%")
		arg++
	}
	if imdbID := strings.TrimSpace(params.IMDBID); imdbID != "" {
		clauses = append(clauses, fmt.Sprintf("c.media_json->>'imdb_id' = $%d", arg))
		args = append(args, imdbID)
		arg++
	}
	if params.TVDBID > 0 {
		clauses = append(clauses, fmt.Sprintf("(c.media_json->>'tvdb_id')::bigint = $%d", arg))
		args = append(args, params.TVDBID)
		arg++
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
		WITH ranked AS (
			SELECT
				c.release_id,
				COALESCE(c.manifest_id, '') AS manifest_id,
				c.title,
				c.normalized_title,
				c.newznab_categories,
				c.size_bytes,
				c.posted_at,
				c.best_score,
				c.availability_score,
				c.trust_score,
				c.resolvable,
				COALESCE(c.source_event_id, '') AS source_event_id,
				s.pool_id,
				s.source_node_id,
				s.manifest_confidence_score,
				ROW_NUMBER() OVER (
					PARTITION BY c.release_id
					ORDER BY s.trust_score DESC, s.availability_score DESC, c.posted_at DESC NULLS LAST
				) AS source_rank
			FROM federated_release_cards c
			JOIN federated_release_sources s ON s.release_id = c.release_id
			WHERE %s
		)
		SELECT
			release_id,
			manifest_id,
			title,
			normalized_title,
			newznab_categories,
			size_bytes,
			posted_at,
			best_score,
			availability_score,
			trust_score,
			resolvable,
			source_event_id,
			pool_id,
			source_node_id,
			manifest_confidence_score
		FROM ranked
		WHERE source_rank = 1
		ORDER BY posted_at DESC NULLS LAST, best_score DESC, release_id ASC
		LIMIT $%d`, strings.Join(clauses, "\n  AND "), arg)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search federated release cards: %w", err)
	}
	defer rows.Close()

	out := make([]FederatedReleaseCardSummary, 0, limit)
	for rows.Next() {
		var (
			item       FederatedReleaseCardSummary
			postedAt   sql.NullTime
			categories []byte
		)
		if err := rows.Scan(
			&item.ReleaseID,
			&item.ManifestID,
			&item.Title,
			&item.NormalizedTitle,
			&categories,
			&item.SizeBytes,
			&postedAt,
			&item.BestScore,
			&item.AvailabilityScore,
			&item.TrustScore,
			&item.Resolvable,
			&item.SourceEventID,
			&item.PoolID,
			&item.SourceNodeID,
			&item.ManifestConfidence,
		); err != nil {
			return nil, fmt.Errorf("scan federated release card: %w", err)
		}
		if postedAt.Valid {
			value := postedAt.Time.UTC()
			item.PostedAt = &value
		}
		_ = json.Unmarshal(categories, &item.NewznabCategories)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate federated release cards: %w", err)
	}
	return out, nil
}

func parseOptionalRFC3339(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
