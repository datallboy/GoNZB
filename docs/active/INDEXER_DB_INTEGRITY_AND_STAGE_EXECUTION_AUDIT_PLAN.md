# Indexer DB Integrity And Stage Execution Audit Plan

Snapshot date: 2026-06-11

This is the active execution guide for the database-integrity audit, hot-query audit, and stage-execution model hardening workstream.

Use this doc together with:

- `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` for the living ownership matrix and allowed stage/table interactions
- `docs/active/INDEXER_FOUNDATION_DOCS.md` for current routing of active versus archived docs

## Scope

- determine the root cause category for the unrecoverable `article_headers` index corruption incident
- audit the highest-risk PostgreSQL write and materialization paths before a stable release
- harden the stage execution model so the system can preserve concurrent stages without unsafe overlap
- define the fresh-database bootstrap profile and the steady-state serve profile
- freeze schema only after the audit shows no more structural changes are needed

## Working Decisions

- do not switch the system to a permanently sequential global pipeline
- keep the long-term model concurrent, but only where hot-table ownership and lock behavior are compatible
- treat `scrape_*` as the highest-risk writer and isolate it operationally during rebuild/bootstrap
- require integrity checks before scrape is allowed to run against a live cluster
- prefer staged/bootstrap profiles and overlap gates before introducing a multi-process topology
- use multi-process stage commands as a later deployment/ops feature, not the first fix

## Active Audit Threads

### 1. Database integrity and incident capture

- map the exact failing relation(s), index definitions, and PostgreSQL error signatures
- keep integrity tooling in-tree:
  - `indexer maintenance check-integrity`
  - `indexer maintenance reindex-critical`
- document which failures are recoverable with reindex and which imply deeper heap / transaction-status corruption

### 2. Hot DBO/query audit

Audit in this order:

1. `InsertArticleHeaders`
2. scrape checkpoint and duplicate-resolution queries
3. any hot `INSERT ... ON CONFLICT` write path on canonical tables
4. `assemble_*` writes to `binaries` / `binary_parts`
5. `recover_yenc` work-item seeding and candidate selection
6. `release_summary_refresh` Phase A / Phase B
7. maintenance cleanup queries on hot tables

For each path, record:

- owning stage
- hot tables/indexes touched
- lock behavior
- expected overlap-safe stages
- whether the path belongs in bootstrap-only or steady-state serve

### 3. Stage execution model

Define and enforce three execution profiles:

#### Bootstrap / fresh DB

- allow `scrape_latest` and `scrape_backfill`
- keep `assemble_*`, `recover_yenc`, `release_summary_refresh`, `release`, inspect, and archive tail disabled

#### Build / regroup

- stop scrape
- allow `assemble_lane_a`, `assemble_lane_b`, `recover_yenc`, `release_summary_refresh`, `release`
- keep inspect and archive tail disabled

#### Steady state

- re-enable inspect and archive/NZB stages only after release formation is healthy
- add overlap rules so `scrape_*` does not run concurrently with the hottest regroup/materialization stages unless explicitly allowed by policy

### 4. Schema freeze readiness

Before calling the schema stable:

- finish the hot-path audit
- confirm the current migrations and tables cover the needed runtime state
- avoid new schema unless the audit reveals a genuine structural gap
- split dirty work into logical commits so the freeze point is reviewable

## Immediate Deliverables

- active doc and schema-doc updates describing the execution model and integrity guardrails
- integrity tooling and scrape preflight guard
- commit split for the current dirty tree
- fresh-DB bring-up playbook using the bootstrap/build/steady-state profiles

## Live Bootstrap Status

Validated on `2026-06-11` against a clean database:

- `indexer maintenance check-integrity --ensure-extension` passed for:
  - `article_headers_pkey`
  - `article_headers_newsgroup_id_article_number_key`
  - `article_headers_newsgroup_id_message_id_key`
- scrape-only bootstrap is currently the active rebuild profile
- `indexer scrape latest` and `indexer scrape backfill` were both launched in CLI mode, not via full `serve`
- concurrent scrape-only execution is currently healthy on the fresh DB

Observed live behavior after the restart:

- `scrape_latest` resumed completing runs after the first bad historical run
- `scrape_backfill` started cleanly and began inserting 20k-row ranges
- `article_headers` grew past `180k` rows without a repeat of the earlier ingest failure

Fresh issue found and fixed during bootstrap:

- some NNTP strings included embedded NUL bytes
- Go still treats those strings as valid UTF-8, so the old sanitizer let them through
- PostgreSQL then rejected poster inserts with:
  - `ERROR: invalid byte sequence for encoding "UTF8": 0x00`
- fix applied:
  - strip `\\x00` in `sanitizeUTF8()` before any header/payload/poster DB write
  - add regression coverage on `InsertArticleHeaders`

Current operational guidance:

- continue building header backlog with CLI scrape-only commands first
- do not enable assemble/recover/release stages until the scrape-only bootstrap has accumulated a meaningful backlog and remains stable over a longer window

## Acceptance Criteria

- the live schema doc clearly states which stages may overlap and which should be isolated
- scrape is blocked when critical ingest indexes fail integrity checks
- the fresh-database rebuild order is documented and testable
- current dirty changes are split into reviewable logical commits on a dedicated audit branch
- the remaining audit backlog is explicit enough to drive the next implementation passes without re-deciding the execution model
