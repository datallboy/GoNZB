# Pool Checkpoints

Pool checkpoints are signed pool-control events that commit to a deterministic
Merkle root over locally known accepted pool events.

Implemented behavior:

- `PoolCheckpoint` is a supported event type and is advertised in capabilities.
- The event is validated as pool control before append/projection.
- The local node recomputes leaf hashes from accepted, non-local events for the
  checkpoint pool ordered by `created_at, event_id`.
- Leaf payloads use canonical JSON over `event_id`, `author_node_id`,
  `sequence`, `body_hash`, and `created_at`.
- Witness signatures are checked against active pool admins and witness-role
  members.
- The pool `checkpoint_witness_threshold` controls the required witness count.
- Accepted checkpoints update `trust_pools.latest_checkpoint_event_id` and
  `trust_pools.latest_merkle_root`.
- `GET /gonzbnet/v1/pools/:pool_id/checkpoint` returns the latest accepted
  signed checkpoint event for the pool.

If the local node does not have the event range named by the checkpoint, the
checkpoint is rejected instead of trusted blindly.

This remains an append-only log feature. It does not add blockchain,
cross-node user identity, or remote user-data sharing.
