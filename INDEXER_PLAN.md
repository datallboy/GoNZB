# Detailed Refactor Plan v3: Milestones + Explicit Module/DB Ownership

## Summary
This plan clarifies module boundaries and removes ambiguity around SQLite vs PostgreSQL ownership.

Key decisions:
1. Downloader module remains the core runtime and owns queue APIs.
2. Aggregator does not require PostgreSQL.
3. Usenet/NZB Indexer requires PostgreSQL.
4. API for enqueue/queue/history remains available with downloader enabled.
5. Web UI is a separate optional module that consumes API endpoints.
6. SQLite does not own catalog `release_files`; downloader file details are stored as queue-scoped runtime metadata.

---

## Glossary + Canonical Terms

Use these terms exactly throughout code, docs, config, and PR descriptions.

1. **Downloader module**
- Meaning: queue/download/post-process runtime.
- Owns: queue APIs, queue runtime state, queue item file details.
- Preferred usage: `Downloader module` (not `engine module` or `queue module` as top-level ownership terms).

2. **Indexer Manager / Aggregator**
- Meaning: source orchestration and search/get behavior across providers.
- Owns: release discovery/search API surface and source fanout.
- Preferred usage: `Aggregator` or `Indexer Manager / Aggregator`.
- Avoid: using `indexer` alone when you specifically mean the usenet scraper/index builder.

3. **Usenet/NZB Indexer**
- Meaning: provider-header scraping, grouping, assembly, and release formation pipeline.
- Owns: PostgreSQL indexing/catalog schema and ingest/formation workflows.
- Preferred usage: `Usenet/NZB Indexer`.
- Avoid: calling this component `Aggregator`.

4. **Queue Item**
- Meaning: downloader job instance (`queue_items` row), identified by queue item id.
- Preferred usage: `queue item` for downloader jobs.

5. **Release**
- Meaning: catalog/search identity from aggregator or usenet indexer domain.
- Preferred usage: `release` for catalog entities.
- Rule: do not treat release id as queue item id; they are distinct identifiers.

6. **Queue Item Files**
- Meaning: downloader-owned per-queue-item file metadata for API/UI (logical queue item file view).
- Physical storage: deduplicated file-set model (`queue_file_sets` + `queue_file_set_items`) referenced by `queue_items.file_set_id`.
- Preferred usage: `queue item files`.
- Avoid: referring to these as catalog `release_files`.

7. **Release Files (PG catalog)**
- Meaning: usenet indexer catalog file metadata (`release_files` in PostgreSQL).
- Preferred usage: `PG release_files` or `catalog release files` when disambiguation is needed.

8. **Web UI module**
- Meaning: optional frontend module that consumes API only.
- Preferred usage: `Web UI module`.
- Rule: never describe Web UI as owning persistence.

9. **API module**
- Meaning: transport/controller layer exposing downloader and aggregator endpoints.
- Preferred usage: `API module`.
- Rule: endpoint ownership is still by feature module (Downloader vs Aggregator), not by transport.

Naming enforcement rules:
1. In milestones, always pair component names with ownership verbs (`owns`, `depends on`, `does not require`).
2. When both queue and release concepts are present, explicitly qualify with `queue item` vs `release`.
3. For DB references, always prefix with storage scope when ambiguous (`SQLite queue_item_files`, `PG release_files`).

---

## Module Ownership (Source of Truth)

1. **Downloader module (core)**
- Owns queue lifecycle, queue API endpoints, and runtime history.
- Uses SQLite runtime state.
- Can run with optional payload cache module enabled or disabled.
- Works without aggregator and without usenet indexer.

2. **Indexer Manager / Aggregator module (optional)**
- Orchestrates sources (remote newznab, local payload cache source, optional local search cache).
- Does not require PostgreSQL.
- May use optional lightweight SQLite cache for release search UX, but can run stateless.

3. **Usenet/NZB Indexer module (optional)**
- Scrapes headers, assembles binaries, forms releases.
- Uses PostgreSQL for heavy relational indexing and release formation.

4. **Web UI module (optional)**
- Separate module consuming API.
- No direct DB ownership.

---

## Module Capability Matrix

| Module Combination | Queue Enqueue/History API | Release Search API | Web UI | SQLite | Payload Cache Module | PostgreSQL |
|---|---|---|---|---|---|---|
| downloader-only | Yes | No | Optional | Required | Optional | Not required |
| aggregator-only | No | Yes | Optional | Optional (settings/cache) | Optional | Not required |
| usenet-indexer-only | No | No | No | Optional (settings state) | Optional | Required |
| all-in-one | Yes | Yes | Optional | Required | Optional | Required |

Notes:
- Web UI requires API module to be enabled.
- Aggregator can run stateless or with optional SQLite cache (`aggregator_release_cache`).
- Payload cache module is optional; no-cache mode is supported by design.
- Downloader queue APIs stay available without aggregator or usenet indexer.

---

## API Surface by Module

Downloader-owned API (enabled when `modules.downloader.enabled` and `modules.api.enabled`):
- `POST /api/v1/queue` (manual NZB upload or enqueue by release id)
- `GET /api/v1/queue`
- `GET /api/v1/queue/history`
- `GET /api/v1/queue/:id`
- `GET /api/v1/queue/:id/events`
- `GET /api/v1/queue/:id/files`
- `POST /api/v1/queue/:id/cancel`
- `POST /api/v1/queue/bulk/cancel`
- `POST /api/v1/queue/bulk/delete`
- `POST /api/v1/queue/history/clear`

Aggregator-owned API (enabled when `modules.aggregator.enabled` and `modules.api.enabled`):
- `GET /api/v1/releases/search`
- Newznab compatibility endpoints (existing behavior preserved under aggregator ownership)

API ownership rule:
- Downloader owns queue lifecycle and queue APIs.
- Aggregator owns search/get behavior and release discovery APIs.

---

## Data Ownership Table

| Data/Table | Owner Module | Storage | Lifecycle | Notes |
|---|---|---|---|---|
| `queue_items` | Downloader | SQLite | Runtime + history | Core queue state |
| `queue_item_events` | Downloader | SQLite | Runtime + history | Stage/event timeline |
| `queue_file_sets` + `queue_file_set_items` (+ `queue_items.file_set_id`) | Downloader | SQLite | Runtime + history | Deduplicated queue item file details backing `GET /api/v1/queue/:id/files` |
for API/UI |
| `blob_cache_index` | Payload Cache Module | SQLite | Runtime cache metadata | Present only when payload cache module is enabled |
| NZB blob files (`*.nzb`) | Payload Cache Module | Filesystem | Runtime cache | Source payload storage for cache/streaming-ready future |
| `aggregator_release_cache` | Aggregator | SQLite (optional) | Search cache | Not required for aggregator operation |
| PG `releases`/`release_files` and indexing tables | Usenet/NZB Indexer | PostgreSQL | Indexing catalog | Never required by downloader-only mode |

Ownership rules:
- Do not reintroduce SQLite catalog `release_files` for cross-module release ownership.
- Downloader uses queue-scoped file metadata; Usenet/NZB Indexer owns PG release catalog metadata.

