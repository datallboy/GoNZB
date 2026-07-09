# Admin: Recompute Scores

GoNZBNet exposes a local score recomputation action for operators.

Endpoint:

- `POST /api/v1/admin/gonzbnet/scores/recompute`

Request body:

```json
{
  "pool_id": "pool.local"
}
```

Behavior:

- recomputes federated release-source availability from health attestations;
- recomputes validation and checksum scores from validation attestations;
- refreshes source trust scores from federation node trust;
- recomputes aggregate federated release-card score fields for the pool.

The action is local-only. It does not contact peers and does not include local
usernames, API keys, search history, grab history, or download history.
