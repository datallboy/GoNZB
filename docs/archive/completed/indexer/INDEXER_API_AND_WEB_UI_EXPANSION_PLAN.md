# Indexer API And Web UI Expansion Plan

Snapshot date: 2026-04-23
Sign-off date: 2026-04-28
Status: Completed and approved for merge

This is Phase 3 of the next indexer era.

The goal is to ship the first production-ready indexer API and web UI experience on top of the stabilized storage model and hardened release contract, while preserving strict module independence across the indexer, downloader, and aggregator domains.

## Final Sign-Off

Phase 3 is complete.

Delivered:

- stable public indexer API and public browse/detail UX
- admin indexer API and admin portal UX
- first-run auth bootstrap with no seeded default admin account
- session auth, API token auth, RBAC, CSRF, audit logging, and admin security surfaces
- bounded downloader handoff integration
- shared Newsnab category normalization consumed by indexer and aggregator-compatible surfaces
- run metrics persistence and admin run detail visibility

Sign-off basis:

- backend contracts implemented and covered by targeted tests
- frontend production build passes
- auth/bootstrap and RBAC flow verified by router-level security tests
- indexer public/admin surfaces are feature-complete for the Phase 3 scope

Residual follow-up work is tuning and future enhancement, not a Phase 3 blocker.

## Scope

- production-ready stable `/api/v1/indexer/*` public contract for release list/detail/search
- production-ready admin/indexer API for stage runtime control, release moderation, and operator visibility
- production-ready auth and authorization model for the indexer web UI and API
- first indexer web UI and admin UI built as a dedicated frontend module inside the existing frontend codebase
- explicit module/plugin behavior so downloader and aggregator remain decoupled and optional
- scheduler/runtime decisions for long-running indexer operation

## Goals

- expose a stable product-facing release catalog API with explicit filtering, sorting, pagination, and response envelopes
- keep indexer, downloader, and aggregator decoupled at backend and frontend boundaries
- replace the current downloader-focused web UI with a new modular shell that implements the indexer UI first
- keep one frontend codebase and one build/embed pipeline, but split feature ownership cleanly by module
- provide a production-ready admin surface for runtime control, stage scheduling, release moderation, auth, and security
- keep the current in-process scheduler as the canonical runtime model

## Non-Goals

- reopening storage or schema debates from completed stabilization phases unless a new blocker is discovered
- building a substantive aggregator web UI in this phase
- rebuilding downloader queue/history UI in this phase
- turning binary/file inspection routes into end-user product views
- exposing unstable provenance payloads, raw inspect artifacts, or internal release identity keys as public product contract
- turning stage execution into a subprocess orchestration system

## Core Phase Decisions

1. The indexer, downloader, and aggregator remain fully decoupled and only plug into each other through explicit interfaces and API capabilities.
2. The existing downloader-focused UI is deleted as part of this phase.
3. The frontend remains one codebase and one embedded build, but with hard internal module boundaries.
4. Aggregator UI is explicitly deferred.
5. Downloader behavior may plug into indexer UI only through a bounded "send to downloader" capability when downloader is enabled.
6. The stable public indexer API remains under `/api/v1/indexer`.
7. Internal/operator routes move behind an explicit admin namespace.
8. Auth for API/UI becomes a first-class app capability and must not depend on PostgreSQL indexer storage.
9. The scheduler remains in-process and stage-driven, with persisted runtime configuration rather than command spawning.

## Why This Comes Third

The user-facing API and UI must be built on a model that has already had:

- storage and identity cleanup where it mattered
- release-quality hardening
- backlog burn-down and throughput validation

The Phase 3 work should build product and operator surfaces on top of that stable backend foundation instead of binding a UI to internal or debug DTOs.

## Stable Public API Contract

### Public Product Routes

Stable public product routes remain under `/api/v1/indexer`:

- `GET /api/v1/indexer/releases`
- `GET /api/v1/indexer/releases/:id`

These routes are the only supported product-facing API contract for the first indexer release/list/detail experience.

### `GET /api/v1/indexer/releases`

Purpose:

- release list
- release search
- stable filtering and sorting of the public catalog

Supported query parameters:

- `q`
- `limit`
- `offset`
- `sort`
- `classification`
- `has_nfo`
- `has_par2`
- `password_state`
- `availability_tier`
- `media_quality_tier`
- `completion_min`
- `posted_after`
- `posted_before`
- `size_min`
- `size_max`
- `metadata_status`