---

## Non-Goals

1. No dual-write between SQLite and PostgreSQL release catalogs.
2. No requirement that aggregator depend on PostgreSQL.
3. No removal of manual NZB upload + queue API workflow.
4. No hidden hard dependency from downloader to usenet-indexer internals.
5. No automatic migration of every historical SQLite catalog row to PG in early milestones.
6. No coupling Web UI directly to DBs; Web UI talks only to API.

---

## Optional Payload Cache Module Policy

This section defines cache decoupling behavior for downloader and aggregator.

1. Payload cache module is optional and provides local filesystem persistence for NZB payloads.
2. In no-cache mode, aggregator uses pass-through fetch from upstream sources per request.
3. In no-cache mode, manual-upload queue jobs are allowed but are non-resumable across restart.
4. On restart, pending/hydrating non-resumable jobs must transition to failed/cancelled with explicit reason (`payload_not_persisted`).
5. Future streaming is out of scope now, but payload/cache ownership is designed so streaming can consume this module later without ownership changes.

Implementation contracts:
1. Separate mandatory payload fetch path from optional payload cache path.
2. Queue items carry explicit payload durability metadata:
- `payload_mode` (`cached` | `ephemeral`)
- `resumable` (`true` | `false`)
3. Queue and history APIs remain available regardless of cache mode.

---

## Database Schemas (Explicit)

## A. SQLite schema (downloader runtime + optional lightweight aggregator cache + optional payload cache metadata)

### A1. `queue_items` (decoupled)
```sql
CREATE TABLE IF NOT EXISTS queue_items (
  id TEXT PRIMARY KEY,                        -- KSUID
  status TEXT NOT NULL,
  out_dir TEXT NOT NULL,
  error TEXT,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('manual','aggregator','usenet_index')),
  source_release_id TEXT,                     -- nullable for manual direct upload
  release_title TEXT NOT NULL DEFAULT '',
  release_size INTEGER NOT NULL DEFAULT 0,
  release_snapshot_json TEXT NOT NULL DEFAULT '{}',
  started_at_unix INTEGER NOT NULL DEFAULT 0,
  completed_at_unix INTEGER NOT NULL DEFAULT 0,
  download_seconds INTEGER NOT NULL DEFAULT 0,
  postprocess_seconds INTEGER NOT NULL DEFAULT 0,
  avg_bps INTEGER NOT NULL DEFAULT 0,
  downloaded_bytes INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_queue_items_status ON queue_items(status);
CREATE INDEX IF NOT EXISTS idx_queue_items_completed_at_unix ON queue_items(completed_at_unix);
CREATE INDEX IF NOT EXISTS idx_queue_items_source_kind ON queue_items(source_kind);
CREATE INDEX IF NOT EXISTS idx_queue_items_source_release_id ON queue_items(source_release_id);
```

### A2. `queue_item_events`
```sql
CREATE TABLE IF NOT EXISTS queue_item_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_item_id TEXT NOT NULL,
  stage TEXT NOT NULL,
  status TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  meta_json TEXT NOT NULL DEFAULT '',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(queue_item_id) REFERENCES queue_items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_queue_item_events_queue_item_id_created_at
ON queue_item_events(queue_item_id, created_at);
```

### A3. Queue item files (downloader-owned deduplicated file-set model for API/UI)
```sql
CREATE TABLE IF NOT EXISTS queue_file_sets (
  id TEXT PRIMARY KEY,                        -- KSUID
  content_hash TEXT NOT NULL UNIQUE,          -- sha256(normalized file list)
  total_files INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS queue_file_set_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_set_id TEXT NOT NULL,
  file_name TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  file_index INTEGER NOT NULL DEFAULT 0,
  is_pars BOOLEAN NOT NULL DEFAULT 0,
  subject TEXT NOT NULL DEFAULT '',
  date_unix INTEGER NOT NULL DEFAULT 0,
  poster TEXT NOT NULL DEFAULT '',
  groups_json TEXT NOT NULL DEFAULT '[]',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(file_set_id) REFERENCES queue_file_sets(id) ON DELETE CASCADE,
  UNIQUE(file_set_id, file_index)
);

CREATE INDEX IF NOT EXISTS idx_queue_file_set_items_file_set_id
ON queue_file_set_items(file_set_id);

-- queue_items includes:
-- file_set_id TEXT NULL REFERENCES queue_file_sets(id)
CREATE INDEX IF NOT EXISTS idx_queue_items_file_set_id ON queue_items(file_set_id);
```

### A4. `blob_cache_index` (optional metadata for local NZB blobs)
```sql
CREATE TABLE IF NOT EXISTS blob_cache_index (
  key TEXT PRIMARY KEY,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  mtime_unix INTEGER NOT NULL DEFAULT 0,
  last_verified_unix INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
```

### A5. `aggregator_release_cache` (optional, lightweight SQLite cache for aggregator search UX)
```sql
CREATE TABLE IF NOT EXISTS aggregator_release_cache (
  release_id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  guid TEXT NOT NULL DEFAULT '',
  publish_date_unix INTEGER NOT NULL DEFAULT 0,
  nzb_cached BOOLEAN NOT NULL DEFAULT 0,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_aggregator_release_cache_title ON aggregator_release_cache(title);
CREATE INDEX IF NOT EXISTS idx_aggregator_release_cache_source ON aggregator_release_cache(source);
```

