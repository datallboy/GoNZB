# Indexer Inspection Tuning 2026-04-22

## Scope

Overnight operational tuning pass focused on the live `inspect_par2` and `inspect_media` stages after assemble and release had largely caught up.

## Baseline

Captured against the live `gonzb-postgres` database on 2026-04-22 before code changes:

- `article_headers` pending assembly: `0`
- assemble was no longer blocked on missing work from the current scrape window
- `inspect_par2` was the dominant inspection bottleneck:
  - 12 hour average runtime about `2962.93s`
  - max runtime about `3990s`
  - repeated failures included:
    - `release ... has no file for binary ...`
    - `decode article ... checksum mismatch ...`
- `inspect_media` had a smaller queue but expensive archive-backed outliers:
  - recent archive probe rows materialized about `268,436,xxx` bytes each
  - those rows were `ffprobe_archive` probes against `.7z.001` payloads

## Changes Landed

### PAR2 inspection

- switched `inspect_par2` from full binary materialization to `SampleBinaryPrefix(...)`
- changed PAR2 evidence from temporary `decoded_file` artifacts to lightweight `prefix_sample` artifacts
- deterministic PAR2 input problems now complete with:
  - `probe_skip_reason`
  - `probe_error_detail`
- deterministic cases no longer stay in the retry churn path as hard failures

### Archive-backed media probing

- reduced default archive probe budget from `128 MiB` archive prefix + `128 MiB` extracted output to:
  - `64 MiB` archive prefix
  - `32 MiB` extracted output
- reduced sample-entry budgets to:
  - `24 MiB` archive prefix
  - `16 MiB` extracted output
- added focused limit tests around the archive media budgeting helpers

## Validation

Automated validation:

- `go test ./internal/indexing/inspect/... ./internal/store/pgindex`

Live runtime validation:

- ran `indexer maintenance`
  - cleared `14` abandoned binary inspections
  - purged `213` orphan releases
- identified and stopped older long-running source-built workers started around `20:41`
  - `indexer assemble`
  - `indexer release`
  - `indexer inspect`
- ran `indexer maintenance repair-runtime`
  - final repair pass: `abandoned_runs=1`, `cleared_stale_leases=1`
- ran a fresh `indexer inspect par2 --once` after lease cleanup

Observed live PAR2 result after the new code took over:

- one clean PAR2 pass updated `100` rows
- recent PAR2 rows now show:
  - `artifact_role = 'prefix_sample'`
  - `bytes_total = 4096`
  - `materialized_bytes` around `4465` to `4480`
  - empty `artifact_path` as expected for non-persisted prefix evidence
- failed `inspect_par2` rows dropped from `18` to `16`

Example effect:

- before: large PAR2 rows were materializing full files such as `62,918,684` bytes and `125,833,313` bytes
- after: the same stage now records about `4.4 KiB` of sampled evidence per binary

## Operational Read

- assemble is not currently waiting on more scraped article headers; the present pending assembly count is `0`
- PAR2 throughput improved materially once the live worker switched to the new codepath
- media tuning is landed and covered by tests, but there were `0` currently eligible media candidates after the worker restart, so live post-change archive probe sizes still need the next natural archive-backed media sample to confirm the new `64 MiB / 32 MiB` budget in production

## Next Operator Check

When convenient, confirm the next live archive-backed media completion with:

```sql
select
  binary_id,
  release_id,
  materialized_bytes,
  started_at,
  finished_at,
  summary_json->>'probe_mode' as probe_mode,
  summary_json->>'archive_entry' as archive_entry
from binary_inspections
where stage_name = 'inspect_media'
order by updated_at desc
limit 10;
```

Expected post-change archive-backed media rows should be much lower than the earlier `~268 MB` materialization pattern.

## Follow-up Hardening Pass

Additional cleanup landed later on 2026-04-22 after the first tuning pass:

- stopped persisting transient temp-workspace file paths from:
  - `inspect_par2`
  - `inspect_nfo`
  - `inspect_archive`
  - `inspect_password`
  - `inspect_media`
- removed now-misleading summary fields like `workspace_path` and `probe_path` from new inspection rows
- scrubbed already-persisted transient paths from the live DB

Live cleanup result:

- `binary_inspection_artifacts` non-empty `artifact_path` rows for inspection stages: `0`
- `binary_inspections.summary_json` rows containing `workspace_path`: `0`
- `binary_inspections.summary_json` rows containing `probe_path`: `0`

Reason for the cleanup:

- those values referenced files under temp workspaces that are deleted during stage cleanup
- keeping them in the read model would make Phase 3 API/UI surfaces appear to expose inspect artifacts that no longer exist
