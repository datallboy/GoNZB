package pgindex

import (
	"context"
	"fmt"
)

func (s *Store) RefreshIndexerGroupProfiles(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store is required")
	}
	var updated int64
	if err := s.db.QueryRowContext(ctx, `
		WITH bucket_metrics AS (
			SELECT
				provider_id,
				newsgroup_id,
				COUNT(*) AS articles_scraped_1d
			FROM article_headers
			WHERE COALESCE(date_utc, scraped_at) >= NOW() - INTERVAL '1 day'
			GROUP BY provider_id, newsgroup_id
		),
		binary_metrics AS (
			SELECT
				provider_id,
				newsgroup_id,
				COUNT(*) FILTER (WHERE total_parts > 0 AND observed_parts >= total_parts) AS binaries_completed_1d
			FROM binary_observation_stats
			WHERE COALESCE(posted_at, updated_at) >= NOW() - INTERVAL '1 day'
			GROUP BY provider_id, newsgroup_id
		),
		release_metrics AS (
			SELECT
				r.provider_id,
				ng.id AS newsgroup_id,
				COUNT(*) AS releases_created_1d
			FROM releases r
			JOIN newsgroups ng ON ng.group_name = r.group_name
			WHERE COALESCE(r.posted_at, r.created_at) >= NOW() - INTERVAL '1 day'
			GROUP BY r.provider_id, ng.id
		),
		recovery_metrics AS (
			SELECT
				provider_id,
				newsgroup_id,
				COUNT(*) AS recovery_queued_1d,
				COUNT(*) FILTER (WHERE status IN ('done', 'stale', 'missing', 'failed')) AS yenc_probes_attempted_1d,
				COUNT(*) FILTER (WHERE status = 'done') AS yenc_probes_successful_1d,
				AVG(EXTRACT(EPOCH FROM updated_at - ready_at)) FILTER (WHERE status = 'done' AND updated_at >= ready_at) AS avg_recovery_lag_seconds,
				MAX(EXTRACT(EPOCH FROM updated_at - ready_at)) FILTER (WHERE status = 'done' AND updated_at >= ready_at) AS max_recovery_lag_seconds
			FROM yenc_recovery_work_items
			WHERE source_posted_at >= NOW() - INTERVAL '1 day'
			GROUP BY provider_id, newsgroup_id
		),
		metrics AS (
			SELECT
				igp.provider_id,
				igp.newsgroup_id,
				COALESCE(bm.articles_scraped_1d, 0) AS articles_scraped_1d,
				COALESCE(rm.recovery_queued_1d, 0) AS recovery_queued_1d,
				COALESCE(rm.yenc_probes_attempted_1d, 0) AS yenc_probes_attempted_1d,
				COALESCE(rm.yenc_probes_successful_1d, 0) AS yenc_probes_successful_1d,
				COALESCE(bin.binaries_completed_1d, 0) AS binaries_completed_1d,
				COALESCE(rel.releases_created_1d, 0) AS releases_created_1d,
				COALESCE(rm.avg_recovery_lag_seconds, 0) AS avg_recovery_lag_seconds,
				COALESCE(rm.max_recovery_lag_seconds, 0) AS max_recovery_lag_seconds
			FROM indexer_group_profiles igp
			LEFT JOIN bucket_metrics bm ON bm.provider_id = igp.provider_id AND bm.newsgroup_id = igp.newsgroup_id
			LEFT JOIN binary_metrics bin ON bin.provider_id = igp.provider_id AND bin.newsgroup_id = igp.newsgroup_id
			LEFT JOIN release_metrics rel ON rel.provider_id = igp.provider_id AND rel.newsgroup_id = igp.newsgroup_id
			LEFT JOIN recovery_metrics rm ON rm.provider_id = igp.provider_id AND rm.newsgroup_id = igp.newsgroup_id
		),
		scored AS (
			SELECT
				*,
				(
					releases_created_1d * 1000.0
					+ binaries_completed_1d * 1.0
					+ CASE WHEN articles_scraped_1d > 0 THEN binaries_completed_1d::double precision / articles_scraped_1d::double precision * 10000.0 ELSE 0 END
					+ CASE WHEN yenc_probes_attempted_1d > 0 THEN yenc_probes_successful_1d::double precision / yenc_probes_attempted_1d::double precision * 250.0 ELSE 0 END
					- CASE WHEN releases_created_1d > 0 THEN recovery_queued_1d::double precision / releases_created_1d::double precision / 100.0 ELSE recovery_queued_1d::double precision / 1000.0 END
				) AS score
			FROM metrics
		),
		updated AS (
			UPDATE indexer_group_profiles igp
			SET articles_scraped_1d = scored.articles_scraped_1d,
			    recovery_queued_1d = scored.recovery_queued_1d,
			    yenc_probes_attempted_1d = scored.yenc_probes_attempted_1d,
			    yenc_probes_successful_1d = scored.yenc_probes_successful_1d,
			    binaries_completed_1d = scored.binaries_completed_1d,
			    releases_created_1d = scored.releases_created_1d,
			    avg_recovery_lag_seconds = scored.avg_recovery_lag_seconds,
			    max_recovery_lag_seconds = scored.max_recovery_lag_seconds,
			    score = scored.score,
			    tier = CASE
			    	WHEN igp.tier_override IS NOT NULL THEN igp.tier
			    	WHEN scored.releases_created_1d >= 5 AND scored.score >= 750 THEN 'hot'
			    	WHEN scored.releases_created_1d >= 1 OR scored.binaries_completed_1d >= 250 THEN 'warm'
			    	WHEN scored.recovery_queued_1d >= 10000 AND scored.releases_created_1d = 0 THEN 'cold'
			    	ELSE 'warm'
			    END,
			    last_scored_at = NOW(),
			    updated_at = NOW()
			FROM scored
			WHERE igp.provider_id = scored.provider_id
			  AND igp.newsgroup_id = scored.newsgroup_id
			RETURNING 1
		)
		SELECT COUNT(*) FROM updated`,
	).Scan(&updated); err != nil {
		return 0, fmt.Errorf("refresh indexer group profiles: %w", err)
	}
	return updated, nil
}
