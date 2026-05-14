# Indexer Database Growth Trim Plan

Snapshot date: 2026-05-14

This is the active plan for reducing indexer database growth after the grouping-model sprint proved the release/readiness improvements were landing but overnight retention growth pushed the PostgreSQL database to about `92 GB`.

This plan is the execution tracker. Use `docs/active/INDEXER_DATABASE_SCHEMA_AUDIT.md` as the live current-state audit and column-ownership reference for this sprint.

This sprint runs on one branch. Treat the commit order below as the working execution order for Codex sessions on that branch.

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

## Execution Tracks

### Track 0. Documentation baseline

Goals:

- keep this doc as the active sprint entrypoint
- maintain a separate active audit tracker for schema truth and column usage
- keep `docs/active/INDEXER_FOUNDATION_DOCS.md` aligned with both active docs

Deliverables:

- active execution plan
- active schema audit doc
- updated foundation doc pointers

## Priority Work

### Phase 1. Reconfirm canonical ownership

Use `docs/archive/completed/indexer/INDEXER_SCHEMA_AND_SERVICE_DATAFLOW.md` as the reference map and answer:

- what must stay in `article_headers`
- what can be compacted or aged out from `article_header_ingest_payloads`
- what portions of `binary_grouping_evidence` need full retention versus rolling retention
- whether `release_family_readiness_summaries` needs pruning for stale families

Deliverables:

- completed hot-table column inventory in the active schema audit doc
- per-column writer and reader map
- keep, compact, prune, migrate, or drop-candidate disposition for each hot-table column

### Phase 2. Trim ingest payload retention

Target:

- reduce or age out `article_header_ingest_payloads` aggressively once typed fields are persisted and assemble/recovery no longer need the bulky payload row

Candidates:

- null or compact old `raw_overview_json`
- bound retention by age, assembled state, or recovery usefulness
- move retry/backoff-only fields out of the bulky payload row if needed

Implementation notes:

- do not remove payload fields until the audit confirms all remaining readers and writers
- split low-risk retention cleanup from any schema move or field removal

### Phase 3. Trim grouping evidence retention

Target:

- keep useful identity audit trails without letting `binary_grouping_evidence` grow into a second giant history store

Candidates:

- retain only the most recent or most meaningful evidence rows per binary
- keep promotion/change-point evidence and age out redundant steady-state rows
- add a maintenance path to compact superseded evidence

Implementation notes:

- audit whether `binaries.grouping_evidence_json` and `binary_grouping_evidence` currently duplicate each other
- keep enough promotion history to debug grouping regressions after cleanup

### Phase 4. Prune stale readiness and weak junk

Target:

- stop stale weak/test families from inflating summaries forever

Candidates:

- age out stale `weak_single_binary` and `stale_cleanup_only` rows whose source binaries are gone or permanently filtered
- explicitly quarantine known test/noise contextual families
- add maintenance metrics for pruned family counts

Implementation notes:

- validate which readiness buckets are productively consumed versus stale queue residue
- keep actionable family visibility and release throughput checks intact

### Phase 5. Add operator-visible storage reporting

Target:

- make storage problems visible before a one-night run hits `100 GB`

Minimum reporting:

- top table sizes
- row counts
- retained payload/evidence percentages
- cleanup run history

## Handoff Action Items

Use these as concise Codex-sized work chunks. Each chunk should update either this plan, the schema audit doc, or both.

Codex ownership for this sprint:

- perform the live Docker database review
- audit schema columns against code usage and active migrations
- document current-state findings and cleanup decisions
- implement the approved code, migration, and documentation changes on this same branch
- validate before and after behavior and storage outcomes

## Suggested Commit Order

Use this order for commits on the sprint branch unless a later audit finding forces a dependency change.

### Commit 1. `docs-baseline-sync`

Purpose:

- lock the active docs structure
- keep this file as the sprint entrypoint
- keep the schema audit doc as the source of truth for current-state findings

Expected commit contents:

- active doc pointer fixes
- sprint workflow notes
- no schema or code behavior changes

### Commit 2. `schema-audit-live-db`

Purpose:

- capture live table sizes, live columns, row counts, and schema notes from Docker Postgres
- reconcile live shape against `internal/store/pgindex/migrations`

Expected commit contents:

- audit doc baseline sections filled in
- documented schema mismatches or under-documented live fields
- no code behavior changes

### Commit 3. `code-usage-map-ingest-and-assembly`

Purpose:

- map `article_headers` and `article_header_ingest_payloads` readers and writers
- classify ingest fields as canonical, derived, debug-only, or cleanup candidates

Expected commit contents:

- audit doc updates for ingest ownership
- links to store and service code paths
- no schema changes yet

### Commit 4. `code-usage-map-binary-identity`

Purpose:

- map `binaries`, `binary_parts`, and `binary_grouping_evidence`
- identify duplicated grouping evidence and identity helper fields

Expected commit contents:

- audit doc updates for binary identity ownership
- keep versus compact versus migrate recommendations
- no schema changes yet

