# Indexer DB Integrity And Stage Execution Audit Plan

Snapshot date: 2026-06-11

This is the active execution guide for the database-integrity follow-up, full stage/DBO audit, and stage-execution hardening workstream.

Use this doc together with:

- `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` for the living ownership matrix and stage/table interaction rules
- `docs/active/INDEXER_FOUNDATION_DOCS.md` for current routing of active versus archived docs

## Summary

The audit goal is no longer just “understand the corruption incident.” It is now:

- prove which write/query paths are safe enough for a stable release
- document how every major stage reaches the database
- identify query, lock, and overlap risks in execution order
- freeze schema only after the hot-path audit shows no remaining structural gaps

Working decisions already locked:

- do not switch the product to a permanently sequential global pipeline
- keep the long-term execution model concurrent where hot-table ownership is disjoint
- treat `scrape_*` as the highest-risk canonical writer and isolate it during bootstrap/recovery
- prefer stage overlap gates and phased runtime profiles over introducing multi-process topology first
- automatic maintenance must not purge ingest payloads during normal supervisor operation; destructive payload purge is manual-only
- `binaries` identity fields are confidence-guarded; lower-confidence rediscovery must not rewrite indexed family identity after a stronger match exists
- detailed matcher traces are no longer retained in PostgreSQL during normal assemble; compact inline summaries remain for release/admin behavior
- behavior-bearing binary evidence should be columned, not kept in hot JSONB fields

## Current DB Corruption Follow-up Findings

Observed on `2026-06-12` during the repeated corruption investigation:

- PostgreSQL reported `compressed pglz data is corrupt` while `indexer_maintenance` read old `binary_grouping_evidence.payload_json`
- `amcheck` separately reported `heap tuple (355980,1) from table "binaries" lacks matching index tuple within index "idx_binaries_release_family_key"`
- the maintenance read exposed the corrupt JSONB value, but it was not the causative write path
- the confirmed corrupt index is on `(provider_id, newsgroup_id, release_family_key)`, which is written by assemble/recovery identity updates
- the assemble binary upsert path was found to replace indexed identity fields unconditionally, even when incoming matcher confidence was lower than the persisted match
- this allowed weaker rediscovery passes to churn `release_family_key` and neighboring identity columns while leaving the stronger `match_confidence` in place
- detailed matcher evidence retention also wrote millions of JSONB rows to `binary_grouping_evidence`, creating high varlena/WAL pressure without being required for release formation

Landed hardening direction:

- guard binary identity replacement by match confidence
- keep monotonic counters such as expected file count and total parts able to advance under lower-confidence rediscovery
- read back actual persisted identity after upsert before queueing release-summary refresh keys
- stop persisting detailed matcher traces into `binary_grouping_evidence` by default
- set fresh Docker Postgres clusters to initialize with data checksums
- set hot binary evidence JSONB storage to avoid pglz compression where those fields remain
- move remaining hot `binaries.grouping_evidence_json` behavior fields into scalar columns:
  - grouping summary kind/status/fallback
  - PAR2 target base/file/source markers
- treat `binaries.grouping_evidence_json` as legacy read compatibility only; detail reads may synthesize JSON from scalar columns for the UI

Remaining validation requirement:

- restart from a fresh checksummed database and run a staged soak with `amcheck` checkpoints between scrape, assemble, recovery, release-summary-refresh, and release formation

## Current Live Bootstrap Status

Validated on `2026-06-11` against a clean database:

- `indexer maintenance check-integrity --ensure-extension` passed for:
  - `article_headers_pkey`
  - `article_headers_newsgroup_id_article_number_key`
  - `article_headers_newsgroup_id_message_id_key`
- scrape-only bootstrap is the active rebuild profile
- `indexer scrape latest` and `indexer scrape backfill` were launched in CLI mode, not via full `serve`
- concurrent scrape-only execution is currently healthy on the fresh DB

Observed live behavior after the restart:

- `scrape_latest` resumed completing runs after the first bad historical run
- `scrape_backfill` started cleanly and began inserting 20k-row ranges
- `article_headers` moved past the initial bootstrap threshold and is continuing to grow under concurrent scrape-only load

Fresh issue found and fixed during bootstrap:

- some NNTP strings included embedded NUL bytes
- Go still treats those strings as valid UTF-8, so the old sanitizer let them through
- PostgreSQL then rejected poster inserts with:
  - `ERROR: invalid byte sequence for encoding "UTF8": 0x00`
- fix applied:
  - strip `\x00` in `sanitizeUTF8()` before any header/payload/poster DB write
  - add regression coverage on `InsertArticleHeaders`

