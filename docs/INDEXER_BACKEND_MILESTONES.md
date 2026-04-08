# Indexer Backend Milestones

## Purpose

This document is the handoff plan for expanding the Usenet/NZB Indexer into a usable binary file indexer. It is designed so each milestone can be completed in a separate Codex session and committed independently without relying on prior chat context.

Future Codex sessions should treat this file as primary task scope for indexer backend milestone work.

## Global Decisions

- Runtime model: in-process supervisor inside `gonzb serve`
- No tmux support or design in this phase
- Backend-first only; no Web UI implementation in this phase
- Dedicated indexer APIs first; do not merge PG catalog search into aggregator search yet
- Metadata/enrichment scope for v1: PreDB + TMDB
- Inspection scope for v1: metadata-first, transient workspace, no persistent assembled artifact storage
- Password candidates are indexer catalog data, not downloader secrets, and may be stored in cleartext when discovered
- Passwords are not expected from structured NNTP article headers; candidate passwords may come from NZB metadata, release titles, NFO text, PreDB, archive comments, or manual/admin input
- Archive inspection is the authoritative step for determining whether a release is encrypted and whether a candidate password is valid
- Store multiple password candidates per release or artifact with source, confidence, and verification status
- Password rollup uses both flags and a state value:
  - `passworded`
  - `passworded_known`
  - `passworded_unknown`
  - `password_state`
- Release ranking is multi-axis, not one composite score:
  - `availability_score` for completeness and usability
  - `media_quality_score` for source/resolution/codec quality
  - `identity_confidence_score` for how confidently content is identified
- Persist both raw inspection facts and flattened release-level tags/summaries for later API/UI consumption
- Inspect/post-process is a family of independent submodules, not one mandatory linear phase
- PostgreSQL is the source of truth for indexer-derived catalog, inspection, and enrichment data
- Inspect phase package path: `internal/indexing/inspect`
- Inspect transient workspace setting: `indexing.inspect_work_dir`
- Default inspect workspace path: `/store/indexer/inspect`
- Keep API/UI feature requests and nested-archive follow-ups in a separate roadmap doc so this milestone plan stays focused on backend delivery order

## Sequencing Rules

- Milestones are intended to land in order unless explicitly stated otherwise.
- Each milestone should produce one reviewable Git commit.
- A milestone must leave the repository building and tests relevant to the changed area passing.
- CLI one-shot commands and long-running `serve` workers must share the same stage runner logic where applicable.
- Inspect and enrich submodules must be independently runnable from CLI and from the in-process supervisor.
- Do not add Web UI implementation in these milestones.
- Do not introduce a hard dependency from aggregator or downloader modules onto PostgreSQL indexer internals.

## Milestone 1: Indexer Supervisor Runtime

### Goal

Add the in-process supervisor that runs indexer stages continuously under `serve`.

### Depends On

- Current usenet indexer runtime and settings reload flow

### Concrete Deliverables

- Add `internal/indexing/supervisor`
- Add stage abstractions for:
  - `scrape_latest`
  - `scrape_backfill`
  - `assemble`
  - `release`
  - `inspect_par2`
  - `inspect_nfo`
  - `inspect_archive`
  - `inspect_password`
  - `inspect_media`
  - `enrich_predb`
  - `enrich_tmdb`
- Update usenet indexer runtime start path so `serve` starts these loops
- Keep existing CLI commands, but route their execution through shared stage runner logic where possible
- Add explicit one-shot CLI command support for each inspect/enrich submodule so they do not depend on a generic `inspect` pipeline command

### Code Areas

- `internal/indexing/supervisor`
- `internal/indexing/service.go`
- `internal/runtime/wiring/indexer_runtime.go`
- `internal/runtime/wiring/runtime_modules.go`
- `internal/runtime/commands/indexer.go`

### Schema Changes

- None required in this milestone if the supervisor can start with in-memory control only
- If run state is needed immediately, defer durable schema to Milestone 2

### API/Settings Changes

- None yet, beyond internal runtime config wiring

### Acceptance Criteria

- `serve` starts the indexer supervisor when the usenet indexer module is enabled
- Stages can run independently on intervals
- Existing `indexer scrape/assemble/release` commands still work
- Each inspect/enrich submodule can run on its own schedule without requiring the other submodules to have already run
- Missing upstream data causes a no-op or partial result, not a hard process-wide failure
- Settings reload can rebuild supervisor configuration safely

