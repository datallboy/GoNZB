package pgindex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/segmentio/ksuid"
)

type ArticleHeader struct {
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	DateUTC       *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

// unassembled header row used by Milestone 6 assembly service.
type AssemblyCandidate struct {
	ID            int64
	ProviderID    int64
	NewsgroupID   int64
	ArticleNumber int64
	MessageID     string
	Subject       string
	Poster        string
	DateUTC       *time.Time
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

// binary upsert input for assembly service.
type BinaryRecord struct {
	ProviderID  int64
	NewsgroupID int64
	PosterID    int64
	ReleaseKey  string
	ReleaseName string
	BinaryKey   string
	BinaryName  string
	FileName    string
	TotalParts  int
	PostedAt    *time.Time
}

// binary part upsert input for assembly service.
type BinaryPartRecord struct {
	BinaryID        int64
	ArticleHeaderID int64
	MessageID       string
	PartNumber      int
	TotalParts      int
	SegmentBytes    int64
	FileName        string
}

// binary summary returned to release formation.
type BinarySummary struct {
	BinaryID           int64
	ProviderID         int64
	NewsgroupID        int64
	ReleaseKey         string
	ReleaseName        string
	BinaryKey          string
	BinaryName         string
	FileName           string
	Poster             string
	PostedAt           *time.Time
	TotalParts         int
	ObservedParts      int
	TotalBytes         int64
	FirstArticleNumber int64
	LastArticleNumber  int64
}

// grouped release candidate used by release formation.
type ReleaseCandidate struct {
	ProviderID  int64
	NewsgroupID int64
	ReleaseKey  string
	ReleaseName string
	PostedAt    *time.Time
	BinaryCount int
	TotalBytes  int64
}

// release catalog upsert input.
type ReleaseRecord struct {
	ReleaseID     string
	GUID          string
	ProviderID    int64
	ReleaseKey    string
	Title         string
	SearchTitle   string
	Category      string
	Poster        string
	SizeBytes     int64
	PostedAt      *time.Time
	FileCount     int
	ParFileCount  int
	CompletionPct float64
}

// article mapping row per release file.
type ReleaseFileArticleRecord struct {
	ArticleHeaderID int64
	PartNumber      int
}

// release file upsert input.
type ReleaseFileRecord struct {
	BinaryID  int64
	FileName  string
	SizeBytes int64
	FileIndex int
	IsPars    bool
	Subject   string
	Poster    string
	PostedAt  *time.Time
	Articles  []ReleaseFileArticleRecord
}

// read DTOs for PG-backed NZB materialization in Milestone 7.
type CatalogReleaseFile struct {
	ID        int64
	FileName  string
	Subject   string
	Poster    string
	PostedAt  *time.Time
	SizeBytes int64
	IsPars    bool
	FileIndex int
}

type CatalogArticleRef struct {
	MessageID  string
	Bytes      int64
	PartNumber int
}

// StableReleaseGUID returns a deterministic GUID for PG release rows.
// This keeps release identity stable across repeat formation passes.
func StableReleaseGUID(providerID int64, releaseKey string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", providerID, strings.TrimSpace(strings.ToLower(releaseKey)))))
	return hex.EncodeToString(sum[:])
}

// EnsureProvider creates/updates a provider row and returns its id.
func (s *Store) EnsureProvider(ctx context.Context, providerKey, displayName string) (int64, error) {
	providerKey = strings.TrimSpace(providerKey)
	displayName = strings.TrimSpace(displayName)

	if providerKey == "" {
		return 0, fmt.Errorf("provider key is required")
	}
	if displayName == "" {
		displayName = providerKey
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO usenet_providers (provider_key, display_name)
		VALUES ($1, $2)
		ON CONFLICT (provider_key) DO UPDATE
		SET display_name = EXCLUDED.display_name
		RETURNING id`,
		providerKey, displayName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure provider %q: %w", providerKey, err)
	}

	return id, nil
}

// EnsureNewsgroup creates/updates a newsgroup row and returns its id.
func (s *Store) EnsureNewsgroup(ctx context.Context, groupName string) (int64, error) {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return 0, fmt.Errorf("newsgroup name is required")
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO newsgroups (group_name)
		VALUES ($1)
		ON CONFLICT (group_name) DO UPDATE
		SET group_name = EXCLUDED.group_name
		RETURNING id`,
		groupName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure newsgroup %q: %w", groupName, err)
	}

	return id, nil
}

// StartScrapeRun creates a running scrape_run row.
func (s *Store) StartScrapeRun(ctx context.Context, providerID int64) (int64, error) {
	if providerID <= 0 {
		return 0, fmt.Errorf("provider id is required")
	}

	var runID int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO scrape_runs (provider_id, status, started_at)
		VALUES ($1, 'running', NOW())
		RETURNING id`,
		providerID,
	).Scan(&runID)
	if err != nil {
		return 0, fmt.Errorf("start scrape run: %w", err)
	}

	return runID, nil
}

// FinishScrapeRun closes a scrape_run row.
func (s *Store) FinishScrapeRun(ctx context.Context, runID int64, status, errorText string) error {
	if runID <= 0 {
		return fmt.Errorf("run id is required")
	}

	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "completed"
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE scrape_runs
		SET status = $2,
		    error_text = $3,
		    finished_at = NOW()
		WHERE id = $1`,
		runID, status, errorText,
	)
	if err != nil {
		return fmt.Errorf("finish scrape run %d: %w", runID, err)
	}

	return nil
}

// GetCheckpoint returns the last article number for provider+group.
// Returns 0 when no checkpoint exists.
func (s *Store) GetCheckpoint(ctx context.Context, providerID, newsgroupID int64) (int64, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}

	var last int64
	err := s.db.QueryRowContext(ctx, `
		SELECT last_article_number
		FROM scrape_checkpoints
		WHERE provider_id = $1 AND newsgroup_id = $2`,
		providerID, newsgroupID,
	).Scan(&last)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get checkpoint p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return last, nil
}

