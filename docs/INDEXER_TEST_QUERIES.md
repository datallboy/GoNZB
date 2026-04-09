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
