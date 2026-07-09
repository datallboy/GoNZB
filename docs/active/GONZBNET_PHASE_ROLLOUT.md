# GoNZBNet Phase Rollout

This document tracks implementation chunks against
`docs/GoNZBNet_Codex_Implementation_Spec.md`.

- Phase 1: identity, canonical signed events, event storage.
- Phase 2: local indexer ReleaseCard publishing and projection.
- Phase 3: manual federation pull sync, public node/caps/outbox/event endpoints,
  peer config, cursor sync, remote event verification, and rejection storage.
- Phase 4: inbox push sync and signed mutating node requests.
- Phase 5: local RBAC and aggregator/Newznab federated cache integration.
- Phase 6: trust pools and M-of-N membership validation.
- Phase 7: resolution manifests and local NZB generation from manifests.
- Phase 8: health attestations and scoring.
- Phase 9: moderation and tombstones.
- Phase 10: WebSocket gossip and peer exchange.

Each phase is implemented on a branch from `feature/gonzbnet`, committed,
merged back to `feature/gonzbnet`, and followed by the next phase branch.
