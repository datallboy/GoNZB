# Security: Event Time Windows

Remote signed events are verified in two layers:

- deterministic hash/signature validation with `events.Verify`
- wall-clock validity with `events.VerifyAt` or `events.VerifyWithin`

Network acceptance paths use `VerifyWithin` so future, expired, or stale signed
events are rejected before projection or manifest caching. This applies to:

- public federation inbox events
- manual and scheduled pull-sync outbox events
- on-demand signed ResolutionManifest fetches

The tolerance uses `gonzbnet.time_tolerance_seconds` where config is available,
and defaults to two minutes for direct package use.

Remote event age is limited by `gonzbnet.max_event_age_hours`, which defaults to
720 hours. This implements the spec abuse-resistance requirement to limit event
age even when a signed event omits `expires_at`.
