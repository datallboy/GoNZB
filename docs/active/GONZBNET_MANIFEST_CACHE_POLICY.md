# GoNZBNet Manifest Cache Policy

Status: complete

The existing PostgreSQL `resolution_manifests` store now applies the typed
GoNZBNet cache settings at runtime:

- `manifest_cache_ttl_days` removes stale manifest rows before new writes and
  excludes expired rows from manifest, generated-NZB, and manifest-event reads.
- `manifest_cache_max_bytes` prunes the oldest manifest rows after a write until
  the combined stored manifest and generated-NZB payload size is within the
  configured limit.
- Runtime wiring applies these settings to the concrete PostgreSQL store when
  the GoNZBNet module is built.

Manifest fetch authorization remains pool-membership based. Cache policy does
not weaken signed node authentication or trusted-pool checks.
