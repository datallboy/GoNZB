# Security: Flood Throttle

Public federation routes now add a temporary in-memory throttle after repeated
rate-limit violations from the same node/IP identifier.

The first rate-limit violations return:

```json
{"code":"rate_limited"}
```

Once the flood threshold is crossed, the same identifier receives:

```json
{"code":"temporarily_throttled"}
```

The throttle is local process state. It is an abuse-resistance guard, not a
pool moderation action and not a replacement for signed tombstones or local node
blocking.
