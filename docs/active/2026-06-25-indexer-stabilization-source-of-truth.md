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

Known open signoff items:

- complete a clean 30-minute soak from the final patched serve boundary with
  zero `40P01`, zero `53200`, and no PostgreSQL backend crash;
- record final stage-run counts, yEnc throughput, release counts, deferred gap
  counts, daily bucket stats, and default partition row counts after that soak;
- prove hot/warm/cold tier behavior with at least one configured or observed
  group per tier; current configured scrape work is mostly `warm`;
- collect short `EXPLAIN (ANALYZE, BUFFERS)` notes for any additional hot query
  shape changed after the `2026-06-26` regression patch;
- continue assemble write-path tuning. The current evidence points at binary
  upsert and binary stats refresh cost on large, high-cardinality batches rather
  than the yEnc selector self-join regression.

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
