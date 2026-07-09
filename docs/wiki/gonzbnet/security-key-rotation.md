# Security: Node Key Rotation

GoNZBNet node key rotation is a local admin-only action.

Endpoint:

- `POST /api/v1/admin/gonzbnet/keys/rotate`

Required body:

```json
{
  "confirmation": "rotate-gonzbnet-node-key"
}
```

The endpoint is protected by `gonzbnet.admin.keys` and the normal admin CSRF
middleware.

## Behavior

Rotation:

- loads the current local node identity;
- renames the current key file to a timestamped backup in `gonzbnet.keys_dir`;
- generates a new Ed25519 node identity;
- writes the new private key using `gonzbnet.key_password` when configured;
- returns old/new node IDs and public keys only.

The response never includes private key bytes or encrypted private key material.

## Operator Impact

The node ID is derived from the public key, so key rotation changes the node ID.
Existing peers and trust pools will treat the rotated node as a new node until
membership and trust are re-established.