Operational guidance until the next phase:

- keep building header backlog with CLI scrape-only commands first
- do not enable assemble/recover/release stages until the scrape-only bootstrap has accumulated a meaningful backlog and remains stable over a longer soak window

## Smart Stage Executor Plan

The preferred direction is not a permanently sequential pipeline and not a second scheduler. The preferred direction is an adaptive supervisor policy layered onto the existing stage-gate chain:

- prerequisite gate
- storage guard
- memory guard
- new backlog-aware execution gate

The new execution gate should make overlap decisions from live backlog and capacity signals instead of static operator choreography alone.

### Phase 1 target behavior

1. Let `scrape_latest` and `scrape_backfill` run together during fresh bootstrap.
2. When unclaimed `article_headers` backlog crosses a configured threshold and assemble is enabled, temporarily suppress or reduce scrape until assemble reports that it needs more work.
3. Allow `assemble_*`, `recover_yenc`, `release_summary_refresh`, and `release` to run together only when their upstream backlog signals say the overlap is productive.
4. Keep inspect/archive tail stages deprioritized until release formation is healthy.

### Candidate backlog signals

The first implementation should be based on bounded, cheap signals already available or easy to add:

- unassembled `article_headers` count or capped estimate
- joinable `yenc_recovery_work_items` ready-now count
- `release_family_summary_refresh_queue` count
- `release_ready_candidates` count
- inspect-ready candidate counts already used by dashboard stats

### Candidate decisions

- `scrape_*`
  - allowed when assemble backlog is below threshold
  - paused/throttled when unassembled header backlog is above threshold and assemble is enabled
- `assemble_*`
  - preferred whenever scrape-created backlog is above threshold
- `recover_yenc`
  - preferred when joinable hot queue is above threshold
- `release_summary_refresh`
  - preferred when refresh queue is above threshold
- `release`
  - always allowed when ready candidates are present

### NNTP saturation policy

The supervisor should eventually drive NNTP utilization toward saturation without forcing every stage to its static max concurrency.

The intended control split is:

- keep per-stage `max_concurrent` as the hard ceiling
- add a runtime target such as `desired_nntp_utilization_percent`
- let the adaptive gate decide which stages are allowed to consume connections at a given moment

This means the first smart executor step is stage admission/gating, not automatic per-stage concurrency mutation.

### Cross-process NNTP telemetry requirement

The dashboard and future smart executor both need NNTP visibility outside the local process.

Current change direction:

- every active indexer runtime can publish an NNTP runtime snapshot to PostgreSQL
- the dashboard reads the freshest shared snapshot instead of assuming the local `serve` process owns the active manager

This is required because standalone `indexer ...` CLI runs and `serve --no-indexer-supervisor` do not share an in-memory manager with the dashboard process.

### Landed first gate

The first smart-executor behavior is now implemented:

- scheduled `scrape_latest` and `scrape_backfill` are blocked when:
  - at least one assemble stage is enabled, and
  - unassembled `article_headers` backlog is above the high-water threshold
- scrape resumes only after backlog drops below a lower resume threshold
- manual scrape runs still bypass this gate

Current threshold shape:

- `high_water = max(50000, 20 * enabled_assemble_batch_capacity)`
- `resume_threshold = max(10000, high_water / 2)`

The gate uses `EstimateUnassembledArticleHeaders()` first and falls back to exact count only when the estimate is zero.

Additional landed admission gates:

- global critical-index integrity gate
  - scheduled stages are blocked when `article_headers` critical index checks fail
  - manual runs bypass the gate so repair and diagnostic commands remain usable
  - the gate is cached briefly and runs before backlog/resource admission gates
- `release_summary_refresh`
  - paused when `release_ready_candidates` backlog is already above threshold and `release` is enabled
  - resumes only after ready backlog drops below the lower resume threshold
- inspect family
- light lanes remain eligible under backlog pressure:
  - `inspect_discovery`
  - `inspect_par2`
  - `inspect_discovery` is now pre-release capable and may inspect standalone opaque binaries with no `release_id`
  - heavier lanes are paused while core pipeline backlog is hot:
    - `inspect_nfo`
    - `inspect_archive`
    - `inspect_password`
    - `inspect_media`
  - the core backlog signals are:
    - claimable assemble backlog
    - yEnc hot backlog
    - release summary refresh queue
    - ready release candidates
- NNTP traffic priority gate
  - uses live local NNTP saturation in `serve`
  - keeps stage `concurrency` as the operator-configured hard ceiling
  - when the pool is hot, lower-priority NNTP stages yield first:
    - `inspect_par2`
    - `scrape_backfill`
    - `scrape_latest`
  - `recover_yenc` retains priority when it has meaningful hot backlog

