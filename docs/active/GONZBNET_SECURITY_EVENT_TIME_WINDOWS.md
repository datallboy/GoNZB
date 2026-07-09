# GoNZBNet Security Event Time Windows

## Scope

This cleanup enforces signed federation event validity windows on remote event
acceptance paths.

## Implementation

- Add `events.VerifyAt(event, now, futureTolerance)` on top of deterministic
  signature/hash verification.
- Reject remote events whose `created_at` is too far in the future.
- Reject remote events whose `not_before` is too far in the future.
- Reject remote events whose `expires_at` has passed.
- Apply the check to inbox acceptance, pull sync, and on-demand manifest fetch.

## Out Of Scope

- A fixed maximum age for events without `expires_at`. The current spec has
  tolerance config but no explicit max-age config.
