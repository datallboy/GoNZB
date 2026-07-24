# Indexer Supervisor Soak Findings

Status: continued production hardening on 2026-07-24
Branch: `audit/indexer-sustained-workload`
Base: `dev` at `c7e7dbc`

## Outcome

The normal supervisor successfully scraped the two requested groups, assembled
binaries, recovered yEnc identity, formed a release, generated and archived its
NZB, and completed archive and media inspection. The audit found and fixed
several correctness and hot-query defects. PostgreSQL structural and checksum
checks were clean.

This was a sequence of measured live intervals with controlled restarts for
repairs. It was not an uninterrupted four-hour endurance run and does not certify
the host RAM. It does establish that the repaired pipeline works end to end
under a substantial live backlog and that the resulting database was physically
clean at shutdown.

Continued validation after the initial report resolved the inspection-discovery
global scan and release-partition lookup concerns, found and fixed a restart
correctness defect in assembly ownership, and exercised the repaired paths under
continued live ingest. The remaining durability limitation is the host: one Go
compiler process segfaulted while writing an object file. Two subsequent
single-worker builds from independently cleared build caches were byte-identical,
but that event is consistent with the suspected non-ECC RAM fault and prevents
hardware certification.

## Environment And Evidence

- PostgreSQL: 17.10 in `gonzb-indexer-soak-pg-20260724`
- Database: `gonzb_indexer_soak_after`
- Host port: `127.0.0.1:55434`
- Storage: dedicated Docker volume `gonzb_indexer_soak_20260724`
- Checksums: enabled at cluster initialization
- Groups:
  - `alt.binaries.sleazemovies`
  - `alt.binaries.multimedia.rail`
- Runtime: normal `serve` mode with the built-in supervisor
- Enabled paths: latest scrape, bounded backfill, deferred work, and all
  applicable downstream indexer stages
- Disabled integrations: TMDB, downloader, and GoNZBNet
- Destructive source maintenance: disabled
- Primary evidence root:
  `.tmp/indexer-soak/20260724T142800Z/`
- Earlier baseline root:
  `.tmp/indexer-soak/20260724T133543Z/`

The primary evidence root contains 25 append-only telemetry checkpoints, the
application log, isolated SQLite settings, generated indexer objects, and a
checksummed PostgreSQL custom-format dump. These artifacts are ignored by Git
and may contain provider-derived data, so they must remain local.

The initial clean interval ran for approximately 15 minutes. The repaired live
workload then ran for approximately 35 minutes across controlled restarts while
the same database was drained and replayed. Ingestion and yEnc admission were
disabled near the end so the final release and inspection path could be
observed without continuously adding backlog.

## Workload Result

Final durable counts:

| Object | Count |
| --- | ---: |
| Article headers | 242,642 |
| Binary identities | 123,926 |
| Active binaries | 48,998 |
| Superseded binaries | 74,928 |
| Releases | 1 |
| Release files | 10 |
| Release catalog files | 10 |

The release had:

- 100% known-file completion;
- 10 expected and 10 actual files;
- one main RAR archive plus nine PAR2 sidecars;
- a generated 66,876-byte NZB stored in the configured object store;
- a stable NZB content hash and `purge_pending` archive state;
- successful archive discovery from only 499,705 bytes of a roughly 139 MB
  archive;
- one contained media entry with codec and resolution metadata obtained without
  materializing the complete archive or media payload.

The source evidence contained repost alternatives with duplicate positive part
ordinals. These alternatives remain available as source evidence, while the NZB
query now deterministically emits one segment for each positive ordinal.

## Continued Hardening Result

The live database was kept running after the initial report and grew beyond
178,000 binary identities and 355,000 article headers. The additional interval
established:

- inspection discovery claims `binary_inspection_ready_queue` rows before it
  considers a global seed scan;
- ready work does not wait on the discovery refresh advisory lock;
- global discovery seeding now occurs only when the ready queue is depleted and
  uses an increasing 1/5/15-minute empty-result backoff;
- incomplete opaque binaries without part 1 remain unclaimable until a prefix
  actually exists;
- concrete discovery error reporting exposed a claim-to-sample race: yEnc can
  supersede a binary or move its prefix after inspection claims it. These
  proven stale/no-prefix outcomes now complete with a specific skip reason
  instead of entering the five-minute failure retry loop, while transient NNTP
  failures still retry;
- yEnc recovered 3,279 of 3,279 selected priority-zero headers in one batch and
  merged 3,190 into stronger family evidence, with no fetch, parse, or not-found
  failures;
- no active binary had `observed_parts > total_parts`, duplicate logical part
  ordinals, or duplicate Message-IDs;
