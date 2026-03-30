
[ARCHITECTURE.md](/mnt/home-datallboy/Projects/github.com/datallboy/gonzb/ARCHITECTURE.md)

```md
# GoNZB Architecture

GoNZB is a modular monolith for Usenet downloading, aggregated indexer search, and provider-header indexing.

This document describes the current post-Milestone-9 architecture and ownership boundaries.

## Core Design Rules

1. Downloader, Aggregator, and Usenet/NZB Indexer remain separate ownership domains.
2. The Aggregator must not require PostgreSQL for basic operation.
3. The Downloader must not couple back to PostgreSQL indexer internals.
4. The API layer should be transport-focused and compose module-facing dependencies from `app.Context`.
5. Optional module combinations must keep working:
   - downloader-only
   - aggregator-only
   - usenet-indexer-only
   - all-in-one

## Runtime Composition

`internal/app/context.go` is the runtime composition container.

It holds interfaces and shared resources for the enabled modules, such as:

- NNTP manager
- downloader runtime
- queue manager
- NZB parser
- aggregator
- release resolver
- usenet indexer runtime
- job/settings stores
- payload cache/blob store

`router.go` and runtime wiring packages compose module-facing services from this context.

## Major Modules

## Downloader Module

Primary packages:

- `internal/engine`
- `internal/queue`
- `internal/nntp`
- `internal/processor`
- `internal/nzb`
- `internal/store/sqlitejob`

Responsibilities:

- queue and job lifecycle
- NZB hydration and enqueue flows
- NNTP segment download
- repair/extraction/post-processing
- queue/history/files/events APIs
- SAB-compatible downloader endpoints

Storage:

- SQLite for queue/job/history/event metadata
- filesystem working/output directories

## Indexer Manager / Aggregator Module

Primary packages:

- `internal/aggregator`
- `internal/aggregator/sources/*`
- `internal/resolver`
- `internal/store/blob`
- optional cache metadata in `internal/store/sqlitejob`

Responsibilities:

- search configured external indexer sources
- retrieve NZB payloads
- serve native aggregated release search
- serve Newznab-compatible search/get endpoints
- optional payload cache behavior

Key rule:

- The aggregator can use filesystem cache and optional SQLite metadata, but must not require PostgreSQL to function.

## Usenet/NZB Indexer Module

Primary packages:

- `internal/indexing`
- `internal/indexing/scrape`
- `internal/indexing/assemble`
- `internal/indexing/release`
- `internal/indexing/scheduler`
- `internal/store/pgindex`

Responsibilities:

- scrape article/header data
- assemble grouped candidates
- release catalog formation
- scheduled indexing pipeline execution

Storage:

- PostgreSQL catalog/index state

Key rule:

- This module owns PostgreSQL-backed indexing/catalog behavior.

## API Module

Primary packages:

- `internal/api`
- `internal/api/controllers`
- `internal/telemetry`

Responsibilities:

- register routes by enabled module combination
- bind/validate requests
- map HTTP and compatibility responses
- expose health/readiness endpoints

Supported API surfaces:

### Native API
- `/api/v1/queue*` => downloader-owned queue endpoints
- `/api/v1/releases/search` => aggregator-owned native search
- `/api/v1/admin/settings` => runtime settings admin endpoint
- `/api/v1/events/queue` => downloader queue event stream

### Compatibility API
- `/api?mode=...` => SAB-compatible downloader API
- `/api/sab?mode=...` => explicit SAB-compatible downloader API
- `/api?t=...` => Newznab-compatible aggregator API
- `/nzb/:id` => aggregator-owned NZB download endpoint

### Telemetry
- `/healthz`
- `/readyz`

## Web UI Module

Primary packages:

- `internal/webui`

Responsibilities:

- serve the frontend assets when enabled
- consume the API module rather than directly coupling to stores or runtime internals

## Storage Model

GoNZB now has distinct storage responsibilities.

### SQLite
Used for:

- downloader queue/job/history/event state
- runtime settings state
- optional aggregator cache metadata

Primary packages:

- `internal/store/sqlitejob`
- `internal/store/settings`

### Filesystem Blob Store
Used for:

- cached NZB payloads

Primary packages:

- `internal/store/blob`

### PostgreSQL
Used for:

- usenet indexer catalog/index state

Primary package:

- `internal/store/pgindex`

## Runtime Wiring

Primary packages:

- `internal/runtime/wiring`
- `internal/runtime/commands`

Responsibilities:

- build enabled module runtimes
- validate startup requirements
- start server-mode background loops
- apply runtime settings updates safely
- keep disabled modules from starting routes or loops accidentally

## Controller Design Direction

Controllers should stay transport-focused.

Handlers should own:

- Echo request binding
- validation
- response/status selection
- compatibility response envelopes

Controller-facing services or helper files should own:

- orchestration across runtime dependencies
- request-to-module translation
- response mapping/projection
- compatibility status shaping

`router.go` remains the API composition root using `app.Context`.

## Health and Readiness

Readiness is module-aware.

Examples:

- downloader readiness checks queue/runtime/store dependencies
- aggregator readiness checks source/runtime/cache dependencies
- usenet indexer readiness checks PostgreSQL/runtime dependencies

Enabled schema versions are validated before normal operation.

## Current Refactor Priorities

Milestone 10 focuses on:

- controller hardening
- compatibility cleanup
- transport/runtime safety
- health/readiness/schema checks
- controller/runtime cleanup
- docs and terminology alignment

See [INDEXER_PLAN.md](INDEXER_PLAN.md) for the detailed milestone breakdown.
