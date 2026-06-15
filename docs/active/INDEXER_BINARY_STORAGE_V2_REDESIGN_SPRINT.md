# Indexer Binary Storage V2 Redesign Sprint

Snapshot date: 2026-06-15

This is the live sprint document for the binary-storage redesign. It replaces the assumption that the current `binaries` table is safe to keep extending.

## Summary

The repeated corruption incidents should not be treated as maintenance-stage bugs. Maintenance and release reads exposed already-damaged `public.binaries` pages. The architectural problem is that `binaries` became a wide, heavily indexed, multi-owner, high-churn table touched by assemble, recovery, inspection, release refresh, maintenance, and purge.

PostgreSQL SQL should not physically corrupt heap or index pages by itself. The current shape still maximizes WAL pressure, bloat, autovacuum pressure, checkpoint pressure, and concurrent update exposure on the hottest relation. Because this is alpha software and the old database has been deleted, the correct direction is to redesign the binary storage path instead of preserving the old schema.

## Current Touch Audit

Current direct `binaries` write surfaces:

- `assemble_lane_a` and `assemble_lane_b` insert binaries, update existing identity/stat columns, refresh binary stats, and mark release-family summaries dirty.
- `recover_yenc` updates recovered identity fields, can merge binary parts into another binary, and deletes merged source binaries.
- `binary_recovery` updates recovered kind/extension/source/confidence and can rename sibling binary filenames.
- `indexer_maintenance` historically backfilled grouping summaries from old evidence JSON into `binaries`.
- `release_purge_archived_sources` deletes terminal binary rows after NZB generation and archive state allows purge.
- tests still directly update/delete `binaries` in many places and need to be converted as the store boundary hardens.

Current read-heavy surfaces:

- `release_summary_refresh` reads binary family, identity, expected counts, observed counts, payload/auxiliary flags, and recovered fields.
- `release_formation` reads candidate binaries, release-family grouping, parts, and completeness.
- `inspect_*` stages read binary identity and parts to decide whether work is actionable.
- catalog/admin reads join `binaries` for release detail, file lists, and inspection artifacts.
- yEnc work-item backfill and recovery reads `binaries` for candidate selection and merge decisions.

The old ownership exception policy is too broad. Stage write ownership must be enforced by schema and code, not just documentation.

## Target Architecture

The final target is to remove the monolithic hot `binaries` table as the canonical state store. The transition starts with v2 side tables because many current reads and foreign keys still require the existing anchor while the stores are rewritten.

Target table ownership:

- `binary_core`: assemble-owned immutable anchor projection for provider, group, poster, binary key, original binary name, and creation timestamp.
- `binary_observation_stats`: assemble-owned mutable stats for total parts, observed parts, bytes, first/last article number, posted timestamp, and stats timestamp.
- `binary_identity_current`: assemble/projector-owned current grouping identity for release family, file set, file family, expected counts, match confidence, subject-set identity, auxiliary/main-payload flags, and grouping summary scalars.
- `binary_recovery_current`: recovery-owned current recovered kind, extension, source, confidence, recovered filename, and recovery timestamp.
- `binary_lifecycle`: release/archive/purge-owned lifecycle state for release linkage, archive state, purge eligibility, and terminal timestamps.
- `binary_projection_events`: append-only future event stream for identity, recovery, inspection, and lifecycle changes.
- `release_family_dirty_queue`: the only trigger surface for release summary refresh; no raw binary scans should be needed to discover changed families.

Hot JSONB is not acceptable for behavior-bearing mutable state. JSONB remains acceptable for cold evidence artifacts and append-only diagnostics.

## Migration Direction

The database can be rebuilt from scratch, so no corrupt-data migration is required.

Phase A, implemented first:

- Keep the existing `binaries` table as the temporary foreign-key anchor and read compatibility surface.
- Add v2 side tables for stage-owned binary state.
- Dual-write assemble, yEnc recovery, binary recovery, stat refresh, lifecycle purge, and maintenance cleanup into the v2 tables.
- Document table ownership in a database table so ownership can be surfaced and tested.

Phase B:

- Move release-summary-refresh, release formation, inspect selection, yEnc selection, and catalog reads to the v2 side tables.
- Stop reading behavior-bearing fields from the legacy `binaries` row.
- Add SQL ownership tests that fail on forbidden writes.

Phase C:

- Replace `binaries` with a narrow anchor or compatibility view.
- Move foreign keys to the canonical anchor.
- Drop legacy behavior columns and legacy hot indexes.
- Add hash partitioning for high-volume tables.

## Partitioning Plan

Use PostgreSQL declarative hash partitioning once read paths are off the legacy table:

- Partition `article_headers`, `article_header_ingest_payloads`, `assembly_work_items`, `binary_core`, `binary_parts`, `binary_observation_stats`, `binary_identity_current`, `binary_recovery_current`, and high-cardinality dirty/event tables by `HASH(provider_id, newsgroup_id)`.
- Start with 128 partitions.
- Include partition keys in unique constraints where required.
- Use fillfactor 100 for append-mostly tables and 80 for mutable projection/work tables.
- Set per-table autovacuum thresholds on mutable projection/work tables.

## Enforcement Plan

Add enforcement in code and CI:

- Split store access behind narrow interfaces: scrape, assemble, recovery, inspection, release, archive, purge, and maintenance.
- Add a table ownership policy in migrations and mirror it in a Go test.
- Add SQL scanner tests that reject `INSERT`, `UPDATE`, or `DELETE` against tables outside an allowed owner list.
- Treat direct writes to legacy `binaries` outside the temporary bridge as failures.
- Remove test helpers that mutate `binaries` directly unless they are specifically testing the bridge.

## Validation Plan

Before stable schema freeze:

- Fresh database migrations apply cleanly from zero.
- Scrape, assemble, yEnc recovery, release-summary-refresh, release formation, inspect, NZB generation, archive, and purge all run on the v2-backed schema.
- `pg_amcheck` passes on critical heaps and indexes after sustained all-stage soak.
- EXPLAIN plans show no full scans over raw article or binary state for scheduled hot stages.
- Dashboard exposes relation size, dead tuples, autovacuum timestamps, checksum failures, refresh queue depth, and per-stage throughput.

## Explicit Defaults

- PostgreSQL remains the primary store for v0.8.
- Current corrupt data is discarded.
- The first v2 implementation is a bridge, not the final narrow-anchor schema.
- Partition count target is 128.
- Public API and UI behavior should remain stable while internals move.
