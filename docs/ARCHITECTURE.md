# GoNZB Architecture

GoNZB is a modular monolith for Usenet downloading, aggregated indexer search, and provider-header indexing.

This document describes the current architecture after the modular stabilization work. It is the main reference for how modules are organized, how runtime wiring works, how API and CLI behavior map to module ownership, and how new features should be added without reintroducing cross-module coupling.

## Core Design Rules

1. Downloader, Aggregator, and Usenet/NZB Indexer are separate ownership domains.
2. The Aggregator must not require PostgreSQL for normal operation.
3. The Downloader must not couple to PostgreSQL indexing internals.
4. The Web UI consumes API surfaces; it does not own storage or runtime internals.
5. The API layer stays transport-focused and talks to module-facing facades.
6. Optional module combinations must keep working:
   - downloader-only
   - aggregator-only
   - usenet-indexer-only
   - all-in-one

## Design Pattern

GoNZB now follows a consistent modular-monolith pattern:

1. Runtime wiring builds concrete implementations.
2. `app.Context` holds shared runtime state and module registrations.
3. Module facades expose use cases to transport or other modules.
4. Commands and queries are separated where practical.
5. Long-running runtime behavior is managed through runtime modules.
6. Controllers and CLI commands call facades or module services; they do not assemble internals.

This is not a full framework-heavy DDD system. It is a pragmatic ports-and-adapters shape:

- domain types live near the center
- use-case code lives in module packages
- storage/network/transport are adapters
- `internal/runtime/wiring` is the composition root

## Runtime Composition

### `app.Context`

`internal/app/context.go` is the shared runtime container.

It now carries:

- bootstrap config
- effective config
- logger
- shared runtime dependencies
- module facades
- runtime module registry
- resource closers

`app.Context` still exists because GoNZB runs as one process, but new feature code should avoid taking `*app.Context` directly unless it is part of runtime wiring.

### Runtime Modules

`internal/app/contracts.go` defines a runtime module contract:

- `Name()`
- `Enabled()`
- `Build(ctx)`
- `Start(ctx)`
- `Reload(ctx)`
- `Close()`
- `ReadinessChecks(ctx)`

`internal/runtime/wiring/runtime_modules.go` registers the current runtime modules:

- downloader
- aggregator
- usenet-indexer
- arr-notifier

Current behavior:

- startup calls `Build`
- long-running server behavior calls `Start`
- settings reload calls `Reload`
- shutdown calls `Close`
- readiness probes call `ReadinessChecks`

This keeps startup, reload, readiness, and shutdown using the same module boundary instead of separate ad hoc logic paths.

### Module Facades

`app.Context` now exposes module-facing facades instead of asking transport code to construct concrete services itself.

Current facades:

- `DownloaderModule`
- `AggregatorModule`
- `SettingsAdmin`

This keeps transport code dependent on module behavior, not internal package wiring.

## Current Modules

## Downloader Module

Primary packages:

- `internal/downloader`
- `internal/engine`
- `internal/nntp`
- `internal/processor`
- `internal/nzb`
- `internal/store/sqlitejob`

Responsibilities:

- queueing jobs
- manual NZB enqueue
- release-based enqueue
- queue lifecycle and status transitions
- NZB hydration and task preparation
- segment download
- repair/extraction/post-processing
- downloader queue/history/files/events APIs
- SAB-compatible downloader behavior

Internal structure:

- `internal/downloader/commands.go`
  - enqueue
  - cancel
  - clear history
  - delete terminal jobs
  - pause/resume
- `internal/downloader/queries.go`
  - list queue
  - list history
  - get queue item
  - get files
  - get events
  - paused state
- `internal/engine/manager.go`
  - owns queue state
  - owns scheduling
  - owns live active-item tracking
- `internal/engine/workflow.go`
  - owns per-job execution flow:
    - hydrate
    - download
    - post-process
    - finalize handoff

Storage:

- SQLite for queue/job/history/event metadata
- filesystem working/output directories

Key rule:

- downloader features must not depend on PostgreSQL release/indexing internals

## Indexer Manager / Aggregator Module

Primary packages:

- `internal/aggregator`
- `internal/aggregator/sources/*`
- `internal/resolver`
- `internal/store/blob`
- optional cache metadata in `internal/store/sqlitejob`

