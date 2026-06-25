ALTER TABLE IF EXISTS public.binary_lifecycle
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_completion_keys
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_grouping_evidence
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_projection_events
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_superseded_sources
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_inspections
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_inspection_artifacts
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_archive_entries
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_text_evidence
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_media_streams
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_par2_sets
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_par2_targets
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.release_stage_dirty_families
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

UPDATE public.binary_lifecycle bl
SET source_posted_at = COALESCE(bl.source_posted_at, bc.source_posted_at, bos.source_posted_at, bos.posted_at)
FROM public.binary_core bc
LEFT JOIN public.binary_observation_stats bos ON bos.binary_id = bc.binary_id
WHERE bl.binary_id = bc.binary_id
  AND bl.source_posted_at IS NULL;

UPDATE public.binary_completion_keys bck
SET source_posted_at = COALESCE(bck.source_posted_at, bos.source_posted_at, bck.posted_at, bos.posted_at)
FROM public.binary_observation_stats bos
WHERE bck.binary_id = bos.binary_id
  AND bck.source_posted_at IS NULL;

UPDATE public.binary_grouping_evidence bge
SET source_posted_at = COALESCE(bge.source_posted_at, bc.source_posted_at)
FROM public.binary_core bc
WHERE bge.binary_id = bc.binary_id
  AND bge.source_posted_at IS NULL;

UPDATE public.binary_projection_events bpe
SET source_posted_at = COALESCE(bpe.source_posted_at, bc.source_posted_at, bpe.created_at)
FROM public.binary_core bc
WHERE bpe.binary_id = bc.binary_id
  AND bpe.source_posted_at IS NULL;

UPDATE public.binary_projection_events
SET source_posted_at = COALESCE(source_posted_at, created_at)
WHERE source_posted_at IS NULL;

UPDATE public.binary_superseded_sources bss
SET source_posted_at = COALESCE(bss.source_posted_at, bc.source_posted_at, bss.superseded_at)
FROM public.binary_core bc
WHERE bss.source_binary_id = bc.binary_id
  AND bss.source_posted_at IS NULL;

UPDATE public.binary_superseded_sources
SET source_posted_at = COALESCE(source_posted_at, superseded_at)
WHERE source_posted_at IS NULL;

UPDATE public.binary_inspections bi
SET source_posted_at = COALESCE(bi.source_posted_at, bc.source_posted_at, bi.source_updated_at, bi.created_at)
FROM public.binary_core bc
WHERE bi.binary_id = bc.binary_id
  AND bi.source_posted_at IS NULL;

UPDATE public.binary_inspections
SET source_posted_at = COALESCE(source_posted_at, source_updated_at, created_at)
WHERE source_posted_at IS NULL;

UPDATE public.binary_inspection_artifacts bia
SET source_posted_at = COALESCE(bia.source_posted_at, bc.source_posted_at, bia.created_at)
FROM public.binary_core bc
WHERE bia.binary_id = bc.binary_id
  AND bia.source_posted_at IS NULL;

UPDATE public.binary_archive_entries bae
SET source_posted_at = COALESCE(bae.source_posted_at, bc.source_posted_at, bae.created_at)
FROM public.binary_core bc
WHERE bae.binary_id = bc.binary_id
  AND bae.source_posted_at IS NULL;

UPDATE public.binary_text_evidence bte
SET source_posted_at = COALESCE(bte.source_posted_at, bc.source_posted_at, bte.created_at)
FROM public.binary_core bc
WHERE bte.binary_id = bc.binary_id
  AND bte.source_posted_at IS NULL;

UPDATE public.binary_media_streams bms
SET source_posted_at = COALESCE(bms.source_posted_at, bc.source_posted_at, bms.created_at)
FROM public.binary_core bc
WHERE bms.binary_id = bc.binary_id
  AND bms.source_posted_at IS NULL;

UPDATE public.binary_par2_sets bps
SET source_posted_at = COALESCE(bps.source_posted_at, bc.source_posted_at, bps.created_at)
FROM public.binary_core bc
WHERE bps.binary_id = bc.binary_id
  AND bps.source_posted_at IS NULL;

UPDATE public.binary_par2_targets bpt
SET source_posted_at = COALESCE(bpt.source_posted_at, bc.source_posted_at, bpt.created_at)
FROM public.binary_core bc
WHERE bpt.binary_id = bc.binary_id
  AND bpt.source_posted_at IS NULL;

UPDATE public.release_stage_dirty_families rsdf
SET source_posted_at = COALESCE(rsdf.source_posted_at, rfs.source_posted_at, rfs.earliest_posted_at, rsdf.updated_at)
FROM public.release_family_readiness_summaries rfs
WHERE rsdf.provider_id = rfs.provider_id
  AND rsdf.newsgroup_id = rfs.newsgroup_id
  AND rsdf.key_kind = rfs.key_kind
  AND rsdf.family_key = rfs.family_key
  AND rsdf.source_posted_at IS NULL;

UPDATE public.release_stage_dirty_families
SET source_posted_at = COALESCE(source_posted_at, updated_at)
WHERE source_posted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_binary_lifecycle_source_posted
    ON public.binary_lifecycle (source_posted_at, provider_id, newsgroup_id, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_source_posted
    ON public.binary_completion_keys (source_posted_at, provider_id, newsgroup_id, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_grouping_evidence_source_posted
    ON public.binary_grouping_evidence (source_posted_at, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_projection_events_source_posted
    ON public.binary_projection_events (source_posted_at, event_stage, event_kind, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_superseded_sources_source_posted
    ON public.binary_superseded_sources (source_posted_at, provider_id, newsgroup_id, source_binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_inspections_source_posted
    ON public.binary_inspections (source_posted_at, stage_name, status, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_inspection_artifacts_source_posted
    ON public.binary_inspection_artifacts (source_posted_at, stage_name, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_archive_entries_source_posted
    ON public.binary_archive_entries (source_posted_at, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_text_evidence_source_posted
    ON public.binary_text_evidence (source_posted_at, stage_name, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_media_streams_source_posted
    ON public.binary_media_streams (source_posted_at, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_par2_sets_source_posted
    ON public.binary_par2_sets (source_posted_at, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_par2_targets_source_posted
    ON public.binary_par2_targets (source_posted_at, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_release_stage_dirty_families_source_posted
    ON public.release_stage_dirty_families (source_posted_at, provider_id, newsgroup_id, key_kind, family_key)
    WHERE source_posted_at IS NOT NULL;
