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

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/capability"
	"github.com/datallboy/gonzb/internal/gonzbnet/eventbody"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	gonzbnetmetrics "github.com/datallboy/gonzb/internal/gonzbnet/metrics"
)

type FederatedManifestSource struct {
	ManifestID    string
	ReleaseID     string
	SourceNodeID  string
	SourceEventID string
	PoolID        string
	BaseURL       string
	TrustScore    float64
}

type ResolutionManifestRecord struct {
	Manifest              manifest.ResolutionManifest
	SourceNodeID          string
	FetchedFromNodeID     string
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
	var manifestID, nzbSHA, sourceEventID string
	_, ttlDays := s.manifestCachePolicy()
	ttlClause := ""
	args := []any{strings.TrimSpace(releaseID)}
	if ttlDays > 0 {
		ttlClause = " AND rm.updated_at >= NOW() - ($2 * INTERVAL '1 day')"
		args = append(args, ttlDays)
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT rm.manifest_id, rm.generated_nzb, COALESCE(rm.nzb_sha256, ''),
		       manifest_source.source_event_id
		FROM resolution_manifests rm
		JOIN federated_release_cards c ON c.manifest_id = rm.manifest_id
		JOIN federated_release_sources source ON source.release_id = c.release_id
		  AND COALESCE(source.manifest_id, '') = rm.manifest_id
		  AND source.resolvable = TRUE
		JOIN federated_manifest_sources advertised ON advertised.manifest_id = rm.manifest_id
		  AND advertised.release_id = source.release_id
		  AND advertised.source_node_id = source.source_node_id
		  AND advertised.pool_id = source.pool_id
		  AND advertised.advertised = TRUE
		JOIN resolution_manifest_events manifest_source ON manifest_source.manifest_id = rm.manifest_id
		  AND manifest_source.pool_id = source.pool_id
		JOIN federation_events manifest_event ON manifest_event.event_id = manifest_source.source_event_id
		  AND manifest_event.event_type = 'ResolutionManifest'
		  AND manifest_event.validation_status = 'accepted'
		  AND manifest_event.body_json->>'manifest_id' = rm.manifest_id
		  AND manifest_event.body_json->>'release_id' = c.release_id
		  AND manifest_event.pool_ids = jsonb_build_array(source.pool_id)
		JOIN federation_nodes node ON node.node_id = source.source_node_id
		JOIN trust_pools pool ON pool.pool_id = source.pool_id AND pool.enabled = TRUE
		JOIN pool_members member ON member.pool_id = source.pool_id
		  AND member.node_id = source.source_node_id AND member.status = 'active'
		JOIN federation_nodes manifest_author ON manifest_author.node_id = manifest_source.author_node_id
		  AND manifest_author.node_id = manifest_event.author_node_id
		JOIN pool_members manifest_member ON manifest_member.pool_id = source.pool_id
		  AND manifest_member.node_id = manifest_source.author_node_id AND manifest_member.status = 'active'
		WHERE c.release_id = $1
		  AND rm.validation_status = 'accepted'
		  AND rm.cache_integrity_status <> 'failed'
		  AND rm.generated_nzb IS NOT NULL
		  AND node.status NOT IN ('blocked', 'forked')
		  AND manifest_author.status NOT IN ('blocked', 'forked')
		  AND (pool.min_node_trust_score <= 0 OR node.local_trust_score >= pool.min_node_trust_score)
		  AND (pool.min_node_trust_score <= 0 OR manifest_author.local_trust_score >= pool.min_node_trust_score)
		  AND (member.role = 'admin' OR COALESCE(member.allowed_capabilities, '[]'::jsonb) ?| ARRAY['scanner','indexer','release_publisher'])
		  AND (manifest_member.role = 'admin' OR COALESCE(manifest_member.allowed_capabilities, '[]'::jsonb) ?| ARRAY['manifest_builder','manifest_cache','release_publisher'])
		  AND NOT EXISTS (
		    SELECT 1 FROM federated_release_publication_states ps
		    WHERE ps.release_id = source.release_id
		      AND ps.source_node_id = source.source_node_id
		      AND ps.pool_id = source.pool_id
		      AND ps.state = 'withdrawn'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('hide', 'reject', 'local_only')
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND t.effective_at <= NOW()
		      AND (t.pool_id IS NULL OR t.pool_id = source.pool_id)
		      AND (
		        (t.target_type = 'release' AND t.target_id = c.release_id)
		        OR (t.target_type = 'manifest' AND t.target_id = rm.manifest_id)
		        OR (t.target_type = 'event' AND t.target_id IN (source.source_event_id, manifest_source.source_event_id))
		        OR (t.target_type = 'node' AND t.target_id IN (source.source_node_id, manifest_source.author_node_id))
		        OR (t.target_type = 'pool_member' AND t.target_id IN (source.source_node_id, manifest_source.author_node_id))
		      )
		  )`+ttlClause+`
		  ORDER BY source.trust_score DESC, manifest_source.updated_at DESC
		  LIMIT 1`, args...).Scan(&manifestID, &payload, &nzbSHA, &sourceEventID)
	if err == nil {
		if matchesNZBSHA256(payload, nzbSHA) {
			return payload, true, nil
		}
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.ManifestCacheIntegrityFailuresTotal, 1)
		return s.repairCachedFederatedNZB(ctx, manifestID, strings.TrimSpace(releaseID), sourceEventID)
	}
	if isNoRows(err) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("get cached federated nzb: %w", err)
}

func (s *Store) GetFederatedNZBSHA256ByReleaseID(ctx context.Context, releaseID string) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, fmt.Errorf("pgindex store is not initialized")
	}
	var hash string
	_, ttlDays := s.manifestCachePolicy()
	ttlClause := ""
	args := []any{strings.TrimSpace(releaseID)}
	if ttlDays > 0 {
		ttlClause = " AND rm.updated_at >= NOW() - ($2 * INTERVAL '1 day')"
		args = append(args, ttlDays)
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(rm.nzb_sha256, '')
		FROM resolution_manifests rm
		JOIN federated_release_cards card ON card.manifest_id = rm.manifest_id
		JOIN federated_release_sources source ON source.release_id = card.release_id
		  AND COALESCE(source.manifest_id, '') = rm.manifest_id
		  AND source.resolvable = TRUE
		JOIN federated_manifest_sources advertised ON advertised.manifest_id = rm.manifest_id
		  AND advertised.release_id = source.release_id
		  AND advertised.source_node_id = source.source_node_id
		  AND advertised.pool_id = source.pool_id
		  AND advertised.advertised = TRUE
		JOIN resolution_manifest_events manifest_source ON manifest_source.manifest_id = rm.manifest_id
		  AND manifest_source.pool_id = source.pool_id
		JOIN federation_events manifest_event ON manifest_event.event_id = manifest_source.source_event_id
		  AND manifest_event.event_type = 'ResolutionManifest'
		  AND manifest_event.validation_status = 'accepted'
		  AND manifest_event.body_json->>'manifest_id' = rm.manifest_id
		  AND manifest_event.body_json->>'release_id' = card.release_id
		  AND manifest_event.pool_ids = jsonb_build_array(source.pool_id)
		JOIN federation_nodes node ON node.node_id = source.source_node_id
		JOIN trust_pools pool ON pool.pool_id = source.pool_id AND pool.enabled = TRUE
		JOIN pool_members member ON member.pool_id = source.pool_id
		  AND member.node_id = source.source_node_id AND member.status = 'active'
		JOIN federation_nodes manifest_author ON manifest_author.node_id = manifest_source.author_node_id
		  AND manifest_author.node_id = manifest_event.author_node_id
		JOIN pool_members manifest_member ON manifest_member.pool_id = source.pool_id
		  AND manifest_member.node_id = manifest_source.author_node_id AND manifest_member.status = 'active'
		WHERE card.release_id = $1
		  AND rm.validation_status = 'accepted'
		  AND rm.cache_integrity_status <> 'failed'
		  AND rm.generated_nzb IS NOT NULL
		  AND rm.nzb_sha256 IS NOT NULL
		  AND node.status NOT IN ('blocked', 'forked')
		  AND manifest_author.status NOT IN ('blocked', 'forked')
		  AND (pool.min_node_trust_score <= 0 OR node.local_trust_score >= pool.min_node_trust_score)
		  AND (pool.min_node_trust_score <= 0 OR manifest_author.local_trust_score >= pool.min_node_trust_score)
		  AND (member.role = 'admin' OR COALESCE(member.allowed_capabilities, '[]'::jsonb) ?| ARRAY['scanner','indexer','release_publisher'])
		  AND (manifest_member.role = 'admin' OR COALESCE(manifest_member.allowed_capabilities, '[]'::jsonb) ?| ARRAY['manifest_builder','manifest_cache','release_publisher'])
		  AND NOT EXISTS (
		    SELECT 1 FROM federated_release_publication_states ps
		    WHERE ps.release_id = source.release_id
		      AND ps.source_node_id = source.source_node_id
		      AND ps.pool_id = source.pool_id
		      AND ps.state = 'withdrawn'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('hide','reject','local_only')
		      AND t.effective_at <= NOW()
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND (t.pool_id IS NULL OR t.pool_id = source.pool_id)
		      AND (
		        (t.target_type = 'release' AND t.target_id = card.release_id)
		        OR (t.target_type = 'manifest' AND t.target_id = rm.manifest_id)
		        OR (t.target_type = 'event' AND t.target_id IN (source.source_event_id, manifest_source.source_event_id))
		        OR (t.target_type = 'node' AND t.target_id IN (source.source_node_id, manifest_source.author_node_id))
		        OR (t.target_type = 'pool_member' AND t.target_id IN (source.source_node_id, manifest_source.author_node_id))
		      )
		  )`+ttlClause+`
		ORDER BY source.trust_score DESC, manifest_source.updated_at DESC
		LIMIT 1`, args...).Scan(&hash)
	if isNoRows(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get federated nzb checksum: %w", err)
	}
	if strings.TrimSpace(hash) == "" {
		return "", false, nil
	}
	return hash, true, nil
}

