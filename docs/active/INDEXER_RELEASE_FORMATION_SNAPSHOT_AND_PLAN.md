# Indexer Release Formation Snapshot And Plan

Snapshot date: 2026-04-10

This document has two jobs:

1. capture how release formation currently works in code
2. define a concrete target plan for how release formation should work next

Use this as the primary release-design reference during stabilization.

Use `docs/active/INDEXER_STABILIZATION_WORKLIST.md` for the active execution backlog.

Use `docs/active/INDEXER_SCHEMA_TARGET.md` for the intended schema end state.

The main reason this exists is that the current `release_key` is doing too many jobs at once:

- early matcher grouping hint
- coarse release-family label
- debug string
- sometimes human-readable release identifier

## Validation Status

Validation date: 2026-04-16

Release-formation sign-off status:

- release identity cutover work is implemented and validated on the dev DB
- blank family identity rows are repaired and prevented from being re-persisted
- release/title and availability rules are moved out of store persistence helpers and into shared domain helpers
- `release_family_key` is the active family-level grouping key in the current service/store path

Live validation notes:

- blank `source_release_key` / `release_family_key` rows: `0`
- release-family fan-out rows: `0`
- `title_source = 'archive_entry'`: `57`
- non-empty `deobfuscated_title`: `69`

Remaining release-formation caveat:

- this document's timing-based clustering goal is not fully signed off on the current dev DB yet
- persisted timing remains sparse live because only `15 / 178,817` binaries currently connect to any dated raw headers through `binary_parts`
- treat the release-formation design as implemented, but keep live operational validation for timing-sensitive clustering open until historical date lineage is repaired or intentionally re-scoped

That makes it hard to reason about whether two binaries should become one release, and it makes the DB difficult to inspect when keys are long, opaque, or inconsistent.

## Current Behavior In Code

### Pipeline Shape

Today the pipeline is:

1. scrape raw article headers into `article_headers`
2. assemble each article into a `binary` candidate
3. persist matcher output on `binaries`
4. form `releases` by processing one persisted `binaries.release_key` candidate at a time

Relevant code paths:

- `internal/indexing/assemble/service.go`
- `internal/indexing/match/helpers.go`
- `internal/indexing/release/service.go`
- `internal/indexing/release/helpers.go`
- `internal/store/pgindex/repository.go`

### What Assembly Persists

Assembly runs one header at a time, calls the matcher, then writes:

- `release_key`
- `release_name`
- `binary_key`
- `binary_name`
- `file_name`
- `file_index`
- `expected_file_count`
- `total_parts`
- `match_confidence`
- `match_status`
- `grouping_evidence_json`

Code:

- `internal/indexing/assemble/service.go:58`
- `internal/indexing/assemble/service.go:104`
- `internal/store/pgindex/repository.go:1313`

One important detail: binaries are uniquely upserted by `(provider_id, newsgroup_id, binary_key)`, not by `release_key`.

That means the matcher decides both:

- which article parts belong to one binary
- which binaries appear to belong to the same future release

### How The Matcher Builds `release_key` Today

The current matcher chooses `release_key` in this order:

1. use a small indexed archive stem for small opaque split sets
2. otherwise use a contextual fallback key for opaque multi-file posts
3. otherwise use canonicalized `releaseName`
4. otherwise use canonicalized context seed

Code:

- `internal/indexing/match/helpers.go:250`
- `internal/indexing/match/helpers.go:280`
- `internal/indexing/match/helpers.go:343`
- `internal/indexing/match/helpers.go:391`

The contextual fallback key currently mixes signals such as:

- normalized poster
- message host
- xref newsgroups
- posting window
- article bucket
- release family hint like `7z`, `rar`, or `par2`
- expected file count

Code:

- `internal/indexing/match/helpers.go:160`
- `internal/indexing/match/helpers.go:205`

This is why current keys can look like:

- a clean title: `laapataa ladies 2024 hindi uhd`
- an opaque stem: `ce1gjkf0xytjnl1fcssfkqxw3l8p6vbv`
- a long contextual fallback:
  - `hera ... alt binaries wood release 2345778000 7z files 95`

### File Count vs Article Count Parsing

The current code does already separate the two counters correctly:

- pre-`yEnc` counter like `[1/8]` is treated as file index / expected file count
- `yEnc (...)` counter like `(1/228)` is treated as article part / total article parts

Code:

- `internal/indexing/match/helpers.go:476`
- `internal/indexing/match/helpers.go:483`

### How Release Candidates Are Chosen Today

The release stage does not scan all binaries and globally cluster them.

Instead, it first asks Postgres for changed release candidates grouped by persisted `binaries.release_key`.

