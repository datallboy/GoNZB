-- Split transient article ingest payload off the hot article_headers row.

CREATE TABLE IF NOT EXISTS article_header_ingest_payloads (
  article_header_id BIGINT PRIMARY KEY REFERENCES article_headers(id) ON DELETE CASCADE,
  subject TEXT NOT NULL DEFAULT '',
  poster TEXT NOT NULL DEFAULT '',
  xref TEXT NOT NULL DEFAULT '',
  raw_overview_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE article_headers
ADD COLUMN IF NOT EXISTS assembled_at TIMESTAMPTZ;

INSERT INTO article_header_ingest_payloads (
  article_header_id,
  subject,
  poster,
  xref,
  raw_overview_json,
  created_at
)
SELECT
  id,
  subject,
  poster,
  xref,
  raw_overview_json,
  scraped_at
FROM article_headers
ON CONFLICT (article_header_id) DO UPDATE
SET subject = EXCLUDED.subject,
    poster = EXCLUDED.poster,
    xref = EXCLUDED.xref,
    raw_overview_json = EXCLUDED.raw_overview_json;

UPDATE article_headers ah
SET assembled_at = NOW()
WHERE assembled_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM binary_parts bp
    WHERE bp.article_header_id = ah.id
  );

CREATE INDEX IF NOT EXISTS idx_article_headers_pending_assembly
ON article_headers(id DESC)
WHERE assembled_at IS NULL;

ALTER TABLE article_headers
DROP COLUMN IF EXISTS subject,
DROP COLUMN IF EXISTS poster,
DROP COLUMN IF EXISTS xref,
DROP COLUMN IF EXISTS raw_overview_json;

DROP TABLE IF EXISTS article_poster_map;

DROP INDEX IF EXISTS idx_article_poster_map_poster_id;
