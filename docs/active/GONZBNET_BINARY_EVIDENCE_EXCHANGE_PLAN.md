# GoNZBNet Binary Evidence Exchange

## Summary

Use direct, authenticated peer requests rather than pool-wide events. Delivery
is phased:

1. Share recovered yEnc headers by exact Message-ID to avoid duplicate BODY
   requests.
2. Share missing binary segments to complete binaries and releases without
   additional NNTP requests.

Current local `binary_id` and `binary_key` values are not portable between
nodes. Introduce versioned cross-node identities for matching; use exact
Message-IDs as the authoritative segment identity.

## Protocol and Trust

- Add the pool capability `binary_evidence_exchange`.
- Add signed endpoints under `/gonzbnet/v1`:
  - `POST /evidence/yenc/query`
  - `POST /evidence/segments/query`
- Authenticate requests with existing request signing and active pool
  membership checks.
- Return an RFC 8785 canonicalized, Ed25519-signed evidence bundle bound to the
  pool, request, recipient, source node, creation time, and expiry.
- Require both pool policy and local serving permission. Existing pools default
  to disabled.
- Accept one valid authorized source, record provenance, and quarantine
  conflicts.
- Serve only evidence acquired locally through XOVER or BODY. Never re-serve
  imported evidence in v1.
- Keep requests targeted; do not publish Message-IDs or missing-part lists into
  the append-only event stream.
- Treat provider, newsgroup, and article-number ranges as optional locator hints
  only. Article numbers are provider-specific and never establish identity.

## Evidence and Identity

- Persist raw yEnc evidence separately from recovery work state:
  - Message-ID and source date
  - decoded filename
  - part number and total parts
  - file size
  - part offsets
  - `local_body` or `peer` provenance
  - source pool, node, bundle, and acceptance state
- A peer yEnc response contains only raw decoded header facts. The receiving
  node reruns its own matcher and grouping logic.
- Add portable, versioned binary match identities:
  - `subject_multipart_v1`: canonical subject stem, filename, file index/count,
    total parts, and declared size.
  - `yenc_v1`: decoded filename, total parts, and decoded file size.
- Generate IDs with canonical JSON and SHA-256. Exclude provider, group, article
  number, and local database IDs.
- Treat match IDs as candidate locators. Require a unique candidate plus either
  a shared Message-ID anchor or compatible poster/family/time evidence; reject
  ambiguous matches.
- Once complete, calculate `binary_content_id` from the ordered
  `(part_number, message_id)` list.
- Store imported segments separately from immutable `article_headers` and
  locally scraped `binary_parts`.
- An imported segment contains part/total, Message-ID, bytes, posting time,
  observed groups, file descriptor, signed source, and bundle ID.
- Effective-part reads prefer local parts, then accepted peer parts. Peer parts
  count immediately toward completion, release formation, inspection, and NZB
  generation.

## Recovery Flow

- yEnc recovery order:
  1. Check local raw-evidence cache.
  2. Query up to three eligible peers in batches.
  3. Validate, persist, and apply peer hits through the local matcher.
  4. Issue BODY requests only for unresolved Message-IDs.
- Missing-segment repair:
  1. Queue incomplete binaries having a strong match identity.
  2. Send compact missing-part ranges and up to eight distributed known
     Message-ID anchors.
  3. Import only requested, signed segments.
  4. Recalculate completion and enqueue affected release families.
- Peer misses, timeouts, disabled exchange, or ambiguous identities fall
  through to existing NNTP behavior without failing or delaying the pipeline.
- Do not add a refresh-only supervisor stage. Run repair as a real, retryable
  GoNZBNet worker enabled only when both GoNZBNet and the indexer are active.

## Storage and Query Shape

- Add:
  - `yenc_header_evidence`
  - `binary_exchange_identities`
  - `binary_peer_segments`
  - `binary_evidence_repair_work_items`
- Partition evidence with its source date where retention volume requires it.
- Add bounded batch indexes for `(source_date, message_id)`,
  `(scheme, match_id)`, and `(binary_id, part_number, status)`.
- Centralize effective-part DBO reads. Change completion, release/NZB,
  inspection, and yEnc-admission consumers; keep regrouping, partition
  maintenance, and scrape ownership on local tables.
- Purge evidence with its owning article/binary retention. Retain bounded signed
  exchange diagnostics for 90 days.
- Use bulk lookups and writes; prohibit per-Message-ID or per-part N+1 queries.

## Settings and Observability

Runtime defaults:

- Consume evidence: enabled
- Serve evidence: disabled
- Pool exchange policy: disabled
- Peer timeout: 3 seconds
- Peer fanout: 3
- yEnc request batch: 1,000 Message-IDs
- Segment response limit: 5,000 parts
- Response-size limit: 10 MiB
- Failed-peer circuit-breaker cooldown: 5 minutes

Expose settings in the GoNZBNet runtime-settings UI. Add role activity and
metrics for cache hits, peer requests, BODY requests avoided, imported parts,
completed binaries/releases, latency, timeouts, response bytes, conflicts,
quarantines, and source-node provenance. Normal logs and overview screens must
not reveal Message-IDs.

## Delivery Order

1. Add protocol identity and golden-vector tests.
2. Implement local raw yEnc persistence and exact Message-ID peer exchange.
3. Integrate peer lookup before BODY and verify avoided requests.
4. Add portable binary identities and missing-segment repair.
5. Switch selected consumers to effective parts.
6. Add runtime settings, pool policy, UI reporting, documentation, and E2E
   coverage.
7. Commit each phase separately.

## Test Plan

- Golden tests prove portable identities are identical across nodes and
  versions.
- Unit tests cover signatures, malformed Message-IDs, bounds, duplicates,
  conflicts, ranges, ambiguity, and local-part precedence.
- PostgreSQL tests verify evidence persistence, effective completion,
  release/NZB output, retention, indexes, and absence of writes to
  `article_headers`.
- Security tests cover replay, cross-pool access, revoked members, missing
  capabilities, disabled serving, oversized responses, and tampered bundles.
- Two-node E2E: node B has yEnc evidence; node A completes recovery with zero
  BODY requests.
- Multi-node E2E: nodes hold disjoint segments; node A completes the binary and
  produces the expected NZB without NNTP repair.
- Fallback E2E: peer miss, timeout, conflict, and ambiguity all continue through
  normal BODY/XOVER behavior.
- Restart and duplicate-delivery tests prove queue and evidence processing are
  idempotent.
- Load tests exercise 1,000-ID requests and 5,000-part responses without N+1
  queries or supervisor stalls.