Observed live behavior on `2026-06-11` with all supervisor stages enabled:

- `inspect_discovery` completed repeatedly under supervisor even while assemble/yEnc/refresh backlog remained hot
- `inspect_par2` also completed repeatedly and processed actionable work
- `inspect_nfo`, `inspect_archive`, `inspect_password`, and `inspect_media` stayed deferred as intended
- `release_generate_nzb`, `release_archive_nzb`, and `release_purge_archived_sources` were not backlog-gated; they simply completed as no-op passes because there were still no formed releases

### Deliberately deferred

Dynamic worker/concurrency tuning is still deferred.

For now:

- stage concurrency remains operator-configured
- the smart executor only decides stage admission
- CPU/DB-heavy stages keep manual concurrency control
- NNTP-driven stages may eventually gain utilization-target tuning, but that is not part of the current landed behavior

## Maintenance Safety Policy

Effective now:

- scheduled `indexer_maintenance` keeps:
  - stale stage runtime repair
  - yEnc work-item backfill/retire
  - catalog-file backfill
  - bounded derived-state cleanup
- scheduled `indexer_maintenance` no longer purges `article_header_ingest_payloads`
- payload purge is manual-only through:
  - `gonzb indexer maintenance purge-header-payloads`

Rationale:

- `article_header_ingest_payloads` still feeds yEnc recovery and raw fallback evidence
- assemble lane A now uses `article_header_assembly_keys` for claim-time normalized filename matching instead of scanning payload rows
- auto-purging payloads before downstream backlog drains can reduce release formation quality and regroup potential
- intentional source purge remains the only automated destructive lineage cleanup path after NZB generation/archive/purge eligibility

## Pass 1 Findings: Scrape

Status: in progress, but the primary scrape-stage write path has now been reviewed enough to lock the first findings.

### Service-layer findings

- `scrape` is intentionally two-mode:
  - `RunLatestOnceWithMetrics`
  - `RunBackfillOnceWithMetrics`
- both modes share the same provider validation, integrity preflight, run tracking, and concurrency/rotation scheduler
- group fairness is currently provided by `reserveRunGroups`, which rotates groups across runs based on `MaxBatches`
- current stage metrics are useful for throughput and fairness, but `articles_inserted` is operationally misleading: it reflects headers accepted into the insert path, not guaranteed newly unique `article_headers` rows

### Store / DBO findings

Hot scrape-owned store paths confirmed:

- `StartScrapeRun`
- `FinishScrapeRun`
- `GetLatestCheckpoint`
- `UpsertLatestCheckpoint`
- `GetBackfillCheckpoint`
- `GetBackfillCheckpointState`
- `HasBackfillCutoffReachedForGroup`
- `SetBackfillCheckpointState`
- `UpsertBackfillCheckpoint`
- `InsertArticleHeaders`

Observed write/query shape:

- scrape run tracking is a simple insert/update lifecycle in `scrape_runs`
- scrape now also records cross-post discovery telemetry from observed `Xref` memberships into `article_header_crosspost_groups`
- that telemetry is intended for operator review and candidate-group reporting only; it is not part of canonical binary/file lineage or NZB provenance
- the first admin popularity report is intentionally bounded to a recent rolling window so it does not devolve into an all-time aggregate over the full telemetry table
- scrape integrity preflight is now cached briefly in-process so critical-index safety remains in place without re-running `amcheck` before every single scrape pass
- historical cross-post seeding is handled by a manual batched maintenance command, not by automatic replay during scrape startup
- checkpoint state is centralized in `scrape_checkpoints`
- header ingest is a transactional batch that does:
  - poster dimension ensure
  - `article_headers` insert/resolve
  - payload upsert into `article_header_ingest_payloads`
- duplicate resolution in `InsertArticleHeaders` is handled batch-wise through:
  - existing candidate lookup
  - `INSERT ... ON CONFLICT DO NOTHING`
  - resolve-by-ordinal of inserted vs existing rows
- this is materially better than the old per-row re-probe shape, but it still means the “inserted” return value is really “processed/resolved” count

Measured query-shape issue found during Pass 1:

- the old `existing_candidates` join used:
  - `ah.article_number = r.article_number OR ah.message_id = r.message_id`
- on live data, that shape caused a full `article_headers` seq scan and filtered tens of millions of join combinations in the sample plan
- both unique indexes existed, but the `OR` join shape prevented the planner from using them effectively
- fix applied:
  - split article-number and message-id matching into separate branches
  - preserve the existing resolution precedence while making both lookups index-friendly

