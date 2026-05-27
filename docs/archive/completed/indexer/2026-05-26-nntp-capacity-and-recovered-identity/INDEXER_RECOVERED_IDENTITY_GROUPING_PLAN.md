# Indexer Recovered Identity Grouping Plan

Snapshot date: 2026-05-21

This doc carries forward the remaining grouping work from the obfuscated-payload hardening sprint. The completed, non-identifying findings from that sprint now live in `docs/archive/completed/indexer/2026-05-21-obfuscated-payload-hardening/INDEXER_OBFUSCATED_PAYLOAD_FINDINGS.md`.

Do not add source-identifying payload titles, release names, subject text, poster values, newsgroup names, message IDs, or content descriptions here.

## Scope

The hardening sprint proved that importer encoding, extensionless archive extraction, yEnc backlog visibility, and PAR2 inspection observability needed fixes before broader grouping changes. Those fixes are complete enough to merge.

This plan owns the next matching problem: release formation should be able to bridge newsgroup splits when strong recovered identity proves files belong together, while preserving per-group article provenance for fetch routing.

## Current Boundary

Current release-family summaries are keyed by:

- provider id
- newsgroup id
- key kind
- family key

The main release candidate queue originally carried only concrete `newsgroup_id` rows. This sprint now adds a second candidate shape for recovered yEnc identity: provider-wide `recovered_file_set` candidates that intentionally use `newsgroup_id = 0`.

`ListBinariesForReleaseCandidate` already supported omitting the group boundary when called with `newsgroup_id = 0`; the missing work was making the candidate queue, ack path, and release newsgroup persistence treat that as a normal release-formation shape.

Concrete meaning in current code:

- `ReleaseCandidate` is currently keyed by `provider_id`, `newsgroup_id`, `key_kind`, and `family_key`.
- `AckReleaseCandidate` currently assumes one concrete `newsgroup_id` and marks only that summary row processed.
- `ReplaceReleaseNewsgroups` can already store multiple groups, but the release service was still writing only the candidate group instead of the full cluster group set.
- `ListBinariesForReleaseCandidate` already supports provider-wide selection when `newsgroup_id = 0`, so the main missing piece was candidate/ack modeling rather than binary hydration.

## Recovery Signal

Recovered yEnc filenames are stronger identity evidence than intentionally varied header subjects or posters. They can be collected from BODY-prefix recovery without full article download.

The safe grouping rule is not "similar headers across groups." The safe rule is "strong recovered identity across groups, with compatible file counts and close posting proximity."

## Guardrails

- preserve provider/newsgroup/article identity for ingest and fetch routing
- keep group provenance on binaries and releases
- do not bridge groups from poster similarity alone
- do not bridge groups from header-subject similarity alone
- require recovered yEnc identity or equivalent strong archive/PAR2 identity
- require close posting proximity
- require compatible expected file count, archive target evidence, or both
- keep promotion bounded so broad backfills do not turn into noisy global joins

## Action Items

- [x] Design a recovered-identity release candidate shape that can represent cross-group candidates explicitly.
  - Implemented as provider-wide `key_kind='recovered_file_set'` candidates with `newsgroup_id = 0` and `family_key = file_set_key`.
- [x] Define how the candidate ack path handles groupless recovered-identity candidates.
  - Implemented by acking the underlying per-group `release_family` and `base_stem` readiness rows that participate in the recovered file set.
- [x] Add query support for selecting recovered-identity file sets across groups without weakening normal per-group release summaries.
  - Implemented in `ListReleaseCandidates` and `ListBinariesForReleaseCandidate` using recovered `file_set_key` scope while leaving the existing per-group summary queue intact.
- [x] Add a synthetic multi-group recovered-yEnc grouping fixture with randomized header posters and subjects.
  - Implemented in release-store and release-service tests with two-group recovered file-set fixtures.
- [x] Implement bounded cross-group recovered-identity promotion.
  - Current bound is intentionally narrow: only `recovered_source='yenc_header'`, non-empty `file_set_key`, more than one newsgroup, more than one main-payload binary, expected file count evidence, and a posting span within 24 hours.
- [x] Confirm `release_newsgroups` records all participating groups after promotion.
  - Release service now persists all unique cluster newsgroups instead of only the candidate group.
- [x] Confirm internal NZB export remains UTF-8, deterministic, and includes the full release group set while preserving file/article membership accuracy.
  - Verified with catalog/export regressions that `release_newsgroups` round-trips into NZB file group lists and that file/article ordering remains stable.

## Downloader Filename Follow-Up

The downloader now handles extensionless archive payloads by signature, but yEnc header filename adoption remains a separate design problem. Segment workers already write to preallocated task paths by the time yEnc headers are parsed, so adopting recovered names requires a coordinated task/path transition rather than an unsynchronized worker-side rename.

Decision:

- [x] Defer downloader yEnc filename adoption unless future samples prove signature-based extraction is not enough.
  - Current downloader behavior already handles the practical failure mode that triggered this sprint: extensionless/obfuscated archives can be detected and extracted by signature after download.
  - Renaming files mid-download from recovered yEnc names would require rewriting `DownloadFile` task paths, `.part` paths, queue-file persistence, completed-file moves, and post-processing references in one coordinated transition. That is a downloader workflow change, not an indexer grouping prerequisite.

## Sign-Off Checklist

Needs completion:

- [x] Cross-group recovered-identity promotion is implemented behind the guardrails above.
- [x] Synthetic fixtures prove varied headers can still group when recovered identity is strong.
- [x] NZB export preserves multi-group provenance after promotion.
- [x] Downloader yEnc filename adoption is either implemented safely or explicitly deferred with evidence that signature-based extraction is enough.
