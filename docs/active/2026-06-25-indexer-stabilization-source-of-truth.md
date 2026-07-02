# Indexer Stabilization Source Of Truth Sprint

This is the only active execution plan for the current indexer sprint. The
previous dated active plans were archived because they mixed incompatible
models: short work windows, daily buckets, yEnc admission, and partial
partitioning. Use those archived files only as historical context.

## Canonical Reference

`docs/wiki/indexer/` is the canonical long-lived reference for:

- stage ownership;
- table shapes and table ownership;
- allowed reads and writes;
- forbidden write-backs;
- query-shape policy;
- retention and purge policy;
- release formation data contracts.

If another document contradicts that reference, update or remove the
contradiction before changing code. Root-level `docs/INDEXER_*.md` files are
compatibility entry points and must link into the focused wiki instead of
carrying independent, stale pipeline details.

## Sprint Goals

1. Stop schema and query drift by making stage ownership enforceable in tests.
2. Fix assemble regressions caused by partition/retention work.
3. Complete the partial partitioning design for source/work retention.
4. Keep release formation correct while making retention drops cheap.
5. Keep yEnc recovery bounded and stage-owned.

## Non-Negotiable Ownership Rules

- `article_headers` is an immutable scrape-owned fact table. It is not an
  assemble claim, retry, progress, or completion table.
- `article_header_assembly_queue` is the assemble claim/progress surface.
  Assemble completes input work by deleting queue rows after writing
  `binary_parts`.
- Assemble owns binary creation/projection writes:
  `binary_core`, `binary_parts`, `binary_observation_stats`,
  `binary_identity_current`, `binary_completion_keys`, and related assemble
  projections.
- `recover_yenc` owns recovery work and recovered identity state:
  `yenc_recovery_work_items`, `binary_recovery_current`, recovered projection
  updates, superseded-source lineage, completion-key refresh, and release dirty
  queue enqueue.
- `release_summary_refresh` is the heavy writer for readiness summaries and
  ready candidates.
- Release formation writes release-owned catalog/lineage tables and must not
  lock or mutate assemble-owned binary rows as progress state.
- Inspect stages write only inspection-owned history/evidence/ready state.
- Source purge is the only intentional terminal mutator/deleter of upstream
  source lineage, and only after archive/catalog safety gates pass.

## Documentation Tasks

- Keep this file as the only `docs/active/*.md` sprint plan.
- Keep durable indexer documentation in `docs/wiki/indexer/` focused by topic:
  stage ownership, stage flow, schema/partitions, retention, release formation,
  and operations.
- Archive superseded active plans under
  `docs/archive/development/indexer/2026-06-25-superseded-active-plans/`.
- Update root docs so stale text cannot reintroduce old behavior:
  - remove claims that assemble filters or marks `article_headers.assembled_at`;
  - state that queue rows and `binary_parts` relationships are authoritative;
  - mark yEnc retry/backoff in ingest payloads as transitional debt only;
  - point readers to the canonical reference for ownership decisions.

## Query Guardrails

Add automated tests that fail when hot store code violates ownership rules:

- assemble store code must not contain `UPDATE article_headers`,
  `article_headers SET`, `assembled_at`, or `assembly_claimed_until`;
- assemble claim selection must use `article_header_assembly_queue` and
  `binary_completion_keys`, not article-header progress columns;
- binary part upsert must carry `source_posted_at` from the claimed queue/header
  and must delete completed assembly queue rows by
  `(source_posted_at, article_header_id)`;
- yEnc recovery must not write retry/progress state into `article_headers`;
- any remaining yEnc writes to `article_header_ingest_payloads` must be covered
  by a failing TODO test or disabled before this sprint is signed off;
- release formation must not mutate assemble-owned source/binary projections
  except documented release ack/queue state.

Any hot query changed in this sprint must have a focused test and a short
`EXPLAIN (ANALYZE, BUFFERS)` note recorded in the sprint signoff or PR notes.

## Assemble Stabilization Tasks

- Restore Lane A/B behavior using queue-owned state:
  - Lane A targets structured queue rows that can extend existing incomplete
    binaries via `binary_completion_keys`;
  - Lane B pulls recent general queue rows for fresh binary creation;
  - combined mode preserves configured Lane A target and Lane B minimum.
- Claim only queue keys inside the claim transaction.
- Hydrate article facts only after claiming exact
  `(source_posted_at, article_header_id)` keys.
- Remove or quarantine unused legacy helpers that still select from
  `article_headers.assembled_at`.
- Refresh binary stats with partition-key joins:
  `binary_parts.source_posted_at` plus `binary_parts.article_header_id`.
- Keep scrape blocked while the assemble queue is above configured backlog
  limits; do not let `scrape_latest` trickle into a saturated assemble queue.

## Partition And Retention Design

Use daily UTC range partitions keyed by `source_posted_at` only. Do not
subpartition by provider or newsgroup in this sprint. Provider/newsgroup
control belongs in indexed predicates, runtime group profiles, deferred ranges,
and explicit admin purge workflows.

Keep durable roots/catalog unpartitioned:

- `binary_core`
- `releases`
- `release_files`
- `release_catalog_files`
- `release_newsgroups`
- `release_archive_state`
- `release_archive_detail_*`
- `release_archive_lineage_*`
- `nzb_cache`
- enrichment and override tables

Complete partitioning for high-volume source/work/projection tables:

- source/header lineage: `article_headers`,
  `article_header_ingest_payloads`, `article_header_crosspost_groups`,
  `article_header_poster_refs`, `article_header_assembly_queue`,
  `poster_materialization_queue`;
- binary work/projection: `binary_parts`, `binary_observation_stats`,
  `binary_identity_current`, `binary_recovery_current`, `binary_lifecycle`,
  `binary_completion_keys`, `binary_grouping_evidence`,
  `binary_projection_events`, `binary_superseded_sources`;
- yEnc/inspect work and evidence: `yenc_recovery_work_items`,
  `binary_inspection_ready_queue`, `binary_inspections`,
  `binary_inspection_artifacts`, `binary_archive_entries`,
  `binary_text_evidence`, `binary_media_streams`, `binary_par2_sets`,
  `binary_par2_targets`;
- release-derived work: `release_family_readiness_summaries`,
  `release_ready_candidates`, `release_recovered_file_set_candidates`,
  `release_stage_dirty_families`.

Native partition conversion completed in this branch:

- fresh schema migration pre-creates a rolling partition horizon from
  `CURRENT_DATE - 21` through `CURRENT_DATE + 9`; scrape can still retry
  runtime creation for older gaps, but broader precreation must wait for
  partition-pruned query shapes or PostgreSQL lock tuning because a 60-day
  horizon caused `out of shared memory` under the current workload;
- binary projection writers use partition-key conflict targets for
  `binary_observation_stats`, `binary_identity_current`,
  `binary_recovery_current`, `binary_lifecycle`, `binary_completion_keys`,
  `binary_grouping_evidence`, `binary_projection_events`, and
  `binary_superseded_sources`;
- inspect work/evidence writers carry `source_posted_at` for
  `binary_inspection_ready_queue`, `binary_inspections`,
  `binary_inspection_artifacts`, `binary_archive_entries`,
  `binary_text_evidence`, `binary_media_streams`, `binary_par2_sets`, and
  `binary_par2_targets`;
- release-derived work writers use partition-key conflict targets for
  `release_family_readiness_summaries`, `release_ready_candidates`,
  `release_recovered_file_set_candidates`, and
  `release_stage_dirty_families`;
- migration `026_native_projection_work_partitions` rebuilds the high-volume
  projection/work tables as native daily partition parents on a fresh schema;
- parent indexes and foreign keys are restored on the converted partition
  parents;
- guardrail tests reject forbidden assemble/yEnc write-backs, partitioned
  source joins without `source_posted_at`, old partition-incompatible conflict
  targets in hot writer files, and partitioned inspection evidence inserts that
  omit `source_posted_at`;
- fresh migration smoke verified 28 target partition parents, non-null
  `source_posted_at` on those parents, restored parent indexes, and restored
  parent foreign keys.

Retention drop order:

1. ready/work queues;
2. inspect/yEnc evidence;
3. binary projections/work;
4. `binary_parts`;
5. article support rows;
6. `article_headers`;
7. prune unreferenced old `binary_core` roots after archive/catalog gates.

Retention must refuse to drop a day when there are running claims, active
source work, non-terminal release/archive dependencies, or default partition
rows that would make the drop incomplete.

## yEnc Recovery Boundary Tasks

- Keep yEnc admission limits and deferred range behavior from the existing
  runtime-settings implementation.
