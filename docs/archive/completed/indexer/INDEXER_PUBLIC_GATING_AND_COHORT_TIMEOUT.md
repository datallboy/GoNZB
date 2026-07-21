# Indexer Public Gating And Cohort Timeout

## Scope

- Make conclusive, unencrypted partial-volume archive inspections eligible for
  public release readiness.
- Keep subject-cohort scheduling below its statement timeout under a large
  partitioned backlog.
- Ensure GoNZBNet publication uses the same release-readiness policy as the
  local indexer catalog.
- Preserve bootstrap aggregator source settings for a fresh node with no
  persisted runtime settings revision.

## Findings

- The persisted public completion threshold was already 90 percent. Releases
  at roughly 99 percent overall completion also had complete main payloads.
- A leading RAR volume produced a valid 7-Zip listing with `Encrypted = -`, but
  the expected partial-volume warning caused the archive service to leave the
  release password state as `unknown`.
- Subject-cohort scheduling attempted two equivalent large scans and up to
  20,000 queue writes under a 20-second per-statement timeout while disabling
  PostgreSQL hash and merge joins across the partitioned queues.
- The GoNZBNet publisher used the hardcoded default readiness policy instead of
  the persisted indexer policy.
- A fresh settings database ignored the supplied bootstrap config and returned
  disabled runtime defaults, which disabled the configured GoNZBNet aggregator
  source on a consumer-only node.

## Implementation

- Accept a negative archive-encryption result despite a probe warning only
  when the listing contains at least one parsed archive entry.
- Requeue legacy completed archive inspections that have conclusive
  unencrypted entries but an `unknown` release rollup.
- Admit at most 1,000 subject-cohort rows per scheduler pass while retaining
  the existing 20,000-row queue capacity.
- Materialize the eligible article set once per pass and share it between the
  cohort-state upsert and assembly-queue insert.
- Let PostgreSQL choose parallel hash/merge plans for the partitioned cohort
  eligibility joins instead of forcing nested-loop plans.
- Pass the runtime release-readiness policy into local ReleaseCard candidate
  projection.
- Derive fresh runtime settings from the bootstrap config when no structured
  settings or legacy revision exists.

## Validation

- `go test ./...` passes.
- Nine consecutive live cohort passes completed in approximately 7.9 to 14.6
  seconds while admitting 1,000 rows per pass, with no statement timeouts.
- The local public indexer endpoint returns 10 payload-complete releases.
- The indexer publishes signed ReleaseCard and ResolutionManifest events for
  those releases into `pool.friends`.
- The consumer receives all 10 cards, returns them through Newznab search, and
  successfully fetches a manifest and generates a 1,526,607-byte NZB on grab.

## Existing Database Issue

The reused indexer database has an unrelated invalid PostgreSQL TOAST page for
`resolution_manifests`. Repair requires a separate, explicitly approved
table-level recovery operation; this work does not alter or discard that data.
