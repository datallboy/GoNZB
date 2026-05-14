# Indexer Database Growth Trim Plan

Snapshot date: 2026-05-14

This is the active plan for reducing indexer database growth after the grouping-model sprint proved the release/readiness improvements were landing but overnight retention growth pushed the PostgreSQL database to about `92 GB`.

## Current Finding

The main storage problem is retention and duplication in ingest and identity-audit tables, not releases.

Largest live tables from the overnight run:

- `article_headers`: `33 GB`
- `article_header_ingest_payloads`: `23 GB`
- `binary_grouping_evidence`: `14 GB`
- `binaries`: `11 GB`
- `binary_parts`: about `5 GB`
- `release_family_readiness_summaries`: about `4.6 GB`

That means roughly `70 GB` is concentrated in:

- article/header retention
- grouping evidence retention
- repeated readiness surfaces

## Immediate Goals

1. stop unnecessary per-header retention from growing without bound
2. identify which tables are canonical versus audit/debug-only
3. add bounded retention and cleanup policies for pre-alpha scale testing
4. preserve enough evidence to debug grouping issues without keeping every repeated row forever

## Priority Work

### Phase 1. Reconfirm canonical ownership

Use `docs/archive/completed/indexer/INDEXER_SCHEMA_AND_SERVICE_DATAFLOW.md` as the reference map and answer:

- what must stay in `article_headers`
- what can be compacted or aged out from `article_header_ingest_payloads`
- what portions of `binary_grouping_evidence` need full retention versus rolling retention
- whether `release_family_readiness_summaries` needs pruning for stale families

### Phase 2. Trim ingest payload retention

Target:

- reduce or age out `article_header_ingest_payloads` aggressively once typed fields are persisted and assemble/recovery no longer need the bulky payload row

Candidates:

- null or compact old `raw_overview_json`
- bound retention by age, assembled state, or recovery usefulness
- move retry/backoff-only fields out of the bulky payload row if needed

### Phase 3. Trim grouping evidence retention

Target:

- keep useful identity audit trails without letting `binary_grouping_evidence` grow into a second giant history store

Candidates:

- retain only the most recent or most meaningful evidence rows per binary
- keep promotion/change-point evidence and age out redundant steady-state rows
- add a maintenance path to compact superseded evidence

### Phase 4. Prune stale readiness and weak junk

Target:

- stop stale weak/test families from inflating summaries forever

Candidates:

- age out stale `weak_single_binary` and `stale_cleanup_only` rows whose source binaries are gone or permanently filtered
- explicitly quarantine known test/noise contextual families
- add maintenance metrics for pruned family counts

### Phase 5. Add operator-visible storage reporting

Target:

- make storage problems visible before a one-night run hits `100 GB`

Minimum reporting:

- top table sizes
- row counts
- retained payload/evidence percentages
- cleanup run history

## Validation

Track before and after:

- total database size
- top 10 table sizes
- `article_headers` and `article_header_ingest_payloads` growth per hour
- `binary_grouping_evidence` growth per hour
- release count and actionable family counts to ensure trimming does not regress the grouping improvements

## Sign-Off

Open.

This is now the active indexer execution doc after the grouping-model re-evaluation sprint was signed off on 2026-05-14.