Responsibilities:

- search configured external indexer sources
- merge and normalize search results
- retrieve NZB payloads
- optionally cache payloads
- serve native aggregated release search
- serve Newznab-compatible search/get behavior

Current facade:

- `AggregatorModule`

Storage:

- filesystem blob store for NZB payloads
- optional SQLite cache metadata for search/cache state

Key rule:

- the aggregator can use filesystem cache and optional SQLite metadata, but must not require PostgreSQL

## Usenet/NZB Indexer Module

Primary packages:

- `internal/indexing`
- `internal/indexing/scrape`
- `internal/indexing/assemble`
- `internal/indexing/release`
- `internal/indexing/scheduler`
- `internal/store/pgindex`

Responsibilities:

- scrape provider article/header data
- track checkpoints
- assemble grouped binaries
- form releases
- maintain PostgreSQL-backed release catalog state
- run scheduled indexing pipelines

Storage:

- PostgreSQL catalog/index state

Runtime notes:

- the usenet indexer runtime now derives a dedicated runtime config before runtime construction
- scrape transport selection is isolated in runtime config derivation
- the current default still uses the first configured NNTP server as the scrape transport

Key rule:

- this module owns PostgreSQL-backed catalog/index behavior

## API Module

Primary packages:

- `internal/api`
- `internal/api/controllers`
- `internal/telemetry`

Responsibilities:

- register routes based on enabled modules
- bind and validate requests
- map transport DTOs
- call module facades
- expose health and readiness endpoints

Controller rule:

- controllers should bind, validate, map, and call facades
- controllers should not build runtimes, stores, or queue/aggregator internals

## Web UI Module

Primary packages:

- `internal/webui`
- `ui/`

Responsibilities:

- serve frontend assets when enabled
- consume API routes

Key rule:

- Web UI does not bypass API and should not couple directly to stores or runtime internals

## ARR Notifier Runtime Support

Primary packages:

- `internal/integrations/arr`
- `internal/runtime/wiring/arr.go`

Responsibilities:

- react to terminal downloader outcomes
- notify external ARR tools when configured

This is treated as a runtime support module, not a primary ownership domain like downloader or indexer.

## API Behavior

The API surface remains module-owned even though transport lives under `internal/api`.

### Native API

Downloader-owned:

- `GET /api/v1/queue`
- `GET /api/v1/queue/history`
- `GET /api/v1/queue/:id`
- `GET /api/v1/queue/:id/files`
- `GET /api/v1/queue/:id/events`
- `POST /api/v1/queue`
- `POST /api/v1/queue/:id/cancel`
- `POST /api/v1/queue/bulk/cancel`
- `POST /api/v1/queue/bulk/delete`
- `POST /api/v1/queue/history/clear`
- `GET /api/v1/events/queue`

Aggregator-owned:

- `GET /api/v1/releases/search`
- `GET /nzb/:id`

Admin/runtime settings:

- `GET /api/v1/admin/settings`
- `PUT /api/v1/admin/settings`

Telemetry:

- `GET /healthz`
- `GET /readyz`

### Compatibility API

Explicit compatibility surfaces:

- `/api?mode=...` => SAB-compatible downloader behavior
- `/api/sab?mode=...` => explicit SAB-compatible downloader behavior
- `/api?t=...` => Newznab-compatible aggregator behavior
- `/nzb/:id` => direct NZB fetch under aggregator ownership

### Route Registration Rules

Routes are only registered when their owning modules are enabled.

Examples:

- downloader-only mode exposes queue and SAB routes, but not release search
- aggregator-only mode exposes search/get routes, but not queue routes
- usenet-indexer-only does not automatically expose downloader or aggregator transport

## CLI Behavior

Primary command entrypoint:

- `cmd/gonzb/main.go`

Runtime command wiring:

- `internal/runtime/commands`

Current CLI behavior:

- `gonzb serve`
  - starts server/API mode
  - builds enabled runtimes
  - validates readiness before serving
- `gonzb --file <nzb>`
  - runs manual downloader flow for a single NZB
- `gonzb indexer scrape`
  - compatibility scrape command
- `gonzb indexer scrape latest`
  - latest scrape mode
- `gonzb indexer scrape backfill --once`
  - backfill scrape mode
- `gonzb indexer assemble --once`
  - assemble pass