- `recover_yenc` selection is priority/time-bucket based, not FIFO. Keep
  `priority_rank = 0` reserved for work likely to unlock grouping or release
  formation, including suspicious near-time opaque singleton cohorts.
- Admit suspicious `opaque_subject_set` singleton cohorts as priority-0 work
  when at least 20 active one-part binaries share the same provider, newsgroup,
  and bounded near-time bucket. Configure the bucket with
  `indexing.recovery_admission.near_time_cohort_bucket_minutes`; the default is
  five minutes because upload speed, throttling, multi-connection posting, and
  provider acceptance order can spread a single large upload across seconds or
  minutes. Use `admission_reason = 'opaque_near_time_cohort'`. This is
  admission priority only; yEnc BODY evidence remains required before stronger
  grouping is trusted.
- Ensure work-item upserts are idempotent on partition-key-inclusive
  `binary_id` and `article_header_id` uniqueness.
- Move retry/backoff/progress state out of scrape-owned ingest payload rows
  into recovery-owned work/evidence state, or block signoff with an explicit
  regression test documenting the remaining debt.
- Maintain recovered identity merge semantics so recovered filenames merge
  fragments into file-level binaries instead of leaving one binary per article.
- Do not admit yEnc recovery just because a post is obfuscated. If the NNTP
  Subject already contains a stable filename/token, `[file_index/file_total]`,
  `(part/total)`, and file size, assemble should have enough HEAD evidence to
  create the file-level binary. yEnc recovery may validate later, but BODY
  `name=` must not override a stronger complete Subject identity when the BODY
  name is random.

### 2026-07-01 Opaque Burst Audit And Remediation

Live DB audit on July 1 showed the retained database had grown to roughly
58 GB while only 218 releases existed. Storage was dominated by source rows and
binary projections, not release catalog rows:

- `article_headers`: 11,769,815 rows;
- `binary_core` / `binary_identity_current`: 9,510,152 rows;
- `binary_parts`: 11,594,487 rows;
- `release_family_readiness_summaries`: 508,185 rows;
- `releases`: 218 rows.

The root bottleneck was upstream of release formation:

- 8,869,420 binaries were `opaque_set` / `opaque_subject_set` /
  `provisional` one-part singletons;
- 8,151,526 of those opaque singletons had no `yenc_recovery_work_items` row;
- 479,355 known-total binaries were incomplete, and 455,938 of those had only
  one observed part;
- median observed coverage for incomplete known-total binaries was 0.07%;
- release summary refresh was drained, so release formation was not waiting on
  summary backlog.

Representative proof from `alt.binaries.big`, minute
`2026-07-01 05:56-04`, showed 3,834 unqueued opaque singletons in one minute.
`articleprobe` on article `4779862558` found:

```text
=ybegin part=176 total=732 line=128 size=524288000 name=4PFvyhnKCjkkVMQ9b.part04.rar
=ypart begin=125440001 end=126156800
```

Adjacent sampled articles in the same burst had the same yEnc `name=` and
`total=732` with different `part=` values. This proves the burst was a real
multi-part archive file stranded as thousands of single-binary families. It
also proves near-time/article-number hints are admission evidence only: sampled
parts were not monotonic enough to assemble without BODY authority.

Root causes identified:

1. Priority opaque cohort admission only scanned a small `updated_at`-ordered
   window from `binary_observation_stats`. Older or high-volume posted-time
   bursts could be missed even when they were dense and recoverable.
2. yEnc work seeding only ran when the generic ready queue was empty. A large
   priority-1 backlog prevented new priority-0 suspicious cohorts from being
   admitted.
3. Opportunistic promotion was gated by a 5,000 open-item watermark, so it was
   effectively disabled while normal recovery backlog was healthy.
4. After a BODY probe proved a burst had authoritative yEnc identity, the
   system did not immediately admit sibling opaque singletons from that same
   near-time cohort.

Required fixes for signoff:

- [x] keep the policy that complete Subject multipart posts do not need yEnc for
  initial grouping;
- [x] refill priority-0 yEnc work from suspicious opaque near-time cohorts even
  when lower-priority ready backlog is nonzero;
- [x] select opaque cohort admission by posted-time/source-time evidence instead
  of only latest `updated_at`;
- [x] when yEnc recovery succeeds for an opaque singleton, admit same-cohort
  siblings as priority-0 work so the pipeline follows proven bursts instead of
  sampling unrelated singletons;
- [x] preserve hard caps: priority-0 may use the configured overflow cap, but
  generic priority-1/2 admission must still stop at normal hard cap;
- [x] continue requiring yEnc BODY evidence before promoting fully randomized
  bursts into real file-level binaries.

Implementation notes:

- migration `042_yenc_opaque_cohort_posted_admission_index` adds a posted-time
  partial index for one-part observation rows used by opaque cohort admission;
- priority-0 cohort refill now runs whenever ready priority-0 work is below
  the recovery batch target, independent of lower-priority ready backlog;
- the refill scan uses posted/source time ordering and a larger bounded scan
  window so dense upload bursts are visible;
- successful streaming yEnc recovery writes now best-effort admit sibling
  opaque singletons from the same near-time cohort before applying the BODY
  evidence batch;
- a store regression test asserts priority opaque cohort work is seeded even
  when generic yEnc ready work already exists.

Live validation after implementation:

- database size was `59 GB`;
- `8,151,526` opaque one-part singleton binaries still had no yEnc work item
  before priority refill caught up;
- the largest unqueued one-minute singleton cohorts observed were
  `alt.binaries.multimedia` with `12,348` and `10,146` singles,
  `alt.binaries.bloaf` with `9,845`, and several `alt.binaries.big` /
  `alt.binaries.cores` cohorts above `6,000`;
- `EXPLAIN (ANALYZE, BUFFERS)` for a 5,000-row priority refill over a
  250,000-row posted-time scan completed in `10.898 s`, under the local
  15-second statement timeout, using the posted-time observation index for the
  candidate slice;
- live serve run refilled priority-0 opaque cohort work while priority-1 work
  remained ready: `5,548` ready `opaque_near_time_cohort` rows were present
  alongside `237,846` ready `bounded_admission` rows;
- first observed full recovery batch after the change recovered `5,000/5,000`
  and merged `3,188`;
- next observed full batch recovered `4,999/5,000` and merged `3,261`;
- release summary refresh consumed the resulting dirty-family backlog in
  small batches, and release formation began seeing candidates again.

Remaining follow-up:

- the priority refill query is acceptable but not cheap; if it becomes a top
  CPU/IO cost, reduce the join fanout by materializing an assemble-owned
  `opaque_singleton_cohort_queue` keyed by provider, newsgroup, posted bucket,
  and source partition;
- scrape is now correctly gated by the yEnc hard cap, but assemble still shows
  periodic 20-30 second runs when scrape fills the assembly queue. Keep the
  existing assemble query-shape investigation open separately from this yEnc
  admission fix;
- inspect_par2 repeatedly reports prefix samples starting after offset `0` for
  some volume files. That is an inspect candidate/mode issue, not the opaque
  yEnc admission bottleneck fixed here.

## Binary Grouping Evidence Tasks

Canonical reference:
`docs/wiki/indexer/binary-grouping-evidence.md`.

Implement evidence priority in assemble/match before this sprint is signed off:

1. Subject multipart identity first:
   - parse Subject shapes like
     `[1/8] - "name.ext" yEnc (7152/28465) 20403308372`;
   - group by provider, newsgroup, normalized Subject filename/token, file
     index, file total, article total, and file size;
   - use `(part/total)` as the article index and total for binary parts;
   - do not let randomized `From`, poster suffix, Message-ID suffix, or
     contextual fallback keys split this class into singleton binaries.
2. Canonical obfuscated Subject:
   - treat a stable random Subject filename/token as strong evidence when it
     carries complete multipart coordinates;
   - classify as `subject_multipart_obfuscated` rather than weak contextual
     fallback;
   - skip yEnc recovery admission unless validation or missing metadata is
     needed.
3. yEnc recovery:
   - use recovered BODY identity as authority only when HEAD evidence is
     incomplete, ambiguous, or randomized without stable Subject coordinates;
   - if BODY `name=` is random but `part`, `total`, and `size` match a complete
     Subject identity, persist BODY name as lower-priority evidence and keep
     the Subject grouping.
4. Weak and random cohorts:
   - use provider/newsgroup, timestamp, byte/line, article-number, and
     message/poster suffix hints only to prioritize probes;
   - seed yEnc work for high-pressure opaque near-time singleton cohorts even
     when lower-priority yEnc backlog is already large;
   - keep unresolved fully random cohorts weak/provisional and prune by
     retention policy after the investigation window.

