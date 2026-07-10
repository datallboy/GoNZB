# Implementation Status

GoNZBNet has a broad working baseline, but it is not yet complete against the
implementation specification. Core phase code and optional-module scaffolding
exist; the remaining work is concentrated in authorization, receive-pipeline
correctness, complete contribution behavior, and end-to-end verification.

Completed during this audit:

- pool-level get/resolve authorization now runs before shared blob-cache reads;
- shared aggregator cache rows are no longer used for GoNZBNet search results.

The current required work is:

- validate typed event bodies before accepted storage and keep projection
  failures quarantined;
- authenticate pool-scoped read endpoints and apply pool visibility to outbox
  and event reads;
- make manual pull synchronize and project all supported event types;
- build local manifests when the manifest-builder module is enabled;
- perform configured validator tiers against the local NNTP provider;
- emit scanner capacity, heartbeat, observations, and periodic checkpoints;
- enforce manifest-cache retention/serving limits;
- prove canonical JSON behavior against RFC 8785 and reject duplicate keys;
- enforce event-chain continuity and advertise only implemented capabilities;
- add PostgreSQL-backed three-node end-to-end coverage and GoNZBNet metrics.

The standalone relay process is not on this list. The specification explicitly
keeps v1 in the modular monolith and lists a separate relay process as future
work.

Detailed implementation evidence and ordering are maintained in
`docs/active/GONZBNET_SPEC_COMPLETION_AUDIT.md`.
