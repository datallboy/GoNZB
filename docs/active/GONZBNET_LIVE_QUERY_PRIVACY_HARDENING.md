# GoNZBNet Live Query Privacy Hardening

This cleanup closes the unused live-query surface for the current
modular-monolith implementation.

## Scope

- Keep `gonzbnet.live_query_enabled=false` as the only valid runtime value.
- Treat `gonzbnet.live_query_enabled=true` as a configuration error, not a
  warning.
- Ensure public node profiles do not advertise live-query support.
- Keep search behavior local-cache based through the local GoNZBNet aggregator
  source.

## Boundary

- This does not remove the config key; it reserves the field for a future spec
  revision that explicitly defines safe live-query behavior.
- This does not add cross-node user searches or any remote search endpoint.