## Binary Projection Repair Tasks

- `binary_observation_stats.observed_parts` must reflect the authoritative
  `binary_parts` relationship. Admin/API pages must not need to live-count
  `binary_parts` for routine display.
- Add bounded maintenance repair for active binaries where
  `COUNT(binary_parts) > binary_observation_stats.observed_parts`.
- Keep the repair assemble-owned and partition-key aware; do not mutate
  scrape-owned source facts or use API-side live aggregates as the normal fix.

### Cross-Second Binary Part Span Fix

Observed regression:

- complete multipart binaries can span many `source_posted_at` seconds while
  `binary_core.source_posted_at` stores only the binary root/projection key;
- catalog/materialization reads used
  `binary_parts.source_posted_at = binary_core.source_posted_at`, which read
  only one source-time slice of a complete binary;
- `inspect_media` then materialized sparse partial payload files with zeroes
  at offset 0 and ffprobe failed with invalid EBML/MKV header errors;
- `release_catalog_files.article_count` reported the one-slice count while
  `observed_parts` came from full binary stats, making the admin/API payload
  details contradictory.

Implementation tasks:

1. [x] Add binary-owned part span metadata to `binary_observation_stats`:
   `part_source_posted_at_min` and `part_source_posted_at_max`.
2. [x] Populate the span in assemble binary stats refresh from the actual
   `binary_parts.source_posted_at` min/max. Refresh may use a bounded
   root-day-adjacent partition window to discover the span, but must not scan
   every child partition for routine batches.
3. [x] Keep `binary_core.source_posted_at` as the projection/root partition key.
   Do not treat it as proof that every `binary_parts` row lives in the same
   source second.
4. [x] Change catalog/materialization reads to query `binary_parts` with both:
   `binary_id = ?` and
   `source_posted_at BETWEEN part_source_posted_at_min AND
   part_source_posted_at_max`, falling back to the root day only when span
   metadata is absent.
   Catalog/materialization and admin detail reads first load the binary span,
   then issue a second bounded partition query with span timestamps as query
   parameters so PostgreSQL can prune daily children.
5. [x] Change `release_catalog_files` sync to count all parts inside the recorded
   span so `article_count`, `observed_parts`, and materialization evidence
   agree.
6. [x] Add a partition-friendly index for cross-second binary part reads:
   `(binary_id, source_posted_at, part_number)`.
7. [x] Make media/archive materialization fail or remain retryable when decoded
   bytes are materially below the expected binary size. Failed ffprobe on a
   direct media payload must not be stored as a successful completed media
   inspection that improves release metadata.
8. [x] Add regression tests for a release file whose binary parts span multiple
   `source_posted_at` seconds. The catalog article read must return all parts,
   not only the `binary_core.source_posted_at` slice.

Signoff evidence:

- [x] `3FgriKwJ1pbOruw8RStUodgaoqV` / binary `2741796` style payloads read all
  `2651` parts through the catalog/materialization path. Live EXPLAIN of the
  bounded span query returned `actual rows=2651`.
- [x] `3FgrioDXoJfL4gLre11NBBDB3Yb` / binary `2990313` style payloads read all
  `1211` parts through the catalog/materialization path. Live EXPLAIN of the
  exact-span query returned `actual rows=1211`, used only
  `binary_parts_20260626` and `article_headers_20260626`, and ran in
  `2.708 ms`.
- [x] `EXPLAIN (ANALYZE, BUFFERS)` for the catalog article read shows daily
  partition pruning through the span predicate rather than appending every
  `binary_parts` child. With literal/parameter span bounds for binary
  `2741796`, PostgreSQL used only `binary_parts_20260626` and
  `article_headers_20260626`; planning time was `2.408 ms`, execution time was
  `6.010 ms`.
- [x] media inspection does not mark partial direct payload materialization or
  failed ffprobe as a successful completed probe. Tests cover incomplete
  materialization and direct ffprobe failure.
- [x] direct `inspect_media` for media payloads no longer materializes the full
  payload. Live reinspection of `3FgriKwJ1pbOruw8RStUodgaoqV` / binary
  `2741796` completed with `probe_mode=ffprobe_direct_prefix`,
  `materialized_bytes=8388608`, resolution/audio/video metadata populated, and
  no 2 GB sparse file materialization.
- [x] direct media prefix fetch is bounded by `ToolTimeout`; stalled catalog or
  NNTP prefix work records a failed/retryable inspection instead of holding a
  long stage lease.
- [x] stale live catalog rows can be repaired by normal maintenance, not only
  by release re-formation. `BackfillMissingReleaseCatalogFiles` now refreshes
  releases whose catalog counts are lower than binary observation stats.
- [x] `release_catalog_files` sync and catalog read fallbacks no longer group
  all `binary_parts` rows to infer poster metadata. They use indexed
  `(binary_id, source_posted_at, part_number)` lookups and first-part metadata
  sampling, which removed multi-minute catalog refresh/read stalls observed
  during media inspection soak.
- [x] completed media inspections with `probe_skip_reason = ffprobe_failed`
  are eligible for media reinspection. The live bad inspection rows for
  `3FgriKwJ1pbOruw8RStUodgaoqV` were removed so inspect stages can recreate
  them with the corrected materialization path.
- [x] admin binary diagnostics are split into a dedicated binary detail route:
  `/admin/indexer/binaries/:id`. Release detail links to that page instead of
  embedding source article rows inline.

Validation run:

- `go test ./internal/store/pgindex`
- `go test ./internal/indexing/inspect/media ./internal/indexing/release`
- `go test ./internal/store/pgindex ./internal/indexing/inspect/...`
- `npm run build` in `ui/`
- Live API check for binary `2741796`: `observed_parts=2651`,
  `total_parts=2651`, `parts_len=2651`.
- Live admin release check for `3FgriKwJ1pbOruw8RStUodgaoqV`: MKV
  `article_count=2651`.

Regression fixture to add:

- `rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv` in
  `alt.binaries.newznzb.bravo`:
  - Subject supplies `[1/8]`, the stable obfuscated filename,
    `(7152/28465)`, and size `20403308372`;
  - sample BODY supplies matching `part=7152 total=28465 size=20403308372`
    but random `name=976e18143f3a00cdd333a41017886215c57ca1653d5bfcf6`;
  - current live data showed 4,882 distinct part numbers split across 4,882
    singleton weak binaries because contextual fallback included randomized
    poster/message-id context;
  - expected result is one file-level binary keyed by the Subject identity,
    with observed parts merged by Subject `(part/total)`.

## Grouped yEnc Evidence Investigation

The archived ChatGPT Pro handoff is still correct that adversarially obfuscated
headers cannot be treated as final grouping proof without yEnc BODY evidence.
However, complete Subject multipart evidence is stronger than randomized BODY
`name=` evidence. Header-level weak patterns may be used to prioritize and
reduce recovery probes only after complete Subject evidence is absent.

### Speculative Weak Binary Grouping TODO

Investigate candidate binaries from weak header evidence when articles share:

- same provider/newsgroup, because article numbers are provider-local even when
  Message-ID is federated;
- `Date` normalized to UTC within 1-2 seconds;
- similar `Message-ID` suffix or `From`/poster suffix when present;
- similar byte and line counts;
- nearby provider article numbers;
- subject and Message-ID shape consistent with ngPost-style UUID
  obfuscation.

Article-number order is only a hypothesis. Posting tools can use multiple
connections, and server article numbers reflect acceptance order. yEnc evidence
remains the authority.

Sample yEnc evidence from representative positions:

- first article;
- roughly 10%;
- middle;
- roughly 90%;
- last article;
- a few random articles for large cohorts.

Use this probe budget until measured evidence says otherwise:

- candidate size under 20 articles: probe all or most articles;
- 20-200 articles: probe 5-8 samples;
- 200-1000 articles: probe 8-16 samples;
- 1000+ articles: probe 16-32 samples.

Promote a candidate to `grouping_method = weak_header_sampled_yenc` only when:

- all sampled yEnc `name=` values match;
- all sampled yEnc `total=` values match;
- sampled `part=` values are mostly monotonic with article-number order;
- sampled `=ypart begin=` and `end=` values roughly align with expected part
  offsets and article sizes;
- bytes/lines are consistent;
- no major article-number gaps are unexplained.

Fall back to full recovery when:

- different yEnc names appear inside the same candidate;
- sampled totals differ;
- sampled part numbers jump backward unexpectedly;
- the same time/suffix bucket contains multiple interleaved binaries;
- the candidate contains mixed extensions or mixed totals;
- confidence is below threshold.

Signoff requires a probe report from live data before release grouping can trust
weak sampled cohorts. The report must compare near-time upload timing,
provider article order, Message-ID/poster suffixes, byte/line consistency, and
sampled yEnc file/part evidence for both formed binaries and weak/stale
binaries. Until that report passes, recovery admission may prioritize likely
cohorts, but release/binary grouping must still be backed by recovered yEnc or
existing strong header evidence.

## Stability TODOs And Signoff Gates

### Deadlock Root Cause

Recent soak runs still showed lock failures. Retrying deadlocks is not signoff.
Before this sprint closes, identify the lock root cause and prove the hot path
is stable.

Observed failures to investigate:

- `assemble` deadlock while hydrating claimed assembly candidates;
- `scrape_latest` deadlock while creating an older source partition for a stale
  group gap;
- earlier `recover_yenc` deadlock while refreshing binary stats;
- PostgreSQL `out of shared memory` during broad partition precreation.

Required work:

- capture PostgreSQL deadlock details with relation names and SQL statements
  from server logs, `log_lock_waits`, `deadlock_timeout`, or live `pg_locks`
  during soak;
- document which store query owns each relation it writes;
- make every hot transaction acquire locks in a stable order;
- keep stages from mutating upstream/source-owned rows as progress state;
- verify partition creation cannot race hot writers in a way that deadlocks
  normal stage work.

Signoff requires a clean 30-minute serve soak with zero `40P01` deadlocks, zero
`53200 out of shared memory` errors, no stage writing non-owned rows, and short
`EXPLAIN (ANALYZE, BUFFERS)` notes for assemble claim hydration, binary stats
refresh, yEnc apply/refresh, and scrape insert batches. Runtime scrape must not
perform partition DDL while hot readers/writers are active.

### Partition Horizon

The failed 60-day precreation attempt meant roughly one daily child partition
per partitioned parent table per day. With 28 partitioned target tables, a
70-day rolling range (`CURRENT_DATE - 60` through `CURRENT_DATE + 9`) implies
about 1,960 daily child partitions before indexes and metadata. Under current
query shapes and runtime DDL, that exceeded PostgreSQL shared lock memory.

Current code intentionally precreates only `CURRENT_DATE - 21` through
`CURRENT_DATE + 9`. That is about 31 daily children per partitioned table, or
about 868 daily child partitions for the current 28 target parents.

Required work:

- keep the narrow horizon until hot queries are partition-pruned by
  `source_posted_at`;
- keep older source partition creation out of scrape hot paths; unexpected
  older source dates should land in default partitions and be surfaced by
  default-partition metrics until controlled maintenance handles them;
- decide whether PostgreSQL lock memory tuning is needed before any broader
  horizon is restored;
- record partition counts, default-partition row counts, and retention dry-run
  output after a fresh schema bootstrap.

Signoff requires fresh migrations, older-gap scrape without runtime partition
DDL/deadlock, retention dry-run, default-partition row counts, and no
shared-memory failures during soak.

### Hot/Warm/Cold And Latest/Backfill Policy

The intended policy is:

- latest scrape keeps configured hot/fresh groups moving toward the provider
  high-water mark;
- backfill walks older ranges backward while downstream hard caps allow room;
- soft yEnc pressure blocks backfill first;
- yEnc hard cap blocks both latest and backfill;
- assemble high-water blocks both latest and backfill until queue depth falls
  below the resume threshold;
- hot groups get freshness priority and larger recovery budget;
- warm groups run while queue depth and recovery lag are healthy;
- cold groups are sampled/deferred and must not starve hot work;
- if the service is down, the gap between the last fresh checkpoint and the
  current provider head should become prioritized gap/backfill work, while
  latest should not indefinitely spend the live lane on stale historical ranges.

Current code evidence:

- `scrape_latest` advances from `scrape_checkpoints.last_article_number + 1`
  toward the provider high-water article number, or starts one batch behind
  head on cold start;
- `scrape_backfill` walks downward from the backfill cursor, or just behind the
  latest cursor when no backfill cursor exists;
- the backlog guard blocks backfill at yEnc soft cap, blocks both scrape lanes
  at yEnc hard cap, and blocks both scrape lanes above the assemble high-water;
- the NNTP traffic guard gives backfill lower priority than latest freshness
  and yEnc recovery;
- group profiles currently default configured scrape work to `warm`; hot/cold
  behavior must be proven by runtime configuration/profile data, not assumed.

Required validation:

- prove latest does not turn multi-day downtime gaps into unbounded stale
  latest work;
- prove old/stale provider-head groups are routed to gap/backfill/deferred work
  or tiered cold instead of occupying the live lane;
- expose or record provider, newsgroup, tier, latest cursor article/date,
  backfill cursor article/date, observed daily boundaries, deferred ranges, and
  selected scrape lane;
- test that cold work cannot starve hot latest/recovery work;
- test that backfill resumes only when yEnc and assemble queues are below their
  configured resume thresholds.

Signoff requires live evidence for at least one hot group, one warm group, and
one cold/deferred group, plus a restart-gap scenario showing the gap lane and
latest lane behave as intended.

## Soak And Signoff Tasks

Before signoff:

- wipe the old local database and run fresh migrations through `gonzb run
  serve`;
- confirm scrape latest feeds current hot groups and backfill runs only while
  downstream hard caps allow room;
- confirm hot/warm/cold group tiering affects scrape/recovery admission and
  cold work does not dominate hot freshness;
- confirm assemble Lane A and Lane B both claim from
  `article_header_assembly_queue`, hydrate exact
  `(source_posted_at, article_header_id)` facts, write only assemble-owned
  binary tables, and delete completed queue rows;
- confirm yEnc recovery consumes `yenc_recovery_work_items`, writes
  recovery-owned projection/evidence, and does not write progress into scrape
  tables;
- confirm release summary refresh and release formation produce releases from
  newly assembled/recovered work;
- run partition retention in dry-run mode and verify blocker reporting and
  drop order use partition metadata instead of broad unbounded source scans;
- validate the latest/backfill restart-gap behavior and record whether stale
  gaps are handled by prioritized gap/backfill work instead of occupying the
  live latest lane indefinitely;
- record partition horizon counts, default-partition row counts, and confirm
  scrape did not perform runtime partition creation during the soak;
- collect deadlock evidence when any lock failure occurs; if a deadlock occurs,
  the sprint is not signed off until the exact relation/query lock cycle is
  documented and fixed;
- sample grouped yEnc evidence from live weak and formed binaries and record
  whether near-time timing, Message-ID/poster suffix, article order, and
  sampled yEnc parts support speculative grouping;
- collect 30 minutes of stage-run, backlog, gate, release, and yEnc throughput
  metrics.

## Current Execution Notes

Branch: `sprint/yenc-retention-throughput-v1`.

Completed in this sprint branch:

- latest scrape now detects stale provider-head gaps, records
  `deferred_article_ranges` with reason `latest_gap`, advances the live lane to
  a head batch, and lets backfill drain the gap when capacity allows;
- `articleprobe --probe-set-yenc-sampled` records sampled yEnc identity,
  total-part, monotonicity, and ypart-offset evidence for speculative grouping
  investigation;
- assemble queue claims now commit before candidate hydration, reducing claim
  lock duration;
- scrape no longer performs runtime source/work partition DDL in hot insert
  paths; unexpected older posted dates land in default partitions for
  maintenance visibility;
- release catalog sync joins partitioned binary/source tables with
  `source_posted_at`;
- admin catalog, binary detail, inspection detail, and default binary listing
  reads now join partitioned tables with `source_posted_at`;
- default admin binary listing has a bounded recent fast path so opening the
  binary workbench does not sort/count the full partitioned projection set.
- default combined assemble claims now use queue-kind lane prioritization:
  structured queue rows first, then general rows. Explicit Lane A still uses
  completion-key probes, but the default mixed lane no longer probes
  `binary_completion_keys` for every general row.
- assemble now records split queue timing for cleanup, claim, and hydration so
  the stage log no longer hides cleanup cost inside `candidate_selection_ms`;
- stale assembly queue cleanup now carries `(source_posted_at,
  article_header_id)` into the `binary_parts` existence probe and queue delete,
  preserving partition-key pruning and avoiding broad `article_header_id`-only
  cleanup probes;
- weak/provisional unformed binary workbench reads now use an identity-first
  fast path: page from `binary_identity_current`, anti-join release files by
  `binary_id`, then hydrate only the selected page.