func (s *Store) repairCachedFederatedNZB(ctx context.Context, manifestID, releaseID, sourceEventID string) ([]byte, bool, error) {
	markFailed := func(reason string) {
		_, _ = s.db.ExecContext(ctx, `
			UPDATE resolution_manifests
			SET generated_nzb = NULL,
			    cache_integrity_status = 'failed',
			    cache_integrity_failed_at = NOW(),
			    cache_integrity_error = $2,
			    updated_at = NOW()
			WHERE manifest_id = $1`, manifestID, reason)
	}

	if strings.TrimSpace(sourceEventID) == "" {
		markFailed("cached_nzb_source_event_missing")
		return nil, false, nil
	}
	event, err := s.GetFederationEvent(ctx, sourceEventID)
	if err != nil {
		markFailed("cached_nzb_source_event_read_failed")
		return nil, false, nil
	}
	validation, err := events.Verify(event)
	if err != nil || validation == nil || !validation.OK {
		markFailed("cached_nzb_source_event_verification_failed")
		return nil, false, nil
	}
	if err := eventbody.Validate(event, time.Now().UTC(), 2*time.Minute); err != nil {
		markFailed("cached_nzb_source_event_body_invalid")
		return nil, false, nil
	}
	var item manifest.ResolutionManifest
	if err := json.Unmarshal(event.Body, &item); err != nil {
		markFailed("cached_nzb_manifest_decode_failed")
		return nil, false, nil
	}
	if item.ManifestID != manifestID || item.ReleaseID != releaseID {
		markFailed("cached_nzb_manifest_binding_mismatch")
		return nil, false, nil
	}
	generated, err := manifest.GenerateNZB(item)
	if err != nil {
		markFailed("cached_nzb_regeneration_failed")
		return nil, false, nil
	}
	hash := nzbSHA256(generated)
	if _, err := s.db.ExecContext(ctx, `
		UPDATE resolution_manifests
		SET generated_nzb = $2,
		    nzb_sha256 = $3,
		    cache_integrity_status = 'verified',
		    cache_integrity_failed_at = NULL,
		    cache_integrity_error = NULL,
		    updated_at = NOW()
		WHERE manifest_id = $1`, manifestID, generated, hash); err != nil {
		return nil, false, fmt.Errorf("repair cached federated nzb: %w", err)
	}
	return generated, true, nil
}

