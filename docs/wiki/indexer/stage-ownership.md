# Indexer Stage Ownership

## Core Rules

- `article_headers` is scrape-owned immutable source fact data. Downstream
  stages must not use it for claim, retry, progress, or completion writes.
- Each long-running stage claims from a stage-owned queue or work table, then
  writes only tables owned by that stage or a documented downstream queue.
- `article_header_assembly_queue` is the assemble claim and progress surface.
  Assemble completes work by writing `binary_parts` and deleting queue rows by
  `(source_posted_at, article_header_id)`.
- `recover_yenc` owns `yenc_recovery_work_items` retry/progress state and
  recovered identity state.
- Release refresh owns release summary and ready-candidate work tables.
- Release formation owns release catalog and release lineage tables.
- Inspect stages own inspection history, artifacts, archive entries, text/media
  evidence, PAR2 sets, and PAR2 targets.
- Source purge and partition retention are the only intentional terminal
  source/work deletion paths.

## Forbidden Hot-Path Writes

- Assemble must not update `article_headers`, `article_headers.assembled_at`,
  or article-header claim columns.
- yEnc recovery must not write retry/progress state into `article_headers` or
  `article_header_ingest_payloads`.
- Downstream stages must not write completion, retry, or claim state back into
  upstream source tables.
- Release formation must not mutate assemble-owned binary projection rows as
  progress state.
- Inspect stages must not mutate scrape or assemble-owned source/projection
  rows.

## Queue And Projection Policy

Stage queues are the only hot-path progress surfaces. A stage may delete,
acknowledge, or update rows in its own queue, and may enqueue documented
downstream work. It must not mark upstream facts as consumed.

Projection tables are owned by the stage that derives them:

- scrape owns article/header facts and scrape-produced input queues;
- assemble owns binary roots, parts, observation, identity, completion, and
  grouping projections;
- yEnc recovery owns recovery work items, recovered identity, superseded-source
  lineage, and recovery-driven dirty-family enqueue;
- release summary refresh owns readiness summaries and ready candidates;
- release formation owns release catalog and lineage;
- inspect owns inspection ready/history/evidence tables.

## Guardrails

`internal/store/pgindex/query_guardrails_test.go` enforces the highest-risk
rules for assemble, yEnc recovery, source joins, and partition-retention target
coverage.
