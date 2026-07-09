# GoNZBNet Wiki

GoNZBNet is a modular-monolith federation extension for GoNZB. It authenticates
nodes, not users, and keeps local GoNZB accounts, API tokens, searches, grabs,
and download history private to the home node.

Current implementation state:

- Phase 1 creates a persistent Ed25519 node identity.
- Node IDs are deterministic hashes of public keys.
- Signed events use canonical JSON bytes for signatures and event IDs.
- PostgreSQL stores accepted event canonical bytes separately from JSONB body
  projections.

Maintained pages:

- [Phase 1 Identity And Events](./phase-1-identity-and-events.md)
