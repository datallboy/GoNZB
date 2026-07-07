# GoNZB Uploader Fingerprint, Ranking, and Ordering Policy

## Purpose

This document defines a wiki-facing behavior policy and a Codex handoff plan for improving GoNZB article grouping, ordering, and yEnc recovery decisions.

The goal is to reduce `recover_yenc` workload by using reliable uploader-specific evidence where possible, while avoiding false complete binaries caused by over-trusting provider-local article numbers or weak header signals.

This policy focuses on two currently important uploader families:

- **Nyuu** by `animetosho`
- **ngPost** by `mbruel`

## Core Principle

Use the strongest available evidence first.

```text
Direct NZB import       = authoritative
yEnc BODY evidence      = authoritative
explicit Subject parts  = strong header evidence
Nyuu Message-ID ms time = strong ordering hint if default pattern is confirmed
ngPost UUID Message-ID  = identity only, not orderable
Article number          = provider-local weak hint only
Date header             = coarse grouping hint only
From / suffix / bytes   = classifier/grouping hint only
```

For fully obfuscated articles, exact grouping and ordering still ultimately comes from yEnc:

```text
=ybegin part=<part> total=<total> ... name=<filename>
=ypart begin=<offset> end=<offset>
```

The purpose of this policy is not to eliminate yEnc recovery. It is to avoid running yEnc recovery on every article when a candidate binary can be validated with samples.

---

## Evidence Ranking

### Rank 0: Direct NZB Ingestion

If an NZB file is directly imported/submitted/generated, it is the best source of truth.

Use NZB segment numbers and message IDs as authoritative.

```text
grouping_method = direct_nzb
ordering_method = nzb_segment_number
verification_method = nzb_schema_and_optional_availability_check
confidence = authoritative
```

No article staging, weak grouping, or yEnc recovery is needed unless the NZB itself is incomplete or corrupt.

### Rank 1: yEnc BODY Evidence

Parsed yEnc fields are authoritative for article-to-binary grouping and ordering.

```text
grouping_method = yenc_name
ordering_method = yenc_part
verification_method = body_probe
confidence = authoritative
```

Use full yEnc recovery only when weaker methods cannot safely establish order.

### Rank 2: Explicit Subject Part Evidence

Subjects that expose filename and part ordering are strong enough to form binaries without BODY recovery.

Examples:

```text
"file.rar" yEnc (12/100)
[67/81] - "archive.7z.067" yEnc (296/366) 262144000
```

If the Subject contains filename, part, total, and size in a recognizable yEnc-style pattern, use it.

```text
grouping_method = subject_filename
ordering_method = subject_part
verification_method = subject_parse
confidence = strong
```

Run sampled yEnc only if the candidate is high-value, suspicious, mixed, or required by config.

### Rank 3: Nyuu Default Message-ID Timestamp

Nyuu default Message-ID format:

```text
<RandomLetters24-EpochMilliseconds@nyuu>
```

Example:

```text
<VmQuDcOlRfOxMnWcWvQuCcHj-1782492955048@nyuu>
```

The trailing 13-digit value is Unix epoch milliseconds.

```text
1782492955048 ms
= 2026-06-26 16:55:55.048 UTC
```

This often lines up with:

```text
Date: Fri, 26 Jun 2026 16:55:55 GMT
X-Trace: 1782492955 ...
```

For Nyuu-default Message-IDs, timestamp order is a strong ordering hint across providers because Message-ID is stable while article number is provider-local.

```text
grouping_method = nyuu_msgid_timestamp_candidate
ordering_method = msgid_epoch_ms
verification_method = sampled_yenc
confidence = medium/high before validation, high after validation
```

Never treat it as authoritative until sampled yEnc confirms the candidate.

### Rank 4: ngPost UUID Message-ID

ngPost full article obfuscation typically uses UUID-style Message-IDs and Subjects.

Example pattern:

```text
Subject: {uuid}
Message-ID: <{uuid}@ngPost>
```

The UUID is random v4-like behavior and is not sortable.

```text
grouping_method = ngpost_uuid_candidate
ordering_method = none
verification_method = sampled_yenc_or_full_yenc
confidence = weak/medium for membership, none for ordering
```

Message-ID identity is useful. Message-ID order is not.

### Rank 5: Article Number

Article number is provider-local and not deterministic across providers.

It may correlate with acceptance order on one provider, but it is not portable and not proof.