### A6. `module_schema_version`
```sql
CREATE TABLE IF NOT EXISTS module_schema_version (
  module_name TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## B. PostgreSQL schema (usenet/nzb indexer only)

### B1. Source/scrape control
- `usenet_providers`
- `newsgroups`
- `scrape_runs`
- `scrape_checkpoints`

Key constraints:
- `newsgroups.group_name UNIQUE`
- `scrape_checkpoints UNIQUE(provider_id, newsgroup_id)`

### B2. Header/raw layer
- `article_headers`
  - unique `(newsgroup_id, article_number)`
  - unique `(newsgroup_id, message_id)`
- `posters`
- `article_poster_map`

### B3. Assembly layer
- `binaries`
- `binary_parts`
- `part_repair_queue`

### B4. Release layer
- `releases`
  - `release_id TEXT PRIMARY KEY` (KSUID)
  - `guid UNIQUE`
- `release_files`
  - unique `(release_id, file_name)`
- `release_file_articles`
- `release_newsgroups`

### B5. Enrichment
- `regex_rules`
- `regex_hits`
- `predb_entries`
- `release_predb_matches`

### B6. NZB cache metadata
- `nzb_cache`
  - `release_id PRIMARY KEY`
  - `generation_status`
  - `nzb_hash_sha256`
  - timestamps/error fields

### B7. Schema version
- `module_schema_version`

---

## Detailed Milestones and Task Breakdown

## Milestone Entry/Exit Gates (Global)

Entry gates (required before starting each milestone):
1. Previous milestone exit criteria are satisfied and documented.
2. Module independence matrix remains valid (`downloader-only`, `aggregator-only`, `usenet-indexer-only`, `all-in-one`).
3. DB ownership changes for the milestone are explicitly listed in this plan.
4. API ownership impact is explicitly listed (endpoints added/changed/removed).

Exit gates (required to mark milestone complete):
1. Compile + tests pass.
2. Startup validation for relevant module combinations passes.
3. Schema checks pass for affected DB(s).
4. No forbidden cross-module imports introduced.
5. Regression smoke checks pass for manual NZB enqueue + queue/history API where downloader is enabled.

## Milestone 0: Terminology + baseline docs
### Files
- `README.md`
- `ARCHITECTURE.md`
- `INDEXER_PLAN.md`

### Tasks
1. Define terms: Indexer Manager / Aggregator vs Usenet/NZB Indexer.
2. Document module matrix and DB requirements.
3. State API ownership:
- queue/enqueue/history endpoints belong to downloader module.
- search/federation endpoints belong to aggregator module.
4. State Web UI as optional module consuming API.

### Exit criteria
- docs align with module boundaries and DB ownership.

---

## Milestone 1: Interface split in `app.Context`
### Files
- `internal/app/context.go`
- `internal/engine/manager.go`
- `internal/queue/service.go`
- `internal/indexer/manager.go` injection points
- `internal/infra/config/config.go`

### Tasks
1. Split into explicit interfaces:
- `JobStore`
- `PayloadFetcher`
- `PayloadCacheStore` (optional module interface)
- `QueueFileStore`
- `ReleaseResolver`
- `IndexerAggregator`
2. Add `SettingsStore` ownership boundary for runtime-editable settings state.
3. Remove queue dependence on catalog store joins.
4. Keep adapters temporarily for compatibility.

### Exit criteria
- compiles with behavior parity.

---

## Milestone 2: Store package separation
### Files
- `internal/store/sqlitejob/*`
- `internal/store/blob/*` (payload cache module implementation, optional at runtime)
- `internal/store/pgindex/*` (scaffold)
- `internal/store/adapters/*`
- `internal/store/settings/*` (new)

### Tasks
1. Queue/event + queue-item-file persistence into `sqlitejob`.
2. Move filesystem payload-cache operations into optional payload cache module (`blob` package path can remain as implementation).
3. Add SQLite settings-state persistence scaffolding (separate from queue/catalog tables).
4. PG index scaffolding only, no downloader dependency.

### Exit criteria
- downloader works with SQLite with cache enabled and cache disabled modes.

---

## Milestone 3: SQLite downloader schema finalization (no release catalog tables)
### Files
- `internal/store/sqlitejob/migrations/001_init.up.sql`
- `internal/store/sqlitejob/migrate.go`
- `internal/store/sqlitejob/queue.go`
- `internal/store/sqlitejob/queue_events.go`
- `internal/store/sqlitejob/dbo.go`
- `internal/store/sqlitejob/*queue item files repo*`
- `internal/engine/manager.go`
- `internal/queue/service.go`
- `internal/api/controllers/queue.go`

### Tasks
1. Keep SQLite tables limited to:
- `queue_items`
- `queue_item_events`
- `queue_file_sets`
- `queue_file_set_items`
- `queue_items.file_set_id` reference
- `blob_cache_index` (only when payload cache module is enabled)
- optional `aggregator_release_cache`
- `module_schema_version`
2. Delete SQLite catalog tables:
- `release_files`
- `release_file_groups`
- `groups`
- `posters`
- `release_cache`
3. Preserve queue file API behavior via downloader-owned queue item file view backed by deduplicated file sets (`queue_file_sets` + `queue_file_set_items`), not release-scoped catalog tables.
4. Add queue payload durability fields/semantics (`payload_mode`, `resumable`) and restart handling rules for non-cached jobs.
5. Preserve manual NZB enqueue and queue/history endpoints.

### Exit criteria
- queue API + web UI still support:
- manual NZB enqueue
- queue/history/events
- per-queue-item file details
- no SQLite dependency on `releases` or catalog `release_files`.
- cache-enabled and no-cache modes both pass defined behavior.

---

## Milestone 4: Aggregator extraction (no PG requirement)
### Files
- `internal/aggregator/manager.go`
- `internal/aggregator/source.go`
- `internal/aggregator/sources/newznab/*`
- `internal/aggregator/sources/localblob/*`
- compatibility wrappers in `internal/indexer/*`

### Tasks
1. Introduce `CatalogSource` interface.
2. Port fanout logic from legacy indexer manager.
3. Add optional SQLite `aggregator_release_cache` usage for faster local search.
4. Support pass-through payload fetch behavior when payload cache module is disabled.
5. Keep aggregator functional without PG.

### Exit criteria
- aggregator module runs with:
- stateless mode, or
- SQLite lightweight cache mode.

---

## Milestone 5: PostgreSQL usenet indexer foundation
### Files
- `internal/store/pgindex/store.go`
- `internal/store/pgindex/migrate.go`
- `internal/store/pgindex/migrations/001_init.up.sql`
- `internal/indexing/scrape/service.go`
- `internal/indexing/scheduler/service.go`

### Tasks
1. Implement PG connection and migrations.
2. Add scrape/checkpoint repositories.
3. Implement header ingest constraints.

### Exit criteria
- `indexer scrape --once` writes checkpoints and headers to PG.

---

## Milestone 6: Assembly and release formation (PG)
### Files
- `internal/indexing/assemble/service.go`
- `internal/indexing/release/service.go`
- `internal/indexing/match/service.go`
- PG migrations `002_*.sql`, `003_*.sql`

### Tasks
1. Build binaries + parts from article headers.
2. Form releases + release_files in PG.
3. Add enrichment and NZB cache metadata.

### Exit criteria
- release catalog queryable from PG by resolver.

---

## Milestone 7: Resolver-based downloader hydration
### Files
- `internal/resolver/release_resolver.go`
- resolver implementations for manual/blob/aggregator/usenet-index
- `internal/engine/manager.go`
- `internal/queue/service.go`

### Tasks
1. Queue Add sets `source_kind` + snapshot consistently.
2. Manual source does not require aggregator/PG.
3. Aggregator source resolves via aggregator interfaces.
4. Usenet index source resolves via PG-backed resolver.
5. Queue item file details continue from downloader-owned deduplicated file-set storage (`queue_file_sets` + `queue_file_set_items`) exposed as queue item files API.

### Exit criteria
- downloader works in downloader-only mode with full queue API/UI behavior.

---

## Milestone 8: Modular runtime wiring (CLI/config/API/Web UI)
### Files
- `internal/infra/config/config.go`
- `config.yaml.example`
- `cmd/gonzb/main.go`
- API route registration files
- web UI bootstrapping/serve wiring

### Tasks
1. Add module flags:
- `modules.downloader.enabled`
- `modules.aggregator.enabled`
- `modules.usenet_indexer.enabled`
- `modules.web_ui.enabled`
- `modules.api.enabled`
2. API ownership:
- downloader queue endpoints enabled when downloader module is on.
- aggregator search endpoints enabled when aggregator module is on.
3. Web UI served only when web_ui module is enabled.

### Exit criteria
- all module combinations validate clearly at startup.

---

## Milestone 8.5: Scrape mode split (`latest` vs `backfill`)
### Why this is deferred
- This is a real product requirement for the Usenet/NZB Indexer, but it is not required to complete Milestones 7-8 module decoupling.
- It changes scrape operational policy, not downloader/aggregator ownership boundaries.
- Deferring it avoids expanding current milestone scope while preserving the design decision in the plan.

### Goal
- Split current scrape behavior into two explicit commands/modes:
  1. `latest`: prioritize current content near the head of the group.
  2. `backfill`: walk backward through older content intentionally and with its own cursor.

### Files
- `internal/store/pgindex/migrations/004_*.sql` (or next available migration number)
- `internal/store/pgindex/repository.go`
- `internal/indexing/scrape/service.go`
- `internal/indexing/service.go`
- `cmd/gonzb/main.go`
- `internal/infra/config/config.go`
- `config.yaml.example`

### Tasks
1. Add separate scrape cursor state for backward traversal.
- Keep existing forward/latest checkpoint semantics.
- Add a separate backfill cursor; do not overload the forward checkpoint for both directions.

2. Split scrape execution into explicit modes.
- `RunLatestOnce(ctx)` for head-following scrape behavior.
- `RunBackfillOnce(ctx)` for reverse historical ingestion.
- `RunOnce(ctx)` may remain an alias for `RunLatestOnce(ctx)` for compatibility.

3. Add explicit CLI/API/runtime ownership for scrape modes.
- Preferred CLI shape:
  - `gonzb indexer scrape latest --once`
  - `gonzb indexer scrape backfill --once`
- Scheduler should run `latest` only by default.
- Backfill should remain manual/intentional unless separately scheduled later.

4. Define cold-start/latest policy explicitly.
- On no forward checkpoint, `latest` should begin near `GROUP high`, bounded by batch size or configured overlap.
- It should not default to oldest-first ingestion.

5. Preserve module boundaries.
- This remains a Usenet/NZB Indexer concern only.
- Downloader and Aggregator must not gain hidden dependencies on scrape mode behavior.

### Exit criteria
- `latest` and `backfill` are separate commands with separate cursor behavior.
- Cold-start indexing prioritizes recent content.
- Historical ingestion is possible without corrupting or reusing the forward/latest checkpoint.

---

## Milestone 8.X: Runtime Settings Completion (SQLite control plane)
### Why this is tracked separately
- Milestone 8 core module wiring can be completed first, but the plan also requires runtime settings API/live reload work before Milestone 8 is truly closed.
- This work should be completed before moving on to Milestone 8.5 or later milestones.
- It is intentionally narrower than a full configuration subsystem rewrite.

### Scope
- Make SQLite settings state real for runtime-editable operational settings.
- Preserve bootstrap-only ownership for YAML/env keys marked restart-only in this plan.
- Apply SQLite runtime settings as an overlay after bootstrap load.

### Chunk 1: Settings domain model + effective overlay
#### Goal
- Replace settings scaffolding with a real runtime settings model and effective-config overlay.

#### Files
- `internal/store/settings/store.go`
- `internal/store/settings/types.go` (or equivalent)
- `internal/infra/config/config.go`
- `internal/app/context.go`

#### Tasks
1. Define runtime-editable settings payload shape for initial scope:
- `servers[*]`
- `indexers[*]`
- `download.*`
- `usenet_indexer.*` scheduler/admin knobs needed by current runtime
2. Change settings store contract from scaffold/no-op to real reads/writes.
3. Load effective config by overlaying SQLite runtime settings on bootstrap YAML/env.
4. Validate effective config by enabled modules after overlay is applied.

#### Exit criteria
- SQLite runtime settings can be read and overlaid onto bootstrap config.
- Effective runtime config is validated after overlay.

### Chunk 2: Settings revision watch + live reload bus
#### Goal
- Make runtime settings changes observable and safely reloadable.

#### Files
- `internal/store/settings/store.go`
- `internal/app/*` runtime reload helpers
- `cmd/gonzb/main.go`

#### Tasks
1. Implement `WatchSettingsChanges(...)` using revision polling or equivalent.
2. Start settings watcher during runtime startup.
3. On runtime settings update, rebuild/reload affected subsystems where safe:
- downloader NNTP providers
- aggregator source list
- usenet-indexer scheduler knobs
4. Prefer whole-subsystem rebuild/swap over ad-hoc mutable patching in initial scope.

#### Exit criteria
- Settings revisions are observable at runtime.
- At least one enabled subsystem reloads live from SQLite settings updates.

### Chunk 3: Admin settings API
#### Goal
- Expose runtime settings read/update through API.

#### Files
- `internal/api/controllers/settings.go`
- `internal/api/router.go`
- supporting DTO files as needed

#### Tasks
1. Add `GET /api/v1/admin/settings`
2. Add `PUT /api/v1/admin/settings`
3. Return effective runtime-editable settings only.
4. Redact secrets on read.
5. Accept only runtime-editable fields defined in this plan.

#### Exit criteria
- Runtime settings can be retrieved and updated through the admin API.

### Chunk 4: Startup and reload integration
#### Goal
- Ensure runtime services use effective settings rather than raw bootstrap config alone.

#### Files
- `cmd/gonzb/main.go`
- `internal/app/context.go`

#### Tasks
1. Load effective settings after bootstrap config and SQLite settings initialization.
2. Build runtime services from effective config.
3. Apply safe live reload on settings revision updates.
4. Keep bootstrap-only keys restart-only:
- `modules.*`
- `port`
- `log.*` (initial scope)
- `store.sqlite_path`
- `store.blob_dir`
- `api.key`
- `api.cors_allowed_origins`
- `postgres.dsn`

#### Exit criteria
- Runtime uses effective settings from bootstrap + SQLite overlay.
- Live reload works for initial runtime-editable setting groups.

### Explicit Deferrals
1. Secret encryption-at-rest implementation details may be completed later if needed, but the settings model/API should already be structured for secret fields.
2. `modules.*` remain bootstrap-only in initial scope.
3. Scrape-mode split (`latest` vs `backfill`) remains Milestone 8.5 and must not be folded into this settings work.

---

## Milestone 9: SAB-compatible downloader API + Arr integration
### Why this milestone is broader than a pure API shim
- To act as a practical drop-in replacement downloader for Radarr/Sonarr, GoNZB must implement both:
  1. a SAB-compatible downloader API surface
  2. downloader-to-Arr notification/reporting behavior for success/failure state changes
- A compatibility transport without notifier/reporting support is not sufficient for a real Radarr/Sonarr replacement workflow.

### Goal
- Implement a SAB-compatible downloader API surface that can be used by Radarr/Sonarr as a downloader integration target.
- Preserve downloader-owned queue state and queue item files as the source of truth.
- Add Arr-facing runtime settings and outbound notification behavior for completed/failed downloads.

### Files
- `internal/api/controllers/*`
- API routing/DTO mapping
- `internal/queue/*`
- `internal/runtime/commands/*`
- `internal/store/settings/*`
- notifier/integration package(s) as needed

### Chunk 1: Compatibility contract + DTOs
#### Goal
- Define SAB-compatible request/response structs before implementing handlers.

#### Tasks
1. Add dedicated SAB compatibility DTOs separate from existing app-native queue controller DTOs.
2. Define queue/history/command envelopes and item mappers.
3. Keep the existing downloader queue API unchanged; SAB compatibility is an additional transport surface.
4. Document which SAB fields are exact matches vs approximations from downloader-owned queue item state.

#### Exit criteria
- SAB compatibility DTOs exist and are stable enough for handler implementation.

### Chunk 2: SAB-compatible controller + routing
#### Goal
- Expose a downloader-owned SAB-compatible API surface.

#### Tasks
1. Add a dedicated SAB compatibility controller.
2. Add route registration under downloader + API module ownership.
3. Support the required downloader subset for drop-in operation:
- enqueue/add
- queue
- history
- cancel/delete
- pause/resume (global and/or mapped equivalents where supported)
4. Keep app-native queue endpoints and Web UI behavior unchanged.

#### Exit criteria
- SAB-compatible routes are reachable and mapped onto downloader runtime operations.

### Chunk 3: Queue/history/status mapping
#### Goal
- Map downloader queue item state into SAB-compatible queue/history/status responses.

#### Tasks
1. Map active queue items into SAB-compatible queue slots.
2. Map terminal queue items into SAB-compatible history entries.
3. Preserve:
- queue item id
- release snapshot-derived title/size/category/source where relevant
- downloader metrics (bytes, duration, avg rate) where fields can be represented
4. Keep `GET /api/v1/queue/:id/files` as the authoritative detailed file view for native API/UI usage.

#### Exit criteria
- SAB queue/history responses are generated from downloader-owned queue state without introducing a second queue model.

### Chunk 4: Arr notifier settings and outbound integration
#### Goal
- Add runtime settings needed for Radarr/Sonarr notification/reporting.

#### Tasks
1. Add downloader-owned runtime settings for Arr integrations, including at minimum:
- target kind (`radarr` / `sonarr`)
- base URL
- API key / auth material
- enabled flag
- optional category/client-name mapping if needed for downloader registration parity
2. Persist these settings in SQLite settings state as downloader/integration runtime settings.
3. Extend the admin settings API to read/update these fields.
4. Redact secrets on read in the same way as NNTP/indexer runtime settings.

#### Exit criteria
- Arr notifier settings are part of the runtime settings control plane and can be configured without editing bootstrap config for normal operation.

### Chunk 5: Download completion/failure notifications
#### Goal
- Inform Radarr/Sonarr when downloader jobs succeed or fail.

#### Tasks
1. Add a notifier/integration client package for Arr callbacks or equivalent completion/failure reporting.
2. Trigger outbound notifications from downloader terminal-state transitions:
- completed
- failed
- cancelled where relevant
3. Include enough queue/release metadata for Radarr/Sonarr to correlate the job outcome.
4. Ensure notifier failures do not corrupt downloader queue state; they should be logged and surfaced separately.

#### Exit criteria
- Downloader terminal-state transitions can be reported to configured Arr integrations.

### Chunk 6: Compatibility validation
#### Goal
- Verify GoNZB can function as a practical SAB-compatible downloader target.

#### Tasks
1. Add compatibility smoke tests for the SAB-compatible endpoints.
2. Validate end-to-end downloader workflows with:
- Radarr
- Sonarr
- optionally Prowlarr where relevant to the downloader integration path
3. Verify:
- add/enqueue works
- queue polling works
- history polling works
- completion/failure reporting works
- existing native queue APIs still work

#### Exit criteria
- Radarr/Sonarr downloader integration works end-to-end against the supported subset.

### Milestone 9 Tasks (Consolidated)
1. Implement a dedicated SAB-compatible downloader API surface rather than overloading the native queue API.
2. Keep downloader queue items, queue events, and queue item files as the sole source of truth.
3. Add Arr notifier runtime settings and outbound reporting for completed/failed downloads.
4. Validate real downloader integration workflows with Radarr/Sonarr before closing the milestone.

### Exit criteria
- SAB-compatible downloader API subset is implemented and mapped from downloader-owned queue state.
- Arr notifier settings are configurable through runtime settings.
- Downloader completion/failure events can be reported to configured Arr integrations.
- Radarr/Sonarr downloader workflow is validated end-to-end.

---

## Milestone 10: Refactor, hardening, compatibility cleanup, and architecture guardrails
### Why this milestone exists
- Milestones 1-9 established the intended module boundaries, runtime composition model, downloader compatibility surface, and Arr integration behavior.
- The next milestone should not reopen those architecture decisions.
- Instead, it should make the current system easier to maintain, safer to operate, stricter at its boundaries, and more predictable for API clients.

### Milestone 10 goals
1. Reduce accumulated code debt in API/controller/runtime wiring without changing module ownership.
2. Harden controller behavior, input validation, error mapping, and transport safety.
3. Clean up compatibility surfaces so native API, SAB-compatible API, and Newznab-compatible API are explicit and stable.
4. Add health/readiness/schema guardrails that reflect enabled module combinations.
5. Make architectural boundaries enforceable in CI rather than relying on convention.
6. Bring docs, terminology, and tests in line with the current post-Milestone-9 codebase.

### Non-goals
1. Do not re-couple Downloader module to PostgreSQL or Usenet/NZB Indexer internals.
2. Do not make Aggregator require PostgreSQL.
3. Do not remove downloader-owned queue APIs or compatibility endpoints that were added in Milestone 9.
4. Do not introduce hidden hard dependencies between:
- downloader-only
- aggregator-only
- usenet-indexer-only
- all-in-one

### Chunk 10.0: Documentation and contract alignment
#### Goal
- Make architecture and user-facing docs reflect the current module split and current API ownership.

#### Files
- `INDEXER_PLAN.md`
- `ARCHITECTURE.md`
- `README.md`
- module README/docs files as needed

#### Tasks
1. Rewrite architecture docs to match current module terminology:
- Downloader module
- Indexer Manager / Aggregator
- Usenet/NZB Indexer
- Web UI module
- API module
2. Remove stale references to the pre-modular single-store/single-indexer design.
3. Document current API ownership and transport surfaces clearly:
- native downloader queue API
- native aggregator search API
- SAB-compatible downloader API
- Newznab-compatible aggregator API
4. Document current module combinations and startup requirements.
5. Document any intentional compatibility approximations for SAB/Newznab responses.

#### Exit criteria
- Docs describe the current architecture rather than the pre-refactor architecture.
- Terminology in docs matches the canonical terminology section in this plan.

### Chunk 10.1: API controller hardening
#### Goal
- Make API controllers strict, predictable, and safe under malformed input and edge cases.

#### Files
- `internal/api/controllers/queue.go`
- `internal/api/controllers/sab.go`
- `internal/api/controllers/newznab.go`
- `internal/api/controllers/compat_api.go`
- `internal/api/controllers/settings.go`
- shared DTO/helper files as needed

#### Tasks
1. Standardize request binding and validation rules across controllers.
2. Replace permissive bind behavior with explicit request validation where needed.
3. Normalize HTTP status mapping:
- `400` for invalid client input
- `404` for missing queue item/release/resource
- `409` for state conflicts where appropriate
- `500` only for real server failures
4. Ensure nil dependencies and disabled-module conditions fail safely and consistently.
5. Add pagination and bounds validation for queue/history endpoints.
6. Harden multipart/manual upload handling:
- explicit missing-file behavior
- explicit empty-upload behavior
- safe filename handling
7. Harden settings update validation and redaction behavior.
8. Extract shared response/error helpers to reduce duplicated controller logic.

#### Exit criteria
- Controller behavior is consistent across native and compatibility endpoints.
- Invalid inputs produce deterministic, documented responses.

### Chunk 10.2: Compatibility transport cleanup
#### Goal
- Make compatibility API surfaces deliberate and maintainable rather than transitional.

#### Files
- `internal/api/router.go`
- `internal/api/controllers/compat_api.go`
- `internal/api/controllers/sab.go`
- `internal/api/controllers/newznab.go`
- compatibility DTO mapping files as needed

#### Tasks
1. Remove or resolve any remaining “staging” route assumptions from Milestone 9.
2. Define the supported contract for:
- `/api`
- `/api/sab`
- `/nzb/:id`
- native `/api/v1/*`
3. Centralize compatibility request dispatch and shared validation logic.
4. Freeze current supported SAB-compatible subset with explicit mapping rules from downloader-owned queue state.
5. Freeze current Newznab-compatible search/get behavior under aggregator ownership.
6. Ensure compatibility controllers do not bypass module ownership boundaries.

#### Exit criteria
- Compatibility routing is explicit, documented, and stable.
- Native API and compatibility API responsibilities are cleanly separated.

### Chunk 10.3: Transport and runtime hardening
#### Goal
- Add operational protections and safer server behavior for long-running API mode.

#### Files
- `internal/api/router.go`
- `internal/runtime/commands/server.go`
- `internal/runtime/wiring/runtime.go`
- middleware/helper files as needed

#### Tasks
1. Add panic recovery middleware for API routes.
2. Add request ID and structured request logging improvements.
3. Add body size limits for JSON and multipart upload surfaces.
4. Add server read/write/idle timeout configuration or fixed sane defaults.
5. Ensure streamed NZB responses and remote compatibility fetches use bounded, timeout-aware behavior.
6. Ensure startup/shutdown logs and failure paths are clear and non-ambiguous.
7. Ensure disabled modules do not register routes or background loops accidentally.

#### Exit criteria
- Server mode has baseline production-safe middleware and timeout behavior.
- Common malformed or abusive request patterns fail safely.

### Chunk 10.4: Health, readiness, and module schema handshakes
#### Goal
- Expose module-aware runtime health and verify storage schema expectations before serving traffic.

#### Files
- `internal/telemetry/*`
- `internal/api/router.go`
- `internal/app/context.go`
- `internal/store/sqlitejob/*`
- `internal/store/settings/*`
- `internal/store/pgindex/*`

#### Tasks
1. Add health/readiness endpoints for API mode.
2. Define per-module readiness rules:
- Downloader module readiness checks SQLite queue/job store and downloader runtime dependencies
- Aggregator readiness checks source configuration and optional cache dependencies
- Usenet/NZB Indexer readiness checks PostgreSQL connectivity and required indexer runtime dependencies
3. Add schema version handshake checks for enabled stores/modules before serving traffic.
4. Fail startup clearly when an enabled module finds an incompatible or incomplete schema version.
5. Surface module status in a machine-readable response shape.
6. Keep health/readiness checks module-aware so optional modules can remain disabled without causing false failures.

#### Exit criteria
- Health/readiness endpoints exist and reflect enabled module state accurately.
- Enabled module schemas are verified before normal operation.

### Chunk 10.5: Architecture guardrails and forbidden import checks
#### Goal
- Enforce module boundaries mechanically so future work cannot silently re-couple modules.

#### Files
- lint/arch test config
- CI workflow files as needed
- architecture test files/scripts

#### Tasks
1. Add forbidden import checks for boundary violations, including at minimum:
- Downloader module must not depend on Usenet/NZB Indexer internals
- Aggregator must not depend on PostgreSQL indexer internals for basic operation
- Web UI must not depend on DB internals
- API controllers must use feature/module interfaces rather than cross-module store shortcuts
2. Add a documented allowed-dependency matrix for major packages.
3. Add CI enforcement for architecture checks.
4. Add a lightweight developer-facing workflow for running the same checks locally.

#### Exit criteria
- Module boundaries are enforceable via CI checks.
- Cross-module dependency violations fail fast.

### Chunk 10.6: Refactor and debt cleanup
#### Goal
- Reduce maintenance cost and ambiguity in large files and mixed-responsibility code paths.

#### Files
- `internal/api/controllers/*`
- `internal/api/router.go`
- `internal/runtime/wiring/*`
- `internal/app/*`
- related helper packages as needed

#### Tasks
1. Extract shared controller helpers for:
- response envelopes
- request parsing/validation
- compatibility mapping
- common error shaping
2. Split large files where responsibilities are currently mixed.
3. Normalize naming and comments to match canonical terminology.
4. Remove stale comments and temporary compatibility notes that are no longer true.
5. Reduce direct controller/runtime coupling where helper abstractions make sense.
6. Keep behavior parity while improving readability and change safety.

#### Exit criteria
- Major controller/runtime files have clearer responsibilities.
- Temporary refactor residue from prior milestones is removed or formalized.

### Chunk 10.7: Tests, validation matrix, and CI hardening
#### Goal
- Backstop Milestone 10 with automated proof that the modular architecture still holds.

#### Files
- `*_test.go` across affected packages
- CI workflow files as needed
- fixture/config test assets as needed

#### Tasks
1. Add unit tests for:
- config validation by enabled module combinations
- controller request validation/error mapping
- compatibility request dispatch
- schema version handshake logic
- queue/history DTO mapping
- settings redaction/update validation
2. Add integration/smoke tests for:
- downloader-only
- aggregator-only
- usenet-indexer-only
- all-in-one
3. Add failure-mode tests for:
- disabled module routes not being registered
- PG not required for aggregator-only startup
- SQLite queue store not required for usenet-indexer-only startup except settings state rules defined in this plan
- readiness reflects broken dependencies correctly
4. Add regression tests for:
- manual NZB enqueue
- queue/history/events/files API behavior
- SAB-compatible downloader subset
- Newznab-compatible search/get behavior
5. Make CI run architecture checks plus the critical validation matrix.

#### Exit criteria
- Milestone 10 hardening is covered by automated tests rather than manual confidence alone.
- CI verifies architecture boundaries, critical contracts, and module-combination startup behavior.

### Milestone 10 consolidated exit criteria
1. Docs match current architecture and ownership rules.
2. Controller and compatibility surfaces are validated and behaviorally consistent.
3. Health/readiness endpoints exist and are module-aware.
4. Enabled module schema versions are checked before normal operation.
5. Forbidden import checks enforce module boundaries in CI.
6. Validation covers the supported module combinations:
- downloader-only
- aggregator-only
- usenet-indexer-only
- all-in-one
7. Milestone 9 compatibility and Arr behavior remain intact after cleanup.

### Recommended implementation order (commit-sized phases)
This section is the recommended delivery sequence for Milestone 10.

Rules for these phases:
1. Each phase should be small enough to review and revert independently.
2. Each phase should leave the repository compiling before the next phase begins.
3. Do not mix docs-only cleanup with runtime behavior changes unless explicitly noted.
4. Do not mix architecture guardrails with broad refactors in the same commit.
5. Prefer one Codex session per phase unless the phase is split further below.

### Phase 1: Doc baseline and milestone framing
#### Scope
- Update Milestone 10 docs and architecture references before code changes begin.

#### Files
- `INDEXER_PLAN.md`
- `ARCHITECTURE.md`
- `README.md`

#### Tasks
1. Align docs with current module ownership and terminology.
2. Document current native API vs compatibility API responsibilities.
3. Remove obviously stale architecture statements from pre-modular design.

#### Why first
- It creates a clean source of truth before implementation sessions start making hardening changes.

#### Suggested commit shape
- `docs(m10): align architecture and milestone 10 hardening plan`

#### Codex handoff note
- This phase should be docs-only. No runtime behavior changes.

### Phase 2: Shared controller helpers and error/response cleanup
#### Scope
- Extract reusable controller helpers without changing endpoint behavior materially.

#### Files
- `internal/api/controllers/*`
- shared DTO/helper files as needed

#### Tasks
1. Introduce shared error response helpers.
2. Introduce shared request parsing/validation helpers where duplication is obvious.
3. Normalize common controller patterns without changing contracts yet.

#### Why second
- It reduces duplication before hardening behavior, making later controller changes smaller and safer.

#### Suggested commit shape
- `refactor(api): extract shared controller helpers for milestone 10`

#### Codex handoff note
- Keep this refactor-only. Avoid changing status codes unless strictly required by the extraction.

### Phase 3: Native API controller hardening
#### Scope
- Harden downloader-native and admin-native controllers first.

#### Files
- `internal/api/controllers/queue.go`
- `internal/api/controllers/settings.go`
- related tests

#### Tasks
1. Tighten request validation and input bounds.
2. Normalize native API status code behavior.
3. Harden multipart/manual NZB upload handling.
4. Harden settings patch validation and redaction flows.

#### Why third
- Native API is the cleanest place to establish hardened patterns before applying the same discipline to compatibility layers.

#### Suggested commit shape
- `fix(api): harden native queue and settings controllers`

#### Codex handoff note
- Focus only on native `/api/v1/*` behavior in this phase.

### Phase 4: Compatibility API hardening and contract cleanup
#### Scope
- Harden SAB-compatible and Newznab-compatible paths after native controller patterns are stable.

#### Files
- `internal/api/controllers/sab.go`
- `internal/api/controllers/newznab.go`
- `internal/api/controllers/compat_api.go`
- `internal/api/router.go`
- compatibility tests

#### Tasks
1. Tighten compatibility request validation and dispatch behavior.
2. Resolve any remaining transitional route assumptions.
3. Make supported compatibility subset explicit and testable.
4. Preserve Milestone 9 behavior while cleaning up the routing surface.

#### Why fourth
- Compatibility code is easier to stabilize once native controller patterns and shared helpers are already in place.

#### Suggested commit shape
- `fix(compat): harden SAB and Newznab compatibility controllers`

#### Codex handoff note
- Do not broaden compatibility scope in this phase. Harden only the supported subset.

### Phase 5: Server transport hardening
#### Scope
- Add middleware and server-level protections after controller behavior is stable.

#### Files
- `internal/api/router.go`
- `internal/runtime/commands/server.go`
- `internal/runtime/wiring/runtime.go`
- middleware/helper files as needed

#### Tasks
1. Add panic recovery.
2. Add request IDs and improve request logging.
3. Add body size limits and safer upload defaults.
4. Add or normalize read/write/idle timeout behavior.
5. Ensure disabled modules do not register routes or background loops unexpectedly.

#### Why fifth
- This phase is operational hardening, and is easier to verify once endpoint behavior is already settled.

#### Suggested commit shape
- `fix(server): add transport hardening and safer middleware defaults`

#### Codex handoff note
- Avoid mixing health/readiness into this phase. Keep it transport-focused.

### Phase 6: Health, readiness, and schema handshake framework
#### Scope
- Add module-aware observability and schema verification primitives.

#### Files
- `internal/telemetry/*`
- `internal/app/context.go`
- `internal/api/router.go`
- `internal/store/sqlitejob/*`
- `internal/store/settings/*`
- `internal/store/pgindex/*`

#### Tasks
1. Implement health/readiness model and response DTOs.
2. Add module-aware readiness checks.
3. Add schema version handshake checks for enabled stores/modules.
4. Fail startup clearly on incompatible enabled-module schema state.

#### Why sixth
- Health/readiness should reflect the hardened runtime, not be built against an unstable transport layer.

#### Suggested commit shape
- `feat(telemetry): add module-aware health and schema handshake checks`

#### Codex handoff note
- Keep this phase focused on telemetry/readiness/schema checks only. Do not mix in architecture linting yet.

### Phase 7: Architecture guardrails and CI enforcement
#### Scope
- Add enforceable dependency checks after runtime and docs reflect the intended boundaries.

#### Files
- lint/arch test config
- CI workflow files as needed
- architecture test files/scripts

#### Tasks
1. Add forbidden import checks.
2. Add allowed dependency matrix docs/comments if needed.
3. Add CI enforcement for module boundary checks.

#### Why seventh
- It is safer to lock boundaries after the cleanup phases have finished moving code around.

#### Suggested commit shape
- `ci(arch): enforce milestone 10 module boundary guardrails`

#### Codex handoff note
- Keep this phase small and enforcement-focused. Avoid unrelated code refactors.

### Phase 8: Test matrix expansion and close-out cleanup
#### Scope
- Add coverage and final cleanup after behavior and boundaries are stable.

#### Files
- `*_test.go` across affected packages
- fixture/config test assets as needed
- minor docs touchups as needed

#### Tasks
1. Add unit coverage for controller validation, settings behavior, schema handshake logic, and config validation.
2. Add module-combination integration/smoke tests.
3. Add compatibility regression tests.
4. Close remaining stale comments and low-risk cleanup that surfaced during prior phases.

#### Why eighth
- This phase validates the final Milestone 10 shape rather than chasing moving targets.

#### Suggested commit shape
- `test(m10): add hardening coverage and module combination regression checks`

#### Codex handoff note
- This phase can include small cleanup follow-ups discovered during test writing, but should not reopen architecture decisions.

### Optional phase splits for smaller Codex sessions
If a phase is still too large for one implementation session, split only along these boundaries:

1. Phase 3A:
- `queue.go`
- native queue validation and upload handling

2. Phase 3B:
- `settings.go`
- runtime settings validation/redaction hardening

3. Phase 4A:
- `compat_api.go`
- routing and dispatcher cleanup

4. Phase 4B:
- `sab.go`
- SAB-compatible request validation and response normalization

5. Phase 4C:
- `newznab.go`
- Newznab-compatible search/get hardening

6. Phase 5A:
- router middleware and request logging

7. Phase 5B:
- server startup/shutdown and timeout behavior

8. Phase 6A:
- health/readiness DTOs and endpoint wiring

9. Phase 6B:
- schema handshake implementation for SQLite/settings/PG stores

### Recommended Codex session handoff format
Use this format when handing a phase to a new Codex session:

1. Target phase:
- example: `Milestone 10 Phase 4B`

2. Allowed scope:
- list only the files and tests that belong to the phase

3. Constraints:
- preserve module independence matrix
- do not widen compatibility scope
- do not change docs unless the phase says so
- leave the repo compiling at the end of the phase

4. Required output:
- implementation
- tests for that phase
- short summary of contract/behavior changes

5. Validation:
- specify exact tests or smoke checks expected for that phase

---

## Public API / Type Changes (Explicit)
1. Queue item model retains:
- `source_kind`, `source_release_id`, `release_snapshot_json`
2. Queue file API remains (downloader-owned data):
- `GET /api/v1/queue/:id/files` remains supported.
- Physical SQLite backing is deduplicated (`queue_file_sets`/`queue_file_set_items` + `queue_items.file_set_id`).
3. API module split:
- downloader queue endpoints vs aggregator search endpoints.
4. Web UI decoupled as optional module.

---

## Testing and Validation Matrix
1. Unit
- queue DBO serialization
- queue item file persistence and retrieval
- resolver routing by `source_kind`
- config validation by module combinations
- queue item file-set dedup hash/reuse behavior (same file list -> same file_set_id)

2. Integration
- downloader-only (cache enabled and cache disabled)
- aggregator-only (stateless/pass-through or SQLite cache + payload cache)
- usenet-indexer-only (PG required, SQLite optional for settings state)
- all-in-one

3. Failure-mode
- PG unavailable does not break downloader-only startup
- SQLite unavailable does not break usenet-indexer-only startup
- aggregator-only without PG remains functional
- no-cache restart marks non-resumable pending/hydrating jobs failed/cancelled with explicit reason

4. Regression
- manual NZB enqueue via API still works
- queue/history/events/file-details endpoints remain functional
- existing Newznab behavior under aggregator remains intact
- pass-through NZB fetch works when payload cache module is disabled
- repeated enqueue/hydrate of identical payload does not duplicate file-set rows

---

## Assumptions and Defaults
1. Breaking internal refactors are acceptable pre-1.0.
2. `release_id` remains KSUID across modules.
3. No dual-write between SQLite and PG release catalogs.
4. Aggregator does not require PG by default.
5. Downloader API remains core when downloader is enabled.
6. Web UI is optional and independent from DB ownership.

---

## Settings and Configuration Residency (Wrote In Stone)

This section is the canonical policy for where config/settings live and how they are applied.

### Model Summary
1. Use a **hybrid model**:
- Bootstrap/system config in `config.yaml` + env vars.
- Runtime-editable operational settings in SQLite settings state.
2. Runtime secrets stored in SQLite must be encrypted at rest.
3. For runtime-editable keys, SQLite overrides YAML/env after bootstrap load.
4. Runtime setting edits apply via live reload where safe.

### Settings Residency Table

| Setting Group | Owner Module | Source of Truth | Runtime Editable | Secret | Apply Mode |
|---|---|---|---|---|---|
| `port` | API bootstrap | YAML/env | No | No | Restart |
| `log.path`, `log.level`, `log.include_stdout` | Core bootstrap | YAML/env | No (initial scope) | No | Restart |
| `store.sqlite_path`, `store.blob_dir` | Core bootstrap | YAML/env | No | No | Restart |
| `modules.*.enabled` | Runtime composition | YAML/env | No (initial scope) | No | Restart |
| `api.key`, `api.cors_allowed_origins` | API security/transport | YAML/env | No (initial scope) | `api.key` yes | Restart |
| `postgres.dsn` (usenet-indexer) | Usenet/NZB Indexer bootstrap | YAML/env | No | Yes | Restart |
| `settings.encryption_key` reference | Settings subsystem | Env only | No | Yes | Restart |
| `servers[*]` (NNTP providers) | Downloader runtime | SQLite | Yes | Yes (credentials encrypted) | Live reload |
| `indexers[*]` (aggregator sources) | Aggregator runtime | SQLite | Yes | Yes (`api_key` encrypted) | Live reload |
| `download.*` (`out_dir`, `completed_dir`, `cleanup_extensions`) | Downloader runtime | SQLite | Yes | No | Live reload |
| `arr_integrations[*]` (Radarr/Sonarr notifier targets) | Downloader runtime | SQLite | Yes | Yes (`api_key` or auth secret) | Live reload |
| `aggregator.*` future tuning | Aggregator runtime | SQLite | Yes | Maybe | Live reload |
| `usenet_indexer.*` admin/scheduler knobs | Usenet/NZB Indexer runtime | SQLite settings state (control plane) | Yes | Maybe | Live reload |

### Precedence and Apply Rules
1. Startup flow:
- Load bootstrap YAML/env.
- Open SQLite settings state.
- Overlay runtime-editable settings from SQLite.
- Validate effective config by enabled modules.
2. Runtime edits:
- Validate patch.
- Encrypt secrets.
- Persist atomic revision.
- Publish in-memory update and apply live reload to affected subsystems.
3. Bootstrap-only keys are never overridden by SQLite.

### Runtime Secrets Policy
1. Runtime secret fields (NNTP password, indexer API key) are encrypted before SQLite persistence.
2. Encryption key material is sourced from env/bootstrap only and never stored in SQLite.
3. Key versioning metadata is stored with ciphertext for future rotation.

### Required Interfaces and Data Contracts
1. Add `SettingsStore` interface:
- `LoadEffectiveSettings()`
- `UpdateSettings(...)`
- `WatchSettingsChanges()`
2. Add revisioned settings tables:
- `settings_revision`
- `settings_nntp_servers`
- `settings_indexers`
- `settings_download`
- `settings_module_options` (future)
3. Keep `config.Load(...)` as bootstrap loader; effective runtime config comes from bootstrap + settings overlay.

### Milestone Impact (Backtrack Guidance)
1. **Milestone 1 must include** interface split for settings (`SettingsStore`) in addition to existing store splits.
2. **Milestone 2 must include** SQLite settings-state store scaffolding, separation from queue/catalog stores, and optional payload cache module boundaries.
3. **Milestone 3 remains valid**, but should assume Milestones 1-2 now include settings and payload/cache foundations above.
4. **Milestone 8 must include** admin settings API, live reload plumbing, and module-aware settings validation.

### Gate Checks Before Coding Milestone 3
1. Milestone 1 interfaces include settings ownership and no ambiguous config ownership.
2. Milestone 2 package boundaries include settings persistence package/path and adapter wiring.
3. Residency table above is accepted as canonical by the team.
4. No active design conflict remains between YAML/env bootstrap and SQLite runtime settings.
