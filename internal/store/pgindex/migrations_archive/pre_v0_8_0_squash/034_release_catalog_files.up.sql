CREATE TABLE IF NOT EXISTS release_catalog_files (
    id bigserial PRIMARY KEY,
    release_id text NOT NULL REFERENCES public.releases(release_id) ON DELETE CASCADE,
    file_name text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    file_index integer NOT NULL DEFAULT 0,
    is_pars boolean NOT NULL DEFAULT false,
    subject text NOT NULL DEFAULT '',
    poster text NOT NULL DEFAULT '',
    posted_at timestamp with time zone,
    article_count integer NOT NULL DEFAULT 0,
    total_parts integer NOT NULL DEFAULT 0,
    observed_parts integer NOT NULL DEFAULT 0,
    match_confidence double precision NOT NULL DEFAULT 0,
    match_status text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT release_catalog_files_release_id_file_name_key UNIQUE (release_id, file_name)
);

CREATE INDEX IF NOT EXISTS idx_release_catalog_files_release_order
ON release_catalog_files(release_id, file_index, id);

INSERT INTO release_catalog_files (
    release_id,
    file_name,
    size_bytes,
    file_index,
    is_pars,
    subject,
    poster,
    posted_at,
    article_count,
    total_parts,
    observed_parts,
    match_confidence,
    match_status,
    updated_at
)
SELECT
    rf.release_id,
    rf.file_name,
    rf.size_bytes,
    rf.file_index,
    rf.is_pars,
    COALESCE(rf.subject, ''),
    COALESCE(rf.poster, ''),
    rf.posted_at,
    COUNT(bp.id)::integer AS article_count,
    COALESCE(MAX(b.total_parts), 0)::integer,
    COALESCE(MAX(b.observed_parts), 0)::integer,
    COALESCE(MAX(b.match_confidence), 0),
    COALESCE(MAX(b.match_status), ''),
    NOW()
FROM release_files rf
LEFT JOIN binaries b ON b.id = rf.binary_id
LEFT JOIN binary_parts bp ON bp.binary_id = rf.binary_id
GROUP BY rf.release_id, rf.file_name, rf.size_bytes, rf.file_index, rf.is_pars, rf.subject, rf.poster, rf.posted_at
ON CONFLICT (release_id, file_name) DO UPDATE
SET size_bytes = EXCLUDED.size_bytes,
    file_index = EXCLUDED.file_index,
    is_pars = EXCLUDED.is_pars,
    subject = EXCLUDED.subject,
    poster = EXCLUDED.poster,
    posted_at = EXCLUDED.posted_at,
    article_count = EXCLUDED.article_count,
    total_parts = EXCLUDED.total_parts,
    observed_parts = EXCLUDED.observed_parts,
    match_confidence = EXCLUDED.match_confidence,
    match_status = EXCLUDED.match_status,
    updated_at = NOW();

INSERT INTO release_catalog_files (
    release_id,
    file_name,
    size_bytes,
    file_index,
    is_pars,
    posted_at,
    article_count,
    total_parts,
    observed_parts,
    updated_at
)
SELECT
    adf.release_id,
    adf.file_name,
    adf.size_bytes,
    adf.file_index,
    adf.is_pars,
    adf.posted_at,
    adf.article_count,
    adf.total_parts,
    adf.observed_parts,
    NOW()
FROM release_archive_detail_files adf
LEFT JOIN release_catalog_files cf
  ON cf.release_id = adf.release_id
 AND cf.file_name = adf.file_name
WHERE cf.id IS NULL
ON CONFLICT (release_id, file_name) DO NOTHING;
