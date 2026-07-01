# Binary Grouping Evidence

Binary grouping decides which article headers belong to the same file-level
binary before release formation sees the result. The grouping contract must
prefer the strongest available stable evidence and must not let randomized
poster or Message-ID context split an otherwise canonical multipart Subject.

## Evidence Priority

Use evidence in this order:

1. Explicit NNTP Subject multipart coordinates:
   `[file_index/file_total]`, quoted filename, yEnc marker,
   `(part/total)`, and file size.
2. Stable canonical Subject token when the Subject is obfuscated but consistent
   across all articles belonging to a file or release.
3. Recovered yEnc BODY identity when HEAD evidence is incomplete or
   randomized.
4. Weak cohort hints such as provider/newsgroup, close timestamps, similar
   bytes/lines, nearby article numbers, and Message-ID or poster suffixes.

Do not promote `From`, poster suffix, Message-ID suffix, Organization, or
article-number proximity above a complete Subject multipart identity.

## Posted-Time Boundary Policy

`source_posted_at` is a partition, retention, query-pruning, and supporting
cohort-evidence key. It is not stronger than canonical file identity evidence.

Assemble may merge articles across different `source_posted_at` values and
different daily partitions when stronger file evidence says they are the same
binary. Strong cross-time merge evidence includes:

- same provider and newsgroup;
- same normalized filename or canonical subject token;
- same release-family key, base stem, or file-set key;
- compatible Subject multipart coordinates: file index/file total,
  article part/article total, and file size;
- compatible recovered yEnc coordinates when HEAD is incomplete:
  `name=`, `part=`, `total=`, `size=`, and `ypart` offsets;
- no conflicting sampled evidence from another filename, file size, part total,
  or family key.

When those fields agree, the binary should be represented as one file-level
binary even if related articles arrived minutes or hours apart. Matching source
time strengthens confidence, but mismatched source time must not split a
canonical binary by itself. The partition-key columns remain on
source/projection rows so retention can prune correctly.

`source_posted_at` may still be used to bound candidate scans. Any bounded scan
must include a second evidence join against existing completion keys or binary
identity rows so late/early parts can attach to an already-known binary outside
the current scan window.

## Uploader Fingerprints

Uploader-specific fingerprints can improve scheduling and ordering hypotheses,
but they do not outrank stronger evidence.

### Nyuu

Default Nyuu Message-IDs commonly have this shape:

```text
<RandomLetters24-EpochMilliseconds@nyuu>
```

The 13-digit suffix is a Unix epoch millisecond timestamp. When the Subject is
obfuscated but the Message-ID matches this default form, the timestamp is a
strong ordering hint after sampled yEnc validation confirms:

- one consistent yEnc `name=`;
- one consistent `total=`;
- sampled `part=` values are monotonic with Message-ID timestamp order;
- `Date`/`X-Trace` evidence is not obviously inconsistent with the timestamp.

If Nyuu `postDate` override or custom Message-ID behavior is suspected, do not
trust Message-ID timestamp ordering. Fall back to explicit Subject evidence or
yEnc recovery.

### ngPost

ngPost full article obfuscation often uses UUID-like Subject and Message-ID
values. UUID identity can help classify a post as ngPost-like, but UUID order
is not meaningful. For this class:

- do not sort by Message-ID;
- treat provider article number as a local weak hypothesis only;
- use sampled or full yEnc evidence to prove membership and ordering.

### Article Number

Article numbers are provider-local acceptance order. They can help schedule
nearby probes on one provider, but they are not a cross-provider identity or
ordering source and must not be persisted as authoritative ordering.

## Grouping Classes

### 1. Clear Subject

The Subject contains the clear release/file name, file index and total,
article part and total, and file size. Example shape:

```text
[1/8] - "Release.Name.part01.rar" yEnc (12/400) 123456789
```

Expected behavior:

- group by provider, newsgroup, normalized filename, file size, file index,
  file total, and article part total;
- use `(part/total)` as the article index within the binary;
- do not require yEnc recovery for initial binary assembly;
- yEnc recovery may later validate or enrich, but must not split the binary
  when BODY `name=` is weaker or randomized.

### 2. Canonical Obfuscated Subject

The Subject is obfuscated, but the obfuscated filename/token is stable and the
Subject still contains complete multipart coordinates. Example shape:

```text
[1/8] - "rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv" yEnc (7152/28465) 20403308372
```

Expected behavior:

- treat the Subject filename/token plus multipart coordinates as canonical
  grouping evidence;
- random `From` values must not create separate binaries;
- randomized yEnc BODY `name=` must not override the Subject identity;
- do not enqueue yEnc recovery only to discover article part or total because
  HEAD already supplies them;
- queue yEnc recovery only when validation, missing metadata, or downstream
  inspection needs BODY details.

Live reference case:

- `rZVWpKbxI7KyXz2Oy2BtrOLZzXwmLCoG.mkv`
- group `alt.binaries.newznzb.bravo`
- Subject reports `total=28465` and size `20403308372`;
- observed data had 4,882 distinct part numbers across 4,882 singleton weak
  binaries because randomized poster/context was included in the grouping key;
- this is an over-splitting bug. Subject identity should merge those articles
  into one file-level binary.

### 3. Strong NNTP Obfuscation

The Subject, poster, and other HEAD fields are randomized enough that the
Subject cannot identify file membership. Message-ID or poster suffixes,
timestamps, article numbers, bytes, and lines may still suggest a cohort.