// UpsertCheckpoint stores/advances checkpoint for provider+group.
func (s *Store) UpsertCheckpoint(ctx context.Context, providerID, newsgroupID, lastArticleNumber int64) error {
	if providerID <= 0 || newsgroupID <= 0 {
		return fmt.Errorf("provider id and newsgroup id are required")
	}
	if lastArticleNumber < 0 {
		lastArticleNumber = 0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scrape_checkpoints (provider_id, newsgroup_id, last_article_number, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (provider_id, newsgroup_id) DO UPDATE
		SET last_article_number = GREATEST(scrape_checkpoints.last_article_number, EXCLUDED.last_article_number),
		    updated_at = NOW()`,
		providerID, newsgroupID, lastArticleNumber,
	)
	if err != nil {
		return fmt.Errorf("upsert checkpoint p=%d g=%d: %w", providerID, newsgroupID, err)
	}

	return nil
}

// InsertArticleHeaders inserts header rows with ingest constraints enforced by DB.
// Returns number of inserted rows (conflicts are ignored via DO NOTHING).
func (s *Store) InsertArticleHeaders(ctx context.Context, providerID, newsgroupID int64, headers []ArticleHeader) (int64, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}
	if len(headers) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO article_headers (
			provider_id,
			newsgroup_id,
			article_number,
			message_id,
			subject,
			poster,
			date_utc,
			bytes,
			lines,
			xref,
			raw_overview_json,
			scraped_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,NOW())
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("prepare article_headers insert: %w", err)
	}
	defer stmt.Close()

	var inserted int64
	for _, h := range headers {
		msgID := sanitizeUTF8(h.MessageID)
		if h.ArticleNumber <= 0 || msgID == "" {
			continue
		}

		subject := sanitizeUTF8(h.Subject)
		poster := sanitizeUTF8(h.Poster)
		xref := sanitizeUTF8(h.Xref)

		raw := "{}"
		if len(h.RawOverview) > 0 {
			cleanRaw := sanitizeStringMap(h.RawOverview)
			b, marshalErr := json.Marshal(cleanRaw)
			if marshalErr != nil {
				return inserted, fmt.Errorf("marshal raw_overview for article %d: %w", h.ArticleNumber, marshalErr)
			}
			raw = string(bytes.ToValidUTF8(b, []byte{}))
		}

		var date any
		if h.DateUTC != nil {
			date = h.DateUTC.UTC()
		} else {
			date = nil
		}

		res, execErr := stmt.ExecContext(
			ctx,
			providerID,
			newsgroupID,
			h.ArticleNumber,
			msgID,
			subject,
			poster,
			date,
			h.Bytes,
			h.Lines,
			xref,
			raw,
		)
		if execErr != nil {
			return inserted, fmt.Errorf("insert article header %d: %w", h.ArticleNumber, execErr)
		}

		affected, affErr := res.RowsAffected()
		if affErr == nil {
			inserted += affected
		}
	}

	if err := tx.Commit(); err != nil {
		return inserted, err
	}

	return inserted, nil
}

// CHANGED: list article headers not yet assembled into binary_parts.
func (s *Store) ListUnassembledArticleHeaders(ctx context.Context, limit int) ([]AssemblyCandidate, error) {
	if limit <= 0 {
		limit = 5000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.id,
			ah.provider_id,
			ah.newsgroup_id,
			ah.article_number,
			ah.message_id,
			ah.subject,
			ah.poster,
			ah.date_utc,
			ah.bytes,
			ah.lines,
			ah.xref,
			ah.raw_overview_json
		FROM article_headers ah
		LEFT JOIN binary_parts bp ON bp.article_header_id = ah.id
		WHERE bp.article_header_id IS NULL
		ORDER BY ah.newsgroup_id, ah.article_number
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unassembled article headers: %w", err)
	}
	defer rows.Close()

	out := make([]AssemblyCandidate, 0, limit)
	for rows.Next() {
		var item AssemblyCandidate
		var date sql.NullTime
		var raw string

		if err := rows.Scan(
			&item.ID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.ArticleNumber,
			&item.MessageID,
			&item.Subject,
			&item.Poster,
			&date,
			&item.Bytes,
			&item.Lines,
			&item.Xref,
			&raw,
		); err != nil {
			return nil, fmt.Errorf("scan unassembled article header: %w", err)
		}

		if date.Valid {
			t := date.Time.UTC()
			item.DateUTC = &t
		}

		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &item.RawOverview)
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unassembled article headers: %w", err)
	}

	return out, nil
}

// CHANGED: normalize posters into a dimension table.
func (s *Store) EnsurePoster(ctx context.Context, posterName string) (int64, error) {
	posterName = strings.TrimSpace(posterName)
	if posterName == "" {
		return 0, nil
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO posters (poster_name)
		VALUES ($1)
		ON CONFLICT (poster_name) DO UPDATE
		SET poster_name = EXCLUDED.poster_name
		RETURNING id`,
		posterName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure poster %q: %w", posterName, err)
	}

	return id, nil
}

// CHANGED: map article header -> poster id for later enrichment/debugging.
func (s *Store) LinkArticlePoster(ctx context.Context, articleHeaderID, posterID int64) error {
	if articleHeaderID <= 0 || posterID <= 0 {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO article_poster_map (article_header_id, poster_id)
		VALUES ($1, $2)
		ON CONFLICT (article_header_id) DO UPDATE
		SET poster_id = EXCLUDED.poster_id`,
		articleHeaderID, posterID,
	)
	if err != nil {
		return fmt.Errorf("link article %d to poster %d: %w", articleHeaderID, posterID, err)
	}

	return nil
}

// CHANGED: create/update a binary grouping row.
func (s *Store) UpsertBinary(ctx context.Context, in BinaryRecord) (int64, error) {
	if in.ProviderID <= 0 || in.NewsgroupID <= 0 {
		return 0, fmt.Errorf("provider id and newsgroup id are required")
	}

	in.ReleaseKey = strings.TrimSpace(in.ReleaseKey)
	in.BinaryKey = strings.TrimSpace(in.BinaryKey)
	if in.ReleaseKey == "" || in.BinaryKey == "" {
		return 0, fmt.Errorf("release key and binary key are required")
	}

	var postedAt any
	if in.PostedAt != nil {
		postedAt = in.PostedAt.UTC()
	}

	var posterID any
	if in.PosterID > 0 {
		posterID = in.PosterID
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO binaries (
			provider_id,
			newsgroup_id,
			poster_id,
			release_key,
			release_name,
			binary_key,
			binary_name,
			file_name,
			total_parts,
			posted_at,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
		ON CONFLICT (provider_id, newsgroup_id, binary_key) DO UPDATE
		SET poster_id = COALESCE(EXCLUDED.poster_id, binaries.poster_id),
		    release_key = EXCLUDED.release_key,
		    release_name = EXCLUDED.release_name,
		    binary_name = EXCLUDED.binary_name,
		    file_name = EXCLUDED.file_name,
		    total_parts = GREATEST(binaries.total_parts, EXCLUDED.total_parts),
		    posted_at = COALESCE(binaries.posted_at, EXCLUDED.posted_at),
		    updated_at = NOW()
		RETURNING id`,
		in.ProviderID,
		in.NewsgroupID,
		posterID,
		in.ReleaseKey,
		strings.TrimSpace(in.ReleaseName),
		in.BinaryKey,
		strings.TrimSpace(in.BinaryName),
		strings.TrimSpace(in.FileName),
		in.TotalParts,
		postedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert binary %q: %w", in.BinaryKey, err)
	}

	return id, nil
}

// CHANGED: add/update one binary part row.
func (s *Store) UpsertBinaryPart(ctx context.Context, in BinaryPartRecord) error {
	if in.BinaryID <= 0 || in.ArticleHeaderID <= 0 {
		return fmt.Errorf("binary id and article header id are required")
	}
	if in.PartNumber <= 0 {
		return fmt.Errorf("part number is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO binary_parts (
			binary_id,
			article_header_id,
			message_id,
			part_number,
			total_parts,
			segment_bytes,
			file_name,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (binary_id, part_number) DO UPDATE
		SET article_header_id = EXCLUDED.article_header_id,
		    message_id = EXCLUDED.message_id,
		    total_parts = GREATEST(binary_parts.total_parts, EXCLUDED.total_parts),
		    segment_bytes = EXCLUDED.segment_bytes,
		    file_name = EXCLUDED.file_name,
		    updated_at = NOW()`,
		in.BinaryID,
		in.ArticleHeaderID,
		strings.TrimSpace(in.MessageID),
		in.PartNumber,
		in.TotalParts,
		in.SegmentBytes,
		strings.TrimSpace(in.FileName),
	)
	if err != nil {
		return fmt.Errorf("upsert binary part binary=%d part=%d: %w", in.BinaryID, in.PartNumber, err)
	}

	return nil
}

// CHANGED: recompute binary aggregate stats after parts were inserted.
func (s *Store) RefreshBinaryStats(ctx context.Context, binaryID int64) error {
	if binaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE binaries b
		SET observed_parts = agg.observed_parts,
		    total_bytes = agg.total_bytes,
		    first_article_number = agg.first_article_number,
		    last_article_number = agg.last_article_number,
		    updated_at = NOW()
		FROM (
			SELECT
				bp.binary_id,
				COUNT(*)::INTEGER AS observed_parts,
				COALESCE(SUM(bp.segment_bytes), 0)::BIGINT AS total_bytes,
				COALESCE(MIN(ah.article_number), 0)::BIGINT AS first_article_number,
				COALESCE(MAX(ah.article_number), 0)::BIGINT AS last_article_number
			FROM binary_parts bp
			JOIN article_headers ah ON ah.id = bp.article_header_id
			WHERE bp.binary_id = $1
			GROUP BY bp.binary_id
		) agg
		WHERE b.id = agg.binary_id`, binaryID)
	if err != nil {
		return fmt.Errorf("refresh binary stats %d: %w", binaryID, err)
	}

	return nil
}

// CHANGED: return release groups whose binaries are new or changed since last formation.
func (s *Store) ListReleaseCandidates(ctx context.Context, limit int) ([]ReleaseCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.provider_id,
			b.newsgroup_id,
			b.release_key,
			MAX(b.release_name) AS release_name,
			MIN(b.posted_at) AS posted_at,
			COUNT(*)::INTEGER AS binary_count,
			COALESCE(SUM(b.total_bytes), 0)::BIGINT AS total_bytes
		FROM binaries b
		LEFT JOIN releases r
			ON r.provider_id = b.provider_id
			AND r.release_key = b.release_key
		GROUP BY b.provider_id, b.newsgroup_id, b.release_key, r.updated_at
		HAVING r.updated_at IS NULL OR MAX(b.updated_at) > r.updated_at
		ORDER BY MIN(b.posted_at) NULLS LAST, b.release_key
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list release candidates: %w", err)
	}
	defer rows.Close()

	out := make([]ReleaseCandidate, 0, limit)
	for rows.Next() {
		var item ReleaseCandidate
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.ProviderID,
			&item.NewsgroupID,
			&item.ReleaseKey,
			&item.ReleaseName,
			&postedAt,
			&item.BinaryCount,
			&item.TotalBytes,
		); err != nil {
			return nil, fmt.Errorf("scan release candidate: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release candidates: %w", err)
	}

	return out, nil
}

// CHANGED: fetch binaries that belong to one release candidate.
func (s *Store) ListBinariesForReleaseCandidate(ctx context.Context, providerID, newsgroupID int64, releaseKey string) ([]BinarySummary, error) {
	if providerID <= 0 || newsgroupID <= 0 {
		return nil, fmt.Errorf("provider id and newsgroup id are required")
	}
	releaseKey = strings.TrimSpace(releaseKey)
	if releaseKey == "" {
		return nil, fmt.Errorf("release key is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id,
			b.provider_id,
			b.newsgroup_id,
			b.release_key,
			b.release_name,
			b.binary_key,
			b.binary_name,
			b.file_name,
			COALESCE(p.poster_name, ''),
			b.posted_at,
			b.total_parts,
			b.observed_parts,
			b.total_bytes,
			b.first_article_number,
			b.last_article_number
		FROM binaries b
		LEFT JOIN posters p ON p.id = b.poster_id
		WHERE b.provider_id = $1
		  AND b.newsgroup_id = $2
		  AND b.release_key = $3
		ORDER BY b.file_name, b.first_article_number, b.id`,
		providerID, newsgroupID, releaseKey,
	)
	if err != nil {
		return nil, fmt.Errorf("list binaries for release candidate: %w", err)
	}
	defer rows.Close()

	out := make([]BinarySummary, 0, 32)
	for rows.Next() {
		var item BinarySummary
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.BinaryID,
			&item.ProviderID,
			&item.NewsgroupID,
			&item.ReleaseKey,
			&item.ReleaseName,
			&item.BinaryKey,
			&item.BinaryName,
			&item.FileName,
			&item.Poster,
			&postedAt,
			&item.TotalParts,
			&item.ObservedParts,
			&item.TotalBytes,
			&item.FirstArticleNumber,
			&item.LastArticleNumber,
		); err != nil {
			return nil, fmt.Errorf("scan binary summary: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary summaries: %w", err)
	}

	return out, nil
}

// CHANGED: fetch article ids/part numbers for one binary to build release_file_articles.
func (s *Store) ListBinaryPartArticles(ctx context.Context, binaryID int64) ([]ReleaseFileArticleRecord, error) {
	if binaryID <= 0 {
		return nil, fmt.Errorf("binary id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT article_header_id, part_number
		FROM binary_parts
		WHERE binary_id = $1
		ORDER BY part_number`, binaryID)
	if err != nil {
		return nil, fmt.Errorf("list binary part articles %d: %w", binaryID, err)
	}
	defer rows.Close()

	out := make([]ReleaseFileArticleRecord, 0, 64)
	for rows.Next() {
		var item ReleaseFileArticleRecord
		if err := rows.Scan(&item.ArticleHeaderID, &item.PartNumber); err != nil {
			return nil, fmt.Errorf("scan binary part article: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary part articles: %w", err)
	}

	return out, nil
}

// CHANGED: create/update a release row and keep its id stable.
func (s *Store) UpsertRelease(ctx context.Context, in ReleaseRecord) (string, error) {
	if in.ProviderID <= 0 {
		return "", fmt.Errorf("provider id is required")
	}
	in.ReleaseKey = strings.TrimSpace(in.ReleaseKey)
	if in.ReleaseKey == "" {
		return "", fmt.Errorf("release key is required")
	}
	if strings.TrimSpace(in.ReleaseID) == "" {
		in.ReleaseID = ksuid.New().String()
	}
	if strings.TrimSpace(in.GUID) == "" {
		in.GUID = StableReleaseGUID(in.ProviderID, in.ReleaseKey)
	}

	var postedAt any
	if in.PostedAt != nil {
		postedAt = in.PostedAt.UTC()
	}

	var releaseID string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO releases (
			release_id,
			guid,
			provider_id,
			release_key,
			title,
			search_title,
			category,
			poster,
			size_bytes,
			posted_at,
			file_count,
			par_file_count,
			completion_pct,
			source_kind,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'usenet_index',NOW())
		ON CONFLICT (provider_id, release_key) DO UPDATE
		SET guid = EXCLUDED.guid,
		    title = EXCLUDED.title,
		    search_title = EXCLUDED.search_title,
		    category = EXCLUDED.category,
		    poster = EXCLUDED.poster,
		    size_bytes = EXCLUDED.size_bytes,
		    posted_at = EXCLUDED.posted_at,
		    file_count = EXCLUDED.file_count,
		    par_file_count = EXCLUDED.par_file_count,
		    completion_pct = EXCLUDED.completion_pct,
		    updated_at = NOW()
		RETURNING release_id`,
		in.ReleaseID,
		in.GUID,
		in.ProviderID,
		in.ReleaseKey,
		strings.TrimSpace(in.Title),
		strings.TrimSpace(in.SearchTitle),
		strings.TrimSpace(in.Category),
		strings.TrimSpace(in.Poster),
		in.SizeBytes,
		postedAt,
		in.FileCount,
		in.ParFileCount,
		in.CompletionPct,
	).Scan(&releaseID)
	if err != nil {
		return "", fmt.Errorf("upsert release %q: %w", in.ReleaseKey, err)
	}

	return releaseID, nil
}

// CHANGED: replace release_files and release_file_articles atomically for one release.
func (s *Store) ReplaceReleaseFiles(ctx context.Context, releaseID string, files []ReleaseFileRecord) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_file_articles
		WHERE release_file_id IN (
			SELECT id FROM release_files WHERE release_id = $1
		)`, releaseID); err != nil {
		return fmt.Errorf("delete release_file_articles for %s: %w", releaseID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_files
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete release_files for %s: %w", releaseID, err)
	}

	for _, f := range files {
		var postedAt any
		if f.PostedAt != nil {
			postedAt = f.PostedAt.UTC()
		}

		var releaseFileID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO release_files (
				release_id,
				binary_id,
				file_name,
				size_bytes,
				file_index,
				is_pars,
				subject,
				poster,
				posted_at,
				updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
			RETURNING id`,
			releaseID,
			nullIfZero(f.BinaryID),
			strings.TrimSpace(f.FileName),
			f.SizeBytes,
			f.FileIndex,
			f.IsPars,
			strings.TrimSpace(f.Subject),
			strings.TrimSpace(f.Poster),
			postedAt,
		).Scan(&releaseFileID); err != nil {
			return fmt.Errorf("insert release file %q for %s: %w", f.FileName, releaseID, err)
		}

		for _, article := range f.Articles {
			if article.ArticleHeaderID <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO release_file_articles (
					release_file_id,
					article_header_id,
					part_number
				)
				VALUES ($1,$2,$3)`,
				releaseFileID,
				article.ArticleHeaderID,
				article.PartNumber,
			); err != nil {
				return fmt.Errorf("insert release file article file=%d article=%d: %w", releaseFileID, article.ArticleHeaderID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// CHANGED: replace release_newsgroups atomically for one release.
func (s *Store) ReplaceReleaseNewsgroups(ctx context.Context, releaseID string, newsgroupIDs []int64) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM release_newsgroups
		WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("delete release_newsgroups for %s: %w", releaseID, err)
	}

	for _, newsgroupID := range newsgroupIDs {
		if newsgroupID <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_newsgroups (release_id, newsgroup_id)
			VALUES ($1, $2)`,
			releaseID, newsgroupID,
		); err != nil {
			return fmt.Errorf("insert release_newsgroup release=%s group=%d: %w", releaseID, newsgroupID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// CHANGED: seed/update PG nzb cache metadata row for a release.
func (s *Store) UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return fmt.Errorf("release id is required")
	}
	generationStatus = strings.TrimSpace(strings.ToLower(generationStatus))
	if generationStatus == "" {
		generationStatus = "pending"
	}

	var generatedAt any
	if generationStatus == "ready" {
		generatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nzb_cache (
			release_id,
			generation_status,
			nzb_hash_sha256,
			generated_at,
			last_error,
			updated_at
		)
		VALUES ($1,$2,$3,$4,$5,NOW())
		ON CONFLICT (release_id) DO UPDATE
		SET generation_status = EXCLUDED.generation_status,
		    nzb_hash_sha256 = EXCLUDED.nzb_hash_sha256,
		    generated_at = COALESCE(EXCLUDED.generated_at, nzb_cache.generated_at),
		    last_error = EXCLUDED.last_error,
		    updated_at = NOW()`,
		releaseID,
		generationStatus,
		strings.TrimSpace(hashSHA256),
		generatedAt,
		strings.TrimSpace(lastError),
	)
	if err != nil {
		return fmt.Errorf("upsert nzb_cache for %s: %w", releaseID, err)
	}

	return nil
}

// CHANGED: PG release catalog read by id for later resolver work.
func (s *Store) GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			r.release_id,
			r.title,
			r.guid,
			r.source_kind,
			r.size_bytes,
			r.posted_at,
			r.category,
			r.poster,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.release_id = $1`, releaseID)

	rel, err := scanCatalogRelease(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get catalog release %s: %w", releaseID, err)
	}

	return rel, nil
}

// CHANGED: PG release catalog search for later aggregator/resolver integration.
func (s *Store) SearchCatalogReleases(ctx context.Context, query string, limit int) ([]*domain.Release, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []*domain.Release{}, nil
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.release_id,
			r.title,
			r.guid,
			r.source_kind,
			r.size_bytes,
			r.posted_at,
			r.category,
			r.poster,
			COALESCE(n.generation_status, 'pending')
		FROM releases r
		LEFT JOIN nzb_cache n ON n.release_id = r.release_id
		WHERE r.search_title ILIKE '%' || $1 || '%'
		ORDER BY r.posted_at DESC NULLS LAST, r.title
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search catalog releases %q: %w", query, err)
	}
	defer rows.Close()

	out := make([]*domain.Release, 0, limit)
	for rows.Next() {
		rel, err := scanCatalogRelease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog search result: %w", err)
		}
		out = append(out, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog search results: %w", err)
	}

	return out, nil
}

// CHANGED: list release files for one formed PG release.
func (s *Store) ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]CatalogReleaseFile, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			file_name,
			subject,
			poster,
			posted_at,
			size_bytes,
			is_pars,
			file_index
		FROM release_files
		WHERE release_id = $1
		ORDER BY file_index, id`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release files %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]CatalogReleaseFile, 0, 32)
	for rows.Next() {
		var item CatalogReleaseFile
		var postedAt sql.NullTime

		if err := rows.Scan(
			&item.ID,
			&item.FileName,
			&item.Subject,
			&item.Poster,
			&postedAt,
			&item.SizeBytes,
			&item.IsPars,
			&item.FileIndex,
		); err != nil {
			return nil, fmt.Errorf("scan catalog release file: %w", err)
		}

		if postedAt.Valid {
			t := postedAt.Time.UTC()
			item.PostedAt = &t
		}

		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release files: %w", err)
	}

	return out, nil
}

// CHANGED: list article refs for one release_file row.
func (s *Store) ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]CatalogArticleRef, error) {
	if releaseFileID <= 0 {
		return nil, fmt.Errorf("release file id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ah.message_id,
			ah.bytes,
			rfa.part_number
		FROM release_file_articles rfa
		JOIN article_headers ah ON ah.id = rfa.article_header_id
		WHERE rfa.release_file_id = $1
		ORDER BY rfa.part_number`, releaseFileID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release file articles %d: %w", releaseFileID, err)
	}
	defer rows.Close()

	out := make([]CatalogArticleRef, 0, 128)
	for rows.Next() {
		var item CatalogArticleRef
		if err := rows.Scan(&item.MessageID, &item.Bytes, &item.PartNumber); err != nil {
			return nil, fmt.Errorf("scan catalog article ref: %w", err)
		}
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog article refs: %w", err)
	}

	return out, nil
}

// CHANGED: list newsgroups attached to a formed release.
func (s *Store) ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ng.group_name
		FROM release_newsgroups rng
		JOIN newsgroups ng ON ng.id = rng.newsgroup_id
		WHERE rng.release_id = $1
		ORDER BY ng.group_name`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release newsgroups %s: %w", releaseID, err)
	}
	defer rows.Close()

	out := make([]string, 0, 8)
	for rows.Next() {
		var groupName string
		if err := rows.Scan(&groupName); err != nil {
			return nil, fmt.Errorf("scan catalog release newsgroup: %w", err)
		}
		out = append(out, strings.TrimSpace(groupName))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog release newsgroups: %w", err)
	}

	return out, nil
}

type releaseScanner interface {
	Scan(dest ...any) error
}

func scanCatalogRelease(scanner releaseScanner) (*domain.Release, error) {
	var rel domain.Release
	var source string
	var postedAt sql.NullTime
	var generationStatus string

	if err := scanner.Scan(
		&rel.ID,
		&rel.Title,
		&rel.GUID,
		&source,
		&rel.Size,
		&postedAt,
		&rel.Category,
		&rel.Poster,
		&generationStatus,
	); err != nil {
		return nil, err
	}

	if postedAt.Valid {
		rel.PublishDate = postedAt.Time.UTC()
	}
	rel.Source = source
	rel.CachePresent = strings.EqualFold(generationStatus, "ready")

	return &rel, nil
}

func nullIfZero(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

// PostgreSQL text/jsonb columns require valid UTF-8.
// Use byte-level repair so raw NNTP bytes cannot leak through.
func sanitizeUTF8(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.ValidString(s) {
		return s
	}
	return string(bytes.ToValidUTF8([]byte(s), []byte{}))
}

func sanitizeStringMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		cleanKey := sanitizeUTF8(k)

		switch tv := v.(type) {
		case string:
			out[cleanKey] = sanitizeUTF8(tv)
		case map[string]any:
			out[cleanKey] = sanitizeStringMap(tv)
		case []any:
			out[cleanKey] = sanitizeAnySlice(tv)
		default:
			out[cleanKey] = v
		}
	}

	return out
}

func sanitizeAnySlice(in []any) []any {
	if len(in) == 0 {
		return []any{}
	}

	out := make([]any, 0, len(in))
	for _, v := range in {
		switch tv := v.(type) {
		case string:
			out = append(out, sanitizeUTF8(tv))
		case map[string]any:
			out = append(out, sanitizeStringMap(tv))
		case []any:
			out = append(out, sanitizeAnySlice(tv))
		default:
			out = append(out, v)
		}
	}

	return out
}
