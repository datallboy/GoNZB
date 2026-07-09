# Phase 3 Manual Pull Sync

Phase 3 adds pull-based federation between manually configured GoNZBNet peers.
It does not add inbox push sync, pool authorization, RBAC search integration,
or manifest fetching.

Public endpoints:

- `GET /.well-known/gonzbnet` returns the local node ID, public key, and
  GoNZBNet base URL.
- `GET /gonzbnet/v1/node` returns the public node profile and capabilities.
- `GET /gonzbnet/v1/caps` returns supported protocol versions, event types,
  encodings, compression types, transports, and size limits.
- `GET /gonzbnet/v1/outbox` returns accepted local signed events with an opaque
  event cursor.
- `GET /gonzbnet/v1/events/:event_id` returns one accepted event.
- `POST /gonzbnet/v1/handshake` verifies a signed handshake body and returns a
  signed handshake response.

Manual peers:

- Peer URLs are configured with `gonzbnet.manual_peers`.
- Runtime build upserts those URLs into `federation_peers`.
- Pull sync is enabled separately with `gonzbnet.pull_sync_enabled`.
- Cursors are stored in `federation_peer_cursors` per peer and event type.

Pull sync flow:

1. Fetch peer `/.well-known/gonzbnet`.
2. Fetch peer `/node` and `/caps`.
3. Validate that node ID matches the peer public key.
4. Send a signed handshake request.
5. Pull `/outbox?type=ReleaseCard&since=<cursor>`.
6. Verify each event's body hash, event ID, author key, and signature.
7. Append accepted events to `federation_events`.
8. Store invalid events in `federation_rejected_events`.
9. Project accepted ReleaseCards into `federated_release_cards` and
   `federated_release_sources`.
10. Advance the peer cursor.

Privacy boundary:

- Pull sync sends node identity only.
- It does not send local usernames, API keys, search history, grab history,
  download history, or downloader identity.
- Search remains local cache based; no live user search broadcast exists in
  Phase 3.