Use article number only as a local weak ordering hypothesis when no better signal exists.

```text
ordering_method = provider_article_number_hypothesis
confidence = weak
```

Do not persist article-number order as if it were authoritative.

---

# Nyuu Policy

## Nyuu Behavior Summary

Nyuu default behavior:

- Default Message-ID contains 24 random letters, a hyphen, epoch milliseconds, and `@nyuu`.
- Default Subject includes filename and yEnc part/total.
- Default Date header aligns with post generation time.
- `postDate` can override Message-ID timestamp, Date header, and NZB timestamps.
- `postHeaders.Subject` can be customized and can fully obfuscate the Subject.
- `postHeaders.Message-ID` can be customized.
- `yencName` can customize the `name=` field inside the yEnc BODY header.

## Nyuu Identification Rules

### Nyuu Default Message-ID Regex

```regex
^<?[A-Za-z]{24}-(\d{13})@nyuu>?$
```

Extract:

```text
msgid_random_prefix
msgid_epoch_ms
msgid_domain = nyuu
```

Normalize to:

```text
msgid_timestamp_utc = epoch_ms / 1000
```

### Optional Supporting Signals

```text
User-Agent: Nyuu/<version>
Subject contains yEnc part pattern
Date header within expected range of Message-ID timestamp
From consistent across candidate
Newsgroups overlap
Lines and X-Received-Bytes consistent
```

Do not require `User-Agent`; uploaders may omit it.

## Nyuu Cases

### Case N1: Default Nyuu Subject, Default Nyuu Message-ID

Subject exposes filename and part order.

Example:

```text
Subject: [67/81] - "9723650626164422.7z.067" yEnc (296/366) 262144000
Message-ID: <VmQuDcOlRfOxMnWcWvQuCcHj-1782492955048@nyuu>
```

Policy:

```text
Use Subject for grouping and ordering.
Use Message-ID timestamp as secondary consistency evidence.
Do not queue recover_yenc unless suspicious.
```

Result:

```text
detected_tool = nyuu
detected_case = nyuu_default_subject
grouping_method = subject_filename
ordering_method = subject_part
verification_method = subject_parse
confidence = strong
```

### Case N2: Obfuscated Subject, Default Nyuu Message-ID

Subject is random or unrelated, but Message-ID matches Nyuu default timestamp format.

Policy:

```text
Group candidate by:
- same group or overlapping posted groups
- same From/poster when stable
- same Message-ID domain @nyuu
- timestamp proximity
- similar Lines / X-Received-Bytes
- Date header sanity

Order candidate by:
- Message-ID epoch-ms ascending

Validate with sampled yEnc:
- same yEnc name=
- same total=
- sampled part numbers match timestamp-rank order
- ypart offsets are consistent
```

Result if validation passes:

```text
detected_tool = nyuu
detected_case = nyuu_obfuscated_subject_default_msgid
grouping_method = nyuu_timestamp_candidate
ordering_method = msgid_epoch_ms
verification_method = sampled_yenc
confidence = high
```

Result if validation fails:

```text
detected_tool = nyuu
detected_case = nyuu_obfuscated_subject_default_msgid_failed_validation
grouping_method = unresolved
ordering_method = unresolved
next_action = deeper_yenc_or_full_recovery
```

### Case N3: Nyuu with postDate Override

`postDate` can cause timestamps to be artificial. Detect this when:

```text
many articles have identical or suspiciously flat Message-ID timestamps
Message-ID timestamp does not line up with Date header or X-Trace
timestamp ordering fails sampled yEnc
```

Policy:

```text
Do not trust Message-ID timestamp ordering.
Use Subject if explicit.
Otherwise use yEnc recovery.
```

Result:

```text
detected_tool = nyuu
detected_case = nyuu_postdate_override_suspected
ordering_method = none
next_action = subject_parse_or_yenc_recovery
```

### Case N4: Nyuu Custom Message-ID

If Message-ID does not match the default Nyuu timestamp regex:

```text
Do not infer order from Message-ID.
Use Subject if available.
Otherwise use sampled/full yEnc.
```

Result:

```text
detected_tool = nyuu_or_unknown
detected_case = nyuu_custom_msgid_or_unknown
ordering_method = none
next_action = subject_parse_or_yenc_recovery
```

### Case N5: Nyuu Interleaved Multi-File Candidate

