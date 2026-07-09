# Admin: Role Pool Access

GoNZBNet supports local role-level federation pool grants.

Endpoints:

- `GET /api/v1/admin/gonzbnet/pools/:pool_id/role-access`
- `POST /api/v1/admin/gonzbnet/pools/:pool_id/role-access`
- `DELETE /api/v1/admin/gonzbnet/pools/:pool_id/role-access/:role_id`

Request body:

```json
{
  "role_id": "admin",
  "can_search": true,
  "can_get": true,
  "can_resolve_manifest": true
}
```

These grants are local RBAC data. Federated searches use them to decide which
accepted local federated cache pools a local user can query. They do not create
cross-node user login and do not send local usernames, API keys, searches, grabs,
or download history to peers.