Live evidence already collected:

- fresh schema migration created the expected 28 native partition parents with
  a narrow rolling partition horizon;
- retention dry-run reported 28 native target tables, 392 eligible old
  partitions across 14 days, no blockers, and warned about the native
  source/work/projection set and current horizon;
- raw-stage retention dry-run reported tier-aware raw retention:
  hot 48h, warm 24h, cold 12h, terminal yEnc 24h, stale probes 48h;
- runtime partition DDL deadlock was reproduced from scrape partition creation
  under load and fixed by removing runtime partition DDL from scrape;
- an older-date scrape for `2026-05-14` completed after the fix by routing rows
  to default partitions instead of creating child partitions in the hot path;
- release formation succeeded after release catalog partition-key joins:
  six complete PAR2-backed releases were created from the fresh database before
  the current soak restart;
- sampled yEnc probe on binary `41032` found one sampled yEnc name and one
  sampled total, valid ypart offsets, and non-monotonic article-number order,
  supporting the policy that article number order is only a hypothesis and
  yEnc remains authoritative;
- final patched API checks at `2026-06-25 15:07-04`: default binaries endpoint
  returned HTTP 200 in 252ms, daily buckets returned HTTP 200 in 5ms;
- after default combined assemble claim fixes, current serve boundary is
  `2026-06-25 15:20:31-04`; backfill was gated by yEnc soft capacity while
  latest immediately detected provider-head gaps and inserted live head
  batches;
- current serve evidence at `2026-06-25 15:23-04`: two assemble batches
  completed without timeout/deadlock, candidate selection dropped from timeout
  failure to about 7s then 4.2s, yEnc recovery was running at concurrency 100,
  and default binaries/daily-buckets APIs remained bounded.
- assemble selection root cause identified at `2026-06-25 15:55-04`: the claim
  query itself was fast (`assembly_claim_ms=66.91`) and hydration was modest
  (`assembly_hydration_ms=1186.61`); the old 30s
  `candidate_selection_ms` was dominated by stale queue cleanup
  (`queue_cleanup_ms=29699.01`);
- after the partition-key cleanup fix, live assemble runs dropped stale cleanup
  to about 90-350ms; examples include `queue_cleanup_ms=102.53`,
  `assembly_claim_ms=133.51`, `assembly_hydration_ms=1379.67` at
  `2026-06-25 15:58-04`, and `queue_cleanup_ms=350.97`,
  `assembly_claim_ms=610.27`, `assembly_hydration_ms=2769.56` for an 8,125-row
  batch at `2026-06-25 16:02-04`;
- weak/unformed binary workbench endpoint improved from the observed
  27-second request to 3.62s after page hydration moved behind the filter, then
  to HTTP 200 in 280ms after identity-first paging. Supporting
  `EXPLAIN (ANALYZE, BUFFERS)` notes: weak count over about 158k rows ran in
  about 704ms, and the identity-first page seed used the
  `binary_identity_current_*_strength_updated_idx` path in about 1.46ms.
- hot query regression patch at `2026-06-26 14:20-04`:
  - yEnc candidate selection removed the self-join against
    `yenc_recovery_work_items`; the selector now locks a bounded ready window,
    computes near-time grouping hints from that window, and orders the selected
    work without rejoining the parent table. Read-only audit timing improved
    from about 58.8s on a hot bucket to about 728ms for the patched selector;
  - assemble hydration now uses bounded partition-key lateral lookups into
    `article_headers`, `article_header_ingest_payloads`, and
    `article_header_poster_refs` after claiming exact queue keys. Read-only
    audit timing improved from about 20.3s to about 425ms for a 2,500-row
    hydration sample;
  - release summary refresh now splits release-family and base-stem summary
    keys and uses materialized requested-key sets plus keyed lateral lookups
    into `binary_identity_current`. The duplicate refresh queue was deduped and
    protected with a unique key on `(provider_id, newsgroup_id, key_kind,
    family_key)`;
  - recovered-file-set impact discovery now uses requested-key lateral lookups
    and partition-key `EXISTS` checks instead of broad joins through
    `binary_identity_current`, `binary_observation_stats`, and
    `binary_recovery_current`;
  - subject multipart regroup now seeds from `binary_identity_current` with a
    supporting partial lookup index and is cadence-limited to once every
    15 minutes per assemble service instance, so routine assemble batches do
    not pay the full regroup tax.
- live patched serve evidence at `2026-06-26 14:32-14:35-04`:
  - yEnc recovery selected full 5,000-item batches and ran at concurrency 100;
    by `14:35:16` it had attempted 3,100 items, recovered 2,500, merged 1,159,
    and continued to be limited mostly by NNTP prefix fetch timeouts rather than
    SQL candidate selection;
  - release summary refresh drained repeated 1,000-key hot batches. After a
    stale pre-patch PostgreSQL backend was terminated, summary aggregate and
    dominant work stayed about 1.1-3.7s per batch, ready sync about 0.3-1.0s,
    and recovered-file-set sync commonly about 0.4-1.2s with occasional
    4-10s batches;
  - scrape latest/backfill ran when downstream capacity allowed, detected
    provider-head gaps, and then gated again once assemble backlog exceeded the
    configured high-water mark (`unassembled_headers=112723`,
    `high_water=100000`);
  - assemble completed 20,000-row batches without deadlock. Recurring
    post-patch examples include `queue_cleanup_ms=3731.01`,
    `assembly_claim_ms=3027.13`, `assembly_hydration_ms=19510.40`,
    `binary_upsert_query_ms=45458.55`, and
    `binary_refresh_stats_update_ms=24334.56` on a 9,558-binary refresh-heavy
    batch. This is no longer the earlier yEnc selector regression, but assemble
    write/refresh cost remains a follow-up optimization target.
- focused validation after the hot query regression patch:
  `go test ./internal/store/pgindex ./internal/indexing/assemble ./internal/indexing/release`
  passed, and `git diff --check` passed.
- release summary refresh regression follow-up at `2026-06-26 15:00-04`:
  - the database aggregate path was still capable of 10,000-key work. A
    read-only `EXPLAIN (ANALYZE, BUFFERS)` of the 10,000-key release-family
    aggregate completed in about 1.62s;
  - the observed 1,000-key behavior came from accumulated code caps:
    `releaseSummaryRefreshTimedBatchCap = 1000`, store hot/cold caps of 1,000,
    and a 100-key query chunk cap. Those caps were removed or restored to
    10,000/5,000 query chunks;
  - a separate dequeue regression came from joining
    `release_family_summary_refresh_queue` to partitioned
    `release_family_readiness_summaries` without a provider/key-first lookup
    index. The slow hot-queue EXPLAIN took about 28.5s and the stale-key
    branch took about 23.7s because each queue key probed every daily
    partition. Migration `034_release_summary_partition_lookup_indexes` adds
    `(provider_id, newsgroup_id, key_kind, family_key, readiness_bucket,
    source_posted_at)`, reducing the same hot-queue check to about 55ms and the
    stale-key branch to about 68ms;
  - live post-index refresh runs drained hundreds of keys with dequeue commonly
    about 30-745ms. Examples include `refreshed=702`,
    `dequeue_duration_ms=359.74`, `refresh_duration_ms=3418.40` and
    `refreshed=750`, `dequeue_duration_ms=267.95`,
    `refresh_duration_ms=1726.86`.
- assemble write/refresh follow-up at `2026-06-26 15:07-04`:
  - binary stats refresh had regressed because the partitioned
    `UPDATE binary_observation_stats ... FROM agg` and identity readback
    scanned broad daily partitions. A read-only EXPLAIN of the old shape took
    about 3.66s for about 6,100 binaries and scanned millions of rows across
    `binary_observation_stats`/`binary_identity_current`; a day-bounded variant
    still scanned the whole 2026-06-26 child and took about 2.26s;
  - stats refresh now groups requested binaries by UTC `source_posted_at` day,
    uses partition bounds, upserts only requested observation rows, and reads
    identity with keyed lateral lookups. The equivalent EXPLAIN dropped to
    about 950ms and live `binary_refresh_stats_update_ms` dropped from the
    earlier 24.3s sample to about 1.0-4.1s depending on refreshed binary count;
  - `binary_completion_keys` cleanup inside binary upsert now deletes by
    `(source_posted_at, binary_id)` instead of `binary_id` alone, preserving
    partition pruning;
  - the default runtime assemble `binary_upsert_db_chunk_size` was raised from
    250 to 1,000 and the live runtime setting was patched through
    `/api/v1/admin/settings`. Post-update assemble used 18 chunks for 17,437
    unique binary upserts with zero retry/deadlock/serialization events;
  - binary upsert remains workload-sensitive on batches with many new binary
    roots. Post-update samples used 18 chunks for 17,437-17,773 unique binary
    upserts with zero retry/deadlock/serialization events. The first sample
    had `binary_upsert_query_ms=30744.27`; the next comparable sample improved
    to `binary_upsert_query_ms=21012.41`,
    `binary_refresh_stats_update_ms=2573.53`, and
    `queue_cleanup_ms=1095.46`. This is improved per binary versus the earlier
    20,000-new-binary, 80-chunk sample, but binary upsert remains the next
    optimization target if assemble drain rate is still insufficient.
