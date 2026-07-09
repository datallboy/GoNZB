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
- Phase 11: relay-ready modular-monolith controls.
- Addendum Phase A: capability registry and module switches.
- Addendum Phase B: validation-only contribution.
- Addendum Phase C: scan-without-index contribution.
- Addendum Phase D: coverage events and manual assignments.
- Addendum Phase E: dedup-aware local scheduler.
- Addendum Phase F: automated coverage planning helpers.
- Addendum Phase G: addendum checklist gaps.
- Addendum Phase H: admin UI.
- Addendum Phase I: admin diagnostics.
- Addendum Phase J: pool and moderation UI.
- Addendum Phase K: peer management.
- Addendum Phase L: RBAC alignment.
- Addendum Phase M: source and health diagnostics.
- Addendum Phase N: node profile and config admin.
- Addendum Phase O: force resolve manifest.
- Addendum Phase P: remove peer.

Spec-required cleanups after the named phases are committed directly on
`feature/gonzbnet` unless a cleanup becomes large enough to deserve its own
phase branch.

Each phase is implemented on a branch from `feature/gonzbnet`, committed,
merged back to `feature/gonzbnet`, and followed by the next phase branch.
