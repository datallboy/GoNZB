package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	articleCohortDefaultBatchSize      = 50000
	articleCohortDefaultAssemblyLimit  = 20000
	articleCohortDefaultYEncLimit      = 25000
	articleCohortSubjectRunLimit       = 1000
	articleCohortStatementTimeout      = 20 * time.Second
	articleCohortOpaqueMinSingletons   = 20
	articleCohortNoIdentityCooldown    = 30 * time.Minute
	articleCohortOpaqueScanMultiplier  = 20
	articleCohortOpaqueScanMax         = 100000
	articleCohortSubjectScanMultiplier = 4
)

type ArticleCohortSchedulerRequest struct {
	BatchSize         int
	AssemblyQueueMax  int
	YEncQueueMax      int
	TargetWindowStart *time.Time
	TargetWindowEnd   *time.Time
	DisableYEnc       bool
}

func (r ArticleCohortSchedulerRequest) HasTargetWindow() bool {
	return r.TargetWindowStart != nil &&
		r.TargetWindowEnd != nil &&
		r.TargetWindowStart.Before(*r.TargetWindowEnd)
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
	if err := s.provisionSchedulerPartitionsForReadyWork(ctx, 32); err != nil {
		return nil, err
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
	var lockAcquired bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('gonzb-article-cohort-scheduler'))`).Scan(&lockAcquired); err != nil {
		return nil, fmt.Errorf("lock article cohort scheduler: %w", err)
	}
	if !lockAcquired {
		out.Duration = time.Since(started)
		return out, nil
	}

	subjectCohorts, assemblyQueued, err := runSubjectCompleteCohortSchedule(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	out.SubjectCohortsUpserted = subjectCohorts
	out.AssemblyQueued = assemblyQueued

	if !req.DisableYEnc {
		bucketSeconds, err := yEncOpaqueCohortBucketSecondsInTx(ctx, tx)
		if err != nil {
			return nil, err
		}
		opaqueCohorts, yencQueued, err := runOpaqueYEncCohortSchedule(ctx, tx, req, bucketSeconds)
		if err != nil {
			return nil, err
		}
		out.OpaqueCohortsUpserted = opaqueCohorts
		out.YEncQueued = yencQueued
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit article cohort scheduler tx: %w", err)
	}
	out.Duration = time.Since(started)
	return out, nil
}

func runSubjectCompleteCohortSchedule(ctx context.Context, tx *sql.Tx, req ArticleCohortSchedulerRequest) (int64, int64, error) {
	batchSize := req.BatchSize
	queueLimit := req.AssemblyQueueMax
	if batchSize <= 0 || queueLimit <= 0 {
		return 0, 0, nil
	}
	effectiveReq := req
	if !effectiveReq.HasTargetWindow() {
		start, end, ok, err := selectSubjectCohortSourceDayInTx(ctx, tx)
		if err != nil {
			return 0, 0, err
		}
		if !ok {
			return 0, 0, nil
		}
		effectiveReq.TargetWindowStart = &start
		effectiveReq.TargetWindowEnd = &end
	}
	if err := cleanupStaleArticleCohortAssemblyQueueInTx(ctx, tx, queueLimit, effectiveReq); err != nil {
		return 0, 0, err
	}
	targetStart, targetEnd := articleCohortTargetWindowArgs(effectiveReq)
	var openQueued int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM article_cohort_assembly_queue cq
		JOIN article_header_assembly_queue q
		  ON q.source_posted_at = cq.source_posted_at
		 AND q.article_header_id = cq.article_header_id
		WHERE cq.status IN ('ready', 'running')
		  AND (
		    NOT $1::boolean
		    OR (cq.source_posted_at >= $2 AND cq.source_posted_at < $3)
		  )`,
		effectiveReq.HasTargetWindow(), targetStart, targetEnd,
	).Scan(&openQueued); err != nil {
		return 0, 0, fmt.Errorf("count open subject-complete cohort assembly queue rows: %w", err)
	}
	if openQueued >= queueLimit {
		return 0, 0, nil
	}
	queueLimit -= openQueued
	queueLimit = subjectCohortRunLimit(queueLimit)

	effectiveWindow := effectiveReq.HasTargetWindow()
	scanLimit := queueLimit * articleCohortSubjectScanMultiplier
	if scanLimit < queueLimit {
		scanLimit = queueLimit
	}
	maxScanLimit := batchSize * articleCohortSubjectScanMultiplier
	if scanLimit > maxScanLimit {
		scanLimit = maxScanLimit
	}
	var cohorts, queued int64
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
			  AND (
			    NOT $3::boolean
			    OR (q.source_posted_at >= $4 AND q.source_posted_at < $5)
			  )
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
			ORDER BY q.source_posted_at DESC, q.article_header_id DESC
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
		),
		queue_rows AS MATERIALIZED (
			SELECT
				source_posted_at,
				article_header_id,
				provider_id,
				newsgroup_id,
				'subject:' || provider_id || ':' || newsgroup_id || ':' ||
					md5(LOWER(BTRIM(subject_file_name)) || ':' || subject_file_index || ':' || subject_file_total || ':' || yenc_total_parts || ':' || yenc_file_size) AS cohort_key
			FROM recent
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
			FROM queue_rows
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
		SELECT
			(SELECT COUNT(*) FROM upserted),
			(SELECT COUNT(*) FROM inserted)`,
		scanLimit, queueLimit, effectiveWindow, targetStart, targetEnd,
	).Scan(&cohorts, &queued); err != nil {
		return 0, 0, fmt.Errorf("schedule subject-complete article cohorts: %w", err)
	}
	return cohorts, queued, nil
}

func selectSubjectCohortSourceDayInTx(ctx context.Context, tx *sql.Tx) (time.Time, time.Time, bool, error) {
	if tx == nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("subject cohort source day tx is required")
	}
	var day sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT date_trunc('day', q.source_posted_at)::timestamptz
		FROM article_header_assembly_queue q
		WHERE q.queue_kind = 'structured'
		  AND (q.claim_until IS NULL OR q.claim_until < NOW())
		ORDER BY q.source_posted_at DESC, q.article_header_id DESC
		LIMIT 1`).Scan(&day); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, time.Time{}, false, nil
		}
		return time.Time{}, time.Time{}, false, fmt.Errorf("select subject cohort source day: %w", err)
	}
	start := day.Time.UTC()
	return start, start.Add(24 * time.Hour), true, nil
}

