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

The main release candidate queue carries `newsgroup_id`, so recovered yEnc identity does not yet produce a single cross-group dirty candidate by default.

`ListBinariesForReleaseCandidate` can already omit the group boundary when called with `newsgroup_id = 0`, but the candidate queue and ack path do not yet model that as a normal candidate shape.

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

- [ ] Design a recovered-identity release candidate shape that can represent cross-group candidates explicitly.
- [ ] Define how the candidate ack path handles groupless recovered-identity candidates.
- [ ] Add query support for selecting recovered-identity file sets across groups without weakening normal per-group release summaries.
- [ ] Add a synthetic multi-group recovered-yEnc grouping fixture with randomized header posters and subjects.
- [ ] Implement bounded cross-group recovered-identity promotion.
- [ ] Confirm `release_newsgroups` records all participating groups after promotion.
- [ ] Confirm internal NZB export remains UTF-8, deterministic, and includes the full release group set while preserving file/article membership accuracy.

## Downloader Filename Follow-Up

The downloader now handles extensionless archive payloads by signature, but yEnc header filename adoption remains a separate design problem. Segment workers already write to preallocated task paths by the time yEnc headers are parsed, so adopting recovered names requires a coordinated task/path transition rather than an unsynchronized worker-side rename.

Action item:

- [ ] Design downloader yEnc filename adoption only if post-extraction signature handling is not sufficient for future samples.

## Sign-Off Checklist

Needs completion:

- [ ] Cross-group recovered-identity promotion is implemented behind the guardrails above.
- [ ] Synthetic fixtures prove varied headers can still group when recovered identity is strong.
- [ ] NZB export preserves multi-group provenance after promotion.
- [ ] Downloader yEnc filename adoption is either implemented safely or explicitly deferred with evidence that signature-based extraction is enough.
