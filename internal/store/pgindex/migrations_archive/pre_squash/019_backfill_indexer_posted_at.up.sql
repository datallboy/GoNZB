-- Restore persisted posting-time lineage for existing binary and release rows.

UPDATE binaries b
SET posted_at = agg.posted_at,
    updated_at = NOW()
FROM (
	SELECT
		bp.binary_id,
		MIN(ah.date_utc) AS posted_at
	FROM binary_parts bp
	JOIN article_headers ah ON ah.id = bp.article_header_id
	WHERE ah.date_utc IS NOT NULL
	GROUP BY bp.binary_id
) agg
WHERE b.id = agg.binary_id
  AND (
	b.posted_at IS NULL
	OR b.posted_at <> agg.posted_at
  );

UPDATE release_files rf
SET posted_at = b.posted_at,
    updated_at = NOW()
FROM binaries b
WHERE rf.binary_id = b.id
  AND b.posted_at IS NOT NULL
  AND (
	rf.posted_at IS NULL
	OR rf.posted_at <> b.posted_at
  );

UPDATE releases r
SET posted_at = agg.posted_at,
    updated_at = NOW()
FROM (
	SELECT
		rf.release_id,
		MIN(rf.posted_at) AS posted_at
	FROM release_files rf
	WHERE rf.posted_at IS NOT NULL
	GROUP BY rf.release_id
) agg
WHERE r.release_id = agg.release_id
  AND (
	r.posted_at IS NULL
	OR r.posted_at <> agg.posted_at
  );