func articleCohortTargetWindowArgs(req ArticleCohortSchedulerRequest) (time.Time, time.Time) {
	if !req.HasTargetWindow() {
		return time.Time{}, time.Time{}
	}
	return req.TargetWindowStart.UTC(), req.TargetWindowEnd.UTC()
}

func subjectCohortRunLimit(queueCapacity int) int {
	if queueCapacity <= 0 {
		return 0
	}
	if queueCapacity > articleCohortSubjectRunLimit {
		return articleCohortSubjectRunLimit
	}
	return queueCapacity
}

func cleanupStaleArticleCohortAssemblyQueueInTx(
	ctx context.Context,
	tx *sql.Tx,
	limit int,
	req ArticleCohortSchedulerRequest,
) error {
	if tx == nil {
		return fmt.Errorf("article cohort assembly cleanup tx is required")
	}
	if limit <= 0 {
		limit = articleCohortDefaultAssemblyLimit
	}
	targetStart, targetEnd := articleCohortTargetWindowArgs(req)
	_, err := tx.ExecContext(ctx, `
		WITH stale AS MATERIALIZED (
			SELECT cq.source_posted_at, cq.article_header_id
			FROM article_cohort_assembly_queue cq
			WHERE cq.status IN ('ready', 'running')
			  AND (
			    NOT $2::boolean
			    OR (cq.source_posted_at >= $3 AND cq.source_posted_at < $4)
			  )
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
		limit, req.HasTargetWindow(), targetStart, targetEnd,
	)
	if err != nil {
		return fmt.Errorf("cleanup stale article cohort assembly queue rows: %w", err)
	}
	return nil
}

func runOpaqueYEncCohortSchedule(ctx context.Context, tx *sql.Tx, req ArticleCohortSchedulerRequest, bucketSeconds int) (int64, int64, error) {
	batchSize := req.BatchSize
	queueLimit := req.YEncQueueMax
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
	openRecovery, err := countOpenYEncRecoveryWorkItemsInTx(ctx, tx, queueLimit)
	if err != nil {
		return 0, 0, fmt.Errorf("count open yenc recovery work items for opaque cohort schedule: %w", err)
	}
	if openRecovery >= queueLimit {
		return 0, 0, nil
	}
	queueLimit -= openRecovery

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
	dayStart, dayEnd := articleCohortTargetWindowArgs(req)
	if !req.HasTargetWindow() {
		var ok bool
		var err error
		dayStart, dayEnd, ok, err = selectOpaqueCohortSourceDayInTx(ctx, tx)
		if err != nil {
			return 0, 0, err
		}
		if !ok {
			return 0, 0, nil
		}
	}
	scanKey := fmt.Sprintf("opaque:%s:%s", dayStart.UTC().Format(time.RFC3339Nano), dayEnd.UTC().Format(time.RFC3339Nano))
	cursorPostedAt, cursorBinaryID, err := lockArticleCohortScanCursorInTx(ctx, tx, scanKey, dayStart, dayEnd)
	if err != nil {
		return 0, 0, err
	}
	cursorEnabled := cursorPostedAt.Valid && cursorBinaryID.Valid

	var cohorts int64
	var pageLastPostedAt sql.NullTime
	var pageLastBinaryID sql.NullInt64
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
			  AND source_posted_at >= $4
			  AND source_posted_at < $5
			  AND (
				NOT $6::boolean
				OR (posted_at, binary_id) < ($7::timestamptz, $8::bigint)
			  )
			ORDER BY posted_at DESC, binary_id DESC
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
			    recovery_decision = CASE
					WHEN EXCLUDED.article_count > article_cohort_candidates.decision_article_count
					 AND article_cohort_candidates.recovery_decision = 'no_yield'
					THEN 'sample'
					ELSE article_cohort_candidates.recovery_decision
				END,
			    status = CASE
					WHEN EXCLUDED.article_count > article_cohort_candidates.decision_article_count
					 AND article_cohort_candidates.recovery_decision = 'no_yield'
					THEN 'active'
					WHEN article_cohort_candidates.status = 'cooldown'
					 AND article_cohort_candidates.cooldown_until > NOW()
					THEN article_cohort_candidates.status
					ELSE 'active'
				END,
			    cooldown_until = CASE
					WHEN EXCLUDED.article_count > article_cohort_candidates.decision_article_count
					 AND article_cohort_candidates.recovery_decision = 'no_yield'
					THEN NULL
					ELSE article_cohort_candidates.cooldown_until
				END,
			    last_scheduled_at = NOW(),
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT
			(SELECT COUNT(*) FROM upserted),
			(SELECT posted_at FROM bos_recent ORDER BY posted_at, binary_id LIMIT 1),
			(SELECT binary_id FROM bos_recent ORDER BY posted_at, binary_id LIMIT 1)`,
		scanLimit,
		bucketSeconds,
		articleCohortOpaqueMinSingletons,
		dayStart,
		dayEnd,
		cursorEnabled,
		cursorPostedAt,
		cursorBinaryID,
	).Scan(&cohorts, &pageLastPostedAt, &pageLastBinaryID); err != nil {
		return 0, 0, fmt.Errorf("upsert opaque article cohorts: %w", err)
	}

	var queued int64
	if err := tx.QueryRowContext(ctx, `
		WITH cfg AS MATERIALIZED (
			SELECT
				CASE WHEN profile.recovery_profile = 'exhaustive' THEN 32 ELSE 16 END AS sample_limit,
				CASE WHEN profile.recovery_profile = 'exhaustive' THEN 20000 ELSE 2048 END AS cohort_cap,
				profile.recovery_profile
			FROM (
				SELECT COALESCE(
					(SELECT recovery_profile FROM indexer_recovery_capacity_state WHERE id = true),
					'balanced'
				) AS recovery_profile
			) profile
		),
		bos_recent AS MATERIALIZED (
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
			  AND source_posted_at >= $5
			  AND source_posted_at < $6
			  AND (
				NOT $7::boolean
				OR (posted_at, binary_id) < ($8::timestamptz, $9::bigint)
			  )
			ORDER BY posted_at DESC, binary_id DESC
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
			  AND NOT EXISTS (
				SELECT 1
				FROM article_cohort_yenc_queue cyq
				WHERE cyq.source_posted_at = bp.source_posted_at
				  AND cyq.article_header_id = bp.article_header_id
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
		queue_rows AS MATERIALIZED (
			SELECT DISTINCT ON (r.source_posted_at, r.article_header_id)
				r.source_posted_at,
				r.binary_id,
				r.article_header_id,
				'opaque:' || r.provider_id || ':' || r.newsgroup_id || ':' || r.posted_bucket AS cohort_key,
				cc.source_posted_at AS cohort_source_posted_at,
				r.provider_id,
				r.newsgroup_id,
				'opaque_near_time' AS cohort_kind,
				0 AS priority_rank,
				'opaque_near_time_cohort' AS admission_reason,
				(c.cohort_size::double precision * 1000) + LEAST(999::double precision, COALESCE(r.total_bytes, 0)::double precision / 1000000) AS score,
				'ready' AS status,
				r.posted_at,
				c.cohort_size,
				r.total_bytes
			FROM recent r
			JOIN cohorts c
			  ON c.provider_id = r.provider_id
			 AND c.newsgroup_id = r.newsgroup_id
			 AND c.posted_bucket = r.posted_bucket
			JOIN article_cohort_candidates cc
			  ON cc.source_posted_at = c.cohort_source_posted_at
			 AND cc.cohort_key = c.cohort_key
			 AND NOT (cc.status = 'cooldown' AND cc.cooldown_until > NOW())
			 AND cc.recovery_decision <> 'no_yield'
			ORDER BY r.source_posted_at, r.article_header_id, c.cohort_size DESC, r.posted_at DESC, r.total_bytes DESC, r.binary_id
		),
		queue_deduped AS MATERIALIZED (
			SELECT DISTINCT ON (source_posted_at, binary_id)
				*
			FROM queue_rows
			ORDER BY source_posted_at, binary_id, cohort_size DESC, posted_at DESC, total_bytes DESC, article_header_id
		),
		queue_ranked AS MATERIALIZED (
			SELECT
				q.*,
				ROW_NUMBER() OVER (
					PARTITION BY q.cohort_key
					ORDER BY q.posted_at DESC, q.total_bytes DESC, q.binary_id
				) AS cohort_rank,
				cc.recovery_decision,
				COALESCE(existing.queued_count, 0) AS queued_count,
				cfg.sample_limit,
				cfg.cohort_cap
			FROM queue_deduped q
			JOIN article_cohort_candidates cc
			  ON cc.source_posted_at = q.cohort_source_posted_at
			 AND cc.cohort_key = q.cohort_key
			 AND cc.provider_id = q.provider_id
			 AND cc.newsgroup_id = q.newsgroup_id
			CROSS JOIN cfg
			LEFT JOIN LATERAL (
				SELECT COUNT(*)::integer AS queued_count
				FROM article_cohort_yenc_queue existing_queue
				WHERE existing_queue.cohort_key = q.cohort_key
				  AND existing_queue.provider_id = q.provider_id
				  AND existing_queue.newsgroup_id = q.newsgroup_id
			) existing ON TRUE
		),
		inserted AS (
			INSERT INTO article_cohort_yenc_queue (
				source_posted_at, binary_id, article_header_id, cohort_key, provider_id,
				newsgroup_id, cohort_kind, priority_rank, admission_reason, score, status, updated_at
			)
			SELECT
				q.source_posted_at,
				q.binary_id,
				q.article_header_id,
				q.cohort_key,
				q.provider_id,
				q.newsgroup_id,
				q.cohort_kind,
				q.priority_rank,
				q.admission_reason,
				q.score,
				q.status,
				NOW()
			FROM queue_ranked q
			WHERE q.cohort_rank <= CASE
					WHEN q.recovery_decision = 'promoted' THEN 256
					ELSE q.sample_limit
				END
			  AND q.queued_count + q.cohort_rank <= CASE
					WHEN q.recovery_decision = 'promoted' THEN q.cohort_cap
					ELSE q.sample_limit
				END
			ORDER BY q.cohort_size DESC, q.posted_at DESC, q.total_bytes DESC, q.binary_id
			LIMIT $4
			ON CONFLICT (source_posted_at, binary_id) DO UPDATE
			SET priority_rank = LEAST(article_cohort_yenc_queue.priority_rank, EXCLUDED.priority_rank),
			    admission_reason = EXCLUDED.admission_reason,
			    score = GREATEST(article_cohort_yenc_queue.score, EXCLUDED.score),
			    status = CASE WHEN article_cohort_yenc_queue.status = 'done' THEN 'done' ELSE 'ready' END,
			    updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM inserted`,
		scanLimit,
		bucketSeconds,
		articleCohortOpaqueMinSingletons,
		queueLimit,
		dayStart,
		dayEnd,
		cursorEnabled,
		cursorPostedAt,
		cursorBinaryID,
	).Scan(&queued); err != nil {
		return 0, 0, fmt.Errorf("queue opaque cohort yenc rows: %w", err)
	}
	if err := advanceArticleCohortScanCursorInTx(ctx, tx, scanKey, pageLastPostedAt, pageLastBinaryID); err != nil {
		return 0, 0, err
	}
	return cohorts, queued, nil
}