### Schema / overlap findings

- scrape owns more than `article_headers` and `article_header_ingest_payloads`; the audit and schema doc must explicitly include:
  - `scrape_runs`
  - `scrape_checkpoints`
  - `posters` as a shared support dimension currently written during scrape ingest
- scrape remains bootstrap-safe and should stay isolated from assemble/recovery/refresh by default
- scrape-only overlap (`latest` + `backfill`) is currently validated as safe on the fresh DB
- `scrape_runs` is provider-scoped run history only; it does not encode latest-vs-backfill mode itself, so operational tooling must not treat it as the sole source of truth for stage-kind reporting

### Changes landed from Pass 1

- service-layer scrape sanitizer now strips embedded NUL bytes too, not just the repository-layer sanitizer
- added service regression coverage proving NULs are removed before scrape hands headers to the repo
- `InsertArticleHeaders` duplicate resolution now uses split article-number and message-id branches so both unique indexes remain usable under load

### Remaining scrape-specific follow-up

- decide whether `articles_inserted` should be renamed or supplemented with a more truthful metric name before stable release
- decide whether poster resolution should remain part of scrape ingest or be reduced further during a later hot-path pass
- rerun the fresh scrape bootstrap on the patched binary and confirm the live insert path no longer spends time in the old `OR`-join shape
- decide whether stale `scrape_runs` cleanup should remain maintenance-only or whether scrape startup should adopt a more direct stale-run handoff

## Pass 2 Findings: Assemble

Status: primary service/store/query audit completed; a small hot-path cleanup has been landed.

### Service-layer findings

- `assemble` runs in two service modes:
  - concurrent `RunOnceWithMetrics` that claims one batch and fans out work to in-process workers
  - single-worker `runOnceWithMetricsSingle` that performs the actual binary/part/stat refresh flow
- service policy already does the right thing for release-summary ownership:
  - `UpsertBinaries` is called with `WithDeferredReleaseFamilySummaryRefresh`
  - `RefreshBinaryStatsBatch` is also called with `WithDeferredReleaseFamilySummaryRefresh`
  - so normal assemble service execution only enqueues dirty release-family keys; it does not inline heavy summary recomputation
- current metrics are strong on binary upsert and refresh timings, but they still collapse claim selection into one coarse `candidate_selection_duration_ms`
  - the audit could not isolate claim/dequeue time from hydration time from service metrics alone

### Store / DBO findings

Hot assemble-owned store paths confirmed:

- `ClaimUnassembledArticleHeaders`
- `listPriorityAssemblyHeaderIDs`
- `listRecentUnassembledHeaderIDs`
- `hydrateAssemblyCandidates`
- `UpsertBinaries`
- `UpsertBinaryParts`
- `RefreshBinaryStatsBatch`

Observed lane behavior:

- lane A is a structured-completion path:
  - it scans a recent unassembled header window
  - joins into `article_header_ingest_payloads`
  - tries to match those subject file names against existing incomplete binaries by normalized filename
  - it prefers continuing partially observed binaries that already have structured file identity
- lane B is a recent backlog path:
  - it scans recent unassembled headers
  - optionally excludes structured-progress matches
  - it is the catch-all feed for general subject matching and new binary formation
- two helper functions remain unused in current code:
  - `listPriorityAssemblyBinaries`
  - `listPendingHeadersForProgressBinaries`
  - these appear to be inactive/dead helper debt, not part of the active service path

Measured live query-shape findings on the fresh scrape backlog:

- lane B recent-header selection is cheap:
  - `article_headers_pkey` backward scan
  - `LIMIT 1000/5000`
  - about `0.6 ms`
- lane A structured-priority selection is more complex but acceptable at current scale:
  - recent pending scan over 5000 headers
  - 5000 payload lookups via `article_header_ingest_payloads_pkey`
  - lateral binary lookup via `idx_binaries_normalized_file_identity`
  - about `29.7 ms` on the fresh DB, returning zero rows because no binaries existed yet
- hydration of 1000 claimed headers is acceptable:
  - 1000 point lookups into `article_headers`, `article_header_ingest_payloads`, `newsgroups`, and `posters`
  - about `8.7 ms`
- claim/update itself is not free, even on point IDs:
  - claiming 1000 rows took about `26.2 ms`
  - most of that cost is the write itself, not candidate lookup

Hot-path inefficiencies found:

