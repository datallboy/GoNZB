package pgindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
)

type ValidationTask struct {
	TaskID        int64
	ManifestID    string
	ReleaseID     string
	SourceNodeID  string
	SourceEventID string
	PoolID        string
	Attempts      int
}

type ValidationTaskRequest struct {
	ManifestID    string
	ReleaseID     string
	PoolID        string
	SourceNodeID  string
	SourceEventID string
	Priority      int
	DueAt         *time.Time
}

type ArticleAvailabilityProjection struct {
	Attestation  validation.ArticleAvailabilityAttestation
	EventID      string
	AuthorNodeID string
	PoolID       string
}

type ChecksumAttestationProjection struct {
	Attestation  validation.ChecksumAttestation
	EventID      string
	AuthorNodeID string
	PoolID       string
}

type ValidatorCapacityProjection struct {
	Capacity     validation.ValidatorCapacity
	EventID      string
	AuthorNodeID string
}

func (s *Store) EnqueueFederationValidationTask(ctx context.Context, request ValidationTaskRequest) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("pgindex store is not initialized")
	}
	manifestID := strings.TrimSpace(request.ManifestID)
	releaseID := strings.TrimSpace(request.ReleaseID)
	poolID := firstNonBlank(request.PoolID, "pool.local")
	if manifestID == "" || releaseID == "" {
		return false, fmt.Errorf("manifest_id and release_id are required")
	}
	dueAt := time.Now().UTC()
	if request.DueAt != nil {
		dueAt = request.DueAt.UTC()
	}
	var queued bool
	err := s.federationExecutor(ctx).QueryRowContext(ctx, `
		INSERT INTO federation_validation_tasks (
			manifest_id, release_id, source_node_id, source_event_id, pool_id,
			status, priority, due_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, 'pending', $6, $7, NOW())
		ON CONFLICT (manifest_id, pool_id) DO UPDATE SET
			release_id = EXCLUDED.release_id,
			source_node_id = COALESCE(EXCLUDED.source_node_id, federation_validation_tasks.source_node_id),
			source_event_id = COALESCE(EXCLUDED.source_event_id, federation_validation_tasks.source_event_id),
			status = CASE
				WHEN federation_validation_tasks.status = 'completed' THEN federation_validation_tasks.status
				ELSE 'pending'
			END,
			priority = GREATEST(federation_validation_tasks.priority, EXCLUDED.priority),
			due_at = CASE
				WHEN federation_validation_tasks.status = 'completed' THEN federation_validation_tasks.due_at
				WHEN federation_validation_tasks.due_at <= EXCLUDED.due_at THEN federation_validation_tasks.due_at
				ELSE EXCLUDED.due_at
			END,
			updated_at = NOW()
		RETURNING federation_validation_tasks.status <> 'completed'`,
		manifestID,
		releaseID,
		strings.TrimSpace(request.SourceNodeID),
		strings.TrimSpace(request.SourceEventID),
		poolID,
		request.Priority,
		dueAt,
	).Scan(&queued)
	if err != nil {
		return false, fmt.Errorf("enqueue validation task: %w", err)
	}
	return queued, nil
}