### Commit 5. `code-usage-map-readiness-and-ui`

Purpose:

- map `release_family_readiness_summaries` usage across release selection, admin, API, and debug surfaces
- identify stale weak-family dependencies

Expected commit contents:

- audit doc updates for readiness ownership
- bucket-usage findings
- no schema changes yet

### Commit 6. `trim-policy-payloads-and-evidence`

Purpose:

- turn audit findings into explicit retention and compaction rules for ingest payloads and grouping evidence

Expected commit contents:

- plan updates with cleanup predicates
- validation criteria for future implementation commits
- docs-only unless a trivial safe cleanup is proven

### Commit 7. `trim-policy-readiness-and-junk-families`

Purpose:

- define prune rules for stale readiness surfaces and weak junk families
- define required operator-visible metrics

Expected commit contents:

- plan updates for readiness cleanup policy
- non-regression checkpoints
- docs-only unless a trivial safe cleanup is proven

### Commit 8. `cleanup-implementation-wave-1`

Purpose:

- apply low-risk cleanup supported by the completed audit
- prefer bounded retention, doc/query cleanup, and dead-code removal before risky schema surgery

Expected commit contents:

- code and migration changes that are low risk
- validation query updates if needed
- before and after measurements added to the active docs

### Commit 9. `cleanup-implementation-wave-2`

Purpose:

- apply higher-risk changes that require schema rewiring, migration updates, or UI/debug surface cleanup

Expected commit contents:

- remaining code and migration changes
- active doc updates describing what moved, what was removed, and why
- validation evidence

### Commit 10. `validation-and-signoff`

Purpose:

- close the sprint with measured results and remaining follow-up items

Expected commit contents:

- final storage and behavior measurements
- resolved versus deferred cleanup items
- sign-off notes in the active docs

### 1. `docs-baseline-sync`

Scope:

- ensure the active docs point at the current sprint correctly
- preserve this doc as the active entrypoint
- add and wire the schema audit tracker

Completion check:

- foundation doc references both active docs correctly

### 2. `schema-audit-live-db`

Scope:

- capture live table sizes, row counts, and column lists from Docker Postgres
- reconcile the hot tables against `internal/store/pgindex/migrations`

Completion check:

- active audit doc has live schema sections for all hot tables

### 3. `code-usage-map-ingest-and-assembly`

Scope:

- document where `article_headers` and `article_header_ingest_payloads` are written and read
- identify hot-path versus debug-only ingest fields

Completion check:

- ingest tables have reader/writer ownership and disposition notes

### 4. `code-usage-map-binary-identity`

Scope:

- document `binaries`, `binary_parts`, and `binary_grouping_evidence`
- identify redundant inline versus side-table grouping evidence

Completion check:

- binary identity tables have clear canonical versus audit-only decisions

### 5. `code-usage-map-readiness-and-ui`

Scope:

- document how `release_family_readiness_summaries` is used by release selection, admin, and debug surfaces
- identify stale or weak-family-only surfaces that keep large state alive

Completion check:

- readiness summary columns and bucket usage are mapped to concrete code surfaces

### 6. `trim-policy-payloads-and-evidence`

Scope:

- define retention or compaction policy for payload rows and grouping evidence
- separate low-risk cleanup from changes that require schema movement

Completion check:

- this plan includes concrete cleanup predicates and validation notes

### 7. `trim-policy-readiness-and-junk-families`

Scope:

- define prune policy for stale readiness buckets and weak junk families
- define operator-visible metrics for cleanup outcomes

Completion check:

- readiness cleanup rules are documented with non-regression checks

### 8. `cleanup-implementation-wave-1`

Scope:

- execute low-risk removals or retention changes supported by the audit
- update docs and validation queries as needed

Completion check:

- before/after storage and behavior metrics are recorded

### 9. `cleanup-implementation-wave-2`

Scope:

- execute schema or code changes that require migrations, rewiring, or UI/debug surface cleanup

Completion check:

- implementation notes and validation results are appended to the active docs

## Codex Session Rules

For each commit-sized session:

1. read this plan and the schema audit doc first
2. update the docs with findings before removing columns or changing retention behavior
3. use the Docker Postgres database as source of truth for current state
4. reconcile against `internal/store/pgindex/migrations`, not `migrations_archive`
5. keep changes focused on the current commit purpose
6. record validation evidence or blockers in the active docs before moving to the next commit

## Validation

Track before and after:

- total database size
- top 10 table sizes
- `article_headers` and `article_header_ingest_payloads` growth per hour
- `binary_grouping_evidence` growth per hour
- release count and actionable family counts to ensure trimming does not regress the grouping improvements

Per implementation wave, also track:

- readiness bucket distribution changes
- any changes in admin/debug surface dependencies
- whether release visibility, actionable families, and grouping quality remain stable

## Sign-Off

Open.

This is now the active indexer execution doc after the grouping-model re-evaluation sprint was signed off on 2026-05-14.