func matchesNZBSHA256(payload []byte, expected string) bool {
	expected = strings.TrimSpace(expected)
	return expected != "" && strings.EqualFold(nzbSHA256(payload), expected)
}

func nzbSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Store) FindFederatedManifestSource(ctx context.Context, releaseID string) (*FederatedManifestSource, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	var out FederatedManifestSource
	err := s.db.QueryRowContext(ctx, `
		SELECT c.manifest_id, c.release_id, fs.source_node_id, rs.source_event_id, fs.pool_id,
		       COALESCE(n.base_url, ''), fs.trust_score
		FROM federated_release_cards c
		JOIN federated_manifest_sources fs ON fs.manifest_id = c.manifest_id
		JOIN federated_release_sources rs ON rs.release_id = c.release_id
		 AND rs.source_node_id = fs.source_node_id
		 AND rs.pool_id = fs.pool_id
		 AND COALESCE(rs.manifest_id, '') = fs.manifest_id
		JOIN federation_events release_event ON release_event.event_id = rs.source_event_id
		JOIN federation_nodes n ON n.node_id = fs.source_node_id
		JOIN trust_pools pool ON pool.pool_id = fs.pool_id AND pool.enabled = TRUE
		JOIN pool_members member ON member.pool_id = fs.pool_id
		 AND member.node_id = fs.source_node_id
		 AND member.status = 'active'
		WHERE c.release_id = $1
		  AND c.manifest_id IS NOT NULL
		  AND fs.advertised = TRUE
		  AND n.status NOT IN ('blocked', 'forked')
		  AND (pool.min_node_trust_score <= 0 OR n.local_trust_score >= pool.min_node_trust_score)
		  AND (member.role = 'admin' OR member.allowed_capabilities ?| ARRAY['manifest_builder','manifest_cache','release_publisher'])
		  AND release_event.validation_status = 'accepted'
		  AND release_event.event_type = 'ReleaseCard'
		  AND release_event.author_node_id = rs.source_node_id
		  AND release_event.pool_ids @> jsonb_build_array(rs.pool_id)
		  AND release_event.body_json = release_event.canonical_event_json::jsonb->'body'
		  AND c.body_json = release_event.body_json
		  AND COALESCE(release_event.body_json->>'release_id', '') = rs.release_id
		  AND COALESCE(release_event.body_json->>'manifest_id', '') = COALESCE(rs.manifest_id, '')
		  AND c.title = COALESCE(release_event.body_json->>'title', '')
		  AND c.normalized_title = COALESCE(release_event.body_json->>'normalized_title', '')
		  AND c.category_json = COALESCE(release_event.body_json->'category', '[]'::jsonb)
		  AND c.newznab_categories = COALESCE(release_event.body_json->'newznab_categories', '[]'::jsonb)
		  AND c.size_bytes IS NOT DISTINCT FROM NULLIF(release_event.body_json->>'size_bytes', '')::bigint
		  AND c.posted_at IS NOT DISTINCT FROM NULLIF(release_event.body_json->>'posted_at', '')::timestamptz
		  AND c.groups_json = COALESCE(release_event.body_json->'groups', '[]'::jsonb)
		  AND c.file_count IS NOT DISTINCT FROM NULLIF(release_event.body_json->>'file_count', '')::integer
		  AND c.segment_count IS NOT DISTINCT FROM NULLIF(release_event.body_json->>'segment_count', '')::integer
		  AND COALESCE(c.poster_hash, '') = COALESCE(release_event.body_json->>'poster_hash', '')
		  AND c.subject_fingerprint = COALESCE(release_event.body_json->>'subject_fingerprint', '')
		  AND c.file_fingerprint = COALESCE(release_event.body_json->>'file_fingerprint', '')
		  AND c.media_json = COALESCE(release_event.body_json->'media', '{}'::jsonb)
		  AND c.quality_json = COALESCE(release_event.body_json->'quality', '{}'::jsonb)
		  AND c.flags_json = COALESCE(release_event.body_json->'flags', '{}'::jsonb)
		  AND c.resolution_json = COALESCE(release_event.body_json->'resolution', '{}'::jsonb)
		  AND c.expires_at IS NOT DISTINCT FROM NULLIF(release_event.body_json->>'expires_at', '')::timestamptz
		  AND NOT EXISTS (
		    SELECT 1 FROM federated_release_publication_states ps
		    WHERE ps.release_id = rs.release_id
		      AND ps.source_node_id = rs.source_node_id
		      AND ps.pool_id = rs.pool_id
		      AND ps.state = 'withdrawn'
		  )
		  AND (
		    COALESCE(c.flags_json->>'passworded', 'unknown') <> 'passworded'
		    OR COALESCE((n.capabilities->>'manifest_archive_password')::boolean, FALSE) = TRUE
		  )
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
		        OR (t.target_type = 'event' AND t.target_id = rs.source_event_id)
		        OR (t.target_type = 'node' AND t.target_id = fs.source_node_id)
		        OR (t.target_type = 'pool_member' AND t.target_id = fs.source_node_id)
		      )
		      AND (t.pool_id IS NULL OR t.pool_id = fs.pool_id)
		  )
		ORDER BY fs.trust_score DESC, fs.last_success_at DESC NULLS LAST, fs.updated_at DESC
		LIMIT 1`, strings.TrimSpace(releaseID)).Scan(
		&out.ManifestID,
		&out.ReleaseID,
		&out.SourceNodeID,
		&out.SourceEventID,
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

// AuthorizeFederatedManifestSource rechecks serving-node and signed-author
// provenance at the point a manifest is fetched or accepted.
func (s *Store) AuthorizeFederatedManifestSource(ctx context.Context, source FederatedManifestSource, authorNodeID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	servingNodeID := strings.TrimSpace(source.SourceNodeID)
	authorNodeID = strings.TrimSpace(authorNodeID)
	if authorNodeID == "" {
		authorNodeID = servingNodeID
	}
	if servingNodeID == "" || strings.TrimSpace(source.ReleaseID) == "" || strings.TrimSpace(source.ManifestID) == "" || strings.TrimSpace(source.PoolID) == "" {
		return fmt.Errorf("manifest source provenance is incomplete")
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM federated_manifest_sources fs
		  JOIN federated_release_sources rs ON rs.release_id = fs.release_id
		    AND rs.source_node_id = fs.source_node_id
		    AND rs.pool_id = fs.pool_id
		    AND COALESCE(rs.manifest_id, '') = fs.manifest_id
		  WHERE fs.manifest_id = $1
		    AND fs.release_id = $2
		    AND fs.source_node_id = $3
		    AND fs.pool_id = $4
		    AND fs.advertised = TRUE
		    AND ($5 = '' OR rs.source_event_id = $5)
		    AND NOT EXISTS (
		      SELECT 1 FROM federated_release_publication_states ps
		      WHERE ps.release_id = rs.release_id
		        AND ps.source_node_id = rs.source_node_id
		        AND ps.pool_id = rs.pool_id
		        AND ps.state = 'withdrawn'
		    )
		    AND NOT EXISTS (
		      SELECT 1 FROM tombstones t
		      WHERE t.active = TRUE
		        AND t.severity IN ('hide','reject','local_only')
		        AND t.effective_at <= NOW()
		        AND (t.expires_at IS NULL OR t.expires_at > NOW())
		        AND (t.pool_id IS NULL OR t.pool_id = fs.pool_id)
		        AND (
		          (t.target_type = 'release' AND t.target_id = fs.release_id)
		          OR (t.target_type = 'manifest' AND t.target_id = fs.manifest_id)
		          OR (t.target_type = 'event' AND t.target_id = rs.source_event_id)
		          OR (t.target_type = 'node' AND t.target_id IN (fs.source_node_id, $6))
		          OR (t.target_type = 'pool_member' AND t.target_id IN (fs.source_node_id, $6))
		        )
		    )
		)`, source.ManifestID, source.ReleaseID, servingNodeID, source.PoolID, source.SourceEventID, authorNodeID).Scan(&exists); err != nil {
		return fmt.Errorf("check manifest source provenance: %w", err)
	}
	if !exists {
		return fmt.Errorf("manifest source is no longer advertised or has been suppressed")
	}
	if err := s.authorizeFederatedManifestParticipant(ctx, source.PoolID, servingNodeID); err != nil {
		return fmt.Errorf("manifest serving node is not authorized: %w", err)
	}
	if authorNodeID != servingNodeID {
		cacheAllowed, err := s.PoolMemberHasCapability(ctx, source.PoolID, servingNodeID, []string{capability.ManifestCache})
		if err != nil {
			return err
		}
		if !cacheAllowed {
			return fmt.Errorf("manifest serving node is not an authorized cache")
		}
	}
	if err := s.authorizeFederatedManifestParticipant(ctx, source.PoolID, authorNodeID); err != nil {
		return fmt.Errorf("manifest author is not authorized: %w", err)
	}
	return nil
}

// authorizeFederatedManifestParticipant applies the pool state and capability
// checks needed by the dedicated manifest-fetch protocol. ResolutionManifest
// events are fetched directly and are intentionally not required to be enabled
// in the pool's general relay accepted_event_types policy.
func (s *Store) authorizeFederatedManifestParticipant(ctx context.Context, poolID, nodeID string) error {
	var (
		nodeStatus        string
		poolEnabled       bool
		activeMember      bool
		capabilityAllowed bool
		trustScore        float64
		minimumTrust      float64
	)
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		SELECT node.status,
		       pool.enabled,
		       node.local_trust_score,
		       pool.min_node_trust_score,
		       EXISTS (
		         SELECT 1
		         FROM pool_members member
		         WHERE member.pool_id = pool.pool_id
		           AND member.node_id = node.node_id
		           AND member.status = 'active'
		       ),
		       EXISTS (
		         SELECT 1
		         FROM pool_members member
		         WHERE member.pool_id = pool.pool_id
		           AND member.node_id = node.node_id
		           AND member.status = 'active'
		           AND (
		             member.role = 'admin'
		             OR COALESCE(member.allowed_capabilities, '[]'::jsonb)
		                ?| ARRAY['manifest_builder','manifest_cache','release_publisher']
		           )
		       )
		FROM federation_nodes node
		CROSS JOIN trust_pools pool
		WHERE node.node_id = $1
		  AND pool.pool_id = $2`, strings.TrimSpace(nodeID), strings.TrimSpace(poolID)).Scan(
		&nodeStatus,
		&poolEnabled,
		&trustScore,
		&minimumTrust,
		&activeMember,
		&capabilityAllowed,
	)
	if err == sql.ErrNoRows {
		return fmt.Errorf("node or pool is unknown")
	}
	if err != nil {
		return fmt.Errorf("check manifest participant authorization: %w", err)
	}
	if nodeStatus == "blocked" {
		return fmt.Errorf("node_blocked")
	}
	if nodeStatus == "forked" {
		return fmt.Errorf("node_forked")
	}
	if !poolEnabled {
		return fmt.Errorf("pool_disabled")
	}
	if !activeMember {
		return fmt.Errorf("not_pool_member")
	}
	if minimumTrust > 0 && trustScore < minimumTrust {
		return fmt.Errorf("node_trust_below_pool_minimum")
	}
	if !capabilityAllowed {
		return fmt.Errorf("node_capability_not_allowed")
	}
	return nil
}

func (s *Store) StoreResolutionManifest(ctx context.Context, record ResolutionManifestRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if _, err := manifest.Validate(record.Manifest); err != nil {
		return err
	}
	if err := s.validateResolutionManifestProvenance(ctx, record); err != nil {
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
		nzbSHA = nzbSHA256(record.GeneratedNZB)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, ttlDays := s.manifestCachePolicy()
	if ttlDays > 0 {
		if _, err = tx.ExecContext(ctx, `
			DELETE FROM resolution_manifests
			WHERE updated_at < NOW() - ($1 * INTERVAL '1 day')`, ttlDays); err != nil {
			return fmt.Errorf("purge expired resolution manifests: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO resolution_manifests (
			manifest_id, release_id, source_node_id, fetched_from_node_id, source_event_id, encoding,
			compression, encrypted, canonical_manifest_json, body_json, body_blob,
			nzb_sha256, generated_nzb, fetched_at, verified_at, validation_status,
			cache_integrity_status, cache_integrity_failed_at, cache_integrity_error, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), 'jcs-json',
		        NULLIF($6, ''), $7, $8, $9::jsonb, $10,
		        NULLIF($11, ''), $12, NOW(), NOW(), 'accepted', 'verified', NULL, NULL, NOW())
		ON CONFLICT (manifest_id) DO UPDATE SET
			release_id = EXCLUDED.release_id,
			source_node_id = COALESCE(EXCLUDED.source_node_id, resolution_manifests.source_node_id),
			fetched_from_node_id = COALESCE(EXCLUDED.fetched_from_node_id, resolution_manifests.fetched_from_node_id),
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
			cache_integrity_status = 'verified',
			cache_integrity_failed_at = NULL,
			cache_integrity_error = NULL,
			updated_at = NOW()`,
		record.Manifest.ManifestID,
		record.Manifest.ReleaseID,
		record.SourceNodeID,
		record.FetchedFromNodeID,
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
		INSERT INTO resolution_manifest_events (
			manifest_id, pool_id, author_node_id, source_event_id,
			fetched_from_node_id, verified_at, updated_at
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NOW(), NOW())
		ON CONFLICT (manifest_id, pool_id, author_node_id) DO UPDATE SET
			source_event_id = EXCLUDED.source_event_id,
			fetched_from_node_id = COALESCE(EXCLUDED.fetched_from_node_id, resolution_manifest_events.fetched_from_node_id),
			verified_at = NOW(),
			updated_at = NOW()`,
		record.Manifest.ManifestID,
		poolID,
		record.SourceNodeID,
		record.SourceEventID,
		record.FetchedFromNodeID,
	); err != nil {
		return fmt.Errorf("store resolution manifest pool provenance: %w", err)
	}
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
	if maxBytes, _ := s.manifestCachePolicy(); maxBytes > 0 {
		if _, err := tx.ExecContext(ctx, `
			WITH ranked AS (
				SELECT manifest_id,
				       SUM(COALESCE(octet_length(generated_nzb), 0) + COALESCE(octet_length(body_blob), 0))
				         OVER (ORDER BY updated_at ASC, manifest_id ASC) AS removed,
				       SUM(COALESCE(octet_length(generated_nzb), 0) + COALESCE(octet_length(body_blob), 0))
				         OVER () AS total
				FROM resolution_manifests
			)
			DELETE FROM resolution_manifests rm
			USING ranked r
			WHERE rm.manifest_id = r.manifest_id
			  AND r.total - r.removed > $1`, maxBytes); err != nil {
			return fmt.Errorf("prune resolution manifest cache: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) validateResolutionManifestProvenance(ctx context.Context, record ResolutionManifestRecord) error {
	sourceNodeID := strings.TrimSpace(record.SourceNodeID)
	sourceEventID := strings.TrimSpace(record.SourceEventID)
	poolID := strings.TrimSpace(record.PoolID)
	if sourceNodeID == "" || sourceEventID == "" || poolID == "" {
		return fmt.Errorf("resolution manifest source_node_id, source_event_id, and pool_id are required")
	}
	event, err := s.GetFederationEvent(ctx, sourceEventID)
	if err != nil {
		return fmt.Errorf("read resolution manifest source event: %w", err)
	}
	validation, err := events.Verify(event)
	if err != nil || validation == nil || !validation.OK {
		return fmt.Errorf("resolution manifest source event verification failed")
	}
	if err := eventbody.Validate(event, time.Now().UTC(), 2*time.Minute); err != nil {
		return fmt.Errorf("resolution manifest source event body is invalid: %w", err)
	}
	if event.EventType != manifest.Type || event.BodySchema != manifest.BodySchema {
		return fmt.Errorf("resolution manifest source event type or schema mismatch")
	}
	if event.AuthorNodeID != sourceNodeID {
		return fmt.Errorf("resolution manifest source author mismatch")
	}
	if len(event.PoolIDs) != 1 || strings.TrimSpace(event.PoolIDs[0]) != poolID {
		return fmt.Errorf("resolution manifest source pool mismatch")
	}
	var signedManifest manifest.ResolutionManifest
	if err := json.Unmarshal(event.Body, &signedManifest); err != nil {
		return fmt.Errorf("decode resolution manifest source event: %w", err)
	}
	signedCanonical, err := canonical.Marshal(signedManifest)
	if err != nil {
		return err
	}
	recordCanonical, err := canonical.Marshal(record.Manifest)
	if err != nil {
		return err
	}
	if !bytes.Equal(signedCanonical, recordCanonical) {
		return fmt.Errorf("resolution manifest does not match signed source event")
	}
	return nil
}

func (s *Store) GetResolutionManifest(ctx context.Context, manifestID string) (*manifest.ResolutionManifest, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	var body []byte
	_, ttlDays := s.manifestCachePolicy()
	ttlClause := ""
	args := []any{strings.TrimSpace(manifestID)}
	if ttlDays > 0 {
		ttlClause = " AND updated_at >= NOW() - ($2 * INTERVAL '1 day')"
		args = append(args, ttlDays)
	}
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
		  )`+ttlClause, args...).Scan(&body)
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

func (s *Store) CanFetchResolutionManifestForSource(ctx context.Context, manifestID, releaseID, poolID, nodeID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	var ok bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM federated_manifest_sources fs
		  JOIN federated_release_sources source ON source.release_id = fs.release_id
		    AND source.source_node_id = fs.source_node_id
		    AND source.pool_id = fs.pool_id
		    AND COALESCE(source.manifest_id, '') = fs.manifest_id
		  JOIN resolution_manifests rm ON rm.manifest_id = fs.manifest_id
		    AND rm.release_id = fs.release_id
		    AND rm.validation_status = 'accepted'
		  JOIN resolution_manifest_events manifest_source ON manifest_source.manifest_id = rm.manifest_id
		    AND manifest_source.pool_id = fs.pool_id
		  JOIN federation_events manifest_event ON manifest_event.event_id = manifest_source.source_event_id
		    AND manifest_event.author_node_id = manifest_source.author_node_id
		    AND manifest_event.event_type = 'ResolutionManifest'
		    AND manifest_event.validation_status = 'accepted'
		    AND manifest_event.body_json->>'manifest_id' = rm.manifest_id
		    AND manifest_event.body_json->>'release_id' = rm.release_id
		    AND manifest_event.pool_ids = jsonb_build_array(fs.pool_id)
		  JOIN trust_pools pool ON pool.pool_id = fs.pool_id AND pool.enabled = TRUE
		  JOIN federation_nodes source_node ON source_node.node_id = fs.source_node_id
		  JOIN pool_members source_member ON source_member.pool_id = fs.pool_id
		    AND source_member.node_id = fs.source_node_id
		    AND source_member.status = 'active'
		  JOIN federation_nodes author_node ON author_node.node_id = manifest_event.author_node_id
		  JOIN pool_members author_member ON author_member.pool_id = fs.pool_id
		    AND author_member.node_id = manifest_event.author_node_id
		    AND author_member.status = 'active'
		  JOIN pool_members requester_member ON requester_member.pool_id = fs.pool_id
		    AND requester_member.node_id = $4
		    AND requester_member.status = 'active'
		  JOIN federation_nodes requester ON requester.node_id = requester_member.node_id
		  WHERE fs.manifest_id = $1
		    AND fs.release_id = $2
		    AND fs.pool_id = $3
		    AND fs.advertised = TRUE
		    AND requester.status NOT IN ('blocked', 'forked')
		    AND source_node.status NOT IN ('blocked', 'forked')
		    AND author_node.status NOT IN ('blocked', 'forked')
		    AND (pool.min_node_trust_score <= 0 OR source_node.local_trust_score >= pool.min_node_trust_score)
		    AND (pool.min_node_trust_score <= 0 OR author_node.local_trust_score >= pool.min_node_trust_score)
		    AND (source_member.role = 'admin' OR source_member.allowed_capabilities ?| ARRAY['scanner','indexer','release_publisher'])
		    AND (author_member.role = 'admin' OR author_member.allowed_capabilities ?| ARRAY['manifest_builder','manifest_cache','release_publisher'])
		    AND NOT EXISTS (
		      SELECT 1 FROM federated_release_publication_states ps
		      WHERE ps.release_id = source.release_id
		        AND ps.source_node_id = source.source_node_id
		        AND ps.pool_id = source.pool_id
		        AND ps.state = 'withdrawn'
		    )
		    AND NOT EXISTS (
		      SELECT 1 FROM tombstones t
		      WHERE t.active = TRUE
		        AND t.severity IN ('hide','reject','local_only')
		        AND t.effective_at <= NOW()
		        AND (t.expires_at IS NULL OR t.expires_at > NOW())
		        AND (t.pool_id IS NULL OR t.pool_id = fs.pool_id)
		        AND (
		          (t.target_type = 'release' AND t.target_id = fs.release_id)
		          OR (t.target_type = 'manifest' AND t.target_id = fs.manifest_id)
		          OR (t.target_type = 'event' AND t.target_id IN (source.source_event_id, manifest_event.event_id))
		          OR (t.target_type = 'node' AND t.target_id IN (source.source_node_id, manifest_event.author_node_id, requester_member.node_id))
		          OR (t.target_type = 'pool_member' AND t.target_id IN (source.source_node_id, manifest_event.author_node_id, requester_member.node_id))
		        )
		    )
		)`, strings.TrimSpace(manifestID), strings.TrimSpace(releaseID), strings.TrimSpace(poolID), strings.TrimSpace(nodeID)).Scan(&ok); err != nil {
		return false, fmt.Errorf("check manifest source fetch authorization: %w", err)
	}
	return ok, nil
}

func (s *Store) GetResolutionManifestEvent(ctx context.Context, manifestID, poolID string) (*events.SignedEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	var eventID string
	_, ttlDays := s.manifestCachePolicy()
	ttlClause := ""
	args := []any{strings.TrimSpace(manifestID), strings.TrimSpace(poolID)}
	if ttlDays > 0 {
		ttlClause = " AND manifest.updated_at >= NOW() - ($3 * INTERVAL '1 day')"
		args = append(args, ttlDays)
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT manifest_source.source_event_id
		FROM resolution_manifests manifest
		JOIN resolution_manifest_events manifest_source
		  ON manifest_source.manifest_id = manifest.manifest_id
		 AND manifest_source.pool_id = $2
		JOIN federation_events event ON event.event_id = manifest_source.source_event_id
		  AND event.author_node_id = manifest_source.author_node_id
		  AND event.event_type = 'ResolutionManifest'
		  AND event.validation_status = 'accepted'
		  AND event.body_json->>'manifest_id' = manifest.manifest_id
		  AND event.body_json->>'release_id' = manifest.release_id
		  AND event.pool_ids = jsonb_build_array(manifest_source.pool_id)
		WHERE manifest.manifest_id = $1
		  AND manifest.validation_status = 'accepted'
		  AND NOT EXISTS (
		    SELECT 1
		    FROM tombstones t
		    WHERE t.active = TRUE
		      AND t.severity IN ('hide', 'reject', 'local_only')
		      AND (t.expires_at IS NULL OR t.expires_at > NOW())
		      AND t.effective_at <= NOW()
		      AND (t.pool_id IS NULL OR t.pool_id = manifest_source.pool_id)
		      AND (
		        (t.target_type = 'release' AND t.target_id = manifest.release_id)
		        OR (t.target_type = 'manifest' AND t.target_id = manifest.manifest_id)
		        OR (t.target_type = 'event' AND t.target_id = manifest_source.source_event_id)
		        OR (t.target_type = 'node' AND t.target_id = manifest_source.author_node_id)
		        OR (t.target_type = 'pool_member' AND t.target_id = manifest_source.author_node_id)
		      )
		  )`+ttlClause+`
		ORDER BY manifest_source.updated_at DESC
		LIMIT 1`, args...).Scan(&eventID)
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
