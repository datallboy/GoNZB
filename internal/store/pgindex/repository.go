package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
		msgID := strings.TrimSpace(h.MessageID)
		if h.ArticleNumber <= 0 || msgID == "" {
			continue
		}

		subject := strings.TrimSpace(h.Subject)
		poster := strings.TrimSpace(h.Poster)
		xref := strings.TrimSpace(h.Xref)

		raw := "{}"
		if len(h.RawOverview) > 0 {
			b, marshalErr := json.Marshal(h.RawOverview)
			if marshalErr != nil {
				return inserted, fmt.Errorf("marshal raw_overview for article %d: %w", h.ArticleNumber, marshalErr)
			}
			raw = string(b)
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
