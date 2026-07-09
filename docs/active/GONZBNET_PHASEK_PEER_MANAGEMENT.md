# GoNZBNet Phase K: Peer Management

## Spec Scope

Add local admin support for peer management actions called out by the GoNZBNet
implementation spec:

- add manual peer
- enable/disable peer
- force pull sync
- force push sync
- force WebSocket gossip pass

## Implementation Plan

1. Add pgindex store methods for enabling/disabling a federation peer.
2. Add admin controller endpoints for peer upsert, peer enable/disable, and
   one-shot sync actions using the existing GoNZBNet sync service.
3. Extend the GoNZBNet admin WebUI peer diagnostics panel with add peer,
   enable/disable, and sync action controls.
4. Document the behavior in `docs/wiki/gonzbnet/`.
5. Run UI build and Go tests.

## Out Of Scope

- Editing pinned public keys.
- Destructive peer deletion.
- Per-peer force sync.
- Runtime config mutation for `gonzbnet.manual_peers`.
