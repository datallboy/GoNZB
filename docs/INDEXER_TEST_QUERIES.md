# Indexer Test Queries

Saved commands and SQL for checking scrape, assemble, release, and inspect behavior while the indexer backend is being built out.

## Continuous Backfill

Let backfill run until you have enough header depth to form binaries and releases:

```bash
gonzb --config config.yaml indexer scrape backfill
```

The backfill loop uses `indexing.scrape_backfill.interval_minutes` between passes. Fractional values are supported, so `0.25` means 15 seconds.

Single pass:

```bash
gonzb --config config.yaml indexer scrape backfill --once
```

Recent backfill stage runs:

```sql
select
  id,
  stage_name,
  trigger_kind,
  status,
  started_at,
  finished_at,
  error_text
from indexer_stage_runs
where stage_name = 'scrape_backfill'
order by id desc
limit 20;
```

Latest and backfill checkpoint progress per group:

```sql
select
  ng.group_name,
  sc.last_article_number as latest_checkpoint,
  sc.backfill_article_number as backfill_checkpoint,
  sc.updated_at
from scrape_checkpoints sc
join newsgroups ng on ng.id = sc.newsgroup_id
order by ng.group_name;
```

How much header coverage each group has:

```sql
select
  ng.group_name,
  min(ah.article_number) as oldest_article,
  max(ah.article_number) as newest_article,
  count(*) as header_count,
  max(ah.scraped_at) as last_scraped_at
from article_headers ah
join newsgroups ng on ng.id = ah.newsgroup_id
group by ng.group_name
order by header_count desc, ng.group_name;
```

## Assemble And Release

Run the next stages after backfill has collected enough articles:

```bash
gonzb --config config.yaml indexer assemble --once
gonzb --config config.yaml indexer release --once
```

Continuous background mode is also supported:

```bash
gonzb --config config.yaml indexer assemble
gonzb --config config.yaml indexer release
```

Recent assemble and release stage runs:

```sql
select
  id,
  stage_name,
  trigger_kind,
  status,
  started_at,
  finished_at,
  error_text
from indexer_stage_runs
where stage_name in ('assemble', 'release')
order by id desc
limit 50;
```

Pending assembly summary:

```sql
select
  count(*) as pending_headers,
  count(*) filter (
    where nullif(btrim(p.subject_file_name), '') is not null
  ) as structured_identity_headers,
  count(*) filter (
    where nullif(btrim(p.subject_file_name), '') is not null
      and exists (
        select 1
        from binaries b
        where b.provider_id = ah.provider_id
          and b.newsgroup_id = ah.newsgroup_id
          and lower(btrim(coalesce(nullif(b.file_name, ''), nullif(b.binary_name, '')))) = lower(btrim(p.subject_file_name))
      )
  ) as lane_a_progress_headers
from article_headers ah
join article_header_ingest_payloads p on p.article_header_id = ah.id
where ah.assembled_at is null;
```

To suppress tiny partial releases while you are trying to get one inspectable result, set:

```yaml
indexing:
  release:
    min_completion_pct: 25
```

Quick release summary:

```sql
select
  r.release_id,
  r.release_key,
  r.group_name,
  r.title,
  r.source_title,
  r.deobfuscated_title,
  round(r.match_confidence::numeric, 3) as match_confidence,
  r.identity_status,
  round(r.completion_pct::numeric, 2) as completion_pct,
  round(r.availability_score::numeric, 2) as availability_score,
  round(r.media_quality_score::numeric, 2) as media_quality_score,
  round(r.identity_confidence_score::numeric, 2) as identity_confidence_score,
  r.file_count,
  r.par_file_count,
  r.has_par2,
  r.has_nfo,
  r.archive_count,
  r.video_count,
  r.audio_count,
  r.password_state,
  r.poster,
  r.posted_at,
  r.updated_at
from releases r
order by r.updated_at desc
limit 50;
```

Source keys that still are not forming releases:

```sql
select
  b.provider_id,
  b.release_key,
  count(*) as binary_count,
  round(avg(b.match_confidence)::numeric, 3) as avg_binary_confidence,
  min(b.posted_at) as first_posted_at,
  max(b.posted_at) as last_posted_at
from binaries b
left join releases r
  on r.provider_id = b.provider_id
 and r.release_key = b.release_key
where r.release_id is null
group by b.provider_id, b.release_key
order by avg_binary_confidence desc, binary_count desc, b.release_key;
```