func (s *Store) ClaimValidationTasks(ctx context.Context, nodeID, poolID string, limit int) ([]ValidationTask, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 {
		limit = 25
	}
	rows, err := s.federationExecutor(ctx).QueryContext(ctx, `
		WITH picked AS (
			SELECT task_id
			FROM federation_validation_tasks
			WHERE status IN ('pending', 'failed')
			  AND pool_id = $3
			  AND due_at <= NOW()
			  AND attempts < 3
			ORDER BY priority DESC, due_at, task_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE federation_validation_tasks t
		SET status = 'claimed',
		    claimed_by_node_id = $1,
		    claimed_at = NOW(),
		    attempts = attempts + 1,
		    updated_at = NOW()
		FROM picked
		WHERE t.task_id = picked.task_id
		RETURNING t.task_id, t.manifest_id, t.release_id,
		          COALESCE(t.source_node_id, ''), COALESCE(t.source_event_id, ''),
		          t.pool_id, t.attempts`,
		strings.TrimSpace(nodeID),
		limit,
		firstNonBlank(poolID, "pool.local"),
	)
	if err != nil {
		return nil, fmt.Errorf("claim validation tasks: %w", err)
	}
	defer rows.Close()
	out := []ValidationTask{}
	for rows.Next() {
		var item ValidationTask
		if err := rows.Scan(&item.TaskID, &item.ManifestID, &item.ReleaseID, &item.SourceNodeID, &item.SourceEventID, &item.PoolID, &item.Attempts); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CompleteValidationTask(ctx context.Context, taskID int64, status, message string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "completed"
	}
	var completedAt any
	if status == "completed" {
		completedAt = time.Now().UTC()
	}
	_, err := s.federationExecutor(ctx).ExecContext(ctx, `
		UPDATE federation_validation_tasks
		SET status = $2,
		    last_error = NULLIF($3, ''),
		    completed_at = COALESCE($4, completed_at),
		    updated_at = NOW()
		WHERE task_id = $1`, taskID, status, strings.TrimSpace(message), completedAt)
	if err != nil {
		return fmt.Errorf("complete validation task: %w", err)
	}
	return nil
}

func (s *Store) ProjectValidatorCapacity(ctx context.Context, projection ValidatorCapacityProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if err := validation.ValidateCapacity(projection.Capacity, time.Now().UTC(), 2*time.Minute); err != nil {
		return err
	}
	bodyJSON, err := json.Marshal(projection.Capacity)
	if err != nil {
		return err
	}
	_, err = s.federationExecutor(ctx).ExecContext(ctx, `
		INSERT INTO federation_node_capabilities (
			node_id, capabilities, module_status, validator_capacity, updated_at
		)
		VALUES ($1, '{}'::jsonb, '{}'::jsonb, $2::jsonb, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			validator_capacity = EXCLUDED.validator_capacity,
			updated_at = NOW()`,
		strings.TrimSpace(projection.AuthorNodeID),
		string(bodyJSON),
	)
	if err != nil {
		return fmt.Errorf("project validator capacity: %w", err)
	}
	return nil
}

func (s *Store) ProjectArticleAvailabilityAttestation(ctx context.Context, projection ArticleAvailabilityProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	item := projection.Attestation
	if err := validation.ValidateArticleAvailability(item, time.Now().UTC(), 2*time.Minute); err != nil {
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
	poolID := firstNonBlank(projection.PoolID, "pool.local")
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
	score := validation.ArticleAvailabilityScore(item)

	tx, commit, rollback, err := s.beginFederationProjection(ctx)
	if err != nil {
		return err
	}
	defer rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO article_availability_attestations (
			attestation_id, release_id, manifest_id, author_node_id, pool_id,
			checked_at, status, articles_total, articles_available, missing_articles,
			provider_backbone_hash, retention_days_observed, confidence,
			validation_score, method, body_json, source_event_id, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			NULLIF($11, ''), $12, $13,
			$14, NULLIF($15, ''), $16::jsonb, $17, NOW()
		)
		ON CONFLICT (attestation_id) DO UPDATE SET
			checked_at = EXCLUDED.checked_at,
			status = EXCLUDED.status,
			articles_total = EXCLUDED.articles_total,
			articles_available = EXCLUDED.articles_available,
			missing_articles = EXCLUDED.missing_articles,
			provider_backbone_hash = EXCLUDED.provider_backbone_hash,
			retention_days_observed = EXCLUDED.retention_days_observed,
			confidence = EXCLUDED.confidence,
			validation_score = EXCLUDED.validation_score,
			method = EXCLUDED.method,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		eventID,
		item.ReleaseID,
		item.ManifestID,
		authorNodeID,
		poolID,
		checkedAt.UTC(),
		item.Status,
		item.ArticlesTotal,
		item.ArticlesAvailable,
		item.MissingArticles,
		providerBackboneHash,
		item.ProviderScope.RetentionDaysObserved,
		item.Confidence,
		score,
		item.Method,
		string(bodyJSON),
		eventID,
	); err != nil {
		return fmt.Errorf("insert article availability attestation: %w", err)
	}
	if err := recomputeValidationScores(ctx, tx, item.ReleaseID, item.ManifestID, poolID); err != nil {
		return err
	}
	return commit()
}

func (s *Store) ProjectChecksumAttestation(ctx context.Context, projection ChecksumAttestationProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	item := projection.Attestation
	if err := validation.ValidateChecksum(item, time.Now().UTC(), 2*time.Minute); err != nil {
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
	poolID := firstNonBlank(projection.PoolID, "pool.local")
	bodyJSON, err := json.Marshal(item)
	if err != nil {
		return err
	}
	checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.CheckedAt))
	if err != nil {
		return err
	}
	score := validation.ChecksumScore(item)

	tx, commit, rollback, err := s.beginFederationProjection(ctx)
	if err != nil {
		return err
	}
	defer rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO checksum_attestations (
			attestation_id, release_id, manifest_id, author_node_id, pool_id,
			checked_at, status, checksums_total, checksums_verified,
			checksums_failed, confidence, checksum_score, method,
			body_json, source_event_id, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, NULLIF($13, ''),
			$14::jsonb, $15, NOW()
		)
		ON CONFLICT (attestation_id) DO UPDATE SET
			checked_at = EXCLUDED.checked_at,
			status = EXCLUDED.status,
			checksums_total = EXCLUDED.checksums_total,
			checksums_verified = EXCLUDED.checksums_verified,
			checksums_failed = EXCLUDED.checksums_failed,
			confidence = EXCLUDED.confidence,
			checksum_score = EXCLUDED.checksum_score,
			method = EXCLUDED.method,
			body_json = EXCLUDED.body_json,
			source_event_id = EXCLUDED.source_event_id,
			updated_at = NOW()`,
		eventID,
		item.ReleaseID,
		item.ManifestID,
		authorNodeID,
		poolID,
		checkedAt.UTC(),
		item.Status,
		item.ChecksumsTotal,
		item.ChecksumsVerified,
		item.ChecksumsFailed,
		item.Confidence,
		score,
		item.Method,
		string(bodyJSON),
		eventID,
	); err != nil {
		return fmt.Errorf("insert checksum attestation: %w", err)
	}
	if err := recomputeValidationScores(ctx, tx, item.ReleaseID, item.ManifestID, poolID); err != nil {
		return err
	}
	return commit()
}

func recomputeValidationScores(ctx context.Context, exec healthScoreExecutor, releaseID, manifestID, poolID string) error {
	if _, err := exec.ExecContext(ctx, `
		WITH article_scores AS (
			SELECT release_id, manifest_id, pool_id,
			       AVG(validation_score) AS validation_score,
			       COUNT(*) AS attestation_count
			FROM article_availability_attestations
			WHERE release_id = $1
			  AND manifest_id = $2
			  AND pool_id = $3
			GROUP BY release_id, manifest_id, pool_id
		),
		checksum_scores AS (
			SELECT release_id, manifest_id, pool_id,
			       AVG(checksum_score) AS checksum_score
			FROM checksum_attestations
			WHERE release_id = $1
			  AND manifest_id = $2
			  AND pool_id = $3
			GROUP BY release_id, manifest_id, pool_id
		)
		UPDATE federated_release_sources s
		SET validation_score = COALESCE(article_scores.validation_score, s.validation_score),
		    validation_attestation_count = COALESCE(article_scores.attestation_count, s.validation_attestation_count),
		    checksum_score = COALESCE(checksum_scores.checksum_score, s.checksum_score),
		    availability_score = GREATEST(s.availability_score, COALESCE(article_scores.validation_score, 0)),
		    last_seen_at = NOW()
		FROM article_scores
		LEFT JOIN checksum_scores
		  ON checksum_scores.release_id = article_scores.release_id
		 AND checksum_scores.manifest_id = article_scores.manifest_id
		 AND checksum_scores.pool_id = article_scores.pool_id
		WHERE s.release_id = article_scores.release_id
		  AND s.manifest_id = article_scores.manifest_id
		  AND s.pool_id = article_scores.pool_id`,
		strings.TrimSpace(releaseID),
		strings.TrimSpace(manifestID),
		strings.TrimSpace(poolID),
	); err != nil {
		return fmt.Errorf("recompute federated source validation scores: %w", err)
	}
	if err := recomputeFederatedReleaseHealthScores(ctx, exec, releaseID, poolID); err != nil {
		return err
	}
	return nil
}
