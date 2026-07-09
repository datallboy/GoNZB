package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/health"
)

type HealthAttestationProjection struct {
	Attestation  health.Attestation
	EventID      string
	AuthorNodeID string
	PoolID       string
}

type FederatedScoreRecomputeResult struct {
	PoolID        string `json:"pool_id"`
	SourceUpdates int64  `json:"source_updates"`
	CardUpdates   int64  `json:"card_updates"`
}

func (s *Store) ProjectHealthAttestation(ctx context.Context, projection HealthAttestationProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	item := projection.Attestation
	if err := health.Validate(item, time.Now().UTC(), 2*time.Minute); err != nil {
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
	poolID := strings.TrimSpace(projection.PoolID)
	if poolID == "" {
		poolID = "pool.local"
	}

	bodyJSON, err := json.Marshal(item)
	if err != nil {
		return err
	}
	checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.CheckedAt))
	if err != nil {
		return err
	}
	providerBackboneHash := ""
	if item.ProviderScope.ProviderBackboneHash != nil {
		providerBackboneHash = strings.TrimSpace(*item.ProviderScope.ProviderBackboneHash)
	}
	availabilityScore := health.AvailabilityScore(item)
	delta, reason := health.TrustDelta(item)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO health_attestations (
			attestation_id, manifest_id, release_id, author_node_id, pool_id,
			checked_at, status, articles_total, articles_available,
			missing_articles, repair_available, repair_confidence,
			provider_backbone_hash, retention_days_observed, confidence,
			availability_score, method, body_json, source_event_id, updated_at
		)
		VALUES (
			$1, NULLIF($2, ''), $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12,
			NULLIF($13, ''), $14, $15,
			$16, NULLIF($17, ''), $18::jsonb, $19, NOW()
		)
		ON CONFLICT (attestation_id) DO UPDATE SET
			manifest_id = EXCLUDED.manifest_id,
			release_id = EXCLUDED.release_id,
			author_node_id = EXCLUDED.author_node_id,
			pool_id = EXCLUDED.pool_id,
			checked_at = EXCLUDED.checked_at,
			status = EXCLUDED.status,
			articles_total = EXCLUDED.articles_total,
			articles_available = EXCLUDED.articles_available,
			missing_articles = EXCLUDED.missing_articles,
			repair_available = EXCLUDED.repair_available,
			repair_confidence = EXCLUDED.repair_confidence,
			provider_backbone_hash = EXCLUDED.provider_backbone_hash,
			retention_days_observed = EXCLUDED.retention_days_observed,
			confidence = EXCLUDED.confidence,
			availability_score = EXCLUDED.availability_score,
			method = EXCLUDED.method,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		eventID,
		item.ManifestID,
		item.ReleaseID,
		authorNodeID,
		poolID,
		checkedAt.UTC(),
		item.Status,
		item.ArticlesTotal,
		item.ArticlesAvailable,
		item.MissingArticles,
		item.RepairAvailable,
		item.RepairConfidence,
		providerBackboneHash,
		item.ProviderScope.RetentionDaysObserved,
		item.Confidence,
		availabilityScore,
		item.Method,
		string(bodyJSON),
		eventID,
	); err != nil {
		return fmt.Errorf("insert health attestation: %w", err)
	}

	if delta != 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO reputation_events (node_id, pool_id, event_id, delta, reason)
			VALUES ($1, $2, $3, $4, $5)`,
			authorNodeID,
			poolID,
			eventID,
			delta,
			reason,
		); err != nil {
			return fmt.Errorf("insert reputation event: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE federation_nodes
			SET local_trust_score = LEAST(1.0, GREATEST(0.0,
					CASE WHEN local_trust_score = 0 THEN 1.0 ELSE local_trust_score END + $2
				)),
			    updated_at = NOW()
			WHERE node_id = $1`,
			authorNodeID,
			delta,
		); err != nil {
			return fmt.Errorf("update node trust score: %w", err)
		}
	}

	if err := recomputeFederatedReleaseHealthScores(ctx, tx, item.ReleaseID, poolID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecomputeFederatedScores(ctx context.Context, poolID string) (FederatedScoreRecomputeResult, error) {
	out := FederatedScoreRecomputeResult{PoolID: strings.TrimSpace(poolID)}
	if s == nil || s.db == nil {
		return out, fmt.Errorf("pgindex store is not initialized")
	}
	if out.PoolID == "" {
		out.PoolID = "pool.local"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		WITH health AS (
			SELECT release_id, pool_id, AVG(availability_score) AS availability_score
			FROM health_attestations
			WHERE pool_id = $1
			GROUP BY release_id, pool_id
		),
		article_scores AS (
			SELECT release_id, manifest_id, pool_id,
			       AVG(validation_score) AS validation_score,
			       COUNT(*) AS validation_attestation_count
			FROM article_availability_attestations
			WHERE pool_id = $1
			GROUP BY release_id, manifest_id, pool_id
		),
		checksum_scores AS (
			SELECT release_id, manifest_id, pool_id,
			       AVG(checksum_score) AS checksum_score
			FROM checksum_attestations
			WHERE pool_id = $1
			GROUP BY release_id, manifest_id, pool_id
		),
		validation AS (
			SELECT
				COALESCE(a.release_id, c.release_id) AS release_id,
				COALESCE(a.manifest_id, c.manifest_id) AS manifest_id,
				COALESCE(a.pool_id, c.pool_id) AS pool_id,
				a.validation_score,
				a.validation_attestation_count,
				c.checksum_score
			FROM article_scores a
			FULL OUTER JOIN checksum_scores c
			  ON c.release_id = a.release_id
			 AND c.manifest_id = a.manifest_id
			 AND c.pool_id = a.pool_id
		)
		UPDATE federated_release_sources s
		SET trust_score = COALESCE(NULLIF(n.local_trust_score, 0), s.trust_score),
		    availability_score = GREATEST(
			s.availability_score,
			COALESCE(health.availability_score, 0),
			COALESCE(validation.validation_score, 0)
		    ),
		    validation_score = COALESCE(validation.validation_score, s.validation_score),
		    validation_attestation_count = COALESCE(validation.validation_attestation_count, s.validation_attestation_count),
		    checksum_score = COALESCE(validation.checksum_score, s.checksum_score),
		    last_seen_at = NOW()
		FROM federated_release_sources src
		LEFT JOIN health
		  ON health.release_id = src.release_id
		 AND health.pool_id = src.pool_id
		LEFT JOIN validation
		  ON validation.release_id = src.release_id
		 AND validation.pool_id = src.pool_id
		 AND COALESCE(validation.manifest_id, '') = COALESCE(src.manifest_id, '')
		LEFT JOIN federation_nodes n
		  ON n.node_id = src.source_node_id
		WHERE s.release_id = src.release_id
		  AND s.source_node_id = src.source_node_id
		  AND s.pool_id = src.pool_id
		  AND s.pool_id = $1`,
		out.PoolID,
	)
	if err != nil {
		return out, fmt.Errorf("recompute federated release source scores: %w", err)
	}
	out.SourceUpdates, _ = result.RowsAffected()

	result, err = tx.ExecContext(ctx, `
		WITH affected AS (
			SELECT DISTINCT release_id
			FROM federated_release_sources
			WHERE pool_id = $1
		),
		ranked AS (
			SELECT
				s.release_id,
				MAX(s.availability_score) AS availability_score,
				MAX(s.manifest_confidence_score) AS manifest_confidence_score,
				MAX(s.trust_score) AS trust_score,
				BOOL_OR(s.resolvable) AS resolvable,
				LEAST(1.0, COUNT(*)::double precision / 3.0) AS quorum_score
			FROM federated_release_sources s
			JOIN affected ON affected.release_id = s.release_id
			GROUP BY s.release_id
		)
		UPDATE federated_release_cards c
		SET availability_score = ranked.availability_score,
		    manifest_confidence_score = ranked.manifest_confidence_score,
		    trust_score = ranked.trust_score,
		    resolvable = ranked.resolvable,
		    best_score = LEAST(1.0, GREATEST(0.0,
				(0.35 * ranked.trust_score) +
				(0.25 * ranked.manifest_confidence_score) +
				(0.25 * ranked.availability_score) +
				(0.10 * ranked.quorum_score) +
				(0.05 * CASE
					WHEN c.posted_at IS NULL THEN 0.5
					WHEN c.posted_at > NOW() - INTERVAL '14 days' THEN 1.0
					WHEN c.posted_at > NOW() - INTERVAL '90 days' THEN 0.5
					ELSE 0.1
				END)
		    )),
		    updated_at = NOW()
		FROM ranked
		WHERE c.release_id = ranked.release_id`,
		out.PoolID,
	)
	if err != nil {
		return out, fmt.Errorf("recompute federated release card scores: %w", err)
	}
	out.CardUpdates, _ = result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

type healthScoreExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func recomputeFederatedReleaseHealthScores(ctx context.Context, exec healthScoreExecutor, releaseID, poolID string) error {
	if _, err := exec.ExecContext(ctx, `
		WITH health AS (
			SELECT release_id, pool_id, AVG(availability_score) AS availability_score, COUNT(*) AS attestation_count
			FROM health_attestations
			WHERE release_id = $1
			  AND pool_id = $2
			GROUP BY release_id, pool_id
		)
		UPDATE federated_release_sources s
		SET availability_score = health.availability_score,
		    trust_score = COALESCE(NULLIF(n.local_trust_score, 0), s.trust_score),
		    last_seen_at = NOW()
		FROM health, federation_nodes n
		WHERE s.release_id = health.release_id
		  AND s.pool_id = health.pool_id
		  AND n.node_id = s.source_node_id`,
		strings.TrimSpace(releaseID),
		strings.TrimSpace(poolID),
	); err != nil {
		return fmt.Errorf("recompute federated release source health scores: %w", err)
	}

	if _, err := exec.ExecContext(ctx, `
		WITH ranked AS (
			SELECT
				release_id,
				MAX(availability_score) AS availability_score,
				MAX(manifest_confidence_score) AS manifest_confidence_score,
				MAX(trust_score) AS trust_score,
				LEAST(1.0, COUNT(*)::double precision / 3.0) AS quorum_score
			FROM federated_release_sources
			WHERE release_id = $1
			GROUP BY release_id
		)
		UPDATE federated_release_cards c
		SET availability_score = ranked.availability_score,
		    manifest_confidence_score = ranked.manifest_confidence_score,
		    trust_score = ranked.trust_score,
		    best_score = LEAST(1.0, GREATEST(0.0,
				(0.35 * ranked.trust_score) +
				(0.25 * ranked.manifest_confidence_score) +
				(0.25 * ranked.availability_score) +
				(0.10 * ranked.quorum_score) +
				(0.05 * CASE
					WHEN c.posted_at IS NULL THEN 0.5
					WHEN c.posted_at > NOW() - INTERVAL '14 days' THEN 1.0
					WHEN c.posted_at > NOW() - INTERVAL '90 days' THEN 0.5
					ELSE 0.1
				END)
		    )),
		    updated_at = NOW()
		FROM ranked
		WHERE c.release_id = ranked.release_id`,
		strings.TrimSpace(releaseID),
	); err != nil {
		return fmt.Errorf("recompute federated release card health scores: %w", err)
	}
	return nil
}
