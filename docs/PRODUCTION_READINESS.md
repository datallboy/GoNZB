# Production Readiness

## Current Assessment

GoNZB is suitable for a controlled personal/private beta after it is installed
on known-good hardware with a fresh, checksummed PostgreSQL database. It is not
yet ready to be exposed directly to the public Internet or advertised as an
unattended production indexer.

The recommended personal layout is one GoNZB application container plus one
PostgreSQL container on the same trusted host, using `compose.yml`. Splitting
GoNZBNet roles into more containers on that same host adds operational work but
does not create independent validation or fault isolation. Independent
validator/health evidence becomes useful when it comes from other trusted
hosts or pool members.

## Verified Baseline

- `go test ./...` passes.
- `go vet ./...` passes.
- `govulncheck ./...` reports no reachable vulnerabilities.
- `npm audit --omit=dev` reports no vulnerabilities.
- The production UI build succeeds.
- A clean Compose deployment reaches `/healthz` and first-run setup while the
  indexer provisions partitions in the background.
- Fresh PostgreSQL installs use data checksums and load `amcheck`,
  `pgstattuple`, `pg_visibility`, and `pg_stat_statements`.
- The complete four-node GoNZBNet E2E suite passes admission quorum, two-pool
  isolation, signed event propagation, authentication/replay defenses,
  release-card search, manifest resolution, cache reuse, real NNTP access, and
  observability reporting.
- The inspected live database had no invalid or definition-equivalent duplicate
  indexes.
- Synthetic RAR and ZIP media-inspection fixtures verify that sparse, bounded
  archive ranges can expose a selected member without downloading the complete
  archive. Matroska members are decoded with the streaming EBML parser and MP4
  members are inspected through bounded `ffprobe` input.

## Completed Audit Remediation

- Reachable Go and production UI dependency vulnerabilities found during the
  audit were upgraded; the recorded baseline reports clean `govulncheck` and
  production `npm audit` results.
- Local settings database files are owner-only, generated runtime stores are
  excluded from the Docker build context, and the checked-in Compose baseline
  publishes the application on localhost by default.
- HTTPS session and CSRF cookies are marked Secure, the runtime `unrar` source
  archive is checksum verified, and fresh PostgreSQL installs enable checksums
  and diagnostic extensions.
- The runtime image uses pinned Alpine 3.23 package revisions. Only the pinned
  `par2cmdline-turbo` package is sourced from edge/testing; stable packages no
  longer inherit the edge repository override.
- Static indexer correctness findings, obsolete service helpers, Newznab
  filtering/pagination behavior, and the original excessive partition horizon
  were addressed in focused changes.
- UI lint is clean and the production TypeScript/Vite build passes. Route-level
  lazy loading reduces the shared application chunk to roughly 244 KB; the
  largest route chunk is roughly 104 KB.
- The frontend has its own Go module boundary, so root `go test ./...` and
  `go vet ./...` no longer traverse Go sources embedded in `ui/node_modules`.
- Direct and archive-backed media metadata inspection is bounded. Single/split
  7z, RAR, ZIP, TAR, and other 7z-readable families use sparse archive probes;
  no complete contained media file is materialized merely for metadata.

## Release Blockers

### Database and hardware integrity

Do not certify an indexer database that has reported invalid pages. Recreate it
only after the host has passed a meaningful memory/storage stability test, then
keep PostgreSQL checksums enabled and run periodic application integrity checks.
Performance results from a corrupt database are not release evidence.

Older defaults provisioned 180 days behind, today, and eight days ahead. On the
inspected database this contributed to 6,157 public tables, 27,235 public
indexes, 33,124 table/index inheritance entries, and 6,076 table partitions;
6,014 of those table partitions had no estimated live tuples. New installs now
use a five-day rolling window and refresh it every six hours. Existing
installations still need an operator-reviewed partition
retention dry run and cleanup; do not blindly drop partitions from a database
that contains wanted backfill data.