Default sort:

- `posted_at_desc`

Allowed sort values:

- `posted_at_desc`
- `posted_at_asc`
- `size_desc`
- `size_asc`
- `title_asc`
- `availability_desc`
- `quality_desc`

Response envelope:

- `{ items, total, limit, offset, sort, filters, has_more }`

Stable summary fields per item:

- `release_id`
- `guid`
- `title`
- `posted_at`
- `size_bytes`
- `file_count`
- `completion_pct`
- `classification`
- `has_par2`
- `has_nfo`
- `password_state`
- `availability_score`
- `availability_tier`
- `media_quality_score`
- `media_quality_tier`
- `metadata_updated_at`
- `tmdb_id`
- `tvdb_id`
- nullable `imdb_id`
- `external_media_type`
- `external_title`
- `external_year`

### `GET /api/v1/indexer/releases/:id`

Purpose:

- one stable release detail record
- stable release file inventory
- stable inspect/enrichment summary
- bounded module capability signaling

Response shape:

- `{ release, files, media, external, capabilities }`

Stable detail sections:

- `release`
  - same stable public release metadata family as list
- `files`
  - stable file summaries only
  - ordered by `file_index ASC`
- `media`
  - `runtime_seconds`
  - `primary_resolution`
  - `primary_video_codec`
  - `primary_audio_codec`
  - `subtitle_languages`
  - `sample_present`
  - `archive_count`
  - `video_count`
  - `audio_count`
- `external`
  - `tmdb_id`
  - `tvdb_id`
  - nullable `imdb_id`
  - `external_media_type`
  - `external_title`
  - `external_year`
  - `metadata_updated_at`
- `capabilities`
  - transport-level flags such as `can_send_to_downloader`

### Public Visibility Rules

These are treated as part of the stable public contract:

- suppress seed/test rows
- suppress placeholder titles like `unknown-release`
- suppress weak fragment rows
- suppress rows below approved identity/completion thresholds
- suppress unstable password states

The UI must assume those rules are enforced server-side and must not attempt to reimplement public visibility policy from raw fields.

## Admin And Operator API

### Admin Namespace

Operator and admin routes move behind:

- `/api/v1/admin/indexer/*`

### Stable Admin Routes

- `GET /api/v1/admin/indexer/overview`
- `GET /api/v1/admin/indexer/stages`
- `GET /api/v1/admin/indexer/stages/:stage`
- `PATCH /api/v1/admin/indexer/stages/:stage`
- `POST /api/v1/admin/indexer/stages/:stage/actions/run`
- `POST /api/v1/admin/indexer/stages/:stage/actions/pause`
- `POST /api/v1/admin/indexer/stages/:stage/actions/resume`
- `GET /api/v1/admin/indexer/runs`
- `GET /api/v1/admin/indexer/releases`
- `GET /api/v1/admin/indexer/releases/:id`
- `PATCH /api/v1/admin/indexer/releases/:id`
- `POST /api/v1/admin/indexer/releases/:id/actions/reinspect`
- `POST /api/v1/admin/indexer/releases/:id/actions/reenrich`
- `POST /api/v1/admin/indexer/releases/:id/actions/hide`
- `POST /api/v1/admin/indexer/releases/:id/actions/unhide`

### Stage Runtime Configuration

`PATCH /api/v1/admin/indexer/stages/:stage` updates only stage-owned runtime settings:

- `enabled`
- `interval_minutes`
- `batch_size`
- `concurrency`
- `backoff_seconds`

The semantics are explicit:

- `enabled` controls scheduled participation
- `paused` is temporary operational suppression
- `run` triggers immediate one-shot execution

### Release Moderation Model

Generated release rows are not treated as directly hand-edited canonical records.

Admin mutation uses an override layer for curated and operationally safe fields:

- `display_title`
- `classification_override`
- `tmdb_id`
- `tvdb_id`
- `imdb_id`
- `visibility`
- `notes`
- `tags`

Public/admin read models merge generated release data with approved overrides.

### Transitional Existing Routes

Current routes such as:

- `/api/v1/indexer/overview`
- `/api/v1/indexer/stages`
- `/api/v1/indexer/runs`
- `/api/v1/indexer/binaries/:id`
- `/api/v1/indexer/files/:id`