- `gonzb indexer release --once`
  - release pass
- `gonzb indexer pipeline --once`
  - full scrape/assemble/release pipeline

CLI rules:

- CLI commands should call runtime commands or module services
- CLI should not duplicate business logic that already exists in a module
- if a new feature needs CLI support, add the use case first, then add the command adapter

## Storage Model

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
- payload durability for downloader/aggregator flows

Primary packages:

- `internal/store/blob`

### PostgreSQL

Used for:

- usenet indexer catalog/index state

Primary package:

- `internal/store/pgindex`

## Health, Readiness, and Settings Reload

### Health and Readiness

`internal/telemetry/readiness.go` now reads module-owned readiness checks from the runtime module registry.

That means readiness is now reported by module contracts rather than by hand-inspecting raw fields everywhere.

Examples:

- downloader checks queue manager, downloader runtime, parser, NNTP manager, and SQLite store health
- aggregator checks runtime presence, payload store, and optional cache/store readiness
- usenet indexer checks runtime presence, PostgreSQL readiness, and settings store readiness

### Settings Reload

`internal/runtime/wiring/settings.go` watches runtime settings changes and asks runtime modules to reload themselves.

Current behavior:

- effective config is recalculated
- runtime modules are reloaded through the registry
- downloader reload may still defer while work is active
- readiness continues to use the same runtime module layer

## How To Add New Features

## Adding A Feature To An Existing Module

Use this order:

1. Decide which module owns the feature.
2. Add or extend a module use case first.
3. Add storage/network adapters only where needed.
4. Call the use case from HTTP, CLI, scheduler, or integration code.
5. Add tests at the module seam.

Do:

- add downloader writes to downloader commands
- add downloader reads to downloader queries
- add aggregator search/get features behind aggregator facade logic
- add indexer scrape/assemble/release features inside `internal/indexing/*`

Do not:

- put new feature logic directly in controllers
- pass `*app.Context` into new feature logic outside runtime wiring
- add cross-module shortcuts to reach another module’s storage directly

## Adding A New Module

If GoNZB gains a new major module:

1. Define its ownership clearly.
2. Decide whether it needs:
   - a facade
   - a runtime module
   - storage
   - API routes
   - CLI commands
3. Register it through `internal/runtime/wiring`.
4. Add readiness checks through the runtime module contract.
5. Keep API/CLI adapters thin.

A new module should be able to answer:

- what it owns
- what it depends on
- what storage it needs
- what routes/commands it exposes
- whether it can be enabled independently

## Adding Features To The Usenet Indexer

New indexing features should follow the existing indexing stages instead of mixing concerns:

- scrape
- assemble
- release
- scheduler/runtime

Examples:

- new article/header ingestion behavior belongs in `internal/indexing/scrape`
- binary grouping or matching changes belong in `internal/indexing/assemble` or `internal/indexing/match`
- release formation changes belong in `internal/indexing/release`
- schedule/restart behavior belongs in `internal/indexing/scheduler` or runtime wiring
- PG schema/repository changes belong in `internal/store/pgindex`

Rules for new usenet indexing work:

1. Keep PostgreSQL ownership inside the usenet indexer module.
2. Do not leak PG catalog assumptions into downloader or aggregator code.
3. If a feature needs API exposure later, add a module use case first, then a transport adapter.
4. If a feature needs runtime configuration, add it to indexer runtime config derivation instead of hardcoding it inside deep runtime assembly.

## Code Writing Rules For Future Work

1. Start with the owning module.
2. Add a use case before adding adapters.
3. Keep transport code transport-only.
4. Keep commands and queries separate where it improves ownership clarity.
5. Keep runtime behavior behind runtime modules.
6. Preserve optional deployment combinations.
7. Prefer explicit interfaces at module boundaries, not generic shared helpers.

## Current Testing Direction

The current architecture now has coverage for:

- downloader command/query seams
- route registration behavior
- runtime-module readiness behavior

Future coverage that is still valuable:

- module-combination startup tests
- settings reload tests
- more queue workflow tests
- more usenet indexer runtime tests

## Related Documents

- [MODULAR_REFACTOR.md](MODULAR_REFACTOR.md)
- [README.md](../README.md)
- [INDEXER_PLAN.md](archive/INDEXER_PLAN.md)
