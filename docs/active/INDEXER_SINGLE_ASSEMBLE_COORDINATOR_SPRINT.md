# Indexer Single Assemble Coordinator Sprint

Snapshot date: 2026-06-17

This is the active execution note for replacing the split `assemble_lane_a` / `assemble_lane_b` runtime model with one canonical `assemble` stage.

## Target

`assemble` is the only scheduled binary assembly writer. Lane A and Lane B remain internal work classes:

- Lane A completes existing partial binaries using `binary_completion_keys` and structured queue rows.
- Lane B creates new binary anchors from general queued article headers.
- The coordinator claims a mixed batch from `article_header_assembly_queue` and exposes the split through `lane_a_selected` and `lane_b_selected` metrics.

## Current Implementation

- `article_header_assembly_queue` is the assemble header claim surface.
- Scrape seeds queue rows as downstream work signals.
- Assemble owns queue claims, errors, and completion deletes.
- `article_headers` is no longer used for assemble claim/progress writes.
- Successful `binary_parts` writes delete completed queue rows.
- `UpsertBinaryParts` stages rows in `tmp_binary_parts`, uses fixed array bind parameters, writes parts in deterministic `(binary_id, part_number, article_header_id)` order, and deletes queue rows through the temp table.
- Supervisor, CLI, runtime settings, API stage lists, and Admin UI expose `assemble` only.

## Runtime Settings

`indexing.assemble` owns:

- `enabled`
- `interval_minutes`
- `batch_size`
- `concurrency`
- `lane_a_target_pct`
- `lane_b_min_pct`
- `binary_upsert_db_chunk_size`

Defaults:

- `lane_a_target_pct`: `70`
- `lane_b_min_pct`: `30`
- `binary_upsert_db_chunk_size`: `250`

## Validation

Required checks for this sprint:

- `go test ./...`
- `npm run build` from `ui/`
- Fresh database migration and scrape/assemble soak before release promotion.
- EXPLAIN on the queue claim query with realistic queue cardinality before increasing supervisor defaults.