If the same timestamp window contains multiple files/binaries:

```text
Do not assume one continuous timestamp sequence equals one binary.
Split by explicit Subject if available.
If Subject is obfuscated, sample yEnc across suspected clusters.
```

Use yEnc `name=` and `total=` to split.

---

# ngPost Policy

## ngPost Behavior Summary

ngPost article obfuscation can make the article header intentionally unsearchable.

Known behavior:

- Obfuscated article Subject becomes UUID-like.
- Message-ID local part is the same UUID-like value.
- From may be random.
- Message-ID suffix defaults to `ngPost` but can be configured.
- Article size often defaults to 716800 bytes.
- yEnc BODY contains the authoritative `part`, `total`, `size`, and `name`.
- ngPost creates yEnc parts sequentially while reading the file.
- ngPost posts over multiple connections/threads, so server acceptance order and provider article numbers are not guaranteed to match yEnc part order.
- Article numbers differ by provider.

## ngPost Identification Rules

### Strong ngPost Article-Obfuscation Signals

```text
Subject equals Message-ID local part
Subject parses as UUID-like
Message-ID local part parses as UUID-like
UUID version appears v4/random
Message-ID suffix is @ngPost or a stable configured suffix
From looks random or has same configured suffix
X-Received-Bytes / Lines near expected yEnc article size
```

Example:

```text
Subject: {caa0f2d8-8e9c-4c9a-8a1f-1b9a9b9d4e30}
Message-ID: <{caa0f2d8-8e9c-4c9a-8a1f-1b9a9b9d4e30}@ngPost>
```

### ngPost UUID Regex

Accept both braced and non-braced UUIDs:

```regex
^\{?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\}?$
```

Extract UUID version from the first hex digit of the third group.

```text
4xxx = UUID v4/random
```

If UUID v4:

```text
ordering_signal = none
```

## ngPost Cases

### Case G1: ngPost Non-Obfuscated Subject

If Subject exposes filename and yEnc part/total, use Subject.

Result:

```text
detected_tool = ngpost_or_unknown
detected_case = explicit_subject
grouping_method = subject_filename
ordering_method = subject_part
verification_method = subject_parse
confidence = strong
```

### Case G2: ngPost Full Article Obfuscation

Subject and Message-ID are UUID-like. No filename or part data in XOVER.

Policy:

```text
Build weak candidate by:
- same group or overlapping groups
- close Date window
- same Message-ID suffix when present
- same From suffix when present
- similar Lines / X-Received-Bytes
- article proximity only as provider-local hint

Do not order by Message-ID.
Do not rely on article number as cross-provider order.
Use sampled yEnc to establish membership.
Use full/deeper yEnc if ordering cannot be proven.
```

Result before validation:

```text
detected_tool = ngpost
detected_case = ngpost_full_obfuscation
grouping_method = header_weak_candidate
ordering_method = none
verification_method = pending_sampled_yenc
confidence = weak/medium
```

### Case G3: ngPost Candidate with Single yEnc Name and Validated Local Order

If sampled yEnc shows:

```text
same name=
same total=
sampled parts are monotonic under candidate's selected ordering
ypart offsets are consistent
```

Then the candidate may be promoted.

However, for ngPost, promotion should be stricter than Nyuu because Message-ID UUIDs do not provide an order key.

Possible ordering hypotheses:

```text
provider_article_number
Date + article_number
queue/discovery order
```

These are weak. Keep provenance.

Result:

```text
detected_tool = ngpost
detected_case = ngpost_sampled_validated
grouping_method = sampled_yenc_name
ordering_method = provider_article_number_hypothesis
verification_method = sampled_yenc
confidence = medium
```

For critical/high-quality releases, prefer deeper recovery.

### Case G4: ngPost Candidate with Mixed yEnc Names

If sampled yEnc finds multiple names or totals in one candidate:

```text
Split candidate by yEnc name/total.
Queue unresolved articles for deeper yEnc recovery.
Do not promote the mixed candidate.
```

Result:

```text
detected_tool = ngpost
detected_case = ngpost_mixed_candidate
grouping_method = split_required
next_action = deeper_yenc
confidence = rejected
```

### Case G5: ngPost Ordering Cannot Be Proven

If all samples share the same `name=` but sampled part numbers are not monotonic under article-number or date ordering:

