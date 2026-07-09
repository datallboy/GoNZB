# Phase 1 Identity And Events

Phase 1 implements only the local cryptographic foundation for GoNZBNet.

Node identity:

- The local node key is an Ed25519 private key stored in `gonzbnet.keys_dir`.
- The node ID is `node_<base32nopad(sha256(raw_public_key))>`.
- The private key is local node material and is never sent to peers.

Signed events:

- Events use `spec_version: gonzbnet/1.0`.
- `body_hash` is `sha256:<base64urlnopad(sha256(canonical_body_json))>`.
- `event_id` is `evt_<base32nopad(sha256(canonical_unsigned_event_json))>`.
- The Ed25519 signature covers the canonical unsigned event payload.
- The unsigned payload excludes `event_id` and `signature`.

Storage:

- `federation_nodes` stores known node public keys and profile metadata.
- `federation_events` stores accepted or locally verified events.
- `canonical_event_json` stores the exact signed canonical bytes.
- `body_json` stores a queryable JSONB projection and must not be used as the
  signature source.
- `federation_rejected_events` stores raw rejected input for later inspection.

Privacy boundary:

- Phase 1 has no peer transport.
- No local usernames, API keys, search history, grab history, or download
  history are included in event primitives.