Code:

- `internal/store/pgindex/repository.go:1458`

This means:

- one source `release_key` becomes one release-candidate batch
- binaries under different `release_key` values are never considered together in the same `formCandidate` call

Reform mode does the same thing for already existing release rows:

- `internal/store/pgindex/repository.go:1521`

### How Clustering Works Today

Within one source candidate, the release service clusters binaries greedily:

1. sort binaries by posted time / article number
2. try to join each binary into the best existing cluster
3. if no cluster score reaches `releaseJoinThreshold = 0.55`, create a new cluster

Code:

- `internal/indexing/release/helpers.go:57`
- `internal/indexing/release/helpers.go:109`

Current cluster scoring uses:

- indexed file-layout compatibility as a hard gate
- same dominant poster
- close posting timestamps
- related filename stems
- related titles
- complementary file types
- size coherence
- indexed file-layout reinforcement
- average binary match confidence

Code:

- `internal/indexing/release/helpers.go:109`
- `internal/indexing/release/helpers.go:153`

### How Final Release Rows Are Written Today

When a cluster is accepted, the release record keeps the source candidate's `release_key`, but the actual upsert identity is `group_name`.

Code:

- `internal/indexing/release/helpers.go:199`
- `internal/indexing/release/helpers.go:225`
- `internal/store/pgindex/repository.go:1803`

`group_name` is derived from:

- source `release_key`
- dominant poster
- representative stem
- cluster time bucket

Code:

- `internal/indexing/release/helpers.go:279`

So today:

- `release_key` is a source-family string
- `group_name` is the actual unique per-release cluster key
- `release_id` is the primary ID

### What Is Working Well Today

- article-part vs file-count parsing is mostly correct
- binaries can be reformed into release clusters within one source family
- clustering does use more evidence than file name alone
- release-level scores and metadata are already separated from raw completeness

### What Is Still Broken Or Confusing

#### 1. `release_key` is not a clean canonical release identifier

It is partly title, partly heuristic context, partly fallback identity.

That makes the field hard to reason about in SQL, logs, and UI/API output.

#### 2. Different source keys can still represent one logical release

Because candidate selection happens by `binaries.release_key`, release reform cannot merge across different source keys.

Examples of sources of fragmentation:

- article bucket differences
- time-window differences
- `7z` vs `par2` family hint differences
- readable vs opaque fallback differences

#### 3. Auxiliary files can still drift away from the main archive family

PAR2 / NFO / SFV attachment depends on stem or cluster evidence after candidate selection.

If the matcher places them under a different source key first, release clustering never gets the chance to compare them to the main archive files.

#### 4. Small incomplete clusters are easy to persist as separate releases

Because clusters are persisted once they are accepted inside a source candidate, you can still get multiple small release rows that look like fragments of one future release.

#### 5. One field is overloaded across three different concepts

These concepts should not share one string:

- source grouping hint from the matcher
- release-family label for debugging / reconciliation
- unique final release identity

## Target Plan

This target plan is the end goal for how release formation should behave once stabilization is complete.

## Guiding Rules

Release formation should answer one question:

Which binaries belong to the same future NZB/release package?

That means the system should be driven by:

- same posting wave
- same release title or same archive family
- same expected file set
- same poster / group context
- compatible file indices
- compatible auxiliary files

It should not be driven by a single overloaded string.

### Identity Model We Should Move To

Use three separate identifiers:

1. `source_release_key`
   - exact matcher output
   - debug and repair only
   - not a canonical release ID

2. `release_family_key`
   - stable family key used to gather candidate binaries that might belong to the same future release
   - consistent across main payload files and auxiliary files
   - human-inspectable when possible

3. `group_name` / `release_id`
   - final unique cluster identity for one actual release row
   - can safely distinguish reposts or separate posting waves of the same title

Short version:

- `source_release_key` = what the matcher first guessed
- `release_family_key` = what family of binaries should be compared together
- `release_id` = the final unique release row

## Concrete Formation Plan

### Step 1: Persist Better Binary-Level Signals

During assembly, persist normalized release-formation fields on `binaries`:

- `source_release_key`
- `release_family_key`
- `file_family_key`
- `family_kind`
  - `readable_title`
  - `archive_stem`
  - `contextual_obfuscated`
- `is_auxiliary`
- `is_main_payload`
- `base_stem`
- `posting_bucket`

The important change is this:

- `release_family_key` must be shared by `.7z.001`, `.r00`, `.part01.rar`, `.par2`, `.volNN+MM.par2`, `.nfo`, `.sfv`, and sample files that belong to the same future release

For split archives, derive `base_stem` by stripping suffixes such as:

- `.7z.001`
- `.zip.001`
- `.part01.rar`
- `.r00`
- `.vol00+01.par2`
- `.par2`

When a readable title exists, `release_family_key` should prefer that normalized readable title.

When the post is obfuscated, `release_family_key` should prefer the shared archive base stem if one exists.

Only when neither title nor archive-family stem exists should we fall back to a contextual key.

### Step 2: Make Contextual Fallback Consistent Across File Types

For contextual-obfuscated fallback keys:

- keep poster
- keep newsgroup
- keep tight posting window
- keep expected file count
- keep article locality when needed
- do not include file-type family tokens that split main payload from PAR2/NFO

In other words, `7z` and `par2` for the same posting wave must land in the same `release_family_key`.

### Step 3: Candidate Selection Must Be By `release_family_key`, Not Raw `source_release_key`

The release service should process changed binaries grouped by:

- provider
- newsgroup
- `release_family_key`

That is the unit of comparison.

This is the most important structural change, because today clustering cannot merge across different source keys.

### Step 4: Form Core Release Clusters From Main Payload Files First

Within one `release_family_key`, build clusters in two passes.

First pass: main payload only.

Main payload means:

- split archives
- single archives
- direct media payload files

Not:

- PAR2
- NFO
- SFV
- SRR
- samples

Hard gates for joining a main payload binary into a cluster:

- same provider and newsgroup
- same `release_family_key`
- expected file count compatible
  - exact match if both are known
  - allow one side unknown
  - reject if both are known and materially different
- posting span compatible
  - strong preference within 2 hours
  - allow up to 24 hours only for strong readable-title / stem matches
- file index compatibility
  - distinct main payload `file_index` values should not collide in one cluster
  - if the same `file_index` appears twice, treat as duplicate-or-conflict and keep the better binary unless evidence proves it is a repost

Suggested join score for main payload binaries:

- +0.30 exact or strongly related base stem
- +0.20 same normalized readable title
- +0.15 same dominant poster
- +0.15 posting time within 2 hours
- +0.10 compatible expected file count
- +0.05 coherent size distribution
- +0.05 compatible file-index continuity

Suggested threshold:

- join cluster at `>= 0.65`

### Step 5: Attach Auxiliary Files After Core Clusters Exist

Second pass: attach auxiliary files to the best existing main cluster.

Auxiliary files include:

- PAR2
- NFO
- SFV
- SRR
- sample files

Auxiliary attachment should use:

- shared base stem
- same poster
- close posting time
- compatible expected file count
- proximity to main cluster posting wave

Important rule:

- PAR2/NFO/SFV should not create their own preferred release when a compatible main payload cluster exists

### Step 6: Do Not Publish Fragment Noise As Final Releases

For multi-file archive sets where `expected_file_count > 1`, do not persist a user-facing release row until the cluster has enough evidence that multiple files from the same future release exist.

Suggested minimum:

- at least 2 distinct main payload files

Alternative stronger threshold for later:

- `min(2, expected_file_count)` for small sets
- or 2 distinct main files plus at least one auxiliary file

Readable single-file payload posts can still form a release with one binary.

This change is meant to reduce the current clutter of one-file fragments pretending to be separate releases.

### Step 7: Final Release Identity

Final release persistence should work like this:

- `release_family_key` stored for debugging and reconciliation
- `source_release_key` stored for matcher traceability
- `group_name` remains the unique cluster identity

`group_name` should be derived from stable cluster-level signals such as:

- provider
- newsgroup
- `release_family_key`
- dominant poster
- earliest posting bucket
- representative base stem

This lets one family create multiple distinct releases when needed, for example:

- reposts on different days
- two different posting waves with the same title

### Step 8: Reform Must Be Able To Merge And Split

Release reform should work at the `release_family_key` level.

That means it must be able to:

- merge two previously split source families into one release cluster
- split one over-broad family into multiple release clusters
- reattach auxiliary files when new main payload files appear

## Explicit Behavioral Rules

These are the rules we should implement, test, and document.

### Rule 1: `release_key` Should Stop Being Ambiguous

Either:

- rename current matcher output to `source_release_key`

or:

- repurpose `release_key` to mean `release_family_key` and store the old matcher string separately

Recommendation:

- keep `release_key` user-facing and consistent
- add `source_release_key` explicitly

### Rule 2: Main Payload Defines The Release Core

The main payload files define whether a release exists.

Auxiliary files enrich a release, but should not usually define it.

### Rule 3: `file_index` Is A Strong Constraint

When a post provides indexed file counts like `[12/95]`, those file indices should be one of the strongest signals in clustering.