```text
Membership is likely.
Ordering is unknown.
Need yEnc for all articles or enough yEnc probes to build a complete part→message-id map.
```

Result:

```text
detected_tool = ngpost
detected_case = ngpost_membership_only
grouping_method = sampled_yenc_name
ordering_method = unresolved
next_action = full_or_deeper_yenc_mapping
confidence = partial
```

---

# Candidate Confidence Model

Use explicit evidence fields instead of one opaque boolean.

Suggested fields:

```text
detected_tool
detected_tool_confidence
detected_case
grouping_method
ordering_method
verification_method
confidence_score
confidence_reason
needs_yenc_recovery
needs_yenc_sampling
needs_full_yenc_mapping
```

Suggested confidence levels:

```text
100 = direct NZB authoritative
95  = full yEnc authoritative
90  = explicit Subject part/total parsed
80  = Nyuu default Message-ID timestamp + sampled yEnc validation
65  = ngPost sampled yEnc membership + ordering hypothesis validated
50  = weak header candidate only
30  = mixed/ambiguous candidate
0   = rejected
```

Do not allow final NZB generation unless one of these is true:

```text
direct NZB authoritative
full yEnc mapping available
explicit Subject part mapping available
Nyuu timestamp mapping sampled and validated above threshold
ngPost sampled mapping passes configured risk threshold
```

---

# Sampling Policy

## Default Sample Positions

For each candidate binary:

```text
first 3 articles
middle 3 articles
last 3 articles
plus random samples
```

Scale by candidate size:

```text
< 20 articles       → probe all or most
20–200 articles     → 5–8 probes
200–1000 articles   → 8–16 probes
1000+ articles      → 16–32 probes
```

## Required Sample Checks

Every sampled yEnc probe must check:

```text
name=
part=
total=
size=
ypart begin=
ypart end=
```

Validation rules:

```text
all samples must share expected yEnc name for a binary
all samples must share total
part numbers must be monotonic under selected ordering method
ypart begin/end should match expected offsets for part and article size
sampled final part should be smaller or equal to normal article size if applicable
```

## Failure Actions

```text
mixed name= values      → split candidate
mixed total= values     → split or reject candidate
ordering violation      → deeper yEnc recovery
missing yEnc header     → mark article suspect; do not use as proof
BODY failure            → retry by policy, then defer
```

---

# Implementation Plan for Codex

## Phase 1: Add Header Classifier

Implement a classifier that parses XOVER/HEAD fields into structured evidence.

Input fields:

```text
subject
message_id
from
newsgroups
date
lines
bytes / x_received_bytes
article_number
```

Output fields:

```text
detected_tool
detected_case
msgid_local
msgid_domain
msgid_epoch_ms
msgid_uuid
msgid_uuid_version
subject_part
subject_total
subject_filename
subject_size
poster_suffix
date_utc
```

## Phase 2: Add Nyuu Rules

Implement:

```text
nyuu_default_msgid_regex
nyuu_subject_yenc_regex
nyuu_timestamp_sanity_check
nyuu_timestamp_ordering
nyuu_postdate_override_detection
```

Expected behavior:

```text
Default Subject → use Subject ordering.
Obfuscated Subject + default Message-ID → order by msgid_epoch_ms, sample yEnc.
Custom Message-ID → no Message-ID ordering.
```

## Phase 3: Add ngPost Rules

Implement:

```text
uuid_subject_msgid_match
uuid_version_extract
ngpost_suffix_detection
ngpost_article_obfuscation_classifier
```

Expected behavior:

```text
UUID v4 Message-ID → not orderable.
Use weak grouping + yEnc sampling.
If ordering not proven, recover deeper/full yEnc mapping.
```

## Phase 4: Candidate Builder

Build candidates by tier:

```text
1. Explicit Subject candidates
2. Nyuu timestamp candidates
3. ngPost weak candidates
4. Unknown weak candidates
```

Candidate grouping should consider:

```text
newsgroup overlap
date/time window
message-id suffix
poster suffix
bytes/lines similarity
subject family if available
```

## Phase 5: Verification and Promotion

Add promotion states:

```text
candidate_unverified
candidate_sampled_valid
candidate_sampled_membership_only
candidate_mixed_split_required
candidate_full_yenc_required
binary_complete
binary_archived
```

Promote only when the selected ordering method has enough proof.

## Phase 6: Metrics

Track:

