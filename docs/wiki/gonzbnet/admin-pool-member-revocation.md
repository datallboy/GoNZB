# Admin: Pool Member Revocation

GoNZBNet can create a local signed `PoolMemberRevoked` event from the admin API
and UI.

Endpoint:

- `POST /api/v1/admin/gonzbnet/pools/:pool_id/members/:node_id/revocations`

Request body:

```json
{
  "reason": "compromised_key",
  "effective_at": "2026-07-09T18:00:00Z",
  "approvals_required": 1
}
```

Behavior:

- signs a revocation approval object with the local node identity;
- wraps the approval in a signed `PoolMemberRevoked` event;
- validates the event against the trust-pool moderation threshold;
- appends the verified event to the federation event log;
- projects the member as revoked in the local pool-member table.

Pools with a moderation threshold greater than the supplied valid approval count
reject the event. The action authenticates nodes, not users, and does not send
local usernames, API keys, searches, grabs, or download history.
