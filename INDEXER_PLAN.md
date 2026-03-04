# Detailed Refactor Plan v2: Milestones + Explicit SQLite/Postgres Schemas

## Summary
Expand the existing refactor plan into execution-ready milestones with file-level tasks and concrete target schemas.

Key goals:
1. Decouple downloader queue from release catalog storage.
2. Keep module combinations independent (`downloader`, `indexer manager/aggregator`, `usenet/nzb indexer`).
3. Make PostgreSQL optional unless usenet indexer is enabled.
4. Keep SQLite sufficient for downloader-only operation.

---

## Current-State Grounding (from code)
1. Queue is currently coupled to release catalog:
- `queue_items.release_id` foreign key to `releases`
- `internal/store/queue.go` does `JOIN releases`
2. Store is monolithic in `internal/app/context.go` (`Store` interface mixes queue/catalog/blob).
3. Current `internal/store/migrations/001_init.up.sql` is legacy mixed schema (queue + releases + release_files, etc.), not module-separated yet.

---

## Architecture Target
1. **Downloader module (core)**
- owns queue + runtime + download lifecycle
- uses SQLite + blob storage only
2. **Indexer Manager/Aggregator (optional)**
- orchestrates sources (remote newznab, local blob, local usenet index)
3. **Usenet/NZB Indexer (optional)**
- scrape headers, build releases in PostgreSQL

---

## Database Schemas (Explicit)

## A. SQLite schema (downloader runtime only)

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

### A3. `blob_cache_index` (optional metadata for local NZB blob files)
```sql
CREATE TABLE IF NOT EXISTS blob_cache_index (
  key TEXT PRIMARY KEY,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  mtime_unix INTEGER NOT NULL DEFAULT 0,
  last_verified_unix INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
```