func lockArticleCohortScanCursorInTx(
	ctx context.Context,
	tx *sql.Tx,
	scanKey string,
	windowStart time.Time,
	windowEnd time.Time,
) (sql.NullTime, sql.NullInt64, error) {
	if tx == nil {
		return sql.NullTime{}, sql.NullInt64{}, fmt.Errorf("article cohort scan cursor tx is required")
	}
	var postedAt sql.NullTime
	var binaryID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO indexer_article_cohort_scan_state (
			scan_key,
			window_start,
			window_end,
			updated_at
		)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (scan_key) DO UPDATE
		SET window_start = EXCLUDED.window_start,
		    window_end = EXCLUDED.window_end,
		    updated_at = NOW()
		RETURNING cursor_posted_at, cursor_binary_id`,
		scanKey,
		windowStart.UTC(),
		windowEnd.UTC(),
	).Scan(&postedAt, &binaryID); err != nil {
		return sql.NullTime{}, sql.NullInt64{}, fmt.Errorf("lock article cohort scan cursor: %w", err)
	}
	return postedAt, binaryID, nil
}

func advanceArticleCohortScanCursorInTx(
	ctx context.Context,
	tx *sql.Tx,
	scanKey string,
	postedAt sql.NullTime,
	binaryID sql.NullInt64,
) error {
	if tx == nil {
		return fmt.Errorf("article cohort scan cursor tx is required")
	}
	if postedAt.Valid && binaryID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE indexer_article_cohort_scan_state
			SET cursor_posted_at = $2,
			    cursor_binary_id = $3,
			    updated_at = NOW()
			WHERE scan_key = $1`,
			scanKey, postedAt.Time.UTC(), binaryID.Int64,
		); err != nil {
			return fmt.Errorf("advance article cohort scan cursor: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE indexer_article_cohort_scan_state
		SET cursor_posted_at = NULL,
		    cursor_binary_id = NULL,
		    wrapped_count = wrapped_count + 1,
		    updated_at = NOW()
		WHERE scan_key = $1`, scanKey); err != nil {
		return fmt.Errorf("wrap article cohort scan cursor: %w", err)
	}
	return nil
}

func selectOpaqueCohortSourceDayInTx(ctx context.Context, tx *sql.Tx) (time.Time, time.Time, bool, error) {
	if tx == nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("article cohort source day tx is required")
	}
	var day sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		WITH active_recovery AS MATERIALIZED (
			SELECT source_posted_at
			FROM yenc_recovery_work_items
			WHERE status IN ('ready', 'running')
			  AND source_posted_at IS NOT NULL
			ORDER BY priority_rank ASC, date_utc DESC NULLS LAST, source_posted_at DESC, binary_id
			LIMIT 1
		),
		active_assembly AS MATERIALIZED (
			SELECT source_posted_at
			FROM article_header_assembly_queue
			WHERE source_posted_at IS NOT NULL
			ORDER BY source_posted_at DESC, article_header_id DESC
			LIMIT 1
		),
		active_singletons AS MATERIALIZED (
			SELECT source_posted_at
			FROM binary_observation_stats
			WHERE total_parts <= 1
			  AND observed_parts <= 1
			  AND posted_at IS NOT NULL
			  AND source_posted_at IS NOT NULL
			ORDER BY source_posted_at DESC, binary_id DESC
			LIMIT 1
		),
		selected AS (
			SELECT source_posted_at, 1 AS priority FROM active_recovery
			UNION ALL
			SELECT source_posted_at, 2 AS priority FROM active_assembly
			UNION ALL
			SELECT source_posted_at, 3 AS priority FROM active_singletons
		)
		SELECT date_trunc('day', source_posted_at)::timestamptz
		FROM selected
		ORDER BY priority, source_posted_at DESC
		LIMIT 1`).Scan(&day); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, time.Time{}, false, nil
		}
		return time.Time{}, time.Time{}, false, fmt.Errorf("select opaque cohort source day: %w", err)
	}
	if !day.Valid {
		return time.Time{}, time.Time{}, false, nil
	}
	start := day.Time.UTC()
	return start, start.Add(24 * time.Hour), true, nil
}

