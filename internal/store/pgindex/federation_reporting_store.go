package pgindex

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type FederationEvidenceSummary struct {
	Total       int64            `json:"total"`
	Fresh       int64            `json:"fresh"`
	Aging       int64            `json:"aging"`
	Stale       int64            `json:"stale"`
	Reporters   int64            `json:"reporters"`
	Statuses    map[string]int64 `json:"statuses"`
	LastChecked *time.Time       `json:"last_checked_at,omitempty"`
}

type FederationMemberContribution struct {
	NodeID              string     `json:"node_id"`
	Alias               string     `json:"alias"`
	ReleaseCards        int64      `json:"release_cards"`
	Manifests           int64      `json:"manifests"`
	HealthAttestations  int64      `json:"health_attestations"`
	ArticleAvailability int64      `json:"article_availability"`
	CoverageEvents      int64      `json:"coverage_events"`
	TotalEvents         int64      `json:"total_events"`
	LastContributionAt  *time.Time `json:"last_contribution_at,omitempty"`
}

type FederationPoolHealthReport struct {
	PoolID              string                         `json:"pool_id"`
	GeneratedAt         time.Time                      `json:"generated_at"`
	FreshBefore         time.Time                      `json:"fresh_before"`
	StaleBefore         time.Time                      `json:"stale_before"`
	ReleaseHealth       FederationEvidenceSummary      `json:"release_health"`
	ArticleAvailability FederationEvidenceSummary      `json:"article_availability"`
	Contributors        []FederationMemberContribution `json:"contributors"`
}

func (s *Store) GetFederationPoolHealthReport(ctx context.Context, poolID string, now time.Time) (FederationPoolHealthReport, error) {
	poolID = strings.TrimSpace(poolID)
	if s == nil || s.db == nil {
		return FederationPoolHealthReport{}, fmt.Errorf("pgindex store is not initialized")
	}
	if poolID == "" {
		return FederationPoolHealthReport{}, fmt.Errorf("pool_id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := FederationPoolHealthReport{
		PoolID: poolID, GeneratedAt: now.UTC(),
		FreshBefore: now.Add(-2 * time.Hour), StaleBefore: now.Add(-24 * time.Hour),
		ReleaseHealth:       FederationEvidenceSummary{Statuses: map[string]int64{}},
		ArticleAvailability: FederationEvidenceSummary{Statuses: map[string]int64{}},
		Contributors:        []FederationMemberContribution{},
	}
	if err := s.loadEvidenceSummary(ctx, `health_attestations`, poolID, out.FreshBefore, out.StaleBefore, &out.ReleaseHealth); err != nil {
		return FederationPoolHealthReport{}, err
	}
	if err := s.loadEvidenceSummary(ctx, `article_availability_attestations`, poolID, out.FreshBefore, out.StaleBefore, &out.ArticleAvailability); err != nil {
		return FederationPoolHealthReport{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.author_node_id, COALESCE(n.alias, ''),
		       COUNT(*) FILTER (WHERE e.event_type = 'ReleaseCard'),
		       COUNT(*) FILTER (WHERE e.event_type = 'ResolutionManifest'),
		       COUNT(*) FILTER (WHERE e.event_type = 'HealthAttestation'),
		       COUNT(*) FILTER (WHERE e.event_type = 'ArticleAvailabilityAttestation'),
		       COUNT(*) FILTER (WHERE e.event_type IN ('CoveragePlan','CoverageAssignment','RangeClaim','TimeWindowClaim','CoverageCheckpoint','RangeComplete','RangeFailed')),
		       COUNT(*), MAX(e.received_at)
		FROM federation_events e
		LEFT JOIN federation_nodes n ON n.node_id = e.author_node_id
		WHERE e.pool_ids ? $1 AND e.received_at >= $2
		GROUP BY e.author_node_id, n.alias
		ORDER BY MAX(e.received_at) DESC`, poolID, now.Add(-30*24*time.Hour))
	if err != nil {
		return FederationPoolHealthReport{}, fmt.Errorf("list federation pool contributors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item FederationMemberContribution
		var lastContribution nullableTime
		if err := rows.Scan(
			&item.NodeID, &item.Alias, &item.ReleaseCards, &item.Manifests,
			&item.HealthAttestations, &item.ArticleAvailability,
			&item.CoverageEvents, &item.TotalEvents, &lastContribution,
		); err != nil {
			return FederationPoolHealthReport{}, err
		}
		item.LastContributionAt = lastContribution.ptr()
		out.Contributors = append(out.Contributors, item)
	}
	return out, rows.Err()
}

func (s *Store) loadEvidenceSummary(ctx context.Context, table, poolID string, freshBefore, staleBefore time.Time, out *FederationEvidenceSummary) error {
	query := fmt.Sprintf(`
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE checked_at >= $2),
		       COUNT(*) FILTER (WHERE checked_at < $2 AND checked_at >= $3),
		       COUNT(*) FILTER (WHERE checked_at < $3),
		       COUNT(DISTINCT author_node_id), MAX(checked_at)
		FROM %s WHERE pool_id = $1`, table)
	var lastChecked nullableTime
	if err := s.db.QueryRowContext(ctx, query, poolID, freshBefore, staleBefore).Scan(
		&out.Total, &out.Fresh, &out.Aging, &out.Stale, &out.Reporters, &lastChecked,
	); err != nil {
		return fmt.Errorf("summarize %s: %w", table, err)
	}
	out.LastChecked = lastChecked.ptr()
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT status, COUNT(*) FROM %s WHERE pool_id = $1 GROUP BY status ORDER BY status`, table), poolID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		out.Statuses[status] = count
	}
	return rows.Err()
}
