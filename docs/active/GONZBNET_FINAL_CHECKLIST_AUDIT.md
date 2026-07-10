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
- Article availability and checksum attestations project into validation-aware
  scores; checksum emission remains feature-flagged.
- Coverage dashboard/admin reads expose gaps, stale claims, active work, and
  duplicate work diagnostics.
- Federation request/event paths authenticate nodes, not users, and do not
  include local usernames, API keys, searches, grabs, downloads, or NNTP
  credentials.

## Remaining Explicit Gap

`POST /gonzbnet/v1/validation/request` remains intentionally unimplemented.
The implementation spec names the endpoint but does not define a concrete
request body, signed event type, idempotency key, target-node semantics, or task
admission policy.

Existing validation tasks are currently enqueued when a signed
`ResolutionManifest` is cached locally. Validator capacity and validation
attestation events flow through the signed inbox path.

## Deferred Operational Work

A full autonomous scanner execution loop is still outside the current
GoNZBNet code. The implemented surface supports scanner contribution through
ReleaseCard publication, scan-output ingestion, coverage assignments, claims,
checkpoints, outcomes, and scheduler suggestions. The actual NNTP scanner loop
that consumes those suggestions and performs distributed scan work remains a
future scanner module integration.