- assemble previously called `EnsurePoster` for raw-only poster names
  - this kept poster dimension writes in the assemble hot path
  - fix landed:
    - assemble now preserves already materialized `poster_id` values but does not write `posters`
    - raw payload poster text remains read-only weak matcher evidence when `poster_materialize` lags
- `RefreshBinaryStatsBatch` was rereading `article_headers` twice per `binary_parts` row through correlated subqueries
  - fix landed:
    - join `article_headers` once inside the materialized part/header intermediate

### Schema / overlap findings

- assemble still has the most important current ownership exception in the system:
  - it updates `article_headers.assembly_claimed_by`
  - it updates `article_headers.assembly_claimed_until`
  - it sets `article_headers.assembled_at`
- this means scrape/assemble overlap is not just “operationally discouraged”; it is structurally high-risk:
  - both stages write `article_headers`
  - poster writes are now isolated to `poster_materialize`, so poster no longer adds scrape/assemble write overlap
  - scrape should stay isolated from assemble during bootstrap and heavy regroup work
- claim selection is serialized globally with:
  - `pg_advisory_xact_lock(hashtext('gonzb-assemble-claim'))`
  - this prevents concurrent claim races across lane workers/processes, but it also means claim throughput has a hard serialized gate
- the store still permits inline `refreshReleaseFamilySummary()` behavior if callers do not set the defer flag
  - the normal service path is safe today
  - but the store surface still encodes a cross-boundary fallback behavior that should stay under review

### Changes landed from Pass 2

- assemble no longer writes `posters`; `poster_materialize` owns poster dimension writes
- `RefreshBinaryStatsBatch` no longer rereads `article_headers` twice per part row when computing aggregate binary stats
- lane A claim selection no longer derives incomplete binary/header filename matches from broad joins over `binary_identity_current`, `binary_observation_stats`, and `article_header_ingest_payloads`
- scrape now seeds `article_header_assembly_keys` inline from already parsed subject filenames; this is an assemble-owned work surface completed/deleted by assemble after binary part assignment
- assemble/recover now materialize `binary_completion_keys` when binary identity/stats change; this is binary-owner state and avoids a separate stage that would only move the same heavy selector elsewhere
- stale `article_header_assembly_keys` rows must not accumulate; migration `054` removes historical completed keys and assemble deletes newly completed keys inline

### Remaining assemble-specific follow-up

- decide whether the inline `refreshReleaseFamilySummary()` fallback in the store should be removed entirely so assemble can only enqueue dirty summary keys
- decide whether `article_headers` claim/progress bookkeeping should remain a bounded transitional exception for `0.8` or be moved behind a dedicated assemble work surface first
- add finer claim-selection metrics if assemble is re-enabled and becomes hot again
- decide whether the unused progress-binary helper functions should be removed or revived explicitly before stable release

## Pass 3 Findings: Recover yEnc

Status: service/store/query audit completed; crash-hardening code change landed after the `2026-06-12` Postgres recovery incident.

### Service-layer findings

- `recover_yenc` is now queue-snapshot-first:
  - `ListYEncRecoveryCandidates`
  - fetch BODY prefix
  - parse yEnc header
  - rematch and persist identity
- service concurrency is bounded and isolated to NNTP BODY work; database fan-out happens in queue selection and persistence, not in service worker orchestration
- metrics are good enough to separate:
  - candidate selection
  - fetch
  - parse
  - match
  - write
  - not-found/noop backoff writes

### Store / DBO findings

Hot recover-owned store paths confirmed:

- `ListYEncRecoveryCandidates`
- `BackfillYEncRecoveryWorkItems`
- `syncYEncRecoveryWorkItemsForBinariesInTx`
- `ApplyYEncHeaderRecovery`
- `RecordYEncRecoveryNotFound`
- `RecordYEncRecoveryNoop`
- `RecordYEncRecoveryTransientFailure`

Observed queue/query behavior:

- candidate listing now reads from denormalized `yenc_recovery_work_items` snapshots and does not join `article_headers`, `article_header_ingest_payloads`, or `binaries`
- candidate listing claims rows with `FOR UPDATE SKIP LOCKED`, marks them `running`, and uses lease expiry to recover abandoned work
- transient fetch/apply failures release the claim with a short queue-local backoff
- stale ready rows with unusable queue identity are retired in bounded batches before candidate selection
- seed/backfill is branch-based, not one giant inferential selector:
  - high-value multipart / expected-file-count binaries
  - blank-family weak/provisional binaries
  - summary-backed weak/recover-pending families
- queue seeding/refresh is the only place that performs source-table joins to populate candidate snapshots

Schema / overlap findings:

