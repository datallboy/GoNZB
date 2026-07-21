package pgindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/manifestavailability"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
)

const scanOutputSourceKind = "local_scan_output"

type ManifestAvailabilityProjection struct {
	Attestation  manifestavailability.Attestation
	EventID      string
	AuthorNodeID string
	PoolID       string
}

func (s *Store) UpsertGoNZBNetScanOutput(ctx context.Context, release releasecard.LocalRelease) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	scanID := strings.TrimSpace(release.LocalReleaseID)
	if scanID == "" {
		return fmt.Errorf("scan output local release id is required")
	}
	release.SourceKind = scanOutputSourceKind
	bodyJSON, err := json.Marshal(release)
	if err != nil {
		return err
	}
	_, err = s.federationExecutor(ctx).ExecContext(ctx, `
		INSERT INTO gonzbnet_scan_outputs (
			scan_id, body_json, status, updated_at
		)
		VALUES ($1, $2::jsonb, 'pending', NOW())
		ON CONFLICT (scan_id) DO UPDATE SET
			body_json = EXCLUDED.body_json,
			status = CASE
				WHEN gonzbnet_scan_outputs.status = 'published' THEN 'pending'
				ELSE gonzbnet_scan_outputs.status
			END,
			updated_at = NOW()`,
		scanID,
		string(bodyJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert gonzbnet scan output: %w", err)
	}
	return nil
}

func (s *Store) ListGoNZBNetScanOutputCandidates(ctx context.Context, poolID string, requireSignedManifest bool, limit int) ([]releasecard.LocalRelease, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		SELECT o.body_json
		FROM gonzbnet_scan_outputs o
		WHERE NOT EXISTS (
			SELECT 1 FROM gonzbnet_scan_output_publications p
			JOIN federation_events card ON card.event_id = p.event_id
			WHERE p.scan_id = o.scan_id
			  AND p.pool_id = $1
			  AND (
			    NOT $2
			    OR NULLIF(card.body_json->>'manifest_id', '') IS NULL
			    OR EXISTS (
			      SELECT 1
			      FROM resolution_manifests cached
			      JOIN federation_events manifest ON manifest.event_id = cached.source_event_id
			      WHERE cached.manifest_id = card.body_json->>'manifest_id'
			        AND manifest.event_type = 'ResolutionManifest'
			        AND manifest.pool_ids = jsonb_build_array(p.pool_id)
			    )
			  )
		)
		ORDER BY o.updated_at, o.scan_id
		LIMIT $3`, strings.TrimSpace(poolID), requireSignedManifest, limit)
	if err != nil {
		return nil, fmt.Errorf("list gonzbnet scan output candidates: %w", err)
	}
	defer rows.Close()
	out := []releasecard.LocalRelease{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var release releasecard.LocalRelease
		if err := json.Unmarshal(body, &release); err != nil {
			return nil, err
		}
		release.SourceKind = scanOutputSourceKind
		out = append(out, release)
	}
	return out, rows.Err()
}

func (s *Store) MarkGoNZBNetScanOutputPublished(ctx context.Context, scanID, eventID, poolID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	tx, commit, rollback, err := s.beginFederationProjection(ctx)
	if err != nil {
		return err
	}
	defer rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO gonzbnet_scan_output_publications (scan_id, pool_id, event_id, published_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (scan_id, pool_id) DO UPDATE SET
			event_id = EXCLUDED.event_id, published_at = EXCLUDED.published_at`,
		strings.TrimSpace(scanID), strings.TrimSpace(poolID), strings.TrimSpace(eventID)); err != nil {
		return fmt.Errorf("record gonzbnet scan output publication: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE gonzbnet_scan_outputs
		SET status = 'published',
		    published_event_id = NULLIF($2, ''),
		    published_at = NOW(),
		    updated_at = NOW()
		WHERE scan_id = $1`,
		strings.TrimSpace(scanID),
		strings.TrimSpace(eventID),
	)
	if err != nil {
		return fmt.Errorf("mark gonzbnet scan output published: %w", err)
	}
	return commit()
}

func (s *Store) ProjectManifestAvailability(ctx context.Context, projection ManifestAvailabilityProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	item := projection.Attestation
	if err := manifestavailability.Validate(item, time.Now().UTC(), 2*time.Minute); err != nil {
		return err
	}
	eventID := strings.TrimSpace(projection.EventID)
	if eventID == "" {
		return fmt.Errorf("source event_id is required")
	}
	authorNodeID := strings.TrimSpace(projection.AuthorNodeID)
	if authorNodeID == "" {
		return fmt.Errorf("author_node_id is required")
	}
	if authorNodeID != strings.TrimSpace(item.SourceNodeID) {
		return fmt.Errorf("source_node_id does not match author_node_id")
	}
	poolID := firstNonBlank(projection.PoolID, item.PoolID)
	if poolID != strings.TrimSpace(item.PoolID) {
		return fmt.Errorf("manifest availability pool_id mismatch")
	}
	bodyJSON, err := json.Marshal(item)
	if err != nil {
		return err
	}
	updatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.UpdatedAt))
	if err != nil {
		return err
	}
	confidence := manifestAvailabilityConfidence(item.Available)
	tx, commit, rollback, err := s.beginFederationProjection(ctx)
	if err != nil {
		return err
	}
	defer rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manifest_availability_attestations (
			attestation_id, release_id, manifest_id, author_node_id, pool_id,
			checked_at, status, confidence, method, body_json, source_event_id,
			source_node_id, available, fetch_policy, compressed_size_bytes,
			wire_updated_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, NULLIF($9, ''), $10::jsonb, $11,
			$12, $13, $14, $15, $16, NOW()
		)
		ON CONFLICT (attestation_id) DO UPDATE SET
			checked_at = EXCLUDED.checked_at,
			status = EXCLUDED.status,
			confidence = EXCLUDED.confidence,
			method = EXCLUDED.method,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			source_node_id = EXCLUDED.source_node_id,
			available = EXCLUDED.available,
			fetch_policy = EXCLUDED.fetch_policy,
			compressed_size_bytes = EXCLUDED.compressed_size_bytes,
			wire_updated_at = EXCLUDED.wire_updated_at,
			updated_at = NOW()`,
		eventID,
		item.ReleaseID,
		item.ManifestID,
		authorNodeID,
		poolID,
		updatedAt.UTC(),
		manifestAvailabilityStatus(item.Available),
		confidence,
		item.FetchPolicy,
		string(bodyJSON),
		eventID,
		item.SourceNodeID,
		item.Available,
		item.FetchPolicy,
		item.CompressedSizeBytes,
		updatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert manifest availability attestation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE federated_release_sources
		SET manifest_confidence_score = $5,
		    resolvable = $4,
		    last_seen_at = NOW()
		WHERE release_id = $1
		  AND manifest_id = $2
		  AND pool_id = $3
		  AND source_node_id = $6`,
		item.ReleaseID,
		item.ManifestID,
		poolID,
		item.Available,
		confidence,
		item.SourceNodeID,
	); err != nil {
		return fmt.Errorf("update manifest availability score: %w", err)
	}
	return commit()
}

func manifestAvailabilityStatus(available bool) string {
	if available {
		return "available"
	}
	return "unavailable"
}

func manifestAvailabilityConfidence(available bool) float64 {
	if available {
		return 1
	}
	return 0
}
