# GoNZBNet Security Flood Throttle

This cleanup adds the spec abuse-resistance requirement for temporary flooder
throttling on public federation routes.

Behavior:

- The existing per-node/IP federation rate limiter still returns
  `rate_limited` for initial limit violations.
- Repeated limit violations by the same identifier activate a short in-memory
  temporary throttle.
- While throttled, requests return `429` with code `temporarily_throttled`.
- Identifiers use the GoNZBNet `node_id` from the authorization header when
  present, otherwise the remote IP, matching the existing rate-limit key.

This is intentionally local and in-memory. It does not write node reputation or
pool moderation state; signed reputation penalties and tombstones remain
separate event-driven mechanisms.
