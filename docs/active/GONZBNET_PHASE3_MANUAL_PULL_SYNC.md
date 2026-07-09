# GoNZBNet Phase 3 Manual Pull Sync

Scope:

- Add public read-only federation endpoints:
  - `GET /.well-known/gonzbnet`
  - `GET /gonzbnet/v1/node`
  - `GET /gonzbnet/v1/caps`
  - `GET /gonzbnet/v1/outbox`
  - `GET /gonzbnet/v1/events/:event_id`
- Add a basic signed handshake endpoint:
  - `POST /gonzbnet/v1/handshake`
- Add manual peer config storage and peer cursor storage.
- Add a pull sync service that fetches peer well-known/node/caps/outbox pages,
  verifies remote signed events, stores accepted events, stores rejected raw
  events, and projects accepted ReleaseCards.
- Keep transport pull-based. No inbox push sync yet.
- Keep local user auth separate from node federation. Do not send local user
  accounts, API keys, search history, grab history, or download history.

Acceptance criteria:

- A manually configured peer URL can be stored.
- Local node profile/caps/outbox/event endpoints are available when GoNZBNet is
  enabled.
- Pull sync fetches ReleaseCard events from a peer outbox.
- Invalid signatures are rejected and recorded.
- Duplicate accepted events are ignored through event ID uniqueness.
- Accepted ReleaseCards are projected into the federated cache tables.

Out of scope:

- Inbox push sync.
- Pool membership authorization.
- Request-signature auth for protected mutating/fetch endpoints beyond the
  handshake body signature.
- RBAC/Newznab federated search integration.
- Resolution manifests.
