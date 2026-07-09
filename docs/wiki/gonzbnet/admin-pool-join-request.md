# Admin: Pool Join Request

GoNZBNet can create a local signed `PoolJoinRequest` event from the admin API
and UI.

Endpoint:

- `POST /api/v1/admin/gonzbnet/pools/:pool_id/join-requests`

Request body:

```json
{
  "requested_roles": ["member"],
  "message": "optional admin-visible message"
}
```

Behavior:

- signs the request with the local node identity;
- appends the verified event to the federation event log;
- leaves pool membership unchanged until admins approve the node with a
  `PoolMemberApproved` event;
- makes the event available through the normal local outbox/sync paths.

The event authenticates the node, not a user. It does not send local usernames,
API keys, searches, grabs, or download history.
