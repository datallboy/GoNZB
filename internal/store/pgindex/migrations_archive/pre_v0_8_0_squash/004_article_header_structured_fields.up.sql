ALTER TABLE article_header_ingest_payloads
    ADD COLUMN IF NOT EXISTS poster_id BIGINT REFERENCES posters(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS subject_file_name TEXT,
    ADD COLUMN IF NOT EXISTS subject_file_index INTEGER,
    ADD COLUMN IF NOT EXISTS subject_file_total INTEGER,
    ADD COLUMN IF NOT EXISTS yenc_part_number INTEGER,
    ADD COLUMN IF NOT EXISTS yenc_total_parts INTEGER,
    ADD COLUMN IF NOT EXISTS yenc_file_size BIGINT;

UPDATE article_header_ingest_payloads p
SET poster_id = po.id
FROM posters po
WHERE p.poster_id IS NULL
  AND p.poster <> ''
  AND po.poster_name = p.poster;

UPDATE article_header_ingest_payloads
SET poster = ''
WHERE poster_id IS NOT NULL
  AND poster <> '';

UPDATE article_header_ingest_payloads
SET subject_file_name = COALESCE((regexp_match(subject, '"([^"]+)"'))[1], ''),
    subject_file_index = COALESCE(NULLIF((regexp_match(subject, '\[(\d{1,5})/(\d{1,5})\]'))[1], '')::INTEGER, 0),
    subject_file_total = COALESCE(NULLIF((regexp_match(subject, '\[(\d{1,5})/(\d{1,5})\]'))[2], '')::INTEGER, 0),
    yenc_part_number = COALESCE(NULLIF((regexp_match(subject, '(?i)yenc\s*\((\d{1,5})/(\d{1,5})\)'))[1], '')::INTEGER, 0),
    yenc_total_parts = COALESCE(NULLIF((regexp_match(subject, '(?i)yenc\s*\((\d{1,5})/(\d{1,5})\)'))[2], '')::INTEGER, 0),
    yenc_file_size = COALESCE(NULLIF((regexp_match(subject, '(?i)yenc\s*\(\d{1,5}/\d{1,5}\)\s+(\d{1,18})\s*$'))[1], '')::BIGINT, 0)
WHERE subject <> '';

ALTER TABLE article_header_ingest_payloads
    ALTER COLUMN subject_file_name SET DEFAULT '',
    ALTER COLUMN subject_file_index SET DEFAULT 0,
    ALTER COLUMN subject_file_total SET DEFAULT 0,
    ALTER COLUMN yenc_part_number SET DEFAULT 0,
    ALTER COLUMN yenc_total_parts SET DEFAULT 0,
    ALTER COLUMN yenc_file_size SET DEFAULT 0;
