CREATE TABLE IF NOT EXISTS release_archive_detail_snapshots (
    release_id text PRIMARY KEY REFERENCES public.release_archive_state(release_id) ON DELETE CASCADE,
    guid text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    posted_at timestamp with time zone,
    added_at timestamp with time zone,
    size_bytes bigint NOT NULL DEFAULT 0,
    file_count integer NOT NULL DEFAULT 0,
    completion_pct double precision NOT NULL DEFAULT 0,
    category_id integer NOT NULL DEFAULT 0,
    category text NOT NULL DEFAULT '',
    classification text NOT NULL DEFAULT '',
    has_par2 boolean NOT NULL DEFAULT false,
    has_nfo boolean NOT NULL DEFAULT false,
    password_state text NOT NULL DEFAULT '',
    availability_score double precision NOT NULL DEFAULT 0,
    availability_tier text NOT NULL DEFAULT '',
    media_quality_score double precision NOT NULL DEFAULT 0,
    media_quality_tier text NOT NULL DEFAULT '',
    tmdb_id bigint NOT NULL DEFAULT 0,
    tvdb_id bigint NOT NULL DEFAULT 0,
    imdb_id text NOT NULL DEFAULT '',
    external_media_type text NOT NULL DEFAULT '',
    external_title text NOT NULL DEFAULT '',
    external_year integer NOT NULL DEFAULT 0,
    metadata_updated_at timestamp with time zone,
    runtime_seconds integer NOT NULL DEFAULT 0,
    primary_resolution text NOT NULL DEFAULT '',
    primary_video_codec text NOT NULL DEFAULT '',
    primary_audio_codec text NOT NULL DEFAULT '',
    sample_present boolean NOT NULL DEFAULT false,
    archive_count integer NOT NULL DEFAULT 0,
    video_count integer NOT NULL DEFAULT 0,
    audio_count integer NOT NULL DEFAULT 0,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS release_archive_detail_files (
    release_id text NOT NULL REFERENCES public.release_archive_detail_snapshots(release_id) ON DELETE CASCADE,
    file_name text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    file_index integer NOT NULL DEFAULT 0,
    is_pars boolean NOT NULL DEFAULT false,
    posted_at timestamp with time zone,
    article_count integer NOT NULL DEFAULT 0,
    total_parts integer NOT NULL DEFAULT 0,
    observed_parts integer NOT NULL DEFAULT 0,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (release_id, file_name)
);

CREATE INDEX IF NOT EXISTS idx_release_archive_detail_files_release_order
ON release_archive_detail_files(release_id, file_index, file_name);

CREATE TABLE IF NOT EXISTS release_archive_detail_subtitle_languages (
    release_id text NOT NULL REFERENCES public.release_archive_detail_snapshots(release_id) ON DELETE CASCADE,
    ordinal integer NOT NULL,
    language text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (release_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_release_archive_detail_subtitle_release
ON release_archive_detail_subtitle_languages(release_id, ordinal);