`pg_stat_statements` was preloaded but absent from the inspected existing
database, so representative hot-query timing could not be audited. New installs
create it. Before production, run a sustained representative indexing workload
and review total/call time, temporary writes, buffer reads, lock waits, and the
high-volume sequential scans on header, ingest-payload, binary, yEnc, and poster
queue partitions.

### Indexer quality and performance

- Validate bounded archive-member probing against representative live RAR4,
  RAR5, multi-volume, solid, encrypted, ZIP, and 7z posts. Synthetic RAR and
  ZIP fixtures pass, but formats whose selected member or compressed stream
  begins beyond the bounded sparse ranges must remain explicitly inconclusive.
- Run race-enabled tests for the supervisor, assemble, recovery, inspection,
  aggregator, and GoNZBNet paths before release.
- Establish repeatable throughput and resource budgets for latest indexing and
  backfill: headers/second, database growth/day, WAL/day, NNTP bandwidth,
  queue lag, inspection latency, and release yield.
- Add regression plans for the documented expensive yEnc admission and release
  family-summary query shapes.

### Code and UI quality gates

- `staticcheck ./...` currently reports 29 unused declarations, all in legacy
  archive-detail, assembly, release-summary, and daily-bucket store paths.
  Remove those paths in focused commits; do not retain multiple unused
  implementations of hot database operations.
### Security and Internet exposure

- SQLite settings files are restricted to the owning user, but configured NNTP
  and external-indexer credentials remain plaintext inside the settings
  database. Document encrypted-disk/backup requirements or add an explicit
  secret-provider/envelope-encryption design before claiming secrets-at-rest
  protection.
- A brand-new instance can create its initial administrator without prior
  authentication. The safe Compose default binds the published port to
  localhost; direct deployments must preserve that protection or add a
  one-time bootstrap secret before binding publicly.
- Session and CSRF cookies are now marked Secure for HTTPS requests. Production
  still requires a trusted TLS reverse proxy and an explicit forwarded-header
  trust policy.
- External Newznab source URLs are administrator-controlled and can reach
  internal addresses. Add an optional outbound allowlist/private-address policy
  for installations where indexer-source administrators are not fully trusted.
## Usability Work Still Needed

1. Add a first-run deployment preset: personal all-in-one, consumer-only, or
   advanced/custom. A preset should set safe role/stage defaults without hiding
   the resulting settings.
2. Add connection tests for PostgreSQL, each NNTP role/provider, each external
   Newznab source, ARR callbacks, and GoNZBNet peers.
3. Add a client-connection card that shows the exact Newznab URL, creates a
   scoped API token, and gives Radarr/Sonarr/Prowlarr setup instructions.
4. Replace generic readiness alerts with a guided path: configure provider,
   select groups, test access, start latest indexing, observe first binary,
   first release, generated NZB, aggregator result, and client grab.
5. Add safe indexer presets for resource budgets and a storage forecast based
   on selected groups, retention, and measured header volume.
6. Keep the detailed GoNZBNet role activity views, but add a simple outcome
   summary: releases received/published, manifests resolved, validations and
   health samples contributed, peer sync status, and last successful exchange.

## Newznab Compatibility

The API supports capabilities, generic/movie/TV search, numeric/root category
filtering, deterministic newest-first ordering, a bounded 1,000-result search
window with `limit`/`offset`, and NZB retrieval. Before claiming broad client
compatibility, add black-box fixtures for current Radarr, Sonarr, Prowlarr, and
AIOStreams behavior, including empty searches, category roots, pagination,
token failures, duplicate releases, unavailable manifests, and retry behavior.

## Branch and Release Strategy

Treat GoNZBNet observability and production hardening as separate review units.
The observability branch can be reviewed as the feature PR after its E2E result
is attached. Production-readiness work should remain on a follow-up branch and
land through focused commits for dependencies, local-secret handling,
correctness, deployment, partition lifecycle, Newznab compatibility, dead-code
removal, UI lint/code splitting, and measured query tuning.