- every opaque singleton created after the assembly ownership repair either had
  active yEnc work or a concrete completed recovery/inspection outcome;
- zero post-repair active singletons had an article whose yEnc work belonged to
  a different binary.
- after deployment, 1,332 raced/superseded samples completed as
  `candidate_no_longer_materializable`, 329 completed as
  `prefix_not_available`, and 135 were sampled normally, with no retryable
  discovery failure in that interval.

The disposable database retains 75 misleading active singleton artifacts created
before the ownership repair. Their articles had already completed yEnc recovery
under the original binary and were later reassigned by a restarted assembly
pass. They are retained as before-state evidence and are excluded from
post-repair conclusions. A fresh deployment cannot create them through the
repaired claim path.

## Correctness Checks

The final database had:

- zero binaries where `observed_parts > total_parts`;
- zero duplicate Message-IDs within a binary;
- zero rows in all 30 default partitions;
- no hot default partition;
- one durable release after repeated release-stage passes;
- matching release, file, catalog, and NZB counts;
- a successful inspection rerun against the current release identifier.

The workload also verified the maintained grouping rules:

- yEnc `(part/total)` counters are binary segment evidence, not file-set
  counters;
- unquoted multipart filenames group by the recovered filename;
- opaque split archive segments group by archive stem;
- recovered strong evidence can merge provisional families;
- a complete standalone archive is a valid main payload;
- fallback base-stem candidates cannot delete a release produced by stronger
  grouping evidence.

## Stage And Query Review

All supervised stage runners and their service/store entry points were traced
against the inventory in `INDEXER_SUPERVISOR_SOAK_AUDIT.md`. Read-only hot
selectors were checked with bounded plans where safe; write shapes were reviewed
from source, `pg_stat_statements`, PostgreSQL duration logs, lock telemetry, and
live row/call counts. Destructive maintenance was reviewed statically and was
not executed merely to collect a plan.

| Area | Live result | Query-shape assessment |
| --- | --- | --- |
| Latest/backfill scrape | Healthy; supplied the requested backlog | Writes remained partition-routed; no default rows |
| Poster materialize | Completed; max stage 7.18 s | Completion update now prunes by source day |
| Cohort schedule | Completed; max stage 4.83 s | Removed per-candidate catalog probes |
| Assemble | Completed; max stage 53.3 s for a 20k high-churn batch | Bounded batch; remaining cost follows yEnc/weak-identity volume |
| yEnc recovery | Completed batches up to 62.6 s | Part and completion mutations are now batched |
| Release summary | Completed; max 15.1 s before lookup repairs | Family lookups and both partition checks are now key-indexed |
| Release/NZB/archive | Complete end-to-end release | Correct source/replacement and segment selection |
| Inspect discovery | Completed; max 2m27.85 s before repair | Queue-first claims, one seed evaluation, bounded empty-scan backoff, and prefix readiness |
| PAR2/archive/media | Completed for the formed release | Partial archive/media inspection worked |
| PreDB enrichment | Ran without SQL errors after qualification fix | Bounded candidate selection |
| TMDB enrichment | Not exercised; no credentials | Static review only |
| Maintenance/retention | Non-destructive tasks only | Destructive paths static-reviewed |
| Dashboard/catalog/Newznab | Release-backed reads exercised | No blocking query observed |

Representative measured query evidence:

- The article cohort scheduler fell from roughly 313-607 ms to 22-27 ms after
  removing one `to_regclass` partition probe per candidate.
- Batched yEnc completion updated 5,792 rows in 23 calls, averaging 36.3 ms and
  peaking at 52.6 ms, instead of issuing one completion statement per recovered
  item.
- A representative discovery eligibility plan on the smaller dataset completed
  in about 78.6 ms for 6,554 roots, but the original live duration grew to 14-25
  seconds as the database reached 123,926 binaries. Queue-first claims, a single
  seed evaluation, and empty-result backoff removed that scan from normal
  ready-queue processing.
- Release partition verification originally hashed/scanned the full identity
  projection and removed 319,868 rows at the join filter for only four requested
  keys. The equivalent keyed plan fell from 250.6 ms to 0.286 ms and reduced
  shared-buffer hits from 13,502 to 80. Larger observed pre-repair batches had
  taken 3.1-4.8 seconds.
- With more than 178,000 identities under active ingest, post-repair release
  partition-verification calls remained between roughly 2 and 64 ms. Complete
  release-summary passes during steady churn were generally 33-128 ms.
- Before the lookup repairs, the live database wrote 239 MB across nine
  PostgreSQL temp files. No sustained memory growth or uncontrolled temp growth
  was observed.

