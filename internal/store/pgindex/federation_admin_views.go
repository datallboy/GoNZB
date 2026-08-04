package pgindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type NodeCapabilityView struct {
	NodeID            string          `json:"node_id"`
	Alias             string          `json:"alias"`
	BaseURL           string          `json:"base_url"`
	Status            string          `json:"status"`
	Capabilities      json.RawMessage `json:"capabilities"`
	ModuleStatus      json.RawMessage `json:"module_status"`
	ScannerCapacity   json.RawMessage `json:"scanner_capacity,omitempty"`
	ValidatorCapacity json.RawMessage `json:"validator_capacity,omitempty"`
	ProviderScope     json.RawMessage `json:"provider_scope,omitempty"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type CoverageGroupCatalogItem struct {
	PoolID        string    `json:"pool_id"`
	Group         string    `json:"group"`
	ObservedAt    time.Time `json:"observed_at"`
	LowWatermark  int64     `json:"low_watermark"`
	HighWatermark int64     `json:"high_watermark"`
	RetentionDays int       `json:"retention_days"`
	Confidence    float64   `json:"confidence"`
	AuthorNodeID  string    `json:"author_node_id"`
}

type ValidationGap struct {
	ReleaseID                  string     `json:"release_id"`
	ManifestID                 string     `json:"manifest_id"`
	PoolID                     string     `json:"pool_id"`
	SourceNodeID               string     `json:"source_node_id"`
	LastValidationTaskAt       *time.Time `json:"last_validation_task_at,omitempty"`
	ValidationAttestationCount int        `json:"validation_attestation_count"`
}

func (s *Store) ListFederationNodeCapabilities(ctx context.Context) ([]NodeCapabilityView, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.node_id, COALESCE(n.alias, ''), COALESCE(n.base_url, ''),
		       n.status, COALESCE(c.capabilities, '{}'::jsonb),
		       COALESCE(c.module_status, '{}'::jsonb),
		       c.scanner_capacity, c.validator_capacity, c.provider_scope,
		       COALESCE(c.updated_at, n.updated_at)
		FROM federation_nodes n
		LEFT JOIN federation_node_capabilities c ON c.node_id = n.node_id
		ORDER BY n.node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeCapabilityView{}
	for rows.Next() {
		var item NodeCapabilityView
		var scanner, validator, providerScope []byte
		if err := rows.Scan(&item.NodeID, &item.Alias, &item.BaseURL, &item.Status, &item.Capabilities, &item.ModuleStatus, &scanner, &validator, &providerScope, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ScannerCapacity = scanner
		item.ValidatorCapacity = validator
		item.ProviderScope = providerScope
		if len(item.Capabilities) == 0 {
			item.Capabilities = json.RawMessage(`{}`)
		}
		if len(item.ModuleStatus) == 0 {
			item.ModuleStatus = json.RawMessage(`{}`)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListCoverageGroupCatalog(ctx context.Context, poolID string) ([]CoverageGroupCatalogItem, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	poolID = firstNonBlank(poolID, "pool.local")
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (pool_id, group_name)
		       pool_id, group_name, observed_at, low_watermark, high_watermark,
		       retention_days, confidence, author_node_id
		FROM coverage_group_observations
		WHERE pool_id = $1
		ORDER BY pool_id, group_name, observed_at DESC`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoverageGroupCatalogItem{}
	for rows.Next() {
		var item CoverageGroupCatalogItem
		if err := rows.Scan(&item.PoolID, &item.Group, &item.ObservedAt, &item.LowWatermark, &item.HighWatermark, &item.RetentionDays, &item.Confidence, &item.AuthorNodeID); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListValidationGaps(ctx context.Context, poolID string, limit int) ([]ValidationGap, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	poolID = firstNonBlank(poolID, "pool.local")
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT fs.release_id, COALESCE(fs.manifest_id, ''), fs.pool_id, fs.source_node_id,
		       MAX(t.created_at) AS last_validation_task_at,
		       COUNT(a.attestation_id)::int AS attestation_count
		FROM federated_release_sources fs
		LEFT JOIN federation_validation_tasks t
		  ON t.release_id = fs.release_id
		 AND t.manifest_id = fs.manifest_id
		 AND t.pool_id = fs.pool_id
		LEFT JOIN article_availability_attestations a
		  ON a.release_id = fs.release_id
		 AND a.manifest_id = fs.manifest_id
		 AND a.pool_id = fs.pool_id
		WHERE fs.pool_id = $1
		  AND fs.manifest_id IS NOT NULL
		GROUP BY fs.release_id, fs.manifest_id, fs.pool_id, fs.source_node_id
		HAVING COUNT(a.attestation_id) = 0
		ORDER BY MAX(t.created_at) NULLS FIRST, fs.release_id
		LIMIT $2`, poolID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ValidationGap{}
	for rows.Next() {
		var item ValidationGap
		var last nullableTime
		if err := rows.Scan(&item.ReleaseID, &item.ManifestID, &item.PoolID, &item.SourceNodeID, &last, &item.ValidationAttestationCount); err != nil {
			return nil, err
		}
		item.LastValidationTaskAt = last.ptr()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) MaterializeCoverageStaleClaimPenalties(ctx context.Context, poolID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		poolID = "pool.local"
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO coverage_stale_claim_penalties (
			claim_id, node_id, pool_id, group_name, expired_at, penalty_score, reason
		)
		SELECT c.claim_id, c.node_id, c.pool_id, c.group_name, c.expires_at,
		       0.05, 'stale_claim'
		FROM coverage_claims c
		WHERE c.pool_id = $1
		  AND c.status = 'active'
		  AND c.expires_at <= NOW()
		  AND NOT EXISTS (
		    SELECT 1 FROM coverage_range_outcomes o
		    WHERE o.claim_id = c.claim_id
		  )
		ON CONFLICT (claim_id) DO NOTHING`, poolID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
