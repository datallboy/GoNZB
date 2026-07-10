# Implementation Status

GoNZBNet has a broad working baseline, but it is not yet complete against the
implementation specification. Core phase code and optional-module scaffolding
exist; the remaining work is concentrated in authorization, receive-pipeline
correctness, complete contribution behavior, and end-to-end verification.

Completed during this audit:

- pool-level get/resolve authorization now runs before shared blob-cache reads;
- shared aggregator cache rows are no longer used for GoNZBNet search results.
- typed event bodies are validated before accepted storage, including
  ReleaseCard identity recomputation and private-field rejection;
- pool-member capability grants and limits are signed in approval events.
- protected federation reads use signed node requests and pool visibility;
- pull synchronizes every supported event type, while pull, push, and gossip
  filter pool events for the remote node.
- RFC 8785 canonicalization is covered by direct vectors, and federation receive
  boundaries reject duplicate JSON object names before decoding.

The current required work is:

- make accepted append plus projection transactional so database projection
  failures cannot leave an accepted, unprojected event;
- build local manifests when the manifest-builder module is enabled;
- perform configured validator tiers against the local NNTP provider;
- emit scanner capacity, heartbeat, observations, and periodic checkpoints;
- enforce manifest-cache retention/serving limits;
- enforce event-chain continuity and advertise only implemented capabilities;
- add PostgreSQL-backed three-node end-to-end coverage and GoNZBNet metrics.

The standalone relay process is not on this list. The specification explicitly
keeps v1 in the modular monolith and lists a separate relay process as future
work.

Detailed implementation evidence and ordering are maintained in
`docs/active/GONZBNET_SPEC_COMPLETION_AUDIT.md`.