### Out of Scope

- External multi-process workers
- Web UI
- New public API endpoints

### Suggested Commit

`feat(indexer): add in-process supervisor for indexer stages`

---

## Milestone 2: PostgreSQL Task and Run Tracking

### Goal

Persist stage state, leases, and run history in PostgreSQL so supervisor work survives restart and can be controlled through APIs later.

### Depends On

- Milestone 1

### Concrete Deliverables

- Add stage/task state tables
- Add run history table
- Add repository methods for:
  - claiming stage work
  - heartbeats
  - completion/failure
  - pause/resume
  - listing status and recent runs
- Make leasing idempotent and restart-safe
- Track independent stage state for each inspect/enrich submodule rather than one shared inspect run

### Code Areas

- `internal/store/pgindex/migrations`
- `internal/store/pgindex/repository.go`
- `internal/app/contracts.go`
- `internal/indexing/supervisor`

### Schema Changes

Add:

- `indexer_stage_state`
- `indexer_stage_runs`

Suggested fields:

`indexer_stage_state`

- `stage_name`
- `enabled`
- `paused`
- `interval_seconds`
- `batch_size`
- `concurrency`
- `backoff_seconds`
- `lease_owner`
- `lease_expires_at`
- `last_heartbeat_at`
- `last_run_id`
- `last_success_at`
- `last_error`
- `updated_at`

`indexer_stage_runs`

- `id`
- `stage_name`
- `trigger_kind`
- `status`
- `claimed_by`
- `started_at`
- `heartbeat_at`
- `finished_at`
- `error_text`
- `metrics_json`

### API/Settings Changes

- None public yet

### Acceptance Criteria

- Stage status survives process restart
- Stale runs can be detected and recovered
- Paused stages do not claim new work
- Supervisor uses PG-backed run tracking instead of process-local state only
- Rerunning one inspect/enrich submodule does not force other post-process stages to rerun

### Out of Scope

- Catalog detail APIs
- Inspect/extraction logic

### Suggested Commit

`feat(pgindex): add stage state and run tracking for indexer supervisor`

---

## Milestone 3: Assembly Matcher Rewrite

### Goal

Replace the current subject-only assembly matcher with a scored grouping system.

### Depends On

- Milestone 1
- Milestone 2 recommended, but not strictly required

### Concrete Deliverables

- Rewrite `internal/indexing/match`
- Use evidence from:
  - normalized subject
  - quoted filename
  - yEnc markers
  - parsed `name/part/total/size`
  - poster
  - posting time window
  - article number proximity
  - xref/newsgroup overlap
  - message-id host pattern
  - extension/PAR2 hints
- Persist binary-level grouping confidence/evidence
- Keep deterministic fallback behavior

### Code Areas

- `internal/indexing/match`
- `internal/indexing/assemble/service.go`
- `internal/store/pgindex/repository.go`
- `internal/store/pgindex/migrations`

### Schema Changes

Extend `binaries` with:

- `match_confidence`
- `match_status`
- `grouping_evidence_json`

### API/Settings Changes

- Internal settings only if thresholds are introduced now

### Acceptance Criteria

- Clean posts still assemble correctly
- Obfuscated or weak subjects can still group with evidence scoring
- Low-confidence matches are explicitly marked

### Out of Scope

- Release clustering rewrite
- Inspection/external enrichment

### Suggested Commit

`feat(indexing): add scored assembly matcher with persisted evidence`

---

## Milestone 4: Release Formation Rewrite

### Goal

Improve release formation so binaries are grouped into releases using richer evidence than `release_key` alone.

### Depends On

- Milestone 3

### Concrete Deliverables

- Rework `internal/indexing/release`
- Use release clustering evidence:
  - shared poster
  - close posting timestamps
  - PAR2 relationships
  - file-count coherence
  - size coherence
  - completion ratio
  - NFO/archive hints when available
- Preserve distinct values for:
  - source title
  - deobfuscated title
  - matched media title
- Add release-level summary state for:
  - password state
  - password flags
  - media tags and media summary fields
  - availability, media quality, and identity-confidence scores
- Keep `completion_pct` as article/part completeness only; do not overload it into overall release quality or usability
- Support re-forming releases when new evidence appears

### Code Areas

