# Security: Key Export

GoNZBNet supports node-key backup through an explicit local admin action.

## Admin API

The key export endpoint is:

- `POST /api/v1/admin/gonzbnet/keys/export`

It requires the `gonzbnet.admin.keys` permission and CSRF protection like other
local admin actions.

Request body:

```json
{
  "backup_password": "operator-chosen backup password",
  "confirmation": "export-gonzbnet-node-key"
}
```

The response includes:

- node ID
- public key
- encrypted backup format
- encrypted private-key envelope
- creation timestamp

The endpoint does not return raw private key bytes and does not echo the backup
password or configured `gonzbnet.key_password`.

## Admin UI

`/admin/gonzbnet` includes a key backup form. The operator must enter a backup
password and the exact confirmation token. The UI displays the encrypted backup
JSON returned by the local admin API.

Restore/import and key rotation are separate admin requirements and are not
implemented by this cleanup.
