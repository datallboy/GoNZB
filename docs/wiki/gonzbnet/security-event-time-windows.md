# Security: Event Time Windows

Remote signed events are verified in two layers:

- deterministic hash/signature validation with `events.Verify`
- wall-clock validity with `events.VerifyAt`

Network acceptance paths use `VerifyAt` so future or expired signed events are
rejected before projection or manifest caching. This applies to:

- public federation inbox events
- manual and scheduled pull-sync outbox events
- on-demand signed ResolutionManifest fetches

The tolerance uses `gonzbnet.time_tolerance_seconds` where config is available,
and defaults to two minutes for direct package use.
