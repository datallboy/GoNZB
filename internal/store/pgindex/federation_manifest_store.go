package pgindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
)

type FederatedManifestSource struct {
	ManifestID   string
	ReleaseID    string
	SourceNodeID string
	PoolID       string
	BaseURL      string
	TrustScore   float64
}

type ResolutionManifestRecord struct {
	Manifest              manifest.ResolutionManifest
	SourceNodeID          string
	SourceEventID         string
	PoolID                string
	CanonicalManifestJSON []byte
	GeneratedNZB          []byte
}

func (s *Store) GetCachedFederatedNZBByReleaseID(ctx context.Context, releaseID string) ([]byte, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("pgindex store is not initialized")
	}
	var payload []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT rm.generated_nzb
		FROM resolution_manifests rm
		JOIN federated_release_cards c ON c.manifest_id = rm.manifest_id
		WHERE c.release_id = $1
		  AND rm.validation_status = 'accepted'
		  AND rm.generated_nzb IS NOT NULL
		  AND NOT EXISTS (
		    SELECT 1
		    FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('reject', 'local_only')
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND t.effective_at <= NOW()
		      AND (
		        (t.target_type = 'release' AND t.target_id = c.release_id)
		        OR (t.target_type = 'manifest' AND t.target_id = rm.manifest_id)
		      )
		  )
		LIMIT 1`, strings.TrimSpace(releaseID)).Scan(&payload)
	if err == nil {
		return payload, true, nil
	}
	if isNoRows(err) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("get cached federated nzb: %w", err)
}

func (s *Store) FindFederatedManifestSource(ctx context.Context, releaseID string) (*FederatedManifestSource, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	var out FederatedManifestSource
	err := s.db.QueryRowContext(ctx, `
		SELECT c.manifest_id, c.release_id, fs.source_node_id, fs.pool_id,
		       COALESCE(n.base_url, ''), fs.trust_score
		FROM federated_release_cards c
		JOIN federated_manifest_sources fs ON fs.manifest_id = c.manifest_id
		JOIN federation_nodes n ON n.node_id = fs.source_node_id
		WHERE c.release_id = $1
		  AND c.manifest_id IS NOT NULL
		  AND fs.advertised = TRUE
		  AND NOT EXISTS (
		    SELECT 1
		    FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('reject', 'local_only')
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND t.effective_at <= NOW()
		      AND (
		        (t.target_type = 'release' AND t.target_id = c.release_id)
		        OR (t.target_type = 'manifest' AND t.target_id = c.manifest_id)
		      )
		      AND (t.pool_id IS NULL OR t.pool_id = fs.pool_id)
		  )
		ORDER BY fs.trust_score DESC, fs.last_success_at DESC NULLS LAST, fs.updated_at DESC
		LIMIT 1`, strings.TrimSpace(releaseID)).Scan(
		&out.ManifestID,
		&out.ReleaseID,
		&out.SourceNodeID,
		&out.PoolID,
		&out.BaseURL,
		&out.TrustScore,
	)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find federated manifest source: %w", err)
	}
	return &out, nil
}

func (s *Store) StoreResolutionManifest(ctx context.Context, record ResolutionManifestRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if _, err := manifest.Validate(record.Manifest); err != nil {
		return err
	}
	bodyJSON, err := json.Marshal(record.Manifest)
	if err != nil {
		return err
	}
	canonicalManifest := record.CanonicalManifestJSON
	if len(canonicalManifest) == 0 {
		canonicalManifest, err = json.Marshal(record.Manifest.ManifestCore)
		if err != nil {
			return err
		}
	}
	nzbSHA := ""
	if len(record.GeneratedNZB) > 0 {
		sum := sha256.Sum256(record.GeneratedNZB)
		nzbSHA = "sha256:" + hex.EncodeToString(sum[:])
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO resolution_manifests (
			manifest_id, release_id, source_node_id, source_event_id, encoding,
			compression, encrypted, canonical_manifest_json, body_json, body_blob,
			nzb_sha256, generated_nzb, fetched_at, verified_at, validation_status,
			updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), 'jcs-json',
		        NULLIF($5, ''), $6, $7, $8::jsonb, $9,
		        NULLIF($10, ''), $11, NOW(), NOW(), 'accepted', NOW())
		ON CONFLICT (manifest_id) DO UPDATE SET
			release_id = EXCLUDED.release_id,
			source_node_id = COALESCE(EXCLUDED.source_node_id, resolution_manifests.source_node_id),
			source_event_id = COALESCE(EXCLUDED.source_event_id, resolution_manifests.source_event_id),
			compression = EXCLUDED.compression,
			encrypted = EXCLUDED.encrypted,
			canonical_manifest_json = EXCLUDED.canonical_manifest_json,
			body_json = EXCLUDED.body_json,
			body_blob = EXCLUDED.body_blob,
			nzb_sha256 = EXCLUDED.nzb_sha256,
			generated_nzb = EXCLUDED.generated_nzb,
			fetched_at = NOW(),
			verified_at = NOW(),
			validation_status = 'accepted',
			rejection_reason = NULL,
			updated_at = NOW()`,
		record.Manifest.ManifestID,
		record.Manifest.ReleaseID,
		record.SourceNodeID,
		record.SourceEventID,
		record.Manifest.Compression,
		record.Manifest.Encrypted,
		string(canonicalManifest),
		string(bodyJSON),
		[]byte(bodyJSON),
		nzbSHA,
		record.GeneratedNZB,
	); err != nil {
		return fmt.Errorf("store resolution manifest: %w", err)
	}
	poolID := firstNonBlank(record.PoolID, "pool.local")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO federation_validation_tasks (
			manifest_id, release_id, source_node_id, source_event_id, pool_id,
			status, due_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, 'pending', NOW(), NOW())
		ON CONFLICT (manifest_id, pool_id) DO UPDATE SET
			release_id = EXCLUDED.release_id,
			source_node_id = COALESCE(EXCLUDED.source_node_id, federation_validation_tasks.source_node_id),
			source_event_id = COALESCE(EXCLUDED.source_event_id, federation_validation_tasks.source_event_id),
			status = CASE
				WHEN federation_validation_tasks.status = 'completed' THEN federation_validation_tasks.status
				ELSE 'pending'
			END,
			due_at = CASE
				WHEN federation_validation_tasks.status = 'completed' THEN federation_validation_tasks.due_at
				ELSE NOW()
			END,
			updated_at = NOW()`,
		record.Manifest.ManifestID,
		record.Manifest.ReleaseID,
		record.SourceNodeID,
		record.SourceEventID,
		poolID,
	); err != nil {
		return fmt.Errorf("enqueue validation task: %w", err)
	}
	return tx.Commit()
}

func (s *Store) GetResolutionManifest(ctx context.Context, manifestID string) (*manifest.ResolutionManifest, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	var body []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT body_json
		FROM resolution_manifests
		WHERE manifest_id = $1
		  AND validation_status = 'accepted'
		  AND NOT EXISTS (
		    SELECT 1
		    FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('reject', 'local_only')
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND t.effective_at <= NOW()
		      AND t.target_type = 'manifest'
		      AND t.target_id = resolution_manifests.manifest_id
		  )`, strings.TrimSpace(manifestID)).Scan(&body)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get resolution manifest: %w", err)
	}
	var out manifest.ResolutionManifest
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) CanFetchResolutionManifest(ctx context.Context, manifestID, nodeID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	var ok bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM federated_manifest_sources fs
			JOIN pool_members pm ON pm.pool_id = fs.pool_id
			WHERE fs.manifest_id = $1
			  AND pm.node_id = $2
			  AND pm.status = 'active'
		)`, strings.TrimSpace(manifestID), strings.TrimSpace(nodeID)).Scan(&ok); err != nil {
		return false, fmt.Errorf("check manifest fetch authorization: %w", err)
	}
	return ok, nil
}

func (s *Store) GetResolutionManifestEvent(ctx context.Context, manifestID string) (*events.SignedEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	var eventID string
	err := s.db.QueryRowContext(ctx, `
		SELECT source_event_id
		FROM resolution_manifests
		WHERE manifest_id = $1
		  AND validation_status = 'accepted'
		  AND source_event_id IS NOT NULL
		  AND NOT EXISTS (
		    SELECT 1
		    FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('reject', 'local_only')
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND t.effective_at <= NOW()
		      AND t.target_type = 'manifest'
		      AND t.target_id = resolution_manifests.manifest_id
		  )`, strings.TrimSpace(manifestID)).Scan(&eventID)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetFederationEvent(ctx, eventID)
}

func (s *Store) RecordFederatedManifestSourceSuccess(ctx context.Context, source FederatedManifestSource) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE federated_manifest_sources
		SET last_success_at = NOW(),
		    failure_count = 0,
		    updated_at = NOW()
		WHERE manifest_id = $1
		  AND source_node_id = $2
		  AND pool_id = $3`,
		source.ManifestID,
		source.SourceNodeID,
		source.PoolID,
	)
	return err
}

func (s *Store) RecordFederatedManifestSourceFailure(ctx context.Context, source FederatedManifestSource) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE federated_manifest_sources
		SET last_failure_at = NOW(),
		    failure_count = failure_count + 1,
		    updated_at = NOW()
		WHERE manifest_id = $1
		  AND source_node_id = $2
		  AND pool_id = $3`,
		source.ManifestID,
		source.SourceNodeID,
		source.PoolID,
	)
	return err
}

func isNoRows(err error) bool {
	return err == sql.ErrNoRows
}
