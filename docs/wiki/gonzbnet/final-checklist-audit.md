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
- signed validation-request task admission for locally cached manifests;
- article availability and checksum attestation projections;
- privacy boundaries that keep local users, API keys, searches, grabs,
  downloads, and NNTP credentials local.

Validation request boundary:

- `POST /gonzbnet/v1/validation/request` is a signed node-to-node HTTP request,
  not a signed append-only federation event.
- It admits validation work only for manifests already cached locally.
- The requester must be an active pool member, the signature must match
  `requesting_node_id`, and an optional `target_node_id` must match the local
  node.

Deferred operational work:

- A full autonomous distributed scanner loop remains future scanner-module work.
  Current GoNZBNet code provides the signed metadata, coverage coordination, and
  scheduler suggestion surfaces that such a worker would consume.
