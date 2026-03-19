package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
	_ "modernc.org/sqlite"
)

const usenetIndexerModuleName = "usenet_indexer"

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create settings db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite settings db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect sqlite settings db: %w", err)
	}

	s := &Store{db: db}
	if err := s.RunMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("could not migrate settings db: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// load latest runtime settings revision and overlay it onto bootstrap config.
func (s *Store) LoadEffectiveSettings(ctx context.Context, base *config.Config) (*config.Config, error) {
	runtime, err := s.GetRuntimeSettings(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("load runtime settings: %w", err)
	}

	effective := ApplyToConfig(base, runtime)
	if effective == nil {
		return nil, fmt.Errorf("effective config is nil")
	}

	// validate the overlaid config, not just bootstrap config.
	if err := effective.ValidateEffective(); err != nil {
		return nil, fmt.Errorf("validate effective config: %w", err)
	}

	return effective, nil
}

// return the latest persisted runtime settings revision.
// If no row exists yet, fall back to bootstrap-derived defaults.
func (s *Store) GetRuntimeSettings(ctx context.Context, base ...*config.Config) (*RuntimeSettings, error) {
	runtime, hasStructuredState, err := s.readStructuredSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("read structured settings: %w", err)
	}

	if hasStructuredState {
		revisionID, revErr := s.latestRevisionID(ctx)
		if revErr == nil {
			runtime.Revision = revisionID
		}
		return runtime, nil
	}

	legacyRevision, err := s.readLatestRevisionSnapshot(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("read latest settings revision: %w", err)
	}
	if err == nil && legacyRevision != nil {
		return legacyRevision, nil
	}

	if len(base) > 0 && base[0] != nil {
		out := FromConfig(base[0])
		out.Revision = 0
		return out, nil
	}

	return &RuntimeSettings{}, nil
}