- `internal/indexing/release/service.go`
- `internal/store/pgindex/repository.go`
- `internal/store/pgindex/migrations`

### Schema Changes

Extend `releases` with:

- `source_title`
- `deobfuscated_title`
- `classification`
- `match_confidence`
- `identity_status`
- `group_name`
- `passworded`
- `passworded_known`
- `passworded_unknown`
- `password_state`
- `preferred_password_id`
- `encrypted`
- `has_par2`
- `has_nfo`
- `archive_count`
- `video_count`
- `audio_count`
- `sample_present`
- `availability_score`
- `availability_tier`
- `media_quality_score`
- `media_quality_tier`
- `identity_confidence_score`
- `runtime_seconds`
- `primary_resolution`
- `primary_video_codec`
- `primary_audio_codec`
- `subtitle_languages_json`
- `media_tags_json`
- `metadata_updated_at`

Optionally extend `binaries` with:

- `inspection_status`
- `inspection_updated_at`

### API/Settings Changes

- None public yet

### Acceptance Criteria

- Releases are no longer formed from `release_key` alone
- Releases can be updated when later inspection changes identity
- False-positive merges are reduced for close-in-time posts
- `completion_pct` remains separate from `availability_score`
- Password rollup invariants are explicit:
  - `passworded=true` when encrypted artifacts are present
  - `passworded_known=true` when a verified usable password exists
  - `passworded_unknown=true` when encrypted artifacts exist without a verified usable password
  - `password_state` is derived from those flags for API/UI convenience

### Out of Scope

- External metadata provider integration
- Public catalog endpoints

### Suggested Commit

`feat(indexing): improve release formation and release identity state`

---

## Milestone 5: Inspect/Post-Process Submodule Framework

### Goal

Add independent inspect/post-process submodules and transient workspace support.

### Depends On

- Milestone 1
- Milestone 2

### Concrete Deliverables

- Add `internal/indexing/inspect`
- Add inspect candidate selection per submodule
- Add workspace manager for temporary materialization
- Add stage wiring to supervisor
- Add inspect status handling and rerun eligibility
- Add password candidate collection and archive-password verification flow
- Roll up artifact-level inspection state into release-level `password_state` and media summary fields
- Mark unresolved encrypted releases with a dedicated tag/state for later filtering
- Define independent submodules:
  - `inspect_par2`
  - `inspect_nfo`
  - `inspect_archive`
  - `inspect_password`
  - `inspect_media`
  - `enrich_predb`
  - `enrich_tmdb`
- Define each submodule as its own service with `RunOnce(ctx)` and its own repository boundary

### Code Areas

- `internal/indexing/inspect/service.go`
- `internal/indexing/inspect/workspace.go`
- `internal/indexing/inspect/par2`
- `internal/indexing/inspect/nfo`
- `internal/indexing/inspect/archive`
- `internal/indexing/inspect/password`
- `internal/indexing/inspect/media`
- `internal/indexing/enrich/predb`
- `internal/indexing/enrich/tmdb`
- `internal/indexing/supervisor`
- `internal/runtime/wiring/indexer_runtime.go`
- `internal/store/pgindex/repository.go`
- `internal/store/pgindex/migrations`

### Schema Changes

Add:

- `binary_inspections`
- `release_password_candidates`

Suggested fields:

- `id`
- `binary_id`
- `status`
- `started_at`
- `finished_at`
- `error_text`
- `materialized_bytes`
- `tool_provenance_json`
- `created_at`
- `updated_at`

Suggested fields for `release_password_candidates`:

- `id`
- `release_id`
- `binary_id`
- `artifact_id`
- `password_value`
- `source_kind`
- `source_ref`
- `confidence`
- `verification_status`
- `last_verified_at`
- `last_error`
- `created_at`
- `updated_at`

### Service and CLI Contract

Following the existing application pattern, add explicit service façade methods and matching CLI entrypoints rather than a single generic inspect pipeline.

Service methods on the usenet indexer façade:

- `InspectPAR2Once(ctx context.Context) error`
- `InspectNFOOnce(ctx context.Context) error`
- `InspectArchiveOnce(ctx context.Context) error`
- `InspectPasswordOnce(ctx context.Context) error`
- `InspectMediaOnce(ctx context.Context) error`
- `EnrichPredbOnce(ctx context.Context) error`
- `EnrichTMDBOnce(ctx context.Context) error`