Cases where one source key split into multiple formed release groups:

```sql
select
  release_key,
  count(*) as formed_release_groups,
  string_agg(group_name, ', ' order by group_name) as group_names,
  round(avg(match_confidence)::numeric, 3) as avg_release_confidence
from releases
group by release_key
having count(*) > 1
order by formed_release_groups desc, release_key;
```

Why a header is still unassembled:

```sql
select
  ah.id,
  ng.group_name,
  ah.article_number,
  ah.message_id,
  p.subject,
  p.subject_file_name,
  p.subject_file_index,
  p.subject_file_total,
  p.yenc_part_number,
  p.yenc_total_parts,
  exists (
    select 1
    from binaries b
    where b.provider_id = ah.provider_id
      and b.newsgroup_id = ah.newsgroup_id
      and lower(btrim(coalesce(nullif(b.file_name, ''), nullif(b.binary_name, '')))) = lower(btrim(p.subject_file_name))
  ) as matches_existing_binary_by_structured_identity
from article_headers ah
join article_header_ingest_payloads p on p.article_header_id = ah.id
join newsgroups ng on ng.id = ah.newsgroup_id
where ah.assembled_at is null
order by ah.id desc
limit 100;
```

Why a family is still dirty but unformed:

```sql
with family_stats as (
  select
    q.provider_id,
    q.newsgroup_id,
    q.key_kind,
    q.family_key,
    q.updated_at,
    count(b.id)::integer as binary_count,
    count(*) filter (
      where b.total_parts > 0 and b.observed_parts = b.total_parts
    )::integer as complete_binary_count,
    max(case when b.expected_file_count > 0 then 1 else 0 end)::integer as has_expected_file_count
  from release_stage_dirty_families q
  left join binaries b
    on b.provider_id = q.provider_id
   and b.newsgroup_id = q.newsgroup_id
   and (
     (q.key_kind = 'release_family' and b.release_family_key = q.family_key)
     or (
       q.key_kind = 'base_stem'
       and b.expected_file_count > 1
       and nullif(btrim(b.base_stem), '') is not null
       and lower(btrim(b.base_stem)) = q.family_key
     )
   )
  group by q.provider_id, q.newsgroup_id, q.key_kind, q.family_key, q.updated_at
)
select
  fs.*,
  case
    when fs.binary_count = 0 then 'stale_cleanup_only'
    when fs.complete_binary_count = 0 then 'fragment_only_cooldown'
    when fs.has_expected_file_count = 0 then 'formable_without_expected_file_count_evidence'
    else 'formable'
  end as release_actionability
from family_stats fs
order by fs.updated_at asc, fs.family_key asc
limit 100;
```

Whether a family is fragment-only or actually release-actionable:

```sql
select
  b.provider_id,
  b.newsgroup_id,
  b.release_family_key,
  count(*) as binary_count,
  count(*) filter (
    where b.total_parts > 0 and b.observed_parts = b.total_parts
  ) as complete_binary_count,
  max(b.expected_file_count) as max_expected_file_count,
  min(b.posted_at) as first_posted_at,
  max(b.posted_at) as last_posted_at,
  case
    when count(*) filter (where b.total_parts > 0 and b.observed_parts = b.total_parts) = 0 then 'fragment_only'
    else 'release_actionable'
  end as family_state
from binaries b
group by b.provider_id, b.newsgroup_id, b.release_family_key
order by last_posted_at desc nulls last, b.release_family_key
limit 100;
```

## Inspect Family

Run all inspect submodules together:

```bash
gonzb --config config.yaml indexer inspect --once
```

Run one submodule by itself:

```bash
gonzb --config config.yaml indexer inspect media --once
```

Recent inspect stage runs:

```sql
select
  stage_name,
  trigger_kind,
  status,
  started_at,
  finished_at,
  error_text
from indexer_stage_runs
where stage_name in (
  'inspect_par2',
  'inspect_nfo',
  'inspect_archive',
  'inspect_password',
  'inspect_media'
)
order by id desc
limit 50;
```

Recent binary inspection rows:

