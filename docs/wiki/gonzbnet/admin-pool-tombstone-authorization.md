# Admin Pool Tombstone Authorization

Pool-scoped Tombstone events are federation moderation votes. A local node may only publish one when its GoNZBNet node identity is an active `admin` member of the target trust pool.

The admin API checks this before signing the event:

- `pool_id` present and severity is not `local_only`: require active pool admin membership.
- no `pool_id`, or severity `local_only`: create a local-only tombstone without pool membership.

This keeps moderation event storage aligned with projection behavior. Projection already counts only distinct active pool-admin votes toward a pool tombstone threshold; creation now rejects unauthorized pool votes before an inert event is appended.