CLI commands:

- `gonzb indexer inspect par2 --once`
- `gonzb indexer inspect nfo --once`
- `gonzb indexer inspect archive --once`
- `gonzb indexer inspect password --once`
- `gonzb indexer inspect media --once`
- `gonzb indexer enrich predb --once`
- `gonzb indexer enrich tmdb --once`

Submodule responsibilities:

- `inspect_par2`: parse PAR2 sets, referenced files, and repairability hints
- `inspect_nfo`: extract NFO text and parse release/password hints
- `inspect_archive`: list archive members, detect encryption, comments, and nested archive hints
- `inspect_password`: aggregate candidate passwords and verify them against encrypted artifacts
- `inspect_media`: probe playable files or archive members for runtime, resolution, codecs, subtitles, channels, and quality hints
- `enrich_predb`: external release/title matching and aliases
- `enrich_tmdb`: movie/TV identity enrichment only

### API/Settings Changes

Extend runtime indexing settings with:

- `inspect_work_dir`
- inspect max bytes
- inspect max archive depth
- per-tool timeout
- enable flags for inspect substeps
- tool paths for `ffprobe`, `7z`, `unrar`, PAR2 helper

### Acceptance Criteria

- Inspect stage runs continuously under `serve`
- Temporary workspace path is configurable
- Inspection candidates can be retried after failure or upstream changes
- Encrypted archives are not marked verified until a password attempt succeeds
- Releases with encrypted artifacts and no verified password are marked with a filterable unresolved-password state/tag
- Each submodule has its own command, service call, and supervisor stage registration
- Running one submodule does not imply a mandatory execution path through the others

### Out of Scope

- Actual extractors
- Public inspect APIs

### Suggested Commit

`feat(indexing): add inspect stage framework and transient workspace support`

---

## Milestone 6: Inspection Extractors

### Goal

Implement metadata extraction from assembled binary content.

### Depends On

- Milestone 5

### Concrete Deliverables

- Implement extractors for:
  - PAR2 metadata
  - yEnc metadata
  - NFO text extraction
  - archive listing for `7z`, `rar`, `zip`
  - media metadata via `ffprobe`
  - basic signature/type detection
- Capture:
  - archive encrypted flag
  - password verification result per candidate
  - archive comment strings where available
  - runtime/playtime
  - resolution
  - video/audio codec
  - subtitle presence and languages
  - channel count and source/quality hints where detectable
- Persist normalized extracted metadata
- Capture extractor errors and provenance per source
- Keep failure isolated per submodule so one extractor pass does not block the others

### Code Areas

- `internal/indexing/inspect/par2.go`
- `internal/indexing/inspect/nfo.go`
- `internal/indexing/inspect/archive.go`
- `internal/indexing/inspect/ffprobe.go`
- `internal/indexing/inspect/service.go`
- `internal/store/pgindex/repository.go`
- `internal/store/pgindex/migrations`

### Schema Changes

Add:

- `binary_inspection_artifacts`
- `binary_archive_entries`
- `binary_media_streams`
- `binary_text_evidence`
- `binary_par2_sets`

Suggested intent:

- artifacts/manifests
- archive members
- media stream summaries
- extracted text tokens
- PAR2 set/file linkage
- encrypted/password verification state
- flattened probe summaries for release rollups

### API/Settings Changes

- None public yet

### Acceptance Criteria

- Inspection results are stored in PG in structured form
- Tool failures do not prevent later retries
- Release formation can consume persisted inspect metadata in later passes
- Media runtime, resolution, codec, and subtitle facts are available for later API display and ranking
- A failed password candidate remains a candidate record but is marked rejected rather than deleted
- `inspect_password` depends on encrypted artifact facts, but other inspect submodules remain independently runnable

### Out of Scope

- PreDB/TMDB enrichment
- Web UI views

### Suggested Commit

`feat(indexing): persist inspect metadata from par2 nfo archives and ffprobe`

---

## Milestone 7: Canonical Media Enrichment and PreDB Fallback

### Goal

Add release enrichment and canonical media matching using TMDB and TVDB first, with PreDB as a weaker scene-release fallback source.

### Depends On

- Milestone 4
- Milestone 6 recommended

### Concrete Deliverables