```sql
select
  bi.stage_name,
  bi.binary_id,
  bi.release_id,
  bi.status,
  bi.started_at,
  bi.finished_at,
  bi.error_text,
  bi.materialized_bytes,
  bi.summary_json,
  bi.tool_provenance_json,
  bi.updated_at
from binary_inspections bi
order by bi.updated_at desc
limit 100;
```

Inspect status counts by submodule:

```sql
select
  stage_name,
  status,
  count(*) as rows
from binary_inspections
group by stage_name, status
order by stage_name, status;
```

Recent inspect failures:

```sql
select
  stage_name,
  binary_id,
  release_id,
  error_text,
  updated_at
from binary_inspections
where status = 'failed'
order by updated_at desc
limit 50;
```

PAR2 rollup check:

```sql
select
  bi.binary_id,
  bi.status,
  bi.summary_json->>'has_par2' as has_par2,
  bi.summary_json->>'base_name' as base_name,
  r.release_id,
  r.title,
  r.has_par2
from binary_inspections bi
join releases r on r.release_id = bi.release_id
where bi.stage_name = 'inspect_par2'
order by bi.updated_at desc
limit 25;
```

NFO rollup check:

```sql
select
  bi.binary_id,
  bi.status,
  bi.summary_json->>'has_nfo' as has_nfo,
  bi.summary_json->'candidate_passwords' as candidate_passwords,
  r.release_id,
  r.title,
  r.has_nfo
from binary_inspections bi
join releases r on r.release_id = bi.release_id
where bi.stage_name = 'inspect_nfo'
order by bi.updated_at desc
limit 25;
```

Archive and password-state rollup check:

```sql
select
  bi.binary_id,
  bi.status,
  bi.summary_json->>'encrypted' as encrypted,
  bi.summary_json->'candidate_passwords' as candidate_passwords,
  r.release_id,
  r.title,
  r.encrypted,
  r.archive_count,
  r.passworded,
  r.passworded_unknown,
  r.password_state,
  r.media_tags_json
from binary_inspections bi
join releases r on r.release_id = bi.release_id
where bi.stage_name = 'inspect_archive'
order by bi.updated_at desc
limit 25;
```

Password candidate and verification status:

```sql
select
  rpc.id,
  rpc.release_id,
  rpc.binary_id,
  rpc.password_value,
  rpc.source_kind,
  rpc.source_ref,
  rpc.confidence,
  rpc.verification_status,
  rpc.last_verified_at,
  rpc.last_error
from release_password_candidates rpc
order by rpc.updated_at desc
limit 100;
```

Media rollup check:

```sql
select
  bi.binary_id,
  bi.status,
  bi.summary_json->>'resolution' as resolution,
  bi.summary_json->>'video_codec' as video_codec,
  bi.summary_json->>'audio_codec' as audio_codec,
  r.release_id,
  r.title,
  r.primary_resolution,
  r.primary_video_codec,
  r.primary_audio_codec,
  r.video_count,
  r.audio_count,
  r.media_quality_score,
  r.media_quality_tier,
  r.media_tags_json
from binary_inspections bi
join releases r on r.release_id = bi.release_id
where bi.stage_name = 'inspect_media'
order by bi.updated_at desc
limit 25;
```

Release-level inspection rollup summary:

```sql
select
  r.release_id,
  r.title,
  r.encrypted,
  r.passworded,
  r.passworded_known,
  r.passworded_unknown,
  r.password_state,
  r.has_par2,
  r.has_nfo,
  r.archive_count,
  r.video_count,
  r.audio_count,
  r.primary_resolution,
  r.primary_video_codec,
  r.primary_audio_codec,
  r.media_tags_json,
  r.metadata_updated_at
from releases r
order by r.metadata_updated_at desc nulls last
limit 100;
```

Password rollup invariant check. This should return zero rows:

```sql
select
  release_id,
  title,
  passworded,
  passworded_known,
  passworded_unknown,
  password_state
from releases
where
  (passworded = false and passworded_known = true)
  or (passworded = false and passworded_unknown = true)
  or (passworded_known = true and passworded_unknown = true)
  or (passworded = true and password_state = 'not_passworded')
  or (passworded_known = true and password_state <> 'passworded_known')
  or (passworded_unknown = true and password_state <> 'passworded_unknown');
```