Expected behavior:

- use weak HEAD cohorts only to prioritize recovery probes;
- recover yEnc BODY samples to find authoritative `name=`, `part=`, `total=`,
  `size=`, and `ypart` offsets;
- promote a cohort only after sampled yEnc evidence is internally consistent;
- article-number order is a hypothesis, not proof.

### 4. Fully Randomized Or Unclassifiable

HEAD and recovered BODY evidence do not provide stable identity, or sampled
evidence conflicts.

Expected behavior:

- keep as weak/provisional only while fresh enough for investigation;
- do not form releases from this class;
- prune after retention/age policy when no stronger evidence appears.

## yEnc Recovery Admission

Do not queue yEnc recovery simply because a post is obfuscated. Queue recovery
when HEAD evidence cannot answer at least one required identity question:

- file name or stable canonical token;
- file index and file total;
- article part and article total;
- file size;
- confidence that a cohort is one binary rather than interleaved binaries.

Subject-complete posts should be assembled from HEAD first. Recovery may be
used as validation, but recovered yEnc `name=` has lower grouping priority when
it is random and conflicts with a complete, stable Subject identity.

### Priority Policy

`recover_yenc` is not FIFO. Candidate selection reads from
`yenc_recovery_work_items` and uses `priority_rank`, posted-time fairness
buckets, and a newest-work lane. Admission into that work table must therefore
preserve the evidence priority:

- `priority_rank = 0`: work likely to unlock binary grouping or release
  formation. This includes incomplete multipart binaries, indexed multi-file
  candidates, and suspicious opaque near-time cohorts.
- `priority_rank = 1`: weak/provisional binaries that may need BODY identity
  but do not yet have strong cohort pressure.
- `priority_rank = 2`: low-value validation or cleanup candidates.

Suspicious opaque cohorts are HEAD-only groups where all of these are true:

- same provider and newsgroup;
- `binary_identity_current.family_kind = 'opaque_set'`;
- `binary_identity_current.identity_reason = 'opaque_subject_set'`;
- each current binary is still a one-part provisional/weak singleton;
- `binary_observation_stats.posted_at` falls in a bounded near-time window;
- the cohort has at least 20 active singleton binaries.

The admission bucket is runtime-configurable as
`indexing.recovery_admission.near_time_cohort_bucket_minutes` and defaults to
five minutes. That is intentionally a hint, not grouping truth: large uploads,
slow uploaders, throttling, multi-connection posting, and provider acceptance
order can spread related articles across seconds or minutes. These cohorts
should be admitted as `priority_rank = 0` with
`admission_reason = 'opaque_near_time_cohort'`. This does not promote the
cohort to a real binary by itself. It only tells `recover_yenc` to spend BODY
probes there before generic weak backlog because the timeframe suggests the
current singleton identities may be incomplete.

Priority-0 suspicious cohort admission must not wait for the generic yEnc
ready queue to drain. A large priority-1 backlog is expected on heavily
obfuscated groups; it must not prevent new high-pressure bursts from entering
the priority-0 lane. Admission should refill priority-0 cohorts whenever the
ready priority-0 pool is below the current recovery batch target, subject to
the configured hard cap and priority-0 overflow cap.

When a BODY probe inside an opaque near-time cohort succeeds and exposes a
multi-part yEnc identity, that success is proof that the surrounding burst is
worth more probes. The recovery stage should immediately admit sibling
singletons from the same provider/newsgroup/near-time bucket as priority-0
work. This still does not trust the cohort as a binary; it only follows proven
BODY evidence so the remaining articles for that file can be recovered and
merged.

Do not rely on article number order or near-time bucketing to probe only a
handful of articles in this class. Article number order and near-time
clustering are scheduling and diagnostic hints only. Until a separate
sampled-yEnc promotion workflow exists, every admitted singleton that needs
BODY identity remains eligible for yEnc recovery; the prioritization decides
which BODY probes happen first.

## Confidence Labels

Use these grouping methods when persisting evidence:

- `subject_multipart_clear`: clear Subject with full multipart coordinates.
- `subject_multipart_obfuscated`: obfuscated but stable Subject with full
  multipart coordinates.
- `weak_header_sampled_yenc`: weak HEAD cohort promoted by sampled yEnc.
- `full_yenc_recovered`: BODY recovery required for authoritative identity.
- `unclassified_random`: insufficient stable identity; retention candidate.

High confidence requires that the selected evidence source supplies a stable
filename/token, part number, total parts, and file size or an equivalent
release-specific identity. Weak confidence must not be upgraded because of
poster-only or message-id-only similarity.

## Implementation Notes

- The assemble matcher should recognize complete Subject multipart coordinates
  before contextual fallback keys are built.
- Contextual fallback keys must not include randomized poster/message-id tokens
  when a stable Subject multipart key exists.
- Subject multipart regrouping must not require exact `source_posted_at`
  equality. Use `(source_posted_at, binary_id)` only to address rows, not to
  decide identity.
- Same filename plus same family plus compatible Subject/yEnc part metadata is
  strong enough to merge one binary across source-time boundaries.
- If HEAD says `(7152/28465)` and yEnc BODY says `part=7152 total=28465` but
  BODY `name=` is random, keep the Subject filename as the binary identity and
  record BODY `name=` as lower-priority recovery evidence.
- Release refresh should prefer subject-derived file-set keys over random
  recovered BODY names when both point to the same part/total/size shape.
