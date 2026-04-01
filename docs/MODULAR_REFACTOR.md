# Modular Refactor

This document tracks the modular-monolith stabilization work that was planned before adding more features.

## Current Status

Completed:

- `app.Context` now exposes module-facing facades instead of forcing transport code to construct concrete services directly.
- Downloader command/query behavior now lives under `internal/downloader/`.
- API controllers and route wiring now consume facades instead of the old `internal/queue/service.go`.
- The legacy `internal/queue/service.go` implementation was removed.
- Runtime wiring now has a module registry for lifecycle behavior instead of ad hoc start/reload handling only.
- Readiness is now sourced from runtime-module checks instead of direct field inspection in telemetry.
- Queue execution flow has been split so `engine.QueueManager` handles queue state/scheduling and `internal/engine/workflow.go` owns per-job execution steps.
- Indexer runtime config is normalized before runtime construction.

Still intentionally lighter than the original ambition:

- Runtime modules are formalized for `Build`, `Start`, `Reload`, `Close`, and `ReadinessChecks`, but they are still pragmatic wrappers around the current runtime assembly rather than a fully isolated module container system.
- Test coverage is improved at the downloader seam, but the broader startup/route/reload matrix is still worth expanding.

## Implemented Direction

### 1. Narrow composition root

Implemented:

- `internal/runtime/wiring` remains the composition root.
- `app.Context` now carries:
  - runtime references
  - module facades
  - runtime module registry
- Transport code now depends on:
  - `DownloaderModule`
  - `AggregatorModule`
  - `SettingsAdmin`

Notes:

- `app.Context` is still present as the shared runtime container, but new feature code should prefer module or use-case interfaces over direct `*app.Context` access.

### 2. Split downloader behavior

Implemented:

- `internal/downloader/commands.go`
- `internal/downloader/queries.go`
- `internal/downloader/module.go`

Implemented queue execution split:

- `internal/engine/manager.go` now focuses on queue ownership, selection, and live runtime state.
- `internal/engine/workflow.go` owns:
  - hydrate
  - download
  - post-process
  - finalize handoff

Removed:

- `internal/queue/service.go`

### 3. Runtime lifecycle contract

Implemented runtime module contract:

- `Build`
- `Start`
- `Reload`
- `Close`
- `ReadinessChecks`

Current runtime modules:

- downloader
- aggregator
- usenet-indexer
- arr-notifier

Current behavior:

- startup builds runtime modules through the registry
- server mode starts long-running modules through the registry
- settings reload reuses the same registry
- readiness reads the same registry

### 4. Indexer boundary cleanup

Implemented:

- usenet indexer runtime config is derived before runtime assembly
- the scrape transport choice is now isolated in runtime config derivation instead of being hardcoded inline deep in assembly logic

Current default:

- the first configured NNTP server is still used as the scrape transport

That default is now explicit and isolated, so replacing it later will be a targeted runtime change instead of a cross-cutting refactor.

## Rules For New Features

1. Start with the owning module.

- downloader
- aggregator
- usenet-indexer
- API
- Web UI

2. Add a use case before adding adapters.

- Put command/query behavior in the owning module first.
- Then call it from HTTP, CLI, scheduler, or integration code.

3. Do not pass `*app.Context` into new feature logic unless the code is part of runtime wiring.

4. Keep commands and queries separate.

- writes through command services
- reads through query services

5. Keep transport code transport-only.

- bind
- validate
- map DTOs
- call facade/use case

6. Preserve module independence.

- downloader-only
- aggregator-only
- usenet-indexer-only
- all-in-one

## Remaining Follow-Up

These are still worth doing, but they are no longer blockers for normal feature work:

- add module-combination startup tests
- add route registration tests
- add settings-reload tests
- expand runtime module coverage if new long-running modules are introduced
- keep moving any future `app.Context`-driven logic behind module or use-case interfaces instead of reintroducing direct context-driven feature code