- binary grouping policy implementation check:
  - `docs/wiki/indexer/binary-grouping-evidence.md` records the evidence
    priority: complete Subject multipart identity first, canonical obfuscated
    Subject before random poster/message-id context, recovered yEnc only when
    HEAD evidence is incomplete/ambiguous, and weak cohorts as prioritization
    hints only;
  - existing matcher/assemble tests cover the reference
    `subject_multipart_obfuscated` case so randomized poster context does not
    split complete Subject multipart binaries;
  - yEnc admission remains priority/rank based rather than FIFO, and the
    near-time opaque singleton bucket is runtime-configurable. Live recovery
    continued to claim full 5,000-item batches at concurrency 100 while ready
    work was available.
- Release formation singleton-payload guard at `2026-06-26`:
  - `docs/wiki/indexer/release-formation.md` now states that sidecars can
    support an authoritative main payload but must not promote a one-article
    payload into a release;
  - `internal/indexing/release` now requires the dominant main payload to carry
    authoritative multipart evidence (`total_parts > 1` and complete) before
    standalone or auxiliary-backed single-main release formation is allowed;
  - regression coverage verifies that a one-article MKV plus PAR2 sidecars is
    skipped, while a complete multipart MKV with PAR2 sidecars can still form an
    internal release.

Known open signoff items:

- review and approve the updated binary grouping and release formation wiki
  policies for the remaining grouping/regrouping work:
  - `docs/wiki/indexer/binary-grouping-evidence.md` now states that
    `source_posted_at` is not identity and strong filename/family/part evidence
    may merge binaries across source-time and partition boundaries;
  - `docs/wiki/indexer/release-formation.md` now separates binary
    completeness, payload completeness, release completeness, public readiness,
    and internal release formation;
  - after approval, implement assemble regrouping so same filename plus same
    family plus compatible Subject/yEnc part metadata forms one binary even
    when source rows span minutes or hours;
  - add regression coverage for the observed split-PAR2 case where the main
    payload is complete but auxiliary PAR2 volume parts are split into many
    one-part binary rows;
- tune assemble query shape before signoff. Current live evidence showed
  20,000-header batches taking 20-60s on high-cardinality batches, mostly in
  candidate selection, header matching, and binary upsert work. Target follow-up
  is bounded candidate selection, partition-keyed lookups, and cross-time
  completion-key joins that do not broaden into unbounded source scans;
- complete a clean 30-minute soak from the final patched serve boundary with
  zero `40P01`, zero `53200`, and no PostgreSQL backend crash;
- record final stage-run counts, yEnc throughput, release counts, deferred gap
  counts, daily bucket stats, and default partition row counts after that soak;
- prove hot/warm/cold tier behavior with at least one configured or observed
  group per tier; current configured scrape work is mostly `warm`;
- collect short `EXPLAIN (ANALYZE, BUFFERS)` notes for any additional hot query
  shape changed after the `2026-06-26` regression patch;
- continue assemble write-path tuning. The current evidence points at binary
  upsert cost on large, high-cardinality batches rather than the yEnc selector
  self-join regression or binary stats refresh.
- Assemble insert-heavy audit at `2026-06-26 23:00-23:17`:
  - read-only selector/hydration EXPLAIN showed the combined lane selector at
    ~189 ms and 2,500-row hydration at ~109 ms when isolated;
  - staged completion-key sync was the insert-heavy regression: the old shape
    scanned/hashed ~436k incomplete `binary_observation_stats` rows for a
    5,000-binary staged batch and took ~5,087 ms; the partition-keyed staged
    lookup shape took ~243 ms for the same sample;
  - live assemble improved from ~5 minutes for 20,000 headers to 99s, 106s,
    97s, 40s, 30s, then 6.7s as insert-heavy backlog drained;
  - `binary_core` had a redundant non-unique index
    `idx_binary_core_provider_group_key` duplicating the unique constraint on
    `(provider_id, newsgroup_id, binary_key)`. It was ~669 MB and was dropped
    from the schema and live test DB;
  - post-drop live run was not insert-heavy (`unique_binary_upserts=92`,
    `binary_upsert_insert_ms=2.25`) but still slow due to hydration/IO
    contention while scrape and daily bucket stats were active. Active
    `pg_stat_activity` showed a minute-long daily bucket `source_stats` query
    doing `DataFileRead` concurrently with scrape inserts. Next tuning target
    is stage concurrency/query isolation, especially daily bucket stats and
    scrape contention with assemble hydration.
- Follow-up soak/query-shape pass at `2026-06-27 00:05-00:50`:
  - scrape insert duplicate resolution was missing `source_posted_at` in both
    article-number and message-id existing-header joins. This defeated daily
    partition pruning and left multi-worker scrape CTEs active for 30+ seconds.
    The lookup and fallback resolver now include `source_posted_at`, and live
    scrape inserts returned to sub-second/low-second active DB time while still
    inserting 5,000-header batches;
  - assemble improved without reducing the 20,000 configured batch size. Under
    scrape pressure it moved from ~90 headers/sec to ~156 headers/sec after the
    scrape lookup fix. With scrape gated by backlog/hard caps, observed
    assemble passes ranged from ~170-600 headers/sec, and existing-binary-heavy
    passes had sub-second binary upsert/query phases;
  - `subject_multipart_regroup` had three unbounded repair shapes. The stale
    stats pre-pass hashed all ~4.9M `article_headers` rows for 100 binaries and
    timed out at 60s in EXPLAIN. It now refreshes from binary-owned projection
    data only; EXPLAIN dropped to ~436 ms for 100 staged binaries. Key-group
    and source-binary staging are now bounded to recent partitioned candidates
    with a 3,000-row cap per pass so regroup repair does not compete with hot
    assemble writes;
  - release formation correctly skipped weak/single-main candidates in the
    observed soak (`skipped_fragments_single_main=2`) instead of forming
    singleton payload releases;
  - yEnc recovery was active and productive. Observed batches included
    `attempted=30 recovered=30 merged=30` and `attempted=23 recovered=23`,
    while scrape latest/backfill correctly paused at the yEnc hard cap;
  - release summary refresh was usually low-second after recovered-file-set
    bounding, but one outlier still reported
    `recovered_file_set_duration_ms=28335.37` for a small batch. Treat this as
    a remaining follow-up EXPLAIN target if it recurs during the longer soak;
  - final observed serve was left running after focused tests passed. Recent
    logs showed scrape gated by yEnc hard cap, assemble draining, yEnc
    recovering, and release refresh processing newly dirtied summaries.