- Add enrichment package
- Implement TMDB matching for likely movie releases
- Implement TVDB matching for likely episodic TV releases
- Keep `enrich_tmdb` as the existing stage name for compatibility, but let it perform canonical identity enrichment using:
  - TMDB for movie candidates
  - TVDB first for TV/episode candidates when configured
  - TMDB TV search as a fallback when TVDB is not configured or produces no usable match
- Treat PreDB as an optional fallback/provider layer rather than the primary source of truth
- Prefer `predb.ovh` as the first PreDB provider because it has public API docs and a dump feed
- Keep `api.predb.net` behind an optional provider abstraction only if needed later
- Persist candidate matches and selected best match
- Allow enrichment sources to contribute password candidates and quality hints, but do not treat them as verified passwords until inspect confirms them
- Keep enrichment non-blocking for baseline catalog formation
- Keep metadata searches as independent enrich submodules rather than folding them into a generic inspect pipeline
- Keep canonical identity metadata separate from local inspect-derived titles:
  - `source_title` remains raw Usenet/source naming
  - `deobfuscated_title` remains local inspect/source cleanup
  - `matched_media_title` is the external canonical identity title from TMDB/TVDB
  - external identity must not overwrite inspect-derived runtime, codec, subtitle, or archive facts

### Code Areas

- `internal/indexing/enrich`
- `internal/indexing/release`
- `internal/store/pgindex/repository.go`
- `internal/store/pgindex/migrations`
- `internal/app/settings_types.go`

### Schema Changes

Use and extend existing:

- `predb_entries`
- `release_predb_matches`

Add:

- `release_tmdb_matches`
- `release_tvdb_matches`

Suggested fields:

- `release_id`
- `tmdb_id`
- `media_type`
- `title`
- `original_title`
- `year`
- `confidence`
- `chosen`
- timestamps

Recommended release-level fields:

- `tmdb_id`
- `tvdb_id`
- `external_media_type`
- `original_media_title`
- `external_year`

### API/Settings Changes

Extend indexing settings with provider config for:

- PreDB sources
- TMDB API key/base config
- TVDB API key/base config
- optional TVDB subscriber PIN if required by the key type

### Acceptance Criteria

- Releases can have stored PreDB matches
- Releases can have stored TMDB matches
- Releases can have stored TVDB matches
- Source title, deobfuscated title, and matched media title remain distinct
- Inspect-derived runtime/codec/subtitle facts are not overwritten by TMDB identity metadata
- TV releases can be matched canonically even when TMDB is not the best source, using TVDB first when available
- PreDB does not block TMDB/TVDB enrichment and does not become the primary truth source for canonical media identity
- `enrich_predb` and `enrich_tmdb` are independently runnable commands/stages

### Out of Scope

- IRC ingestion

### Suggested Commit

`feat(indexing): add tmdb/tvdb enrichment and predb fallback for indexed releases`

---

## Milestone 8: Dedicated Indexer APIs

### Goal

Expose indexer task and catalog data through backend APIs designed for later UI and module consumption.

### Depends On

- Milestone 2
- Milestone 4
- Milestone 5
- Milestone 6 partially recommended

### Concrete Deliverables

Add `/api/v1/indexer` endpoints:

- `GET /api/v1/indexer/overview`
- `GET /api/v1/indexer/stages`
- `GET /api/v1/indexer/runs`
- `POST /api/v1/indexer/stages/:stage/run`
- `POST /api/v1/indexer/stages/:stage/pause`
- `POST /api/v1/indexer/stages/:stage/resume`
- `GET /api/v1/indexer/releases`
- `GET /api/v1/indexer/releases/:id`
- `GET /api/v1/indexer/binaries/:id`
- `GET /api/v1/indexer/files/:id`

### Code Areas

- `internal/api/controllers`
- `internal/api/router.go`
- indexer application/service facade package if needed
- `internal/store/pgindex/repository.go`
- `internal/app/contracts.go`

### Schema Changes

- None required unless API summary views need additional indexes

### API/Settings Changes

- This milestone is the API introduction
- Design responses for future Web UI and possible aggregator consumption
- Do not merge these endpoints into current aggregator endpoints
- Include in release list/detail responses:
  - `passworded`
  - `passworded_known`
  - `passworded_unknown`
  - `password_state`
  - `availability_score` and `availability_tier`
  - `media_quality_score` and `media_quality_tier`
  - `identity_confidence_score`
  - runtime, resolution, primary codecs, subtitle languages, and tags
