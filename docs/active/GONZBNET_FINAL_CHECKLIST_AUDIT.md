# GoNZBNet Final Checklist Audit

This audit maps the implementation addendum checklist to current code and docs.
It is not a new implementation phase; it is a current-state closure document for
remaining GoNZBNet work.

## Proven By Current Code

- Optional module switches are typed config fields under `gonzbnet` and are
  reflected in NodeProfile capability/module-status output.
- Scanner publication can be enabled independently from index projection.
- Validator worker config and validation task processing do not require the
  local usenet-indexer module.
- Consumer/search can run through the GoNZBNet aggregator source without scanner
  or validator modules enabled.
- NodeProfile advertises capabilities, module status, scanner capacity,
  validator capacity, and privacy-preserving provider scope.
- Pool approvals include `allowed_capabilities` and `limits_json`.
- Event acceptance checks required pool capabilities for signed event types.
- Group observations project into the coverage group catalog.
- Coverage plans and assignments can be created by local admin APIs.
- Range claims, time-window claims, range completion, and validation overlap are
  handled by the dedup-aware coverage suggestion path.
- Stale claims can be detected and materialized as reputation penalties.
- Existing indexer scrape ranges can publish local signed claims/outcomes and
  honor trusted, provider-scope-compatible remote active/completed ranges when
  scanner coverage coordination is enabled.
- Existing range `CoverageAssignment` suggestions can be consumed automatically
  by the local scrape loop without advancing latest/backfill cursors.
- Existing time-window `CoverageAssignment` suggestions can be resolved locally
  to article ranges, claimed with `TimeWindowClaim`, and consumed without
  advancing latest/backfill cursors.
- Stale article range claims can create signed replacement
  `CoverageAssignment` events when automatic coverage mode is enabled.
- Article availability and checksum attestations project into validation-aware
  scores; checksum emission remains feature-flagged.
- Signed validation requests can enqueue local validation tasks for manifests
  that are already cached locally.
- Coverage dashboard/admin reads expose gaps, stale claims, active work, and
  duplicate work diagnostics.
- Federation request/event paths authenticate nodes, not users, and do not
  include local usernames, API keys, searches, grabs, downloads, or NNTP
  credentials.

## Validation Request Boundary

`POST /gonzbnet/v1/validation/request` is implemented as a signed node-to-node
HTTP request, not a signed append-only federation event. The request is
admitted only when the requester is an active pool member, the request signature
matches `requesting_node_id`, an optional `target_node_id` matches the local
node, and the referenced manifest is already cached locally.

Validator capacity and validation attestation events continue to flow through
the signed inbox path.

## Scanner Coordination Boundary

The existing usenet-indexer scrape loop can participate in GoNZBNet scanner
coordination when GoNZBNet, scanner mode, coverage mode, and unassigned scanner
work are enabled. It publishes local signed range claims/outcomes and skips
trusted, provider-scope-compatible remote active/completed ranges without
exposing user or provider credentials.

Automatic creation of replacement assignments is implemented for stale article
range and time-window claims in automatic coverage mode. Time-window assignment
execution is implemented through local article-range resolution.