- Assemble/yEnc observation pass at `2026-07-01 13:19-13:26`:
  - current queue state before serve sampling:
    `article_header_assembly_queue=38,872`, with `30,847` general rows and
    `8,025` structured rows; active queue rows were mostly `2026-07-01`, with
    small spillover on `2026-06-30` and `2026-07-02`;
  - current yEnc work state before serve sampling:
    `ready priority0 opaque_near_time_cohort=5,548`,
    `ready priority0 near_complete_or_multipart=309`,
    `ready priority1 bounded_admission=240,836`,
    `running priority1 bounded_admission=4,250`, and prior completed work of
    `274,130` priority-0 opaque cohort rows plus `388,897` bounded-admission
    rows;
  - read-only Lane A/combined claim EXPLAIN completed in about `718 ms` while
    idle, but still used a broad `Parallel Hash Semi Join` over
    `binary_completion_keys`, read `22,428` buffers, and spilled temp data.
    The root shape is still risky: `assemblyClaimCompletionKeyExistsSQL()`
    probes `binary_completion_keys` within `q.source_posted_at +/- 1 day` per
    queue row, so partition pruning is weak and the planner can scan/hash much
    more completion-key data than the small structured queue slice requires;
  - exact fixed-day hydration remained fast in isolation in earlier EXPLAIN
    work, while live hydration still cost seconds under concurrent scrape and
    refresh pressure. Treat hydration spikes as contention/IO symptoms unless a
    new EXPLAIN proves a bad partition-key shape;
  - current yEnc ready claim selection is not the bottleneck: selecting 10,000
    ready rows ordered by priority/date took about `33 ms`;
  - yEnc opaque near-time admission/refill is the heavier recovery path:
    read-only EXPLAIN of the 50,000-row recent scan took about `2.85 s`, did
    about `1.0M` buffer hits and `19,425` reads, and performed many
    partitioned point lookups into binary projection tables. This is acceptable
    as bounded recovery refill work, but it is too expensive to run inside
    assemble's hot binary stats refresh for every large assemble batch;
  - live serve sample recovered a full 5,000-item yEnc batch at concurrency
    `100` with `recovered=5,000` and `merged=4,950`, confirming yEnc BODY work
    is productive when priority cohorts are selected;
  - live assemble sample processed `20,000` headers in about `67.5 s`
    (`headers_per_second=296.40`). Time was spread across several stages:
    `binary_upsert_ms=25,452`, `binary_refresh_ms=20,461`,
    `binary_refresh_yenc_sync_ms=13,948`, `candidate_selection_ms=12,097`,
    `header_match_ms=10,405`, `assembly_hydration_ms=6,506`,
    `binary_part_upsert_ms=6,715`, and `assembly_claim_ms=4,844`;
  - the live sample proves the slow assemble stage is not a single missing
    index. The high-confidence fix order is:
    1. stop doing expensive yEnc work-item admission sync inside assemble's
       binary stats refresh when recovery is already over hard cap, or move
       that sync entirely to recovery/maintenance ownership;
    2. replace Lane A's broad partitioned completion-key existence check with a
       compact assemble-owned lookup/projection keyed by
       `(provider_id, newsgroup_id, normalized_file_name)` plus partition-aware
       target metadata, or otherwise make the queue-first lookup bounded before
       it touches `binary_completion_keys`;
    3. add yEnc selection metrics by `priority_rank`, `admission_reason`,
       `group_tier`, and lane, plus merge-yield metrics by admission reason;
    4. continue watching queue partition bloat. Empty historical
       `article_header_assembly_queue` child partitions previously retained
       large on-disk sizes after deletes, so retention drops/vacuum pressure
       still matters even when live rows are low;
  - documentation added:
    `docs/wiki/indexer/yenc-recovery-queueing.md` now records how
    `recover_yenc` work is admitted, sorted, grouped, filtered, capped, and
    selected. It explicitly states recovery selection is priority/time-bucket
    based rather than FIFO.
- Assemble/yEnc query-shape follow-up at `2026-07-01 13:35-14:00`:
  - do not fix assemble by moving binary stats refresh to a separate stage
    unless the underlying query shape is already proven sound. The observed
    slow run was a stack of several costs, and moving a bad query would only
    hide where the time is spent;
  - current Lane A does not need an arbitrary candidate bound to improve. It
    needs a lookup order that matches the query. The existing
    `binary_completion_keys` indexes were all `source_posted_at` first, while
    Lane A asks for `(provider_id, newsgroup_id, normalized_file_name)` plus a
    relative source-posted window. Read-only EXPLAIN before the fix scanned and
    hashed about `1.47M` completion-key rows and took about `720 ms` idle;
  - adding `idx_binary_completion_keys_filename_lookup` on
    `(provider_id, newsgroup_id, normalized_file_name, source_posted_at,
    binary_id) INCLUDE (is_main_payload, observed_parts, completion_ratio,
    posted_at)` changed the same read-only plan to nested index-only lookups
    and reduced execution to about `120 ms` idle. This keeps the same semantic
    `source_posted_at +/- 1 day` join and does not cap Lane A arbitrarily;
  - migration added:
    `internal/store/pgindex/migrations/043_binary_completion_key_filename_lookup.up.sql`;
  - yEnc queue schema already had the fields needed for runtime visibility:
    `priority_rank`, `admission_reason`, and `group_tier`. No new yEnc queue
    table was required for the metrics pass;
  - yEnc recovery now carries those fields through claimed candidates and stage
    metrics. New useful metrics include:
    `selection_ready_count`, `selection_priority0_ready`,
    `selection_priority_seeded`, `selection_generic_seeded`,
    `selected_priority_0`, `selected_priority_1`,
    `selected_admission_opaque_near_time_cohort`,
    `selected_admission_bounded_admission`, `body_matched_admission_*`,
    `recovered_admission_*`, `merged_admission_*`, and tier/lane variants;
  - application batching review: yEnc BODY fetch/parse stays concurrent,
    recovered records still stream to batched apply flushes, and the new
    metrics map recovered/merged write results back to the selected candidate's
    priority/admission/tier where possible. This should show whether low merge
    yield is caused by selection/admission choice, BODY fetch failures, parse
    failures, noops, or write/merge behavior;
  - follow-up live serve at `2026-07-01 13:44-13:46` still showed assemble as
    the scrape gate. The observed line had `candidate_selection_ms=29649.49`,
    but the sub-metrics showed this was not one undifferentiated selector:
    `queue_cleanup_ms=21335.54`, `assembly_claim_ms=1883.22`,
    `assembly_hydration_ms=6430.73`, `binary_upsert_query_ms=26406.39`, and
    `binary_refresh_yenc_sync_ms=10842.06`;
  - read-only cleanup EXPLAIN for stale queue rows was about `144 ms` after the
    run and returned zero rows, so the earlier `queue_cleanup_ms` spike is not
    currently explained by the steady-state cleanup lookup shape. Continue to
    watch it under write load before changing schema;
  - subject multipart regroup candidate staging was a real query-shape issue.
    The old query joined `binary_core` before reducing the candidate set,
    causing roughly `181k` binary-core lookups and about `2.1 s` read-only
    runtime on the current database. The rewritten query filters and limits the
    partitioned projection candidates first, anti-joins superseded lifecycle
    rows, then joins `binary_core` for the kept candidates. Read-only EXPLAIN
    of the equivalent rewrite was about `0.85 s` while preserving the existing
    7-day candidate policy and not adding an arbitrary assemble cap;
  - yEnc work-item sync inside binary stats refresh now emits sub-metrics:
    `binary_refresh_yenc_admission_ms`,
    `binary_refresh_yenc_priority_open_ms`,
    `binary_refresh_yenc_sync_chunk_count`,
    `binary_refresh_yenc_sync_chunk_binary_count`,
    `binary_refresh_yenc_sync_upserted`,
    `binary_refresh_yenc_sync_retired`,
    `binary_refresh_yenc_sync_upsert_ms`,
    `binary_refresh_yenc_sync_retire_ms`, and
    `binary_refresh_yenc_promotion_ms`. These are intended to show whether
    the yEnc queue-sync cost is admission snapshot, priority-open counting,
    work-item upsert, stale retirement, or opaque-cohort promotion;
  - validation:
    `go test ./internal/indexing/assemble ./internal/indexing/yencrecover ./internal/store/pgindex`.

## Acceptance Criteria

- This file is the only active sprint plan.
- Root indexer docs no longer contradict the canonical ownership reference.
- Guardrail tests fail on forbidden write-backs.
- Fresh database migrations create all required daily partition parents and a
  rolling 21-day-back/9-day-forward partition horizon.
- Assemble drains queue batches without writing to `article_headers`.
- `recover_yenc` consumes ready work and records recovery evidence without
  creating unbounded backlog.
- Release summary refresh and release formation process newly assembled work.
- Partition retention dry-run reports eligible partitions, blockers, and drop
  order without broad unbounded source/work scans.
- Hot/warm/cold group behavior is proven with live tier/cursor/gap evidence,
  not inferred from configuration alone.
- Latest/backfill behavior is proven after a simulated restart gap.
- A 30-minute serve soak completes with zero PostgreSQL deadlocks and zero
  shared-memory partition failures.
- The 21-day-back/9-day-forward partition horizon remains intentionally
  documented until partition-pruned query shapes or PostgreSQL lock tuning
  justify a wider horizon.
- Speculative weak binary grouping remains investigation-only until sampled
  yEnc evidence is recorded and reviewed.
- Focused Go tests and `git diff --check` pass before signoff.

## 2026-06-29 Follow-Up: Clear Title And PreDB Fallback

- [x] Add runtime public policy setting `public_require_clear_title`.
  Default is enabled. Public catalog visibility, NZB generation, and archive
  claiming must reject placeholder, weak, opaque, or `source_obfuscated` titles
  until a real title is derived.