// patch and persist the latest runtime settings state as a new atomic revision.
func (s *Store) UpdateSettings(ctx context.Context, patch any) error {
	var next *RuntimeSettings

	switch v := patch.(type) {
	case *RuntimeSettings:
		next = CloneRuntimeSettings(v)
	case *RuntimeSettingsPatch:
		current, err := s.GetRuntimeSettings(ctx)
		if err != nil {
			return fmt.Errorf("load current runtime settings: %w", err)
		}
		next = ApplyPatch(current, v)
	default:
		return fmt.Errorf("settings update must be *settings.RuntimeSettings or *settings.RuntimeSettingsPatch")
	}

	if next == nil {
		next = &RuntimeSettings{}
	}
	next.Revision = 0

	snapshot, err := encodeRuntimeSettings(next)
	if err != nil {
		return fmt.Errorf("marshal settings snapshot: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := s.writeServers(ctx, tx, next.Servers); err != nil {
		return fmt.Errorf("write settings_nntp_servers: %w", err)
	}
	if err := s.writeIndexers(ctx, tx, next.Indexers); err != nil {
		return fmt.Errorf("write settings_indexers: %w", err)
	}
	if err := s.writeDownload(ctx, tx, next.Download); err != nil {
		return fmt.Errorf("write settings_download: %w", err)
	}
	if err := s.writeUsenetIndexerOptions(ctx, tx, next.Indexing); err != nil {
		return fmt.Errorf("write settings_module_options: %w", err)
	}
	if err := s.writeArrIntegrations(ctx, tx, next.ArrIntegrations); err != nil {
		return fmt.Errorf("write settings_arr_integrations: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings_revision (payload_json)
		VALUES (?)`, string(snapshot)); err != nil {
		return fmt.Errorf("insert settings revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings tx: %w", err)
	}

	return nil
}

// poll settings_revision for new ids and publish a lightweight signal.
// This is intentionally simple for the first live-reload slice.
func (s *Store) WatchSettingsChanges(ctx context.Context) (<-chan struct{}, error) {
	lastSeen, err := s.latestRevisionID(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("read initial settings revision: %w", err)
	}
	if err == sql.ErrNoRows {
		lastSeen = 0
	}

	ch := make(chan struct{}, 1)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, currentErr := s.latestRevisionID(ctx)
				if currentErr != nil {
					continue
				}
				if current > lastSeen {
					lastSeen = current
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	return ch, nil
}

func (s *Store) latestRevisionID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM settings_revision
		ORDER BY id DESC
		LIMIT 1`).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) readLatestRevisionSnapshot(ctx context.Context) (*RuntimeSettings, error) {
	var (
		id      int64
		payload string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT id, payload_json
		FROM settings_revision
		ORDER BY id DESC
		LIMIT 1`).Scan(&id, &payload)
	if err != nil {
		return nil, err
	}

	if payload == "" {
		payload = "{}"
	}

	var runtime RuntimeSettings
	if err := json.Unmarshal([]byte(payload), &runtime); err != nil {
		return nil, fmt.Errorf("unmarshal settings revision %d: %w", id, err)
	}
	runtime.Revision = id
	return &runtime, nil
}

func (s *Store) readStructuredSettings(ctx context.Context) (*RuntimeSettings, bool, error) {
	out := &RuntimeSettings{
		Servers:         make([]ServerRuntimeSettings, 0),
		Indexers:        make([]IndexerRuntimeSettings, 0),
		ArrIntegrations: make([]ArrIntegrationRuntimeSettings, 0),
	}

	hasState := false

	serverRows, err := s.db.QueryContext(ctx, `
		SELECT id, host, port, username, password_ciphertext, tls, max_connections, priority
		FROM settings_nntp_servers
		ORDER BY priority, id`)
	if err != nil {
		return nil, false, err
	}
	defer serverRows.Close()

	for serverRows.Next() {
		hasState = true

		var item ServerRuntimeSettings
		if err := serverRows.Scan(
			&item.ID,
			&item.Host,
			&item.Port,
			&item.Username,
			&item.Password,
			&item.TLS,
			&item.MaxConnection,
			&item.Priority,
		); err != nil {
			return nil, false, err
		}
		out.Servers = append(out.Servers, item)
	}
	if err := serverRows.Err(); err != nil {
		return nil, false, err
	}

	indexerRows, err := s.db.QueryContext(ctx, `
		SELECT id, base_url, api_path, api_key_ciphertext, redirect
		FROM settings_indexers
		ORDER BY id`)
	if err != nil {
		return nil, false, err
	}
	defer indexerRows.Close()

	for indexerRows.Next() {
		hasState = true

		var item IndexerRuntimeSettings
		if err := indexerRows.Scan(
			&item.ID,
			&item.BaseURL,
			&item.APIPath,
			&item.APIKey,
			&item.Redirect,
		); err != nil {
			return nil, false, err
		}
		out.Indexers = append(out.Indexers, item)
	}
	if err := indexerRows.Err(); err != nil {
		return nil, false, err
	}

	arrRows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, enabled, base_url, api_key_ciphertext, client_name, category
		FROM settings_arr_integrations
		ORDER BY kind, id`)
	if err != nil {
		return nil, false, err
	}
	defer arrRows.Close()

	for arrRows.Next() {
		hasState = true
		var item ArrIntegrationRuntimeSettings
		if err := arrRows.Scan(&item.ID, &item.Kind, &item.Enabled, &item.BaseURL, &item.APIKey, &item.ClientName, &item.Category); err != nil {
			return nil, false, err
		}
		out.ArrIntegrations = append(out.ArrIntegrations, item)
	}
	if err := arrRows.Err(); err != nil {
		return nil, false, err
	}

	var (
		outDir      string
		completed   string
		cleanupJSON string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT out_dir, completed_dir, cleanup_extensions_json
		FROM settings_download
		WHERE singleton_id = 1`).Scan(&outDir, &completed, &cleanupJSON)
	if err != nil && err != sql.ErrNoRows {
		return nil, false, err
	}
	if err == nil {
		hasState = true

		var cleanup []string
		if cleanupJSON == "" {
			cleanupJSON = "[]"
		}
		if unmarshalErr := json.Unmarshal([]byte(cleanupJSON), &cleanup); unmarshalErr != nil {
			return nil, false, fmt.Errorf("unmarshal settings_download.cleanup_extensions_json: %w", unmarshalErr)
		}

		out.Download = &DownloadRuntimeSettings{
			OutDir:            outDir,
			CompletedDir:      completed,
			CleanupExtensions: cleanup,
		}
	}

	var optionsJSON string
	err = s.db.QueryRowContext(ctx, `
		SELECT options_json
		FROM settings_module_options
		WHERE module_name = ?`, usenetIndexerModuleName).Scan(&optionsJSON)
	if err != nil && err != sql.ErrNoRows {
		return nil, false, err
	}
	if err == nil {
		hasState = true

		var indexing IndexingRuntimeSettings
		if optionsJSON == "" {
			optionsJSON = "{}"
		}
		if unmarshalErr := json.Unmarshal([]byte(optionsJSON), &indexing); unmarshalErr != nil {
			return nil, false, fmt.Errorf("unmarshal usenet_indexer module options: %w", unmarshalErr)
		}
		out.Indexing = &indexing
	}

	return out, hasState, nil
}

func (s *Store) writeServers(ctx context.Context, tx *sql.Tx, servers []ServerRuntimeSettings) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings_nntp_servers`); err != nil {
		return err
	}

	for _, item := range servers {
		// CHANGED: store in ciphertext-shaped columns; real encryption remains a later step.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings_nntp_servers (
				id, host, port, username, password_ciphertext, tls, max_connections, priority, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			item.ID,
			item.Host,
			item.Port,
			item.Username,
			item.Password,
			item.TLS,
			item.MaxConnection,
			item.Priority,
		); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) writeIndexers(ctx context.Context, tx *sql.Tx, indexers []IndexerRuntimeSettings) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings_indexers`); err != nil {
		return err
	}

	for _, item := range indexers {
		// CHANGED: store in ciphertext-shaped columns; real encryption remains a later step.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings_indexers (
				id, base_url, api_path, api_key_ciphertext, redirect, updated_at
			) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			item.ID,
			item.BaseURL,
			item.APIPath,
			item.APIKey,
			item.Redirect,
		); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) writeArrIntegrations(ctx context.Context, tx *sql.Tx, integrations []ArrIntegrationRuntimeSettings) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings_arr_integrations`); err != nil {
		return err
	}

	for _, item := range integrations {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings_arr_integrations (
				id, kind, enabled, base_url, api_key_ciphertext, client_name, category, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			item.ID, item.Kind, item.Enabled, item.BaseURL, item.APIKey, item.ClientName, item.Category,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) writeDownload(ctx context.Context, tx *sql.Tx, download *DownloadRuntimeSettings) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings_download WHERE singleton_id = 1`); err != nil {
		return err
	}
	if download == nil {
		return nil
	}

	cleanupJSON, err := json.Marshal(download.CleanupExtensions)
	if err != nil {
		return fmt.Errorf("marshal cleanup extensions: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO settings_download (
			singleton_id, out_dir, completed_dir, cleanup_extensions_json, updated_at
		) VALUES (1, ?, ?, ?, CURRENT_TIMESTAMP)`,
		download.OutDir,
		download.CompletedDir,
		string(cleanupJSON),
	)
	return err
}

func (s *Store) writeUsenetIndexerOptions(ctx context.Context, tx *sql.Tx, indexing *IndexingRuntimeSettings) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM settings_module_options
		WHERE module_name = ?`, usenetIndexerModuleName); err != nil {
		return err
	}
	if indexing == nil {
		return nil
	}

	optionsJSON, err := json.Marshal(indexing)
	if err != nil {
		return fmt.Errorf("marshal usenet_indexer options: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO settings_module_options (
			module_name, options_json, updated_at
		) VALUES (?, ?, CURRENT_TIMESTAMP)`,
		usenetIndexerModuleName,
		string(optionsJSON),
	)
	return err
}