One later 19 MB temp-I/O observation was traced entirely to manual audit
`GROUP BY` duplicate checks and a `pg_stat_statements` inspection query. It was
not emitted by a supervised application query; statistics were reset afterward
to keep the final interval attributable. The final app-only interval recorded
zero temp files, deadlocks, blocked lock waiters, and checksum failures.

Backpressure behaved as designed: ingestion paused above approximately 50,000
unassembled binaries and resumed below approximately 10,000. Queue draining
continued while ingest was held.

## Locking And Transactions

No persistent blocker, idle-in-transaction session, or shutdown-resistant claim
was observed. PostgreSQL recorded one deadlock:

- yEnc recovery held a row lock while selecting `binary_core ... FOR UPDATE`;
- inspection ready-queue insertion requested a foreign-key key-share lock on
  the same binary;
- PostgreSQL aborted and the application retry completed the work;
- no stage ended failed from the deadlock and the pipeline did not stall.

This was a real lock-order finding even though retry contained it. Queue seeding
is now less frequent and occurs outside the claim path, and a concurrent
PostgreSQL regression test proves that already-ready inspection work can be
claimed while the refresh advisory lock is held. No second deadlock occurred
during the continued intervals. An isolated retry is operationally acceptable;
repeated deadlocks remain a production investigation threshold because the
underlying foreign-key/identity lock order has not been broadly redesigned.

The stage history contains six failed and two abandoned rows. Every one maps to
an intentional Ctrl-C/restart during repair: context cancellation or lease
cleanup for scrape, assemble, summary, discovery, or yEnc work. There were no
unexplained terminal stage failures in the final interval.

## yEnc Recovery

yEnc recovery was a dominant part of this sample rather than an exceptional
fallback. Repeated 5,000-item batches completed with no normal fetch, parse, or
not-found failures; observed failures were shutdown cancellations. One captured
batch recovered all 5,000 selected headers and merged 4,968 into useful binary
evidence.

The audit found that the original write path performed part merges and work-item
completion per record. Both are now set-based. Remaining seed/target mutations
still include per-record operations and should be profiled again in a longer
run if yEnc-heavy groups are a normal production target.

The initial latest depth of 5,000 articles was too shallow to complete a common
file with more than 12,000 segments. A bounded backfill was required. This is a
usability/tuning issue: initial group setup should explain the relationship
between latest depth, multipart size, and backfill, or select a safer adaptive
initial window.

## Repairs Applied

Each repair is an incremental commit:

| Commit | Finding and repair |
| --- | --- |
| `a3ba581` | Integrity guard now verifies physical child indexes instead of partitioned parents |
| `3adbea9` | Qualified ambiguous release size in PreDB enrichment |
| `a7f8536` | Partition-pruned poster completion updates by source day |
| `87f8e14` | Removed yEnc part counters from file-set metadata |
| `f7eba1b` | Grouped unquoted yEnc multipart subjects by filename |
| `c4098d7` | Allowed recovered segments to merge with provisional families |
| `0864872` | Removed per-row scheduler partition catalog probes |
| `a54ec21` | Deferred inspection discovery while yEnc recovery is active |
| `9c4af95` | Removed exact duplicate database indexes through migration 030 |
| `d5bf84a` | Batched yEnc part merges |
| `52036a5` | Grouped opaque archive segments by file stem |
| `87ab0eb` | Batched yEnc work-item completion updates |
| `6cd9c44` | Allowed complete standalone archive releases |
| `9991e22` | Protected releases from lower-priority fallback stale cleanup |
| `19f0de6` | Reran inspection when a replacement release has a new identifier |
| `3691c02` | Deduplicated positive release article part ordinals for NZB output |
| `f535035` | Kept temporary yEnc conflict keys partition-shaped |
| `5f77ded` | Claimed inspection ready work without unconditional queue top-up or duplicate discovery evaluation |
| `030fc5b` | Added set-based discovery seeding and bounded empty-scan backoff |
| `01ac27e` | Split release partition preflight into keyed family lookups |
| `a25ef49` | Made release preflight imply its partial family-index predicates |
| `c58f488` | Prevented assembly restarts from reclaiming articles already owned by a binary |
| `f78414a` | Deferred discovery until part 1 exists and retained concrete sampling errors |
| `08ec01f` | Split release partition verification into keyed family lookups |
| `c1d3458` | Completed stale/no-prefix discovery samples without retrying while preserving transient NNTP retries |

Every code repair has a focused unit, integration, migration, or query-guardrail
test. Fresh schema inspection after migration 030 found no exact duplicate
indexes.

## Integrity And Memory Observations

Online `pg_amcheck` checked 1,460 relations and 182,367 pages without an error.
After stopping PostgreSQL, `pg_checksums --check` reported:

```text
Files scanned: 5631
Blocks scanned: 213681
Bad checksums: 0
Checksum version: 1
```

PostgreSQL reported zero checksum failures and no invalid-page/`XX001` event.
All default partitions were empty. The clean database dump is:

```text
.tmp/indexer-soak/20260724T142800Z/manifest/final-after-fixes.dump
.tmp/indexer-soak/20260724T142800Z/manifest/final-after-fixes.dump.sha256
```

Host memory remained bounded during the observed run, with approximately
9.4 GB available when sampled. The PostgreSQL container used approximately
1.2 GB of its 4 GB limit. Existing host swap use was already near 4 GB and did
not by itself identify a workload fault. These observations and clean database
checks do not prove the RAM is healthy; only an adequate offline memory test or
ECC monitoring can provide hardware-level confidence.

During the continued interval, `go run` triggered a Go compiler SIGSEGV in
`cmd/internal/obj.(*writer).StringTable` while compiling `internal/store/pgindex`.
The Go build cache was discarded. Two subsequent `GOMAXPROCS=1`, `-trimpath`
builds from separately cleared caches produced byte-identical executables with
SHA-256:

```text
9b887171d0a23456234fa8c5d05c4db68835500b1117b327f380f661f7b3925a
```

After the final discovery repair, two more independently clean builds were also
byte-identical with SHA-256
`914c065ff7c9d407ea6e72f05b253935321fc5b86f15c6e49c2349b9cb85b084`.
The soak now runs a verified disk binary rather than `go run`. Kernel logs
exposed no EDAC/MCE event, but this ThinkPad does not expose useful ECC counters.
`/dev/shm/gonzb-inspect` was empty apart from stage directories, had no open
GoNZB files, and the 12 GB tmpfs had roughly 12 GB free. The compiler failure
was therefore not caused by an uncleared or full inspection RAM workspace.

## Validation

The final validation used a freshly recreated disposable database
`gonzb_soak_test` and required PostgreSQL-backed tests:

```bash
GONZB_REQUIRE_TEST_PG=1 GONZB_QUERY_SOAK=1 go test ./...
go vet ./...
```

The full suite passed. The PostgreSQL query-soak package completed in
114.987 seconds. The first full run exposed the temporary yEnc conflict-key
guardrail fixed by `f535035`; the database was recreated and the entire suite
then passed from a clean state.

After the continued hardening commits, the required suite was repeated with one
Go worker while the application soak used its separate database:

```bash
GOMAXPROCS=1 GONZB_REQUIRE_TEST_PG=1 GONZB_QUERY_SOAK=1 go test ./...
GOMAXPROCS=1 go vet ./...
```

Both passed. The PostgreSQL store/query-soak package completed in 203.955
seconds, including the inspection queue, assembly ownership, partition guard,
and release-summary query changes.

## Remaining Findings

1. **Hardware blocker: suspected host RAM instability.** The compiler SIGSEGV
   is outside the application pipeline and cannot be corrected in GoNZB. Keep
   checksums and integrity monitoring enabled, use the verified disk binary, and
   run an adequate offline memory test before treating this host as unattended
   production infrastructure.
2. **Low/monitoring: one historical cross-stage deadlock was auto-retried.** No
   recurrence occurred after queue/transaction-window reductions. Treat any
   repeated `40P01` or stalled stage as actionable; a broad lock-order redesign
   is not justified by the single contained event.
3. **Medium: initial latest depth can strand very large multipart files.** Add an
   adaptive initial scrape window or clearer setup guidance and progress
   reporting for automatic bounded backfill.
4. **Low/scale: empty generic yEnc fallback seeding still scans three bounded
   branches.** At this volume each branch took about 2-3 seconds when the ready
   queue was empty. The existing 1/5/15-minute low-yield backoff prevents a hot
   loop and active queue work does not use this path. Revisit queue-native
   seeding or a cursor before materially larger identity counts.
5. **Low/measurement: remaining yEnc seed/target writes are partly per-record.**
   Measure WAL and call counts in a longer yEnc-heavy run before redesigning.

The software hot-path findings that previously blocked a longer run have been
addressed. A four-hour run on known-good hardware is still required before
claiming unattended long-duration production capacity.

## Limitations And Next Run

- The run was not four uninterrupted hours.
- TMDB and destructive maintenance were not live-tested.
- Historical timeframe scrape was not part of this workload; bounded backfill
  was exercised.
- Only one release completed, so release-stage throughput at scale remains
  unmeasured.
- The clean checks do not certify unreliable RAM.

Repeat the same isolated run for at least four uninterrupted hours on
known-good RAM. Preserve the current volume and evidence root until the
fixes and before/after plans have been reviewed.