- Keep raw password values out of broad list responses by default; expose password candidate state and evidence summaries instead
- Add stage list/detail responses for all inspect/enrich submodule stage names

### Acceptance Criteria

- Backend can control stages and inspect catalog state through API
- API exposes confidence/evidence summaries without requiring DB access
- Response models are stable enough for future frontend work
- Release and binary detail endpoints surface encryption state, password verification state, and probe summaries
- Stage endpoints expose independent submodule status rather than one shared inspect status

### Out of Scope

- Web UI implementation
- Aggregator search integration
- Nested archive and double-archive recursive inspection improvements beyond current single-level archive-backed probing

### Suggested Commit

`feat(api): add indexer task and catalog endpoints`

---

## Milestone 8.5: Release Title Provenance And Selection

### Goal

Make release title derivation explicit, inspect-aware, and stable so release display titles come from the best available metadata source without losing raw source naming.

### Depends On

- Milestone 4
- Milestone 6
- Milestone 8 recommended

### Concrete Deliverables

- Define and persist separate title roles for:
  - `source_title`
  - `deobfuscated_title`
  - `matched_media_title`
  - `title`
  - `title_source`
  - optional `title_confidence`
- Keep `source_title` as the raw best title derived from Usenet/post naming
- Use inspect-derived metadata to produce local title candidates from:
  - archive member names
  - chosen media filenames
  - NFO-derived titles when parseable
- Allow release re-formation or a dedicated reform pass to adopt better local titles without clearing releases or inspect state
- Keep external enrichment titles distinct from local deobfuscation
- Ensure obfuscated archive names like random `7z` or `volXX+YY` tokens are not treated as successful deobfuscation

### Code Areas

- `internal/indexing/release`
- `internal/indexing/inspect`
- `internal/store/pgindex/repository.go`
- `internal/store/pgindex/migrations`
- `internal/api/controllers`

### Schema Changes

Extend `releases` with:

- `matched_media_title`
- `title_source`
- optional `title_confidence`

If needed for intermediate inspect rollups, add structured storage for release title candidates or fold them into existing inspect summary JSON in a stable way.

### API/Settings Changes

- Expose the separate title fields in release list/detail APIs
- Surface `title_source` so UI/operator tooling can explain why a title was chosen
- Keep `source_title` readable in APIs even when `title` is replaced by better metadata

### Acceptance Criteria

- `source_title`, `deobfuscated_title`, and `matched_media_title` remain distinct
- `title` is the best display title chosen by precedence rather than blindly mirroring source naming
- Inspect-derived runtime/codec/subtitle facts and inspect-derived title candidates can improve release display/identity state without mutating `source_title`
- Obfuscated archive labels no longer appear as fake deobfuscation
- Existing releases can be re-formed in place to adopt better titles without clearing inspect data
- API responses explain the chosen display title source

### Title Semantics

- `source_title`
  - raw best title derived from Usenet/post naming
  - never overwritten by inspect or enrichment
- `deobfuscated_title`
  - clearer human-readable title derived from local evidence only
  - sources may include archive members, direct media filenames, and NFO text
  - must remain empty if no real local deobfuscation happened
- `matched_media_title`
  - canonical identity title from external enrichment such as PreDB or TMDB
  - must stay distinct from local deobfuscation
- `title`
  - final display title used by APIs and future UI
- `title_source`
  - provenance for `title`, such as `source`, `deobfuscated`, `archive_entry`, `media_filename`, `nfo`, `predb`, or `tmdb`

### Title Precedence

Choose `title` in this order:

1. high-confidence primary non-sample media title derived from local inspect evidence
2. high-confidence external matched media title
3. `deobfuscated_title`
4. cleaned `source_title`
5. fallback release key or synthetic fallback only when nothing better exists

For local inspect evidence, rank candidates in this order:

1. chosen non-sample media path from `inspect_media`
2. best non-sample media archive entry from `inspect_archive`
3. archive root folder name when it agrees with media evidence
4. parsed NFO release title when it agrees with archive/media evidence
5. sample media names only when there is no stronger non-sample evidence

Sample media must never override a real non-sample media title for the same release.

### Local Metadata Confidence Rules

High-confidence local title sources:

