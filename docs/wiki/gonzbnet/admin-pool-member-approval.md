# Admin: Pool Member Approval

GoNZBNet can create a local signed `PoolMemberApproved` event from the admin API
and UI.

Endpoint:

- `POST /api/v1/admin/gonzbnet/pools/:pool_id/members/:node_id/approve`

Request body:

```json
{
  "role": "member",
  "proposal_event_id": "evt_join_request",
  "approvals_required": 1
}
```

Behavior:

- signs an approval object with the local node identity;
- wraps the approval in a signed `PoolMemberApproved` event;
- validates the event against the trust-pool approval rules;
- appends the verified event to the federation event log;
- projects the approved member into the local pool-member table.

Pools with a membership threshold greater than the supplied valid approval count
reject the event. The action authenticates nodes, not users, and does not send
local usernames, API keys, searches, grabs, or download history.