- `recover_yenc` candidate listing no longer depends on `article_header_ingest_payloads`; queue snapshots carry the subject, poster, xref, yEnc, and article metadata needed by the service
- payload rows are still read during bounded queue refresh/seeding and updated for legacy retry/backoff compatibility
- overlap with assemble is acceptable only because:
  - assemble owns binary formation
  - recover owns recovery queue state
  - both only communicate downstream through bounded binary refinement and dirty-key enqueue
- overlap with `release_summary_refresh` is acceptable when refresh only consumes dirty summary keys and ready candidates, not live per-binary recovery state

Remaining recover-specific follow-up:

- move the remaining retry/backoff compatibility state out of `article_header_ingest_payloads` if the schema freeze allows one more cleanup pass
- audit the three-branch seed path plans after a clean rebuild; it is bounded, but still the only recover-yEnc path that joins several hot source tables by design
- broaden the same query-safety contract to other stages: no scheduled selector should join multiple giant tables unless it first bounds input with a queue or explicit ID window
- keep the global integrity gate enabled during every long soak; a failed critical index check should stop scheduled stages before they continue exercising a compromised cluster

## Pass 4 Findings: Release Summary Refresh

Status: primary audit completed; prior code changes already addressed the hot issues in this pass.

### Service-layer findings

- `release_summary_refresh` is now clearly two-phase:
  - Phase A summary recompute
  - Phase B ready-candidate and recovered-file-set materialization
- service metrics now expose:
  - dequeue
  - summary aggregation
  - aggregate subquery
  - dominant-row subquery
  - ready sync
  - recovered-file-set sync
  - hot vs cold batch counts

### Store / DBO findings

- hot dequeue is branch-prioritized:
  - missing-summary / actionable / fragment-first
  - weak-single residue last
- Phase A no longer uses the old window-heavy family summary shape
  - grouped aggregate and dominant-row selection are separated
- Phase B no longer forces the same long transaction as Phase A
- ready-candidate materialization is batched

Schema / overlap findings:

- `release_summary_refresh` remains the only heavy writer of:
  - `release_family_readiness_summaries`
  - `release_ready_candidates`
- Phase B recovered-file-set discovery originally scanned the yEnc recovery projection for each refresh batch because key-kind matching was expressed as one `OR` predicate and `COALESCE(recovered_source, '')`. It now splits release-family and base-stem branches and uses direct `recovered_source = 'yenc_header'` predicates so indexed binary identity lookups drive the scan.
- missing-summary dequeue originally used a broad anti-join that scanned all readiness summaries and all queued rows. It now takes an ordered queue window first and probes summaries by primary key.
- other stages may enqueue dirty keys only
- maintenance cleanup must defer while refresh backlog exists; code already reflects this

Remaining refresh-specific follow-up:

- verify on the rebuilt DB whether Phase A remains the dominant cost once assemble/recover start producing real backlog
- if it does, the next likely optimization is workload shaping by family fanout/cardinality, not schema growth

## Pass 5 Findings: Release

Status: audit completed; no new code change required in this pass.

### Service / store findings

- `release` is now cleanly ready-candidate-driven:
  - it consumes `release_ready_candidates`
  - it acks `release_ready_candidate_acks`
  - it no longer churns fragment-only families directly
- persistence is split correctly:
  - candidate selection
  - binary/title reads
  - release snapshot/catalog persistence

Cross-newsgroup behavior confirmed:

- release catalog formation already supports one logical release spanning multiple contributing newsgroups
- `release_newsgroups` stores all contributing groups
- per-file article lineage remains tied to the source file/binary provenance, not merged across groups arbitrarily

Remaining release-specific follow-up:

- revalidate cross-group recovered-file-set formation on the rebuilt DB after enough yEnc and assemble backlog exists

## Pass 6 Findings: Inspect Stages

Status: audit completed at ownership/query-boundary level; no code change required in this pass.

Findings:

- inspect stages are reasonably isolated behind:
  - `binary_inspections`
  - stage-owned evidence tables
- inspect stages may write release-facing metadata/evidence, but they are not primary owners of binary identity
- current boundary is acceptable for `0.8` provided inspect remains downstream of stable release formation and is not enabled during bootstrap/regroup

Remaining inspect-specific follow-up:

- if inspection becomes hot again, split candidate selection and reservation timing further in metrics before changing schema or ownership

## Pass 7 Findings: Archive / NZB / Purge Tail

Status: audit completed at ownership/query-boundary level; no code change required in this pass.

Findings:

- archive/NZB/purge remains downstream and terminal by design
- purge is still the only intentional downstream mutator of upstream lineage
- current transitional surfaces (`release_files`, `nzb_cache`) remain acceptable for `0.8` as compatibility surfaces, not new architectural direction