should be treated as transitional/internal and not used by the new UI as its long-term contract.

## Auth, Authorization, And Middleware

### Auth Model

This phase adds OIDC-ready local auth:

- built-in users and passwords
- secure cookie sessions for browser UI
- revocable API tokens for programmatic access

Auth data must live in app/runtime state and not in PostgreSQL indexer tables.

### Authorization Model

Authorization uses fine-grained RBAC permissions with default bundled roles:

- Viewer
- Operator
- Admin

Actual enforcement is permission-based, not role-name-based.

Minimum permissions to define:

- `indexer.releases.read`
- `indexer.releases.override`
- `indexer.releases.hide`
- `indexer.releases.purge`
- `indexer.runtime.read`
- `indexer.runtime.run`
- `indexer.runtime.pause`
- `indexer.runtime.configure`
- `auth.users.read`
- `auth.users.write`
- `auth.roles.read`
- `auth.roles.write`
- `auth.tokens.read`
- `auth.tokens.write`

### Auth Routes

- `POST /api/v1/auth/session`
- `GET /api/v1/auth/session`
- `DELETE /api/v1/auth/session`
- token CRUD routes
- admin user CRUD routes
- admin role CRUD routes

### Middleware Baseline

Keep existing middleware:

- request ID
- panic recovery
- request logging
- request body limits
- CORS

Add production-ready middleware and transport rules:

- authentication middleware
- RBAC middleware
- CSRF protection for cookie-auth write routes
- rate limiting for auth endpoints
- audit logging for admin actions and stage actions
- stable structured error responses

Error shape should standardize as:

- `{ error: { code, message, request_id } }`

`/healthz` and `/readyz` remain unauthenticated.

## Runtime And Scheduler Decisions

### Canonical Runtime Model

The in-process supervisor remains the canonical indexer runtime.

The scheduler is stage-driven and lease-backed using persisted runtime state. It is not replaced by spawned CLI subprocesses.

### Ongoing Work Selection

"Which commands run ongoing" is modeled as persisted stage runtime configuration, not shell command management.

The admin surface configures:

- whether a stage participates in scheduling
- how often it runs
- batch size
- concurrency
- backoff

### Manual Operations

Admin UI may expose grouped manual actions as orchestrated API conveniences:

- ingest pipeline:
  - `scrape_latest -> assemble -> release`
- inspect pipeline:
  - safe inspect stage order
- enrich pipeline:
  - `enrich_predb -> enrich_tmdb`

These are still API-level orchestrations of stage execution, not an external job system.

### Subprocess Boundary

Inspection tool execution remains internal to inspect stages:

- `ffprobe`
- `7z`
- `unrar`
- `par2`

These subprocesses are implementation details of inspect stages, not the scheduler model for the indexer runtime.

## Web UI Architecture

### Frontend Topology

Keep one frontend codebase and one embedded build, but enforce hard internal module boundaries:

- one `ui/` codebase
- one bundle produced by the existing build pipeline
- one `internal/webui` embed/serve path

Internal frontend ownership modules:

- `auth`
- `indexer`
- `admin-indexer`
- shared app shell/platform primitives

Reserved but not implemented in this phase:

- `downloader-ui`
- `aggregator-ui`

### Current Downloader UI

The existing downloader-focused UI is deleted as part of this phase.

It is replaced by a new app shell designed around module/capability registration rather than a downloader-first product shape.

### Route Structure

Implemented routes:

- `/login`
- `/indexer/releases`
- `/indexer/releases/:id`
- `/admin/indexer/dashboard`
- `/admin/indexer/releases`
- `/admin/indexer/stages`
- `/admin/indexer/runs`
- `/admin/indexer/settings`
- `/admin/security/users`
- `/admin/security/roles`
- `/admin/security/tokens`

Not implemented in this phase:

- downloader queue/history UI routes
- aggregator UI routes

### Capability-Gated UI Registration

Navigation, routes, and actions are module/capability-driven:

- indexer routes register only when indexer capability is present
- downloader actions render only when downloader capability is present
- aggregator UI routes remain absent in this phase

The frontend must not assume downloader or aggregator are enabled.

### Allowed Cross-Module UI Integration

Cross-module behavior is intentionally narrow:

