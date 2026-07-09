# Security: Federation Transport Hardening

This cleanup implements GoNZBNet spec transport requirements that were not a
separate named phase.

## Public Federation Errors

Public `/gonzbnet/v1/*` handlers now return stable machine-readable errors:

```json
{
  "error": "invalid_signature",
  "code": "invalid_signature",
  "message": "request signature verification failed"
}
```

Inbox and gossip item-level rejection codes are normalized to the same spec
vocabulary where possible, including `invalid_event_id`, `invalid_body_hash`,
`invalid_signature`, `unsupported_event_type`, `not_pool_member`,
`insufficient_pool_role`, `insufficient_pool_quorum`, `replayed_nonce`,
`expired_event`, `future_timestamp`, `payload_too_large`, `rate_limited`, and
`internal_error`.

## Body Limits

The public federation route group uses a GoNZBNet-specific body limit rather
than the generic API JSON limit. The limit is derived from:

- `gonzbnet.max_event_bytes`
- `gonzbnet.max_manifest_bytes`
- `gonzbnet.max_batch_events`

Oversized requests receive `payload_too_large`.

## Rate Limiting

Protected inbox and manifest endpoints are rate-limited:

- `POST /gonzbnet/v1/inbox`
- `POST /gonzbnet/v1/manifests/:manifest_id/request`
- `GET /gonzbnet/v1/manifests/:manifest_id`

The limiter keys by the node ID in the GoNZBNet Authorization header when it is
present, then falls back to remote IP before request-auth parsing succeeds.
`gonzbnet.rate_limit_events_per_minute` controls the rate and is also
advertised in the node profile limits.

This is still modular-monolith behavior; no separate relay service or
cross-node user authentication is introduced.