### A4. `module_schema_version` (per-module handshake)
```sql
CREATE TABLE IF NOT EXISTS module_schema_version (
  module_name TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## B. PostgreSQL schema (usenet/nzb indexer catalog)

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
- `module_schema_version` (same concept as SQLite, PG module rows)

---

## Detailed Milestones and Task Breakdown (No Ambiguity)

## Milestone 0: Terminology + baseline docs
### Files
- `README.md`
- `ARCHITECTURE.md`
- `INDEXER_PLAN.md`
- `internal/indexer/indexer.go` comments
- `internal/indexer/manager.go` comments

### Tasks
1. Define terms: “Indexer Manager/Aggregator” vs “Usenet/NZB Indexer”.
2. Document module matrix and DB requirements.
3. Add anti-goals and no-dual-write policy.

### Exit criteria
- docs align with architecture vocabulary.
- no code behavior changes.

---

## Milestone 1: Interface split in `app.Context`
### Files
- `internal/app/context.go`
- `internal/engine/manager.go`
- `internal/queue/service.go`
- `internal/queue/*` call sites
- `internal/indexer/manager.go` injection points

### Tasks
1. Replace mega `Store` with:
- `JobStore`
- `BlobStore`
- `ReleaseResolver`
- `IndexerAggregator`
2. Update queue/engine to depend on interfaces only.
3. Remove direct `GetRelease` expectations from queue store paths.

### Exit criteria
- compiles with old behavior preserved (adapter layer allowed).

---

## Milestone 2: Store package separation
### New paths
- `internal/store/sqlitejob/`
- `internal/store/blob/`
- `internal/store/pgindex/` (scaffold only)
- `internal/store/sqlitejob/migrations/`
- `internal/store/pgindex/migrations/`

### Refactor files
- split code currently in `internal/store/store.go`, `queue.go`, `migrate.go`, etc.

### Tasks
1. Move queue/event persistence into `sqlitejob`.
2. Move filesystem NZB blob methods into `blob`.
3. Keep temporary compatibility adapters so existing imports compile.

### Exit criteria
- downloader works with SQLite+blob, no PG dependency.

---

## Milestone 3: SQLite queue decoupling migration
### Files
- `internal/store/sqlitejob/migrations/001_init.up.sql`
- migration runner in `internal/store/sqlitejob/migrate.go`
- queue queries in `internal/store/sqlitejob/queue.go`
- queue DBO structs (`internal/store/sqlitejob/dbo.go`)

### Tasks
1. Create new downloader-only SQLite schema (above).
2. Remove FK `queue_items -> releases`.
3. Stop `JOIN releases` in queue read paths.
4. Persist snapshot/reference fields on enqueue/update.

### Exit criteria
- manual upload queue works with no `releases` table.
- history endpoints still functional.

---

## Milestone 4: Indexer Manager/Aggregator extraction
### Files
- New: `internal/aggregator/manager.go`, `source.go`
- New: `internal/aggregator/sources/newznab/*`
- New: `internal/aggregator/sources/localblob/*`
- compatibility wrappers in `internal/indexer/*` (or move imports in one pass)

### Tasks
1. Introduce `CatalogSource` interface.
2. Port existing fanout logic from current `internal/indexer/manager.go`.
3. Add local blob source adapter.
4. Keep Newznab API behavior unchanged.

### Exit criteria
- aggregator module runs independently from downloader.

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
2. Add scrape run/checkpoint tables and repositories.
3. Implement initial header ingest with dedupe constraints.

### Exit criteria
- `indexer scrape --once` writes headers/checkpoints to PG.

---

## Milestone 6: Assembly and release formation
### Files
- `internal/indexing/assemble/service.go`
- `internal/indexing/release/service.go`
- `internal/indexing/match/service.go`
- PG migrations `002_*.sql`, `003_*.sql`

### Tasks
1. Build binaries + parts from article headers.
2. Promote candidates into releases/release_files.
3. Add regex + predb enrichment plumbing.
4. Add nzb cache metadata table and service hooks.

### Exit criteria
- formed releases queryable from PG and resolvable by ID.

---

## Milestone 7: Resolver-based downloader hydration
### Files
- `internal/resolver/release_resolver.go`
- resolver implementations for manual/blob/aggregator/usenet-index
- `internal/engine/manager.go`
- `internal/queue/service.go`

### Tasks
1. Queue Add path sets `source_kind` + snapshot.
2. Hydration switches on `source_kind`.
3. Manual source path does not call aggregator/PG.
4. Aggregator/usenet_index sources resolve by `source_release_id`.

### Exit criteria
- downloader remains operational even when aggregator/indexer modules are disabled.

---

## Milestone 8: CLI + config modular runtime
### Files
- `internal/infra/config/config.go`
- `config.yaml.example`
- `cmd/gonzb/main.go`

### Tasks
1. Add module flags:
- `modules.downloader.enabled`
- `modules.aggregator.enabled`
- `modules.usenet_indexer.enabled`
2. Add conditional validation by enabled module.
3. Add subcommands:
- `downloader serve`
- `aggregator serve`
- `indexer scrape`
- `indexer form`
- `indexer retention`

### Exit criteria
- any module combination starts with clear validation errors when misconfigured.

---

## Milestone 9: Downloader SAB-compatible API subset
### Files
- `internal/api/controllers/*` (new SAB subset controller)
- route registration in API server package
- DTO/response mapper files

### Tasks
1. Implement subset endpoints for enqueue/queue/history/pause/resume/cancel.
2. Map queue snapshot/reference fields into responses.
3. Add compatibility tests with Sonarr/Radarr/Prowlarr expected flows.

### Exit criteria
- basic external automation workflow is validated.

---

## Milestone 10: Hardening + architecture guardrails
### Files
- `ARCHITECTURE.md`, `README.md`
- lint/arch tests location (project convention)
- new `internal/telemetry/*`

### Tasks
1. Add forbidden-import checks (downloader cannot import pgindex/indexing).
2. Add health/readiness + module metrics.
3. Add schema version handshake checks at startup.

### Exit criteria
- dependency boundaries are enforceable, not just documented.

---

## Public API / Type Changes (Explicit)
1. Queue item model:
- add `source_kind`, `source_release_id`, snapshot fields.
2. New interfaces in `internal/app`:
- `JobStore`, `BlobStore`, `ReleaseResolver`, `IndexerAggregator`.
3. Config:
- `modules.*` block + PG DSN section + module-specific validation.
4. CLI:
- modular subcommands as above.

---

## Testing and Validation Matrix
1. Unit:
- queue DBO serialization
- resolver routing by source_kind
- config validation combinations
2. Integration:
- downloader-only (SQLite only)
- aggregator-only (no downloader)
- usenet-indexer-only (PG only)
- all-in-one
3. Failure-mode:
- PG unavailable does not break downloader-only startup
- SQLite unavailable does not break indexer-only startup
4. Regression:
- existing Newznab search/get behavior under aggregator module
- existing queue/history UI behavior under downloader mode

---

## Assumptions and Defaults
1. Breaking internal refactors are acceptable before stable `1.0`.
2. `release_id` standard is KSUID string across modules.
3. No dual-write between SQLite and PG.
4. Federation/activitypub stays out of this milestone series.
5. Existing mixed SQLite release tables are transitional and can be deprecated after migration.