## Pass 8 Findings: Maintenance / Runtime / Admin

Status: audit completed; no new code change required in this pass.

Findings:

- maintenance still performs broad operational support work:
  - stale run cleanup
  - yEnc work-item backfill
  - catalog-file backfill
  - payload / grouping-evidence / readiness cleanup
- the important hardening already landed:
  - readiness cleanup defers when refresh backlog exists
  - integrity checks exist for critical ingest indexes
- dashboard backlog surfaces needed prior correction because several were inventory approximations, not true queues; those corrections have already been made in earlier passes

Remaining maintenance-specific follow-up:

- consider whether `RunIndexerMaintenance` should eventually split yEnc seeding from metadata cleanup when steady-state profiles are reintroduced

## Audit Execution Order

Audit stages in this exact order:

1. `scrape_latest` / `scrape_backfill`
2. `assemble_lane_a` / `assemble_lane_b`
3. `recover_yenc`
4. `release_summary_refresh`
5. `release`
6. `inspect_discovery`, `inspect_par2`, `inspect_nfo`, `inspect_archive`, `inspect_password`, `inspect_media`
7. `release_generate_nzb`, `release_archive_nzb`, `release_purge_archived_sources`
8. `indexer_maintenance`, integrity/admin/runtime/stats support queries

This order is intentional:

- it follows upstream fact creation to downstream materialization
- it lets later audits assume upstream ownership is already mapped
- it forces the highest-risk write paths to be audited first
- it keeps release/inspection findings grounded in the ingest and assembly truth they depend on

## Required Audit Method Per Stage

Every stage audit must cover three layers.

### 1. Service layer

For each stage service:

- entrypoint shape (`RunOnce`, `RunOnceWithMetrics`, scheduler loop)
- batch/concurrency/backoff controls
- repo/store interface methods invoked
- current metrics emitted
- missing metrics needed to reason about throughput or contention

### 2. Store / DBO layer

For every hot store/repository method the stage uses:

- SQL shape
- transaction scope
- tables touched
- expected index path
- conflict/locking behavior
- whether it is:
  - canonical fact write
  - derived/materialized write
  - queue/runtime write
  - read-only operational query
  - cleanup/purge query

### 3. Schema / overlap layer

For each hot method, record:

- owning stage
- allowed overlapping stages
- forbidden overlapping stages
- runtime profile classification:
  - bootstrap-only
  - build/regroup
  - steady-state-safe
- whether it assumes a prior upstream stage has already completed

## Stage Audit Checklists

### Pass 1: Scrape ingest and checkpointing

Audit:

- provider validation and scrape-group selection
- latest-range selection
- backfill-range selection
- `InsertArticleHeaders`
- poster-batch writes
- `article_header_ingest_payloads` writes
- checkpoint updates, especially `UpsertBackfillCheckpoint`
- integrity preflight

Required outputs:

- exact list of scrape-owned tables and hot indexes
- conflict/write pattern for header ingest
- duplicate-resolution behavior
- whether any support-table writes should be further reduced or isolated
- final statement of which scrape queries are bootstrap-safe only versus steady-state-safe

### Pass 2: Assemble

Audit:

- header selection/claim path
- `UpsertBinaries`
- `UpsertBinaryParts`
- binary refresh/requeue behavior
- grouping evidence writes
- lane A vs lane B selector differences

Required outputs:

- exact difference in intent and query behavior between lane A and lane B
- hottest indexes on `article_headers`, `binaries`, and `binary_parts`
- remaining redundant lookups or write-backs
- whether scrape/assemble overlap is structurally unsafe or just operationally discouraged

### Pass 3: Recover yEnc

Audit:

- `BackfillYEncRecoveryWorkItems`
- `ListYEncRecoveryCandidates`
- hot queue vs seed/backfill path
- stale/backoff/noop handling
- persistence of recovered identity into `binaries` / `binary_parts`

Required outputs:

- exact distinction between queue-first and seed/backfill query paths
- overlap rules with assemble and release refresh
- whether candidate selection is inferential or fully materialized
- which joins are truly required and which are legacy/transitional

### Pass 4: Release summary refresh

Audit:

- queue claim/dequeue logic
- Phase A summary recompute
- Phase B ready-candidate materialization
- recovered-file-set follow-up work
- cleanup interactions with maintenance

Required outputs:

- exact DBO function list for Phase A and Phase B
- query shapes for:
  - key selection
  - family aggregate recompute
  - dominant row selection
  - ready-candidate sync
