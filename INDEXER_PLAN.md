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
- Meaning: downloader-owned per-queue-item file metadata for API/UI (`queue_item_files`).
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
| `queue_item_files` | Downloader | SQLite | Runtime + history | Queue-scoped file details for API/UI |
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

### A3. `queue_item_files` (downloader-owned file detail cache for API/UI)
```sql
CREATE TABLE IF NOT EXISTS queue_item_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_item_id TEXT NOT NULL,
  file_name TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  file_index INTEGER NOT NULL DEFAULT 0,
  is_pars BOOLEAN NOT NULL DEFAULT 0,
  subject TEXT NOT NULL DEFAULT '',
  date_unix INTEGER NOT NULL DEFAULT 0,
  poster TEXT NOT NULL DEFAULT '',
  groups_json TEXT NOT NULL DEFAULT '[]',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(queue_item_id) REFERENCES queue_items(id) ON DELETE CASCADE,
  UNIQUE(queue_item_id, file_index)
);

CREATE INDEX IF NOT EXISTS idx_queue_item_files_queue_item_id ON queue_item_files(queue_item_id);
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
- `queue_item_files`
- `blob_cache_index` (only when payload cache module is enabled)
- optional `aggregator_release_cache`
- `module_schema_version`
2. Delete SQLite catalog tables:
- `release_files`
- `release_file_groups`
- `groups`
- `posters`
- `release_cache`
3. Preserve queue file API behavior via `queue_item_files` (queue-scoped), not release-scoped catalog tables.
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
5. Queue item file details continue from downloader-owned `queue_item_files`.

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

## Milestone 9: SAB-compatible downloader API subset
### Files
- `internal/api/controllers/*`
- API routing/DTO mapping

### Tasks
1. Implement enqueue/queue/history/pause/resume/cancel subset.
2. Keep snapshot fields and queue-item-file details mapped.
3. Add compatibility tests with Sonarr/Radarr/Prowlarr flows.

### Exit criteria
- automation workflow validated end-to-end.

---

## Milestone 10: Hardening + architecture guardrails
### Files
- `ARCHITECTURE.md`
- `README.md`
- lint/arch tests
- `internal/telemetry/*`

### Tasks
1. Add forbidden import checks for module boundaries.
2. Add readiness/health per module.
3. Enforce schema version handshakes for enabled modules.

### Exit criteria
- module boundaries enforceable via CI checks.

---

## Public API / Type Changes (Explicit)
1. Queue item model retains:
- `source_kind`, `source_release_id`, `release_snapshot_json`
2. Queue file API remains (downloader-owned data):
- `GET /api/v1/queue/:id/files` remains supported.
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
