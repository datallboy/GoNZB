package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	articleCohortDefaultBatchSize      = 50000
	articleCohortDefaultAssemblyLimit  = 20000
	articleCohortDefaultYEncLimit      = 25000
	articleCohortStatementTimeout      = 20 * time.Second
	articleCohortOpaqueMinSingletons   = 20
	articleCohortNoIdentityCooldown    = 30 * time.Minute
	articleCohortNoIdentityThreshold   = 100
	articleCohortOpaqueScanMultiplier  = 20
	articleCohortOpaqueScanMax         = 100000
	articleCohortSubjectScanMultiplier = 4
)

type ArticleCohortSchedulerRequest struct {
	BatchSize        int
	AssemblyQueueMax int
	YEncQueueMax     int
}

type ArticleCohortSchedulerResult struct {
	SubjectCohortsUpserted int64
	OpaqueCohortsUpserted  int64
	AssemblyQueued         int64
	YEncQueued             int64
	Duration               time.Duration
}

func (s *Store) RunArticleCohortScheduler(ctx context.Context, req ArticleCohortSchedulerRequest) (*ArticleCohortSchedulerResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if req.BatchSize <= 0 {
		req.BatchSize = articleCohortDefaultBatchSize
	}
	if req.AssemblyQueueMax <= 0 {
		req.AssemblyQueueMax = articleCohortDefaultAssemblyLimit
	}
	if req.YEncQueueMax <= 0 {
		req.YEncQueueMax = articleCohortDefaultYEncLimit
	}
	started := time.Now()
	out := &ArticleCohortSchedulerResult{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin article cohort scheduler tx: %w", err)
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = %d`, articleCohortStatementTimeout.Milliseconds())); err != nil {
		return nil, fmt.Errorf("set article cohort scheduler statement timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_hashjoin = off`); err != nil {
		return nil, fmt.Errorf("set article cohort scheduler hash join guard: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_mergejoin = off`); err != nil {
		return nil, fmt.Errorf("set article cohort scheduler merge join guard: %w", err)
	}
	var lockAcquired bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('gonzb-article-cohort-scheduler'))`).Scan(&lockAcquired); err != nil {
		return nil, fmt.Errorf("lock article cohort scheduler: %w", err)
	}
	if !lockAcquired {
		out.Duration = time.Since(started)
		return out, nil
	}

	subjectCohorts, assemblyQueued, err := runSubjectCompleteCohortSchedule(ctx, tx, req.BatchSize, req.AssemblyQueueMax)
	if err != nil {
		return nil, err
	}
	out.SubjectCohortsUpserted = subjectCohorts
	out.AssemblyQueued = assemblyQueued

	bucketSeconds, err := yEncOpaqueCohortBucketSecondsInTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	opaqueCohorts, yencQueued, err := runOpaqueYEncCohortSchedule(ctx, tx, req.BatchSize, req.YEncQueueMax, bucketSeconds)
	if err != nil {
		return nil, err
	}
	out.OpaqueCohortsUpserted = opaqueCohorts
	out.YEncQueued = yencQueued

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit article cohort scheduler tx: %w", err)
	}
	out.Duration = time.Since(started)
	return out, nil
}

func runSubjectCompleteCohortSchedule(ctx context.Context, tx *sql.Tx, batchSize, queueLimit int) (int64, int64, error) {
	if batchSize <= 0 || queueLimit <= 0 {
		return 0, 0, nil
	}
	if err := cleanupStaleArticleCohortAssemblyQueueInTx(ctx, tx, queueLimit); err != nil {
		return 0, 0, err
	}
	var openQueued int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_cohort_assembly_queue cq
		JOIN article_header_assembly_queue q
		  ON q.source_posted_at = cq.source_posted_at
		 AND q.article_header_id = cq.article_header_id
		WHERE cq.status IN ('ready', 'running')`).Scan(&openQueued); err != nil {
		return 0, 0, fmt.Errorf("count open subject-complete cohort assembly queue rows: %w", err)
	}
	if openQueued >= queueLimit {
		return 0, 0, nil
	}
	queueLimit -= openQueued

	scanLimit := queueLimit * articleCohortSubjectScanMultiplier
	if scanLimit < queueLimit {
		scanLimit = queueLimit
	}
	maxScanLimit := batchSize * articleCohortSubjectScanMultiplier
	if scanLimit > maxScanLimit {
		scanLimit = maxScanLimit
	}
	var cohorts int64
	if err := tx.QueryRowContext(ctx, `
		WITH recent AS MATERIALIZED (
			SELECT
				q.source_posted_at,
				q.article_header_id,
				q.provider_id,
				q.newsgroup_id,
				q.article_number,
				COALESCE(p.subject_file_name, '') AS subject_file_name,
				COALESCE(p.subject_file_index, 0) AS subject_file_index,
				COALESCE(p.subject_file_total, 0) AS subject_file_total,
				COALESCE(p.yenc_part_number, 0) AS yenc_part_number,
				COALESCE(p.yenc_total_parts, 0) AS yenc_total_parts,
				COALESCE(p.yenc_file_size, 0) AS yenc_file_size
			FROM article_header_assembly_queue q
			JOIN article_header_ingest_payloads p
			  ON p.source_posted_at = q.source_posted_at
			 AND p.article_header_id = q.article_header_id
			WHERE (q.claim_until IS NULL OR q.claim_until < NOW())
			  AND q.queue_kind = 'structured'
			  AND BTRIM(COALESCE(p.subject_file_name, '')) <> ''
			  AND COALESCE(p.subject_file_index, 0) > 0
			  AND COALESCE(p.subject_file_total, 0) > 0
			  AND COALESCE(p.yenc_part_number, 0) > 0
			  AND COALESCE(p.yenc_total_parts, 0) > 1
			  AND NOT EXISTS (
				SELECT 1
				FROM article_cohort_assembly_queue cq
				WHERE cq.source_posted_at = q.source_posted_at
				  AND cq.article_header_id = q.article_header_id
				  AND cq.status IN ('ready', 'running', 'done')
			  )
			ORDER BY q.article_header_id DESC
			LIMIT $1
		),
		cohorts AS MATERIALIZED (
			SELECT
				MIN(source_posted_at) AS source_posted_at,
				'subject:' || provider_id || ':' || newsgroup_id || ':' ||
					md5(LOWER(BTRIM(subject_file_name)) || ':' || subject_file_index || ':' || subject_file_total || ':' || yenc_total_parts || ':' || yenc_file_size) AS cohort_key,
				provider_id,
				newsgroup_id,
				MIN(source_posted_at) AS bucket_start,
				MAX(source_posted_at) + INTERVAL '1 second' AS bucket_end,
				COUNT(*)::integer AS article_count,
				COUNT(*)::integer AS unassembled_count,
				MAX(subject_file_name) AS subject_file_name,
				MAX(subject_file_index) AS subject_file_index,
				MAX(subject_file_total) AS subject_file_total,
				MAX(yenc_total_parts) AS yenc_total_parts,
				MAX(yenc_file_size) AS yenc_file_size,
				MIN(article_number) AS first_article_number,
				MAX(article_number) AS last_article_number
			FROM recent
			GROUP BY provider_id, newsgroup_id, LOWER(BTRIM(subject_file_name)), subject_file_index, subject_file_total, yenc_total_parts, yenc_file_size
		),
		upserted AS (
			INSERT INTO article_cohort_candidates (
				source_posted_at, cohort_key, provider_id, newsgroup_id, cohort_kind,
				priority_rank, admission_reason, score, status, bucket_start, bucket_end,
				article_count, unassembled_count, subject_file_name, subject_file_index,
				subject_file_total, yenc_total_parts, yenc_file_size, first_article_number,
				last_article_number, last_scheduled_at, updated_at
			)
			SELECT
				source_posted_at, cohort_key, provider_id, newsgroup_id, 'subject_complete',
				0, 'subject_complete_head', LEAST(1000000::double precision, article_count::double precision * 1000),
				'active', bucket_start, bucket_end, article_count, unassembled_count,
				subject_file_name, subject_file_index, subject_file_total, yenc_total_parts,
				yenc_file_size, first_article_number, last_article_number, NOW(), NOW()
			FROM cohorts
			ON CONFLICT (source_posted_at, cohort_key) DO UPDATE
			SET article_count = EXCLUDED.article_count,
			    unassembled_count = EXCLUDED.unassembled_count,
			    score = GREATEST(article_cohort_candidates.score, EXCLUDED.score),
			    status = CASE WHEN article_cohort_candidates.status = 'cooldown' AND article_cohort_candidates.cooldown_until > NOW() THEN article_cohort_candidates.status ELSE 'active' END,
			    last_scheduled_at = NOW(),
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM upserted`, scanLimit).Scan(&cohorts); err != nil {
		return 0, 0, fmt.Errorf("upsert subject-complete article cohorts: %w", err)
	}

	var queued int64
	if err := tx.QueryRowContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT
				r.source_posted_at,
				r.article_header_id,
				r.provider_id,
				r.newsgroup_id,
				'subject:' || r.provider_id || ':' || r.newsgroup_id || ':' ||
					md5(LOWER(BTRIM(r.subject_file_name)) || ':' || r.subject_file_index || ':' || r.subject_file_total || ':' || r.yenc_total_parts || ':' || r.yenc_file_size) AS cohort_key,
				r.article_number,
				r.subject_file_name,
				r.yenc_total_parts
			FROM (
				SELECT
					q.source_posted_at,
					q.article_header_id,
					q.provider_id,
					q.newsgroup_id,
					q.article_number,
					COALESCE(p.subject_file_name, '') AS subject_file_name,
					COALESCE(p.subject_file_index, 0) AS subject_file_index,
					COALESCE(p.subject_file_total, 0) AS subject_file_total,
					COALESCE(p.yenc_total_parts, 0) AS yenc_total_parts,
					COALESCE(p.yenc_file_size, 0) AS yenc_file_size,
					COALESCE(p.yenc_part_number, 0) AS yenc_part_number
				FROM article_header_assembly_queue q
				JOIN article_header_ingest_payloads p
				  ON p.source_posted_at = q.source_posted_at
				 AND p.article_header_id = q.article_header_id
				WHERE (q.claim_until IS NULL OR q.claim_until < NOW())
				  AND q.queue_kind = 'structured'
				  AND BTRIM(COALESCE(p.subject_file_name, '')) <> ''
				  AND COALESCE(p.subject_file_index, 0) > 0
				  AND COALESCE(p.subject_file_total, 0) > 0
				  AND COALESCE(p.yenc_part_number, 0) > 0
				  AND COALESCE(p.yenc_total_parts, 0) > 1
				  AND NOT EXISTS (
					SELECT 1
					FROM article_cohort_assembly_queue cq
					WHERE cq.source_posted_at = q.source_posted_at
					  AND cq.article_header_id = q.article_header_id
					  AND cq.status IN ('ready', 'running', 'done')
				  )
				ORDER BY q.article_header_id DESC
				LIMIT $1
			) r
		),
		inserted AS (
			INSERT INTO article_cohort_assembly_queue (
				source_posted_at, article_header_id, cohort_key, provider_id, newsgroup_id,
				cohort_kind, priority_rank, score, queue_reason, status, updated_at
			)
			SELECT
				source_posted_at, article_header_id, cohort_key, provider_id, newsgroup_id,
				'subject_complete', 0, 1000000::double precision, 'subject_complete_head',
				'ready', NOW()
			FROM candidates
			ORDER BY article_header_id DESC
			LIMIT $2
			ON CONFLICT (source_posted_at, article_header_id) DO UPDATE
			SET cohort_key = EXCLUDED.cohort_key,
			    priority_rank = LEAST(article_cohort_assembly_queue.priority_rank, EXCLUDED.priority_rank),
			    score = GREATEST(article_cohort_assembly_queue.score, EXCLUDED.score),
			    queue_reason = EXCLUDED.queue_reason,
			    status = CASE WHEN article_cohort_assembly_queue.status = 'done' THEN 'done' ELSE 'ready' END,
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM inserted`, scanLimit, queueLimit).Scan(&queued); err != nil {
		return 0, 0, fmt.Errorf("queue subject-complete cohort assembly rows: %w", err)
	}
	return cohorts, queued, nil
}

func cleanupStaleArticleCohortAssemblyQueueInTx(ctx context.Context, tx *sql.Tx, limit int) error {
	if tx == nil {
		return fmt.Errorf("article cohort assembly cleanup tx is required")
	}
	if limit <= 0 {
		limit = articleCohortDefaultAssemblyLimit
	}
	_, err := tx.ExecContext(ctx, `
		WITH stale AS MATERIALIZED (
			SELECT cq.source_posted_at, cq.article_header_id
			FROM article_cohort_assembly_queue cq
			WHERE cq.status IN ('ready', 'running')
			  AND NOT EXISTS (
				SELECT 1
				FROM article_header_assembly_queue q
				WHERE q.source_posted_at = cq.source_posted_at
				  AND q.article_header_id = cq.article_header_id
			  )
			ORDER BY cq.source_posted_at, cq.article_header_id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE article_cohort_assembly_queue cq
		SET status = 'done',
		    updated_at = NOW()
		FROM stale
		WHERE cq.source_posted_at = stale.source_posted_at
		  AND cq.article_header_id = stale.article_header_id`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("cleanup stale article cohort assembly queue rows: %w", err)
	}
	return nil
}

func runOpaqueYEncCohortSchedule(ctx context.Context, tx *sql.Tx, batchSize, queueLimit, bucketSeconds int) (int64, int64, error) {
	if batchSize <= 0 || queueLimit <= 0 {
		return 0, 0, nil
	}
	var openQueued int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_cohort_yenc_queue
		WHERE status IN ('ready', 'admitted')`).Scan(&openQueued); err != nil {
		return 0, 0, fmt.Errorf("count open opaque cohort yenc queue rows: %w", err)
	}
	if openQueued >= queueLimit {
		return 0, 0, nil
	}
	queueLimit -= openQueued

	scanLimit := queueLimit * articleCohortOpaqueScanMultiplier
	if scanLimit < queueLimit {
		scanLimit = queueLimit
	}
	maxScanLimit := batchSize * articleCohortOpaqueScanMultiplier
	if maxScanLimit > articleCohortOpaqueScanMax {
		maxScanLimit = articleCohortOpaqueScanMax
	}
	if scanLimit > maxScanLimit {
		scanLimit = maxScanLimit
	}
	if scanLimit <= 0 {
		return 0, 0, nil
	}

	var cohorts int64
	if err := tx.QueryRowContext(ctx, `
		WITH bos_recent AS MATERIALIZED (
			SELECT
				binary_id,
				provider_id,
				newsgroup_id,
				source_posted_at,
				posted_at,
				total_bytes,
				first_article_number,
				last_article_number
			FROM binary_observation_stats
			WHERE total_parts <= 1
			  AND observed_parts <= 1
			  AND posted_at IS NOT NULL
			ORDER BY posted_at DESC, source_posted_at DESC, binary_id DESC
			LIMIT $1
		),
		recent AS MATERIALIZED (
			SELECT
				bos.binary_id,
				bos.provider_id,
				bos.newsgroup_id,
				bos.source_posted_at,
				bos.posted_at,
				bos.total_bytes,
				bos.first_article_number,
				bos.last_article_number,
				FLOOR(EXTRACT(EPOCH FROM bos.posted_at) / $2::double precision)::bigint AS posted_bucket
			FROM bos_recent bos
			WHERE EXISTS (
				SELECT 1
				FROM binary_identity_current bic
				WHERE bic.source_posted_at = bos.source_posted_at
				  AND bic.binary_id = bos.binary_id
				  AND bic.family_kind = 'opaque_set'
				  AND bic.identity_reason = 'opaque_subject_set'
				  AND bic.is_main_payload = TRUE
				  AND bic.identity_strength IN ('weak', 'provisional')
			)
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_recovery_current brc
				WHERE brc.source_posted_at = bos.source_posted_at
				  AND brc.binary_id = bos.binary_id
				  AND brc.recovered_source = 'yenc_header'
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bos.source_posted_at
				  AND bl.binary_id = bos.binary_id
				  AND bl.lifecycle_status = 'superseded'
			  )
			ORDER BY bos.posted_at DESC, bos.source_posted_at DESC, bos.binary_id DESC
			LIMIT $1
		),
		grouped AS MATERIALIZED (
			SELECT
				MIN(source_posted_at) AS source_posted_at,
				'opaque:' || provider_id || ':' || newsgroup_id || ':' || posted_bucket AS cohort_key,
				provider_id,
				newsgroup_id,
				to_timestamp(posted_bucket * $2)::timestamptz AS bucket_start,
				to_timestamp((posted_bucket + 1) * $2)::timestamptz AS bucket_end,
				COUNT(*)::integer AS singleton_count,
				MIN(first_article_number) AS first_article_number,
				MAX(last_article_number) AS last_article_number,
				MAX(posted_at) AS latest_posted_at,
				MAX(total_bytes) AS max_total_bytes
			FROM recent
			GROUP BY provider_id, newsgroup_id, posted_bucket
			HAVING COUNT(*) >= $3
		),
		upserted AS (
			INSERT INTO article_cohort_candidates (
				source_posted_at, cohort_key, provider_id, newsgroup_id, cohort_kind,
				priority_rank, admission_reason, score, status, bucket_start, bucket_end,
				article_count, singleton_count, first_article_number, last_article_number,
				last_scheduled_at, updated_at
			)
			SELECT
				source_posted_at, cohort_key, provider_id, newsgroup_id, 'opaque_near_time',
				0, 'opaque_near_time_cohort',
				(singleton_count::double precision * 1000) + LEAST(999::double precision, COALESCE(max_total_bytes, 0)::double precision / 1000000),
				'active', bucket_start, bucket_end, singleton_count, singleton_count,
				first_article_number, last_article_number, NOW(), NOW()
			FROM grouped
			ON CONFLICT (source_posted_at, cohort_key) DO UPDATE
			SET article_count = EXCLUDED.article_count,
			    singleton_count = EXCLUDED.singleton_count,
			    score = GREATEST(article_cohort_candidates.score, EXCLUDED.score),
			    status = CASE WHEN article_cohort_candidates.status = 'cooldown' AND article_cohort_candidates.cooldown_until > NOW() THEN article_cohort_candidates.status ELSE 'active' END,
			    last_scheduled_at = NOW(),
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM upserted`, scanLimit, bucketSeconds, articleCohortOpaqueMinSingletons).Scan(&cohorts); err != nil {
		return 0, 0, fmt.Errorf("upsert opaque article cohorts: %w", err)
	}

	var queued int64
	if err := tx.QueryRowContext(ctx, `
		WITH bos_recent AS MATERIALIZED (
			SELECT
				binary_id,
				provider_id,
				newsgroup_id,
				source_posted_at,
				posted_at,
				total_bytes
			FROM binary_observation_stats
			WHERE total_parts <= 1
			  AND observed_parts <= 1
			  AND posted_at IS NOT NULL
			ORDER BY posted_at DESC, source_posted_at DESC, binary_id DESC
			LIMIT $1
		),
		recent AS MATERIALIZED (
			SELECT
				bos.binary_id,
				bp.article_header_id,
				bos.provider_id,
				bos.newsgroup_id,
				bos.source_posted_at,
				bos.posted_at,
				bos.total_bytes,
				FLOOR(EXTRACT(EPOCH FROM bos.posted_at) / $2::double precision)::bigint AS posted_bucket
			FROM bos_recent bos
			JOIN binary_parts bp
			  ON bp.source_posted_at = bos.source_posted_at
			  AND bp.binary_id = bos.binary_id
			WHERE EXISTS (
				SELECT 1
				FROM binary_identity_current bic
				WHERE bic.source_posted_at = bos.source_posted_at
				  AND bic.binary_id = bos.binary_id
				  AND bic.family_kind = 'opaque_set'
				  AND bic.identity_reason = 'opaque_subject_set'
				  AND bic.is_main_payload = TRUE
				  AND bic.identity_strength IN ('weak', 'provisional')
			)
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_recovery_current brc
				WHERE brc.source_posted_at = bos.source_posted_at
				  AND brc.binary_id = bos.binary_id
				  AND brc.recovered_source = 'yenc_header'
			  )
			  AND NOT EXISTS (
				SELECT 1
				FROM binary_lifecycle bl
				WHERE bl.source_posted_at = bos.source_posted_at
				  AND bl.binary_id = bos.binary_id
				  AND bl.lifecycle_status = 'superseded'
			  )
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM yenc_recovery_work_items wi
				WHERE wi.source_posted_at = bos.source_posted_at
				  AND wi.binary_id = bos.binary_id
			  	  AND wi.status IN ('ready', 'running', 'done')
			  )
			  AND NOT EXISTS (
			  	SELECT 1
			  	FROM article_cohort_yenc_queue cyq
				WHERE cyq.source_posted_at = bos.source_posted_at
				  AND cyq.binary_id = bos.binary_id
				  AND cyq.status IN ('ready', 'admitted', 'done')
			  )
			ORDER BY bos.posted_at DESC, bos.source_posted_at DESC, bos.binary_id DESC
			LIMIT $1
		),
		cohorts AS MATERIALIZED (
			SELECT
				MIN(source_posted_at) AS cohort_source_posted_at,
				'opaque:' || provider_id || ':' || newsgroup_id || ':' || posted_bucket AS cohort_key,
				provider_id,
				newsgroup_id,
				posted_bucket,
				COUNT(*) AS cohort_size
			FROM recent
			GROUP BY provider_id, newsgroup_id, posted_bucket
			HAVING COUNT(*) >= $3
		),
		inserted AS (
			INSERT INTO article_cohort_yenc_queue (
				source_posted_at, binary_id, article_header_id, cohort_key, provider_id,
				newsgroup_id, cohort_kind, priority_rank, admission_reason, score, status, updated_at
			)
			SELECT
				r.source_posted_at,
				r.binary_id,
				r.article_header_id,
				'opaque:' || r.provider_id || ':' || r.newsgroup_id || ':' || r.posted_bucket,
				r.provider_id,
				r.newsgroup_id,
				'opaque_near_time',
				0,
				'opaque_near_time_cohort',
				(c.cohort_size::double precision * 1000) + LEAST(999::double precision, COALESCE(r.total_bytes, 0)::double precision / 1000000),
				'ready',
				NOW()
			FROM recent r
			JOIN cohorts c
			  ON c.provider_id = r.provider_id
			 AND c.newsgroup_id = r.newsgroup_id
			 AND c.posted_bucket = r.posted_bucket
			JOIN article_cohort_candidates cc
			  ON cc.source_posted_at = c.cohort_source_posted_at
			 AND cc.cohort_key = c.cohort_key
			 AND NOT (cc.status = 'cooldown' AND cc.cooldown_until > NOW())
			ORDER BY c.cohort_size DESC, r.posted_at DESC, r.total_bytes DESC, r.binary_id
			LIMIT $4
			ON CONFLICT (source_posted_at, binary_id) DO UPDATE
			SET priority_rank = LEAST(article_cohort_yenc_queue.priority_rank, EXCLUDED.priority_rank),
			    admission_reason = EXCLUDED.admission_reason,
			    score = GREATEST(article_cohort_yenc_queue.score, EXCLUDED.score),
			    status = CASE WHEN article_cohort_yenc_queue.status = 'done' THEN 'done' ELSE 'ready' END,
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM inserted`, scanLimit, bucketSeconds, articleCohortOpaqueMinSingletons, queueLimit).Scan(&queued); err != nil {
		return 0, 0, fmt.Errorf("queue opaque cohort yenc rows: %w", err)
	}
	return cohorts, queued, nil
}