type articleCohortYEncRecoveryFeedback struct {
	ArticleHeaderID int64
	StableSignalKey string
	GroupingGain    bool
}

func recordArticleCohortYEncRecoveredInTx(ctx context.Context, tx *sql.Tx, feedback []articleCohortYEncRecoveryFeedback) error {
	if tx == nil {
		return fmt.Errorf("article cohort yenc feedback tx is required")
	}
	if len(feedback) == 0 {
		return nil
	}
	args := make([]any, 0, len(feedback)*3)
	values := make([]string, 0, len(feedback))
	seen := make(map[int64]struct{}, len(feedback))
	for _, item := range feedback {
		if item.ArticleHeaderID <= 0 {
			continue
		}
		if _, ok := seen[item.ArticleHeaderID]; ok {
			continue
		}
		seen[item.ArticleHeaderID] = struct{}{}
		base := len(args) + 1
		args = append(args, item.ArticleHeaderID, strings.TrimSpace(item.StableSignalKey), item.GroupingGain)
		values = append(values, fmt.Sprintf("($%d::bigint,$%d::text,$%d::boolean)", base, base+1, base+2))
	}
	if len(values) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		WITH requested(article_header_id, stable_signal_key, grouping_gain) AS (
			VALUES `+strings.Join(values, ",")+`
		),
		signal_counts AS MATERIALIZED (
			SELECT
				cyq.provider_id,
				cyq.newsgroup_id,
				cyq.cohort_key,
				BTRIM(r.stable_signal_key) AS signal_key,
				COUNT(*)::integer AS signal_count
			FROM article_cohort_yenc_queue cyq
			JOIN requested r ON r.article_header_id = cyq.article_header_id
			WHERE cyq.status <> 'done'
			  AND BTRIM(r.stable_signal_key) <> ''
			GROUP BY
				cyq.provider_id,
				cyq.newsgroup_id,
				cyq.cohort_key,
				BTRIM(r.stable_signal_key)
		),
		best_signals AS MATERIALIZED (
			SELECT DISTINCT ON (provider_id, newsgroup_id, cohort_key)
				provider_id,
				newsgroup_id,
				cohort_key,
				signal_key,
				signal_count
			FROM signal_counts
			ORDER BY provider_id, newsgroup_id, cohort_key, signal_count DESC, signal_key
		),
		affected AS MATERIALIZED (
			SELECT
				cyq.provider_id,
				cyq.newsgroup_id,
				cyq.cohort_key,
				COUNT(*)::integer AS recovered_count,
				MAX(bs.signal_key) AS best_signal_key,
				COALESCE(MAX(bs.signal_count), 0)::integer AS best_signal_count,
				COUNT(*) FILTER (WHERE r.grouping_gain)::integer AS grouping_gain_count
			FROM article_cohort_yenc_queue cyq
			JOIN requested r ON r.article_header_id = cyq.article_header_id
			LEFT JOIN best_signals bs
			  ON bs.provider_id = cyq.provider_id
			 AND bs.newsgroup_id = cyq.newsgroup_id
			 AND bs.cohort_key = cyq.cohort_key
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
		),
		cfg AS (
			SELECT
				profile.recovery_profile,
				CASE WHEN profile.recovery_profile = 'exhaustive' THEN 32 ELSE 16 END AS sample_limit
			FROM (
				SELECT COALESCE(
					(SELECT recovery_profile FROM indexer_recovery_capacity_state WHERE id = true),
					'balanced'
				) AS recovery_profile
			) profile
		),
		evaluated AS MATERIALIZED (
			SELECT
				c.source_posted_at,
				c.cohort_key,
				a.recovered_count,
				a.best_signal_key,
				a.best_signal_count,
				a.grouping_gain_count,
				cfg.recovery_profile,
				cfg.sample_limit,
				c.yenc_done_count + a.recovered_count AS next_done_count,
				c.grouping_gain_count + a.grouping_gain_count AS next_grouping_gain_count,
				CASE
					WHEN a.best_signal_key IS NULL THEN c.stable_signal_key
					WHEN c.stable_signal_key = a.best_signal_key THEN c.stable_signal_key
					WHEN a.best_signal_count >= 2 THEN a.best_signal_key
					WHEN c.stable_signal_count < 2 THEN a.best_signal_key
					ELSE c.stable_signal_key
				END AS next_stable_signal_key,
				CASE
					WHEN a.best_signal_key IS NULL THEN c.stable_signal_count
					WHEN c.stable_signal_key = a.best_signal_key
					THEN c.stable_signal_count + a.best_signal_count
					WHEN a.best_signal_count >= 2 THEN a.best_signal_count
					WHEN c.stable_signal_count < 2 THEN a.best_signal_count
					ELSE c.stable_signal_count
				END AS next_stable_signal_count
			FROM article_cohort_candidates c
			JOIN affected a
			  ON a.provider_id = c.provider_id
			 AND a.newsgroup_id = c.newsgroup_id
			 AND a.cohort_key = c.cohort_key
			CROSS JOIN cfg
		),
		decisions AS MATERIALIZED (
			SELECT
				e.*,
				(
					e.next_stable_signal_count >= 2
					OR (
						e.recovery_profile = 'exhaustive'
						AND e.next_grouping_gain_count >= 2
					)
				) AS promote,
				e.next_done_count >= e.sample_limit AS sample_exhausted
			FROM evaluated e
		)
		UPDATE article_cohort_candidates c
		SET yenc_done_count = d.next_done_count,
		    yenc_recovered_count = c.yenc_recovered_count + d.recovered_count,
		    stable_signal_key = d.next_stable_signal_key,
		    stable_signal_count = d.next_stable_signal_count,
		    grouping_gain_count = d.next_grouping_gain_count,
		    recovery_decision = CASE
				WHEN d.promote THEN 'promoted'
				WHEN c.recovery_decision = 'sample' AND d.sample_exhausted THEN 'no_yield'
				ELSE c.recovery_decision
			END,
		    status = CASE
				WHEN d.promote THEN 'active'
				WHEN c.recovery_decision = 'sample' AND d.sample_exhausted THEN 'cooldown'
				ELSE 'active'
			END,
		    cooldown_until = CASE
				WHEN c.recovery_decision = 'sample' AND d.sample_exhausted AND NOT d.promote
				THEN NOW() + INTERVAL '30 minutes'
				ELSE NULL
			END,
		    decision_article_count = CASE
				WHEN c.recovery_decision = 'sample' AND d.sample_exhausted AND NOT d.promote
				THEN c.article_count
				ELSE c.decision_article_count
			END,
		    settled_at = CASE
				WHEN c.recovery_decision = 'sample' AND d.sample_exhausted AND NOT d.promote
				THEN NOW()
				ELSE NULL
			END,
		    score = LEAST(1000000000::double precision, c.score + (d.recovered_count::double precision * 500)),
		    updated_at = NOW()
		FROM decisions d
		WHERE c.source_posted_at = d.source_posted_at
		  AND c.cohort_key = d.cohort_key`,
		args...,
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
		),
		marked AS (
			UPDATE article_cohort_yenc_queue cyq
			SET status = 'done',
			    updated_at = NOW()
			FROM requested r
			WHERE cyq.article_header_id = r.article_header_id
			  AND cyq.status <> 'done'
			RETURNING cyq.source_posted_at, cyq.cohort_key
		),
		cfg AS (
			SELECT CASE
				WHEN COALESCE(
					(SELECT recovery_profile FROM indexer_recovery_capacity_state WHERE id = true),
					'balanced'
				) = 'exhaustive' THEN 32
				ELSE 16
			END AS sample_limit
		)
		UPDATE article_cohort_candidates c
		SET yenc_done_count = c.yenc_done_count + a.no_identity_count,
		    yenc_no_identity_count = c.yenc_no_identity_count + a.no_identity_count,
		    recovery_decision = CASE
				WHEN c.recovery_decision = 'sample'
				 AND c.stable_signal_count < 2
				 AND c.yenc_done_count + a.no_identity_count >= cfg.sample_limit
				THEN 'no_yield'
				ELSE c.recovery_decision
			END,
		    status = CASE
		        WHEN c.recovery_decision = 'sample'
		         AND c.stable_signal_count < 2
		         AND c.yenc_done_count + a.no_identity_count >= cfg.sample_limit
		            THEN 'cooldown'
		        ELSE c.status
		    END,
		    cooldown_until = CASE
		        WHEN c.recovery_decision = 'sample'
		         AND c.stable_signal_count < 2
		         AND c.yenc_done_count + a.no_identity_count >= cfg.sample_limit
		            THEN NOW() + ($2::bigint * INTERVAL '1 millisecond')
		        ELSE c.cooldown_until
		    END,
		    decision_article_count = CASE
				WHEN c.recovery_decision = 'sample'
				 AND c.stable_signal_count < 2
				 AND c.yenc_done_count + a.no_identity_count >= cfg.sample_limit
				THEN c.article_count
				ELSE c.decision_article_count
			END,
		    settled_at = CASE
				WHEN c.recovery_decision = 'sample'
				 AND c.stable_signal_count < 2
				 AND c.yenc_done_count + a.no_identity_count >= cfg.sample_limit
				THEN NOW()
				ELSE c.settled_at
			END,
		    updated_at = NOW()
		FROM affected a
		CROSS JOIN cfg
		WHERE c.provider_id = a.provider_id
		  AND c.newsgroup_id = a.newsgroup_id
		  AND c.cohort_key = a.cohort_key`,
		articleHeaderIDs,
		articleCohortNoIdentityCooldown.Milliseconds(),
	)
	if err != nil {
		return fmt.Errorf("record article cohort yenc no-identity feedback: %w", err)
	}
	return nil
}
