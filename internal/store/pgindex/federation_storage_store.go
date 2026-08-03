package pgindex

import (
	"context"
	"fmt"
)

type FederationStorageRelation struct {
	Name          string `json:"name"`
	Category      string `json:"category"`
	EstimatedRows int64  `json:"estimated_rows"`
	TotalBytes    int64  `json:"total_bytes"`
}

type FederationStorageReport struct {
	Available       bool                        `json:"available"`
	DatabaseBytes   int64                       `json:"database_bytes"`
	GoNZBNetBytes   int64                       `json:"gonzbnet_bytes"`
	ProtocolBytes   int64                       `json:"protocol_bytes"`
	ProjectionBytes int64                       `json:"projection_bytes"`
	EvidenceBytes   int64                       `json:"evidence_bytes"`
	Relations       []FederationStorageRelation `json:"relations"`
}

func (s *Store) GetFederationStorageReport(ctx context.Context) (FederationStorageReport, error) {
	report := FederationStorageReport{Relations: []FederationStorageRelation{}}
	if s == nil || s.db == nil {
		return report, fmt.Errorf("pgindex store is not initialized")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&report.DatabaseBytes); err != nil {
		return report, fmt.Errorf("read database size: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH requested(name, category) AS (
		  VALUES
		    ('federation_events', 'protocol'),
		    ('federation_rejected_events', 'protocol'),
		    ('federation_nonce_replay_cache', 'protocol'),
		    ('federation_nodes', 'protocol'),
		    ('federation_peer_deliveries', 'protocol'),
		    ('federated_release_cards', 'projection'),
		    ('federated_release_sources', 'projection'),
		    ('resolution_manifests', 'projection'),
		    ('federated_manifest_sources', 'projection'),
		    ('health_attestations', 'projection'),
		    ('article_availability_attestations', 'projection'),
		    ('federation_activity_rollups', 'projection'),
		    ('yenc_header_evidence', 'evidence'),
		    ('binary_exchange_identities', 'evidence'),
		    ('binary_peer_segments', 'evidence'),
		    ('binary_evidence_repair_work_items', 'evidence'),
		    ('binary_evidence_exchange_diagnostics', 'evidence')
		)
		SELECT requested.name,
		       requested.category,
		       GREATEST(COALESCE(class.reltuples::bigint, 0), 0),
		       COALESCE(pg_total_relation_size(class.oid), 0)
		FROM requested
		LEFT JOIN pg_class class
		  ON class.oid = to_regclass('public.' || requested.name)
		ORDER BY requested.category, COALESCE(pg_total_relation_size(class.oid), 0) DESC, requested.name`)
	if err != nil {
		return report, fmt.Errorf("read gonzbnet relation sizes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var relation FederationStorageRelation
		if err := rows.Scan(&relation.Name, &relation.Category, &relation.EstimatedRows, &relation.TotalBytes); err != nil {
			return FederationStorageReport{}, err
		}
		report.Relations = append(report.Relations, relation)
		report.GoNZBNetBytes += relation.TotalBytes
		switch relation.Category {
		case "protocol":
			report.ProtocolBytes += relation.TotalBytes
		case "projection":
			report.ProjectionBytes += relation.TotalBytes
		case "evidence":
			report.EvidenceBytes += relation.TotalBytes
		}
	}
	if err := rows.Err(); err != nil {
		return FederationStorageReport{}, err
	}
	report.Available = true
	return report, nil
}