- [x] Route `release_archive_nzb` through the same runtime
  `ReleaseReadyPolicy` used by public/NZB generation instead of
  `DefaultReleaseReadyPolicy()`.
- [x] Improve PreDB metadata-only fallback for opaque media:
  - include release `size_bytes` and largest non-PAR catalog payload size in
    enrichment candidates;
  - score PreDB `size_kb` against payload size as primary evidence;
  - use posted time and media codec/resolution as corroborating evidence;
  - stop treating runtime alone as a `MOVIE` category hint;
  - widen the local window candidate limit to 1,000 rows.
- [x] Restart serve with the new policy code, ensure
  `public_require_clear_title=true` in runtime settings, sync/backfill PreDB
  around the June 26 indexed window, run metadata fallback, and record whether
  release `3Fgri8qDFNbgZWo5JtnVrj7tPLp` can be identified. Result:
  PreDB backfill reached June 26 before `predb.club` returned HTTP 429. The
  first metadata fallback pass incorrectly identified the release as
  `Fleabag.S02E02.iTALiAN.720p.WEB.H264-NTROPiC` from a 51-minute-away
  size/codec collision. That is now treated as a false positive. The release
  was reset to `source_obfuscated`, the public API hides it again, and metadata
  fallback records top PreDB candidates for manual review without choosing one
  unless the best candidate is inside the tight auto-apply posted-time window.
- [x] Add release-detail manual PreDB matching workflow:
  - show the top 5 stored PreDB candidates on release details, including title,
    category, posted time delta, size delta, decoded-size source,
    resolution/codec evidence, and why the candidate was or was not
    auto-applied;
  - allow an admin to manually choose a PreDB candidate as the release identity
    or manually suggest/override the title/content when PreDB is close but not
    conclusive;
  - persist manual choices separately from automatic enrichment so later
    metadata jobs do not overwrite operator-confirmed identity;
  - keep public/NZB visibility gated by `public_require_clear_title` until a
    manual or high-confidence automatic identity is present;
  - include the `3Fgri8qDFNbgZWo5JtnVrj7tPLp` investigation as a test case:
    manual review confirmed `The Doomies S01E22`, while local PreDB contained
    plausible but incomplete/ambiguous `S01E22` candidates and no explicit
    Romanian marker.
- [x] Fix PreDB size evidence to prefer decoded payload size:
  - use valid PAR2 target size when available;
  - otherwise use stored `yenc_file_size` when available. Current schema does
    not distinguish recovered BODY `=ybegin size=` from Subject trailing-size
    hints, so admin evidence labels this as yEnc-or-Subject size evidence
    rather than a guaranteed BODY-derived value;
  - for split archives, sum decoded archive-part sizes when multiple archive
    payload files and split markers are present; direct media/software payloads
    continue to use the largest decoded payload file;
  - only fall back to observed/catalog article byte totals when decoded payload
    size is unknown. The Doomies case showed catalog size `571,231,197` bytes
    while PAR2/filesystem payload size was `553,127,684` bytes (`527.5 MiB`).
- [x] Signoff: manual identity now uses
  `POST /api/v1/admin/indexer/releases/:id/actions/identify`, writes real
  release identity fields with `manual` or `manual_predb` title sources, marks
  selected PreDB rows chosen, and keeps automatic enrichment from overwriting
  operator-confirmed identity. Admin release details show PreDB candidate
  evidence and actions.

## 2026-07-02 Follow-Up: Article Cohort Scheduler

- [x] Add a real scheduler stage, `article_cohort_schedule`, between
  crosspost materialization and assemble. The stage writes scheduler-owned
  partitioned tables and does not mutate upstream article/source facts.
- [x] Add native daily partition parents for:
  `article_cohort_candidates`, `article_cohort_assembly_queue`, and
  `article_cohort_yenc_queue`. Partition creation and retention drop include
  these tables.
- [x] Materialize complete Subject multipart cohorts into
  `article_cohort_assembly_queue` so assemble consumes HEAD-complete work
  before broad Lane B fallback. These rows do not need yEnc BODY recovery for
  initial binary formation.
- [x] Materialize suspicious opaque singleton cohorts into
  `article_cohort_yenc_queue` so recover_yenc priority admission drains likely
  high-yield cohorts before generic bounded weak backlog.
- [x] Add runtime setting
  `indexing.recovery_admission.priority0_reservoir_batches`, default `5`, so
  recover_yenc refills priority-0 toward a multi-batch reservoir instead of
  only one batch.
- [x] Add admin cohort visibility:
  `GET /api/v1/admin/indexer/work/cohorts` and the
  `/admin/indexer/cohorts` diagnostics page show cohort kind, bucket,
  provider/newsgroup, queue counts, yEnc counts, score, and status.
- [x] Keep weak/sampled yEnc evidence as priority evidence only. The scheduler
  does not form speculative binaries from unprobed opaque siblings.
- [x] Record cohort yield counters from recover_yenc outcomes back into
  `article_cohort_candidates`: successful yEnc recovery marks scheduler yEnc
  rows done and increments recovered/done counts; repeated no-identity outcomes
  increment no-identity counts and move zero-yield cohorts to cooldown.
- [x] Update wiki docs:
  - `docs/wiki/indexer/stage-flow.md`
  - `docs/wiki/indexer/yenc-recovery-queueing.md`
  - `docs/wiki/indexer/binary-grouping-evidence.md`
- [x] Validation: `go test ./...`, `npm run build` from `ui/`, and
  `git diff --check` pass.
- [x] Live serve signoff on 2026-07-02:
  - `article_cohort_schedule` is enabled in the supervisor stage set.
  - Recent scheduler runs completed without timeout after the open-queue guard;
    saturated yEnc queue runs finished in roughly `0.22-0.28s`.
  - Stale cohort assembly queue projections were self-healed: ready rows went
    from `31,880` stale/open `0` to ready/open `0`, with `39,483` done.
  - yEnc feedback counters reached `87,996` done/recovered and `0`
    no-identity cooldowns during the soak.
  - recover_yenc consumed full `5,000` item batches and release summary refresh
    continued processing queued families while scrape remained gated by
    assemble backlog.

### 2026-07-02 Cohort Priority Soak Follow-Up

- [x] Fixed subject-complete cohort scheduling to count existing open
  `article_cohort_assembly_queue` rows before scanning and to skip rows already
  queued/done in the scheduler projection.
- [x] Fixed assemble claim locking to use `FOR UPDATE OF q SKIP LOCKED`, so the
  claim path locks only `article_header_assembly_queue` source rows and does
  not lock scheduler projection rows.
- [x] Fixed combined/Lane B assemble selection to use a cohort-only claim path
  whenever `article_cohort_assembly_queue` has claimable rows. This prevents
  broad Lane A/Lane B fallback CTEs from consuming the claim timeout before
  scheduler-ranked priority rows are claimed.
- [x] Fixed opaque yEnc cohort scheduling to scale scans by remaining queue
  capacity instead of forcing a full scheduler batch scan when the yEnc cohort
  reservoir is nearly full.
- [x] Live serve evidence after restart:
  - subject-complete cohort assembly drained first:
    `article_cohort_assembly_queue` reached `84,884` done and `0` ready during
    the soak;
  - assemble completed priority cohort passes before broad fallback, including
    `20,000` subject/cohort headers collapsed into `35` binaries, then later
    smaller post-scrape cohort passes such as `9,059` headers into `90`
    binaries and `1,114` headers into `59` binaries;
  - broad fallback still processes opaque singleton/general work and feeds yEnc,
    but no patched assemble claim timeout occurred after restart;
  - scrape resumed automatically when the source assembly queue fell below the
    resume threshold, then paused again once latest/backfill refilled the
    assemble bucket;
  - recover_yenc continued full `5,000` candidate batches, mostly priority-0
    `opaque_near_time_cohort` work, with high recovered/merged counts;
  - after the opaque scan fix, `article_cohort_schedule` completed in sub-second
    to low-single-digit seconds in the observed post-restart runs.
- [x] Fixed yEnc scheduler admission bridge so
  `article_cohort_yenc_queue` rows create priority-0 work items without being
  rejected by the generic weak-family/filename predicates. Scheduler-backed
  admission still requires a main payload without existing recovered yEnc
  authority and still respects recovery hard caps/priority-0 overflow caps.
- [x] Live hard-cap observation: once `open_yenc` reached the configured
  `250,000` hard cap, scrape paused for `recover_yenc hard cap` as expected.
  Priority scheduler rows continued to be admitted within overflow capacity,
  but the selector may still mix priority-1 work while older priority-0 leases
  from interrupted runs remain open until expiry.
