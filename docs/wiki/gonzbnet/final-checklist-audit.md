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
- article availability and checksum attestation projections;
- privacy boundaries that keep local users, API keys, searches, grabs,
  downloads, and NNTP credentials local.

Remaining explicit gap:

- `POST /gonzbnet/v1/validation/request`

That endpoint is named by the addendum but lacks a concrete request schema,
signed event type, target-node behavior, idempotency contract, and validation
task admission policy. Validation work is currently admitted when a signed
ResolutionManifest is cached locally.

Deferred operational work:

- A full autonomous distributed scanner loop remains future scanner-module work.
  Current GoNZBNet code provides the signed metadata, coverage coordination, and
  scheduler suggestion surfaces that such a worker would consume.