- indexer list/detail may show `send to downloader` when downloader capability is available
- no downloader queue/history/status views are embedded in indexer pages
- no shared business-state store spans indexer and downloader domains

## First Implemented Indexer UI Views

### 1. Release List

Purpose:

- show recently posted eligible releases
- support stable search, sort, filters, and pagination

Should include:

- title
- posted time
- size
- file count
- completion
- stable badges such as NFO and PAR2 presence
- quality/availability summaries
- stable external metadata summary where available

### 2. Release Detail

Purpose:

- show one stable release record without leaking internal provenance/debug payloads

Should include:

- stable release metadata
- stable file summaries
- stable media metadata
- stable external metadata summary
- bounded downloader handoff action when supported

### 3. Admin Dashboard

Purpose:

- provide operator overview of indexer runtime health and activity

Should include:

- release counts and high-level overview
- stage status
- latest runs
- links into stages, runs, and release moderation

### 4. Stage Runtime Admin

Purpose:

- configure ongoing stage behavior
- trigger manual runs
- pause/resume stages

Should include:

- stage configuration editor
- stage lease/current state visibility
- latest run and error visibility
- grouped manual actions where appropriate

### 5. Release Moderation / Admin

Purpose:

- allow operator curation without mutating generated release core state directly

Should include:

- visibility controls
- title/classification overrides
- metadata ID overrides
- notes/tags
- reinspect/reenrich actions

### 6. Security Admin

Purpose:

- manage users, roles, and API tokens

Should include:

- session-aware admin controls
- token lifecycle management
- role/permission assignment
- user management

## Module Boundary Guardrails

- do not let indexer UI depend on downloader or aggregator state models
- do not expose internal release identity fields as public contract:
  - `release_key`
  - `source_release_key`
  - `release_family_key`
- do not expose raw inspect payloads or provenance JSON as product contract
- do not let UI depend on `/binaries/:id` or `/files/:id` debug routes for product pages
- keep downloader, aggregator, and indexer backend ownership separate even when all modules are enabled

## Downloader Integration Contract

Downloader remains fully decoupled.

The only UI-level integration planned in this phase is bounded handoff from indexer release views to downloader when downloader is enabled.

To support this cleanly:

- queue enqueue contract should support explicit `source_kind=usenet_index`
- the indexer UI should not rely on aggregator-specific release assumptions
- the API should expose capability signaling so the UI knows whether handoff is available

No downloader UI screens are part of this phase.

## Dependency Order

1. Finalize the stable public indexer release contract.
2. Add admin/indexer transport contract and override model.
3. Add auth/session/token and RBAC transport contract.
4. Replace the current frontend shell and remove the downloader UI.
5. Build the indexer public views.
6. Build the admin/indexer and security views.
7. Add bounded downloader handoff support.
8. Validate module-combination behavior.

## Commit-Sized Execution Order

1. Harden backend public release DTOs and filtering/sorting contract.
2. Add backend tests for public list/detail behavior and suppression rules.
3. Add admin/indexer routes for stages, runs, and release moderation.
4. Add auth/session/token/RBAC backend foundations and middleware.
5. Remove the current downloader UI and introduce the new modular app shell.
6. Add frontend API clients/types for public indexer and admin/indexer contracts.
7. Build release list and detail views.
8. Build admin dashboard, stage admin, release moderation, and security views.
9. Add downloader handoff action using explicit capability detection.
10. Validate `usenet-indexer-only` and `all-in-one` behavior.

## Validation Criteria

- the UI consumes only the stable public/admin indexer contracts
- the current downloader UI is removed
- downloader and aggregator remain optional and decoupled
- indexer UI works in `usenet-indexer-only`
- indexer UI can offer bounded downloader handoff in `all-in-one`
- public API responses do not leak unstable/internal fields
- pagination, sorting, filtering, empty states, bad-ID behavior, auth, and admin permissions are covered
- stage runtime controls behave consistently with the in-process supervisor model

## Must Be Complete Before Calling This Phase Shippable

- stable backend public list/detail routes are finalized and covered
- admin/indexer API is finalized and covered
- auth/session/token/RBAC flows are implemented and covered
- current downloader UI is removed
- new modular app shell exists and ships the indexer UI
- indexer release list/detail/admin/security views are working
- downloader integration is bounded and capability-gated
- aggregator UI remains deferred without leaking aggregator assumptions into indexer UI
