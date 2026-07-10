# Canonical JSON

GoNZBNet uses RFC 8785 JSON Canonicalization Scheme bytes anywhere JSON is
signed or hashed. The `internal/gonzbnet/canonical` package is the single entry
point for event IDs, body hashes, release and manifest IDs, request signatures,
pool approvals, and checkpoints.

The package wraps the RFC 8785 reference Go implementation. Canonical output
uses ECMAScript-compatible number serialization, UTF-16 code-unit property
ordering, minimal required string escaping, and no insignificant whitespace.
Input must be valid UTF-8 and valid JSON.

Raw `json.RawMessage` and byte slices are canonicalized without first decoding
them into maps. This is required because ordinary Go JSON decoding accepts
duplicate object names and keeps one value. JCS rejects duplicates, including
equivalent escaped names such as `"a"` and `"\u0061"`.

Federation receive boundaries validate complete raw payloads before decoding:

- inbox and constrained event batches;
- signed outbox pull responses;
- signed manifest responses and requests;
- handshake and validation requests;
- WebSocket gossip batches.

After that boundary check, event verification independently canonicalizes the
raw body, compares `body_hash`, canonicalizes the unsigned envelope, compares
`event_id`, and verifies the Ed25519 signature.
