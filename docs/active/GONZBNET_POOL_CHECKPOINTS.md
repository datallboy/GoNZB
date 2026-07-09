# GoNZBNet Pool Checkpoints

This cleanup implements the first v1 pool checkpoint path from the GoNZBNet
spec. It is not a new named phase; it closes trust-pool work that earlier phase
docs explicitly deferred.

Scope:

- Add `PoolCheckpoint` as a supported pool-control event type.
- Validate checkpoint witness signatures against active pool admins/witnesses.
- Recompute the checkpoint Merkle root from locally known accepted pool events.
- Project the latest checkpoint event ID and Merkle root onto `trust_pools`.
- Keep checkpoint processing inside the modular monolith and existing
  append-only event log.

Constraints:

- Do not introduce blockchain, consensus, or microservices.
- Do not expose local user identity, API keys, searches, grabs, or downloads.
- Reject checkpoints when the local node does not have the event range needed
  to recompute the Merkle root.
- Keep remote `TrustAttestation` unsupported until a separate trust-score input
  policy is implemented.
