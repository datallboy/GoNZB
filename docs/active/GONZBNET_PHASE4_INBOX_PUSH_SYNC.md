# GoNZBNet Phase 4 Inbox Push Sync

Scope:

- Add signed node-to-node request authentication for protected federation
  endpoints.
- Add nonce replay protection.
- Add `POST /gonzbnet/v1/inbox`.
- Accept single event or `EventBatch` request bodies.
- Return structured accepted/duplicate/rejected batch results.
- Store invalid remote events in `federation_rejected_events`.
- Project accepted ReleaseCards into the federated cache tables.
- Track peer delivery status for push sync.
- Add a push sync service that sends accepted local ReleaseCard events to
  manually configured peers.

Out of scope:

- Pool membership authorization.
- User/RBAC federated search integration.
- Resolution manifests.
- WebSocket gossip.

Privacy boundary:

- Push sync signs as the local node.
- It sends signed events only.
- It does not send local users, API keys, search history, grab history,
  download history, or downloader identity.
