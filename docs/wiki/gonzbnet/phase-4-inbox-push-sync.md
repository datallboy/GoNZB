# Phase 4 Inbox Push Sync

Phase 4 adds authenticated node-to-node push delivery for already signed local
events. It does not add remote user login, live federated searches, pool
authorization, or manifest exchange.

Endpoint:

- `POST /gonzbnet/v1/inbox` accepts a single signed event or an `EventBatch`.
- `POST /gonzbnet/v1/events/batch` is a spec-compatible alias for the same
  handler.
- The request must include `Authorization: GoNZBNet ...`.
- The response is an `InboxResponse` with separate `accepted`, `duplicate`,
  and `rejected` arrays.

Request authentication:

- The request signature covers method, path, raw query hash, body hash,
  timestamp, nonce, and node ID.
- The verifier loads the sender public key from `federation_nodes`.
- The sender node ID must match the stored public key.
- Timestamps use `gonzbnet.time_tolerance_seconds`.
- Nonces are stored in `federation_nonce_replay_cache` and rejected on replay.

Inbox behavior:

1. Verify the node request signature.
2. Decode the batch.
3. Check whether each event ID already exists.
4. Verify each event's body hash, event ID, author key, and Ed25519 signature.
5. Reject invalid events into `federation_rejected_events`.
6. Append accepted events to `federation_events`.
7. Project accepted `ReleaseCard` events into the federated release-card cache.

Push sync behavior:

- `gonzbnet.push_sync_enabled` starts a periodic push pass.
- `gonzbnet.push_sync_interval_minutes` controls the pass interval.
- `gonzbnet.push_sync_batch_size` limits events pushed to a peer per pass.
- The service sends accepted local `ReleaseCard` events that do not have an
  accepted or duplicate delivery record for that peer.
- Failed or rejected delivery records are retried after a small
  attempt-count-based backoff.
- Delivery attempts are stored in `federation_peer_deliveries`.

Privacy boundary:

- Push sync sends node identity and signed federation events only.
- It does not send local usernames, API keys, search history, grab history,
  download history, or downloader state.
- Existing local RBAC and API-key controls still govern local Newznab API
  access.
