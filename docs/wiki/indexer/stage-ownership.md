# Indexer Stage Ownership

## Core Rules

- `article_headers` is scrape-owned immutable source fact data. Downstream
  stages must not use it for claim, retry, progress, or completion writes.
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
- Release formation must not mutate assemble-owned binary projection rows as
  progress state.
- Inspect stages must not mutate scrape or assemble-owned source/projection
  rows.

## Guardrails

`internal/store/pgindex/query_guardrails_test.go` enforces the highest-risk
rules for assemble, yEnc recovery, source joins, and partition-retention target
coverage.
