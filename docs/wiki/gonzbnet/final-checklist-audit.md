# Final Checklist Audit

The GoNZBNet addendum checklist is largely implemented through the phase and
cleanup work documented in this directory.

Implemented surfaces include:

- independent optional module switches;
- scanner-without-index-projection support;
- validator-only task processing and attestations;
- consumer/search without scanner or validator;
- capability, module-status, and capacity advertisement in NodeProfile;
- pool-approved capabilities and event acceptance gates;
- coverage observations, assignments, claims, checkpoints, outcomes, dashboard
  reads, stale-claim penalties, and dedup-aware work suggestions;
- existing indexer scrape coordination with signed local claims/outcomes and
  trusted, provider-scope-compatible remote active/completed range suppression;
- assignment-driven scanner range consumption for existing
  `CoverageAssignment` suggestions;
- assignment-driven scanner time-window consumption by resolving windows to
  article ranges locally and claiming them with `TimeWindowClaim`;
- automatic signed replacement assignment creation for stale article range
  claims when automatic coverage mode is enabled;
- signed validation-request task admission for locally cached manifests;
- article availability and checksum attestation projections;
- privacy boundaries that keep local users, API keys, searches, grabs,
  downloads, and NNTP credentials local.
- reserved live-query config is rejected so user searches remain local-cache
  based.

Validation request boundary:

- `POST /gonzbnet/v1/validation/request` is a signed node-to-node HTTP request,
  not a signed append-only federation event.
- It admits validation work only for manifests already cached locally.
- The requester must be an active pool member, the signature must match
  `requesting_node_id`, and an optional `target_node_id` must match the local
  node.

Scanner coordination boundary:

- The existing usenet-indexer scrape loop can publish signed local
  `RangeClaim`, `RangeComplete`, and `RangeFailed` events and honor trusted,
  provider-scope-compatible remote active/completed ranges when scanner
  coverage coordination is enabled.
- Existing range `CoverageAssignment` suggestions can be consumed automatically.
- Automatic creation of replacement assignments is implemented for stale article
  range and time-window claims in automatic coverage mode.
- Time-window assignment execution is implemented by resolving windows to local
  article ranges.