- primary non-sample archive media entry
- chosen media filename from a direct-media release
- NFO-derived release title when parseable and consistent
- agreement between archive entry, media filename, and NFO evidence
- release-level agreement between multiple local candidates, such as folder name + media filename

Low-confidence sources that must not become deobfuscated titles by themselves:

- raw obfuscated archive filenames
- `.7z.001` / `.rar` / `.volXX+YY.par2`
- sample-only names unless they agree with the main media stem
- purely humanized obfuscated tokens

### Title Formatting

- `deobfuscated_title`
  - release-style title intended to preserve release metadata
  - use period-delimited formatting when derived from local inspect evidence
  - retain useful release tokens such as:
    - title
    - year
    - season/episode
    - resolution
    - source
    - video codec
    - audio codec
    - release group
  - do not include raw container extension like `.mkv` by default
- `title`
  - UI/display-friendly version of the chosen release title
  - same metadata content as `deobfuscated_title`, but with spaces instead of periods
- `matched_media_title`
  - canonical movie/show title from enrichment only
  - may be shorter than the release-style title and should not replace release metadata formatting by itself

### Implementation Plan

1. Add the new release-level fields for `matched_media_title`, `title_source`, and optional `title_confidence`.

2. Add a release-title candidate resolver that can collect every reasonable local title candidate from inspect outputs:
   - prefer the primary non-sample archive media entry from `inspect_archive` / `inspect_media`
   - fallback to direct media filenames
   - fallback to parsed NFO release names when available
   - include sample names only as a last-resort local candidate
   - record enough provenance to explain why one candidate beat another

3. Normalize title candidates into both:
   - a release-style `deobfuscated_title`
   - a spaced display `title`
   without destroying meaningful release tokens such as season/episode, year, resolution, source, codec tags, or release group.

4. Add confidence scoring for title candidates so inspect-derived local titles can be marked `identified`, `probable`, or `unknown` with clearer provenance.
   - boost candidates when multiple local sources agree
   - penalize sample-only candidates
   - penalize auxiliary/archive-volume names
   - keep sample names from winning when a primary media candidate exists

5. Update release formation and release reform to choose `title` by precedence while preserving:
   - `source_title`
   - `deobfuscated_title`
   - `matched_media_title`
   - ensure reform can revisit all existing releases rather than only the first batch of binary-derived candidates

6. Set `deobfuscated_title` only when local inspect evidence produces a clearly better readable title than the source naming.

7. Keep external enrichment titles separate:
   - PreDB/TMDB may populate `matched_media_title`
   - external matches may improve `title` only when their confidence is high enough
   - external matches must not overwrite raw source naming

8. Extend release APIs so list/detail responses include:
   - `source_title`
   - `deobfuscated_title`
   - `matched_media_title`
   - `title`
   - `title_source`
   - optional `title_confidence`

9. Add regression tests for:
   - obfuscated archive-only releases
   - releases with clear archive member titles
   - releases with NFO-only title evidence
   - releases with both inspect-derived and enrichment-derived title candidates
   - title precedence and provenance stability during release reform

### Out of Scope

- Full canonical metadata matching by itself
- UI implementation
- Replacing inspect-derived runtime/codec/subtitle facts with enrichment data

### Suggested Commit

`feat(indexing): add provenance-aware release title selection`

---

## Milestone 9: Runtime Settings Expansion

### Goal

Make stage controls, inspect configuration, and enrichment providers runtime-editable.

### Depends On

- Milestone 1
- Milestone 5
- Milestone 7 recommended

### Concrete Deliverables

Extend runtime settings model for indexing with:

- stage enabled flags
- interval/batch/concurrency/backoff
- grouping thresholds and windows
- inspect workspace path and limits
- provider config for PreDB and TMDB
- ranking thresholds and tag derivation knobs where needed
- validation and reload behavior
- Per-submodule configuration for:
  - `inspect_par2.*`
  - `inspect_nfo.*`
  - `inspect_archive.*`
  - `inspect_password.*`
  - `inspect_media.*`
  - `enrich_predb.*`
  - `enrich_tmdb.*`

### Code Areas

- `internal/app/settings_types.go`
- `internal/app/settings_helpers.go`
- `internal/store/settings/*`
- `internal/settingsadmin/service.go`
- `internal/api/controllers/settings.go`
- `internal/runtime/wiring/settings.go`
- `internal/runtime/wiring/indexer_runtime.go`

