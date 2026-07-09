# GoNZBNet Security Event Time Windows

## Scope

This cleanup enforces signed federation event validity windows on remote event
acceptance paths.

## Implementation

- Add `events.VerifyAt(event, now, futureTolerance)` on top of deterministic
  signature/hash verification for direct package use.
- Add `events.VerifyWithin(event, now, futureTolerance, maxAge)` for remote
  federation acceptance paths that must enforce both clock skew tolerance and a
  maximum event age.
- Reject remote events whose `created_at` is too far in the future.
- Reject remote events whose `not_before` is too far in the future.
- Reject remote events whose `expires_at` has passed.
- Reject remote events older than `gonzbnet.max_event_age_hours`.
- Apply the check to inbox acceptance, pull sync, and on-demand manifest fetch.

## Configuration

- `gonzbnet.time_tolerance_seconds` controls allowed clock skew and defaults to
  120 seconds.
- `gonzbnet.max_event_age_hours` controls maximum accepted remote event age and
  defaults to 720 hours.