func recordArticleCohortYEncRecoveredInTx(ctx context.Context, tx *sql.Tx, articleHeaderIDs []int64) error {
	if tx == nil {
		return fmt.Errorf("article cohort yenc feedback tx is required")
	}
	articleHeaderIDs = dedupeYEncRecoveryInt64s(articleHeaderIDs)
	if len(articleHeaderIDs) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		WITH requested(article_header_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		),
		affected AS MATERIALIZED (
			SELECT cyq.provider_id, cyq.newsgroup_id, cyq.cohort_key, COUNT(*)::integer AS recovered_count
			FROM article_cohort_yenc_queue cyq
			JOIN requested r ON r.article_header_id = cyq.article_header_id
			WHERE cyq.status <> 'done'
			GROUP BY cyq.provider_id, cyq.newsgroup_id, cyq.cohort_key
		),
		marked AS (
			UPDATE article_cohort_yenc_queue cyq
			SET status = 'done',
			    updated_at = NOW()
			FROM requested r
			WHERE cyq.article_header_id = r.article_header_id
			  AND cyq.status <> 'done'
			RETURNING cyq.source_posted_at, cyq.cohort_key
		)
		UPDATE article_cohort_candidates c
		SET yenc_done_count = c.yenc_done_count + a.recovered_count,
		    yenc_recovered_count = c.yenc_recovered_count + a.recovered_count,
		    status = 'active',
		    cooldown_until = NULL,
		    score = LEAST(1000000000::double precision, c.score + (a.recovered_count::double precision * 500)),
		    updated_at = NOW()
		FROM affected a
		WHERE c.provider_id = a.provider_id
		  AND c.newsgroup_id = a.newsgroup_id
		  AND c.cohort_key = a.cohort_key`,
		articleHeaderIDs,
	)
	if err != nil {
		return fmt.Errorf("record article cohort yenc recovered feedback: %w", err)
	}
	return nil
}

func (s *Store) recordArticleCohortYEncNoIdentity(ctx context.Context, articleHeaderIDs []int64) error {
	articleHeaderIDs = dedupeYEncRecoveryInt64s(articleHeaderIDs)
	if len(articleHeaderIDs) == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		WITH requested(article_header_id) AS (
			SELECT DISTINCT unnest($1::bigint[])
		),
		affected AS MATERIALIZED (
			SELECT cyq.provider_id, cyq.newsgroup_id, cyq.cohort_key, COUNT(*)::integer AS no_identity_count
			FROM article_cohort_yenc_queue cyq
			JOIN requested r ON r.article_header_id = cyq.article_header_id
			WHERE cyq.status <> 'done'
			GROUP BY cyq.provider_id, cyq.newsgroup_id, cyq.cohort_key
		)
		UPDATE article_cohort_candidates c
		SET yenc_no_identity_count = c.yenc_no_identity_count + a.no_identity_count,
		    status = CASE
		        WHEN c.yenc_recovered_count = 0
		         AND c.yenc_no_identity_count + a.no_identity_count >= $2
		            THEN 'cooldown'
		        ELSE c.status
		    END,
		    cooldown_until = CASE
		        WHEN c.yenc_recovered_count = 0
		         AND c.yenc_no_identity_count + a.no_identity_count >= $2
		            THEN NOW() + ($3::bigint * INTERVAL '1 millisecond')
		        ELSE c.cooldown_until
		    END,
		    updated_at = NOW()
		FROM affected a
		WHERE c.provider_id = a.provider_id
		  AND c.newsgroup_id = a.newsgroup_id
		  AND c.cohort_key = a.cohort_key`,
		articleHeaderIDs,
		articleCohortNoIdentityThreshold,
		articleCohortNoIdentityCooldown.Milliseconds(),
	)
	if err != nil {
		return fmt.Errorf("record article cohort yenc no-identity feedback: %w", err)
	}
	return nil
}