```text
classified_nyuu_default
classified_nyuu_obfuscated_default_msgid
classified_ngpost_uuid
classified_unknown_obfuscated

subject_only_binaries_created
nyuu_timestamp_binaries_created
ngpost_sampled_binaries_created
full_yenc_binaries_created

yenc_probes_saved_estimate
sample_validation_failures
false_candidate_splits
candidate_confidence_distribution
```

## Phase 7: Tests

Add fixtures for each case below.

### Test N1: Nyuu Default Subject

Input:

```text
Message-ID: <VmQuDcOlRfOxMnWcWvQuCcHj-1782492955048@nyuu>
Subject: [67/81] - "9723650626164422.7z.067" yEnc (296/366) 262144000
Date: Fri, 26 Jun 2026 16:55:55 GMT
```

Expected:

```text
detected_tool = nyuu
detected_case = nyuu_default_subject
ordering_method = subject_part
needs_yenc_recovery = false
```

### Test N2: Nyuu Obfuscated Subject with Default Message-ID

Input:

```text
Message-ID: <VmQuDcOlRfOxMnWcWvQuCcHj-1782492955048@nyuu>
Subject: random-string
```

Expected:

```text
detected_tool = nyuu
detected_case = nyuu_obfuscated_subject_default_msgid
ordering_method = msgid_epoch_ms
needs_yenc_sampling = true
```

### Test N3: Nyuu postDate Override Suspected

Input:

```text
Many articles have identical or non-monotonic msgid_epoch_ms.
Sampled yEnc part order does not match timestamp order.
```

Expected:

```text
detected_case = nyuu_postdate_override_suspected
ordering_method = none
needs_full_yenc_mapping = true
```

### Test G1: ngPost UUID v4

Input:

```text
Subject: {uuid-v4}
Message-ID: <{same-uuid-v4}@ngPost>
```

Expected:

```text
detected_tool = ngpost
detected_case = ngpost_full_obfuscation
ordering_method = none
needs_yenc_sampling = true
```

### Test G2: ngPost Mixed Candidate

Input:

```text
Same time/suffix candidate, but sampled yEnc returns multiple name= values.
```

Expected:

```text
candidate_state = candidate_mixed_split_required
needs_deeper_yenc = true
```

### Test E1: Provider Article Number Mismatch

Input:

```text
Same Nyuu Message-IDs from two providers have different article numbers.
```

Expected:

```text
ordering_method = msgid_epoch_ms
article_number ignored for ordering
```

### Test E2: Direct NZB Import

Input:

```text
Valid NZB file with segment numbers and message IDs.
```

Expected:

```text
grouping_method = direct_nzb
ordering_method = nzb_segment_number
skip_recover_yenc = true
```

---

# Wiki Synopsis

Nyuu and ngPost behave differently enough that GoNZB should not treat all obfuscation the same.

Nyuu default Message-IDs contain an epoch-millisecond timestamp. If the Subject is obfuscated but the Message-ID still matches Nyuu's default format, GoNZB can sort candidate articles by Message-ID timestamp and validate the order with a small number of yEnc probes. This can dramatically reduce recovery work.

ngPost full article obfuscation uses random UUID-like Subjects and Message-IDs. These are stable identifiers but not sortable. For ngPost, GoNZB can use header evidence to build weak candidates, but yEnc sampling or full yEnc recovery is still required to prove membership and ordering.

Provider article numbers must never be treated as authoritative because they differ across providers and reflect local acceptance order. They are only a weak local hint.

The desired policy is:

```text
Use explicit Subject parts when available.
Use Nyuu Message-ID timestamp ordering when default Nyuu format is detected.
Use yEnc sampling to validate shortcuts.
Use full yEnc mapping when order cannot be proven.
Archive completed NZBs and purge disposable article/binary details after archival.
```

---

# Acceptance Criteria

Codex should consider this work successful when:

```text
Nyuu default Subject posts bypass recover_yenc.
Nyuu obfuscated default Message-ID posts use timestamp ordering plus sampled yEnc validation.
ngPost UUID posts are never ordered by Message-ID.
Provider article number is never treated as authoritative.
Confidence/provenance fields explain why a binary was promoted.
Ambiguous candidates fall back to deeper/full yEnc recovery.
Metrics show yEnc probes saved and validation failures.
Tests cover Nyuu, ngPost, direct NZB, provider article-number mismatch, and mixed candidates.
```