- overlap rules with `release`, `recover_yenc`, and maintenance
- explicit identification of batch-size sensitive vs scan-shape sensitive work

### Pass 5: Release formation

Audit:

- `ListReleaseCandidates`
- `ListBinariesForReleaseCandidate`
- title candidate reads
- `UpsertRelease`
- `ReplaceReleaseFiles`
- `ReplaceReleaseNewsgroups`
- ready-candidate ack behavior
- auxiliary sibling cleanup

Required outputs:

- exact split between candidate selection and release persistence
- proof of current cross-newsgroup release behavior in code
- confirmation that release is not writing upstream fact state for orchestration
- final overlap rules with refresh and inspect

### Pass 6: Inspect stages

Audit each inspection stage separately:

- `inspect_discovery`
- `inspect_par2`
- `inspect_nfo`
- `inspect_archive`
- `inspect_password`
- `inspect_media`

Required outputs:

- owned evidence/runtime tables per stage
- candidate listing and claim strategy
- whether any stage still writes fields that should belong to assemble/recover only
- overlap-safe vs overlap-risky inspection combinations

### Pass 7: Archive / NZB / purge tail

Audit:

- NZB generation reads/writes
- archive status writes
- purge selection and deletes
- transitional archive/NZB support surfaces

Required outputs:

- exact terminal cleanup contract
- confirmation that purge is the only intentional downstream mutator of upstream lineage
- list of transitional tables that can remain frozen for `0.8` versus ones still needing redesign

### Pass 8: Maintenance / runtime / admin

Audit:

- maintenance cleanup queries
- integrity tooling
- dashboard/stats queries
- runtime settings state and serve/scheduler support reads

Required outputs:

- which stats surfaces are misleading versus true operational queues
- which maintenance queries are safe but should be deferred under backlog
- final inputs for steady-state `serve` overlap policy

## Known Primary DBO Entry Points By Stage

These are the baseline methods the audit should start from. The audit may expand from here, but it should not skip these.

### Scrape

- `InsertArticleHeaders`
- `UpsertBackfillCheckpoint`
- scrape checkpoint update helpers in `repository.go`
- integrity guard path:
  - `CheckCriticalIndexerIntegrity`

### Assemble

- `UpsertBinaries`
- `UpsertBinaryParts`
- binary refresh/update helpers in `assembly_store.go`

### Recover yEnc

- `BackfillYEncRecoveryWorkItems`
- `ListYEncRecoveryCandidates`
- recovery result persistence methods in `yenc_recovery_store.go`

### Release summary refresh

- `RefreshQueuedReleaseFamilySummaries`
- `RefreshQueuedReleaseFamilySummariesWithMetrics`
- Phase A/Phase B helpers inside `release_family_summary_store.go`

### Release

- `ListReleaseCandidates`
- `ListBinariesForReleaseCandidate`
- `UpsertRelease`
- `ReplaceReleaseFiles`
- `ReplaceReleaseNewsgroups`

### Inspect

- `ListBinaryInspectionCandidates`
- `ListBinaryInspectionCandidatesWithOptions`
- `ClaimBinaryInspectionCandidates`
- stage-specific evidence upsert/update methods in `inspection_store.go`

### Archive / purge

- `MarkReleaseArchiveStored`
- `MarkReleaseArchiveFailed`
- `PurgeArchivedReleaseSources`

## Documentation Deliverables

### `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md`

Expand the living doc during the audit so each stage section includes:

- primary DBO/store functions
- tables and indexes touched
- allowed writes
- forbidden writes
- overlap policy
- runtime profile classification

Do not turn it into a file inventory. Name functions only where they are needed to pin down a critical hot path.

### Audit findings capture

For each pass, record:

- what was audited
- what is safe as-is
- what should change
- whether the issue is:
  - query shape
  - transaction scope
  - overlap policy
  - schema gap
  - observability gap

## Commit Strategy

Commit after each meaningful audit/documentation slice:

1. scrape audit + doc updates
2. assemble + recover_yenc audit + doc updates
3. release_summary_refresh + release audit + doc updates
4. inspect/archive/maintenance/runtime audit + doc updates

If a code change is required by the same audit finding, keep it separate from the doc-only commit unless the doc would be incorrect without the code.

## Acceptance Criteria

- every major stage has an explicit audit checklist
- the audit order is fixed and implementation-safe
- the living schema doc is the source of truth for stage overlap, ownership, and hot DBO entry points
- the next engineer can run the audit without re-deciding what order or depth to use
- the schema remains freeze-targeted, but not frozen, until all audit passes are complete and no unresolved structural gap remains