Two different binaries claiming the same main payload `file_index` should not both live in one release cluster unless they are proven duplicates.

### Rule 4: Time Matters, But Should Not Be The Only Signal

Posting time should narrow the candidate set, not define the release by itself.

Recommended defaults:

- strong match window: 2 hours
- soft match window: 12 hours
- hard cap without readable-title/stem evidence: 24 hours

### Rule 5: Expected File Count Matters

If one binary says `[1/8]` and another says `[1/95]`, they should not join the same release cluster unless one side is clearly missing or wrong and stronger evidence overrides it.

### Rule 6: Obfuscated Sets Need Archive-Family Cohesion

For obfuscated split archives, the shared archive-family stem should beat poster/time buckets whenever it exists.

If no shared stem exists, contextual fallback is allowed, but it must still keep all file types for the same posting wave together.

## Recommended Implementation Order

1. add and backfill `source_release_key` and `release_family_key`
2. change matcher to emit both
3. change release candidate selection to use `release_family_key`
4. change auxiliary-file family derivation so PAR2/NFO join the same family as main payload

## Current Stabilization Assessment

Snapshot date: 2026-04-16

The release-family redesign is moving in the right direction, but the system is still in a hybrid state where the new identity model exists alongside the old one.

### What Is Better

- candidate selection is now centered around `release_family_key`
- release fan-out is dramatically lower than before
- inspect-derived titles can now flow back into `releases`
- the release-family model is materially easier to reason about than the old overloaded `release_key`

### What Still Needs Stabilization Before API/UI

#### 1. Time-based clustering is undermined by missing persisted timestamps

The release clustering code still uses posting-time proximity as one of its core signals, but almost all persisted `binaries` rows currently have empty `posted_at`.

Implication:

- cluster scoring is leaning on a signal that is mostly absent in the live DB
- `release_files.posted_at` is also mostly empty
- release behavior can look inconsistent even when the matcher keys are improved

Practical conclusion:

- fix `binaries.posted_at` persistence and backfill before tuning cluster heuristics further

#### 2. Identity cutover is incomplete

The new model says these concepts should be separate:

- `source_release_key`
- `release_family_key`
- final release identity via `group_name` / `release_id`

But the current DB still keeps old and new identity fields side by side, and some of them currently carry duplicate values.

Practical conclusion:

- complete the cutover instead of carrying both the old and new model indefinitely

#### 3. Release logic is split between domain code and store code

`repository.go` now contains a lot of release behavior:

- candidate-family coercion
- release upsert behavior
- inspection-driven title rollups
- availability adjustments

This helped patch gaps quickly, but it increases maintenance cost and makes the release model harder to test as one coherent unit.

Practical conclusion:

- move release rules back toward shared domain helpers / services
- keep the store focused on persistence and query shape

#### 4. Some release metadata exists in schema without current product value

Several release-facing columns are effectively unused in the live DB today and should be treated as optional future enrichment rather than part of the minimum stable foundation.

Examples:

- `matched_media_title`
- external metadata IDs and fields
- season / episode enrichment fields

Practical conclusion:

- avoid adding more columns or API surface on top of these until the core release pipeline is stable

#### 5. Blank-family handling still needs to be treated as a correctness bug

The remaining cases where releases persist with blank family identity are not cosmetic. They are release-formation correctness issues and should be eliminated before the model is considered stable.

### Stabilization Priority Order

1. restore trustworthy persisted timing signals on `binaries` and `release_files`
2. eliminate blank-family identity cases
3. finish the identity cutover so `release_key` stops carrying duplicate meaning
4. reduce duplicated release logic in `repository.go`
5. trim unused schema and heavyweight debug payload storage
6. then begin API/UI work on top of the cleaned model
5. add minimum-release persistence thresholds for multi-file sets
6. run a repair/reform pass against the dev DB
7. update SQL/debug docs and UI/API labels so operators inspect the right field

## Open Decisions To Confirm Later

These do not block writing the plan, but they should be confirmed before implementation lands.

1. Should auxiliary-only clusters ever be persisted as releases when no main payload files exist yet?
   - default recommendation: no
2. Should reposts of the same title on different days become separate release rows?
   - default recommendation: yes
3. Should the API/UI expose `release_family_key`, `group_name`, or both?
   - default recommendation: show `release_family_key` for debugging, keep `release_id` as the true unique identifier

## Summary

Today release formation is better than simple `release_key` grouping, but it is still bottlenecked by one overloaded source key.

The concrete fix is:

- separate matcher hints from canonical family identity
- cluster by family, not raw source key
- let main payload files define the release core
- attach PAR2/NFO/SFV afterward
- stop persisting tiny multi-file fragments as if they were complete release rows