### Schema Changes

- SQLite settings schema updates only
- No PG schema required unless provider runtime state is also catalog-tracked

### API/Settings Changes

- Extend existing runtime settings admin API
- Keep secrets redacted on read

### Acceptance Criteria

- Indexer supervisor cadence and inspect limits can be changed without restart
- Invalid settings are rejected
- Reload behavior stays consistent with current settings watcher flow

### Out of Scope

- UI forms
- Provider secret encryption overhaul

### Suggested Commit

`feat(settings): add runtime-editable stage inspect and enrichment settings`

---

## Milestone 10: Regression and Fixture Coverage

### Goal

Lock behavior down with realistic fixtures and restart-safe runtime tests.

### Depends On

- All earlier milestones as applicable

### Concrete Deliverables

Add tests for:

- clean scene posts
- obfuscated posts
- PAR2-backed sets
- NFO-led identity
- archive-listed releases
- media identified via `ffprobe`
- encrypted archive release with no password candidate
- encrypted archive release with multiple candidates and one verified password
- mixed encrypted release with both verified and unresolved artifacts
- false-positive password hint that fails verification
- complete article set that is unusable because no password is known
- PAR2-repairable release whose availability score exceeds raw completion alone
- false-positive grouping cases
- stale lease recovery
- rerun eligibility
- stage control APIs
- release re-formation after new evidence

### Code Areas

- `internal/indexing/*_test.go`
- `internal/store/pgindex/*_test.go`
- `internal/api/*_test.go`
- fixture directories as needed

### Schema Changes

- None

### API/Settings Changes

- None

### Acceptance Criteria

- Stage processing is idempotent enough for restart/retry behavior
- Grouping regressions are covered by fixtures
- API and runtime behavior are verified for the new supervisor model
- `completion_pct`, `availability_score`, and `media_quality_score` are validated as independent outputs
- `passworded`, `passworded_known`, `passworded_unknown`, and `password_state` are validated as consistent rollups
- Each inspect/enrich submodule can run alone and only updates its own outputs

### Out of Scope

- UI testing

### Suggested Commit

`test(indexing): add supervisor grouping inspection and enrichment fixtures`

## PostgreSQL Schema Roadmap

These changes should be introduced incrementally with the milestone that first uses them.

### Core Supervisor State

- `indexer_stage_state`
- `indexer_stage_runs`

### Binary Grouping and Inspection State

Extend `binaries` with:

- `match_confidence`
- `match_status`
- `grouping_evidence_json`
- `inspection_status`
- `inspection_updated_at`

### Release Identity State

Extend `releases` with:

- `source_title`
- `deobfuscated_title`
- `classification`
- `match_confidence`
- `identity_status`
- `group_name`
- `passworded`
- `passworded_known`
- `passworded_unknown`
- `password_state`
- `preferred_password_id`
- `encrypted`
- `has_par2`
- `has_nfo`
- `archive_count`
- `video_count`
- `audio_count`
- `sample_present`
- `availability_score`
- `availability_tier`
- `media_quality_score`
- `media_quality_tier`
- `identity_confidence_score`
- `runtime_seconds`
- `primary_resolution`
- `primary_video_codec`
- `primary_audio_codec`
- `subtitle_languages_json`
- `media_tags_json`
- `metadata_updated_at`

### Inspection Tables

- `binary_inspections`
- `release_password_candidates`
- `binary_inspection_artifacts`
- `binary_archive_entries`
- `binary_media_streams`
- `binary_text_evidence`
- `binary_par2_sets`

### Enrichment Tables

- extend `predb_entries` if needed for aliases/source metadata
- extend `release_predb_matches` if needed for richer confidence fields
- add `release_tmdb_matches`

## Handoff Rules For Future Codex Sessions

- Treat this file as primary scope for indexer backend milestone work.
- Complete at most one milestone per session unless the user explicitly asks to combine them.
- Keep changes focused on the chosen milestone.
- If a milestone requires schema changes, include migrations and repository methods in the same commit.
- Do not add Web UI implementation in milestone sessions unless the user explicitly changes scope.
- Do not merge usenet indexer catalog search into aggregator search unless the user explicitly changes scope.
- Keep downloader, aggregator, and usenet indexer module boundaries intact.
