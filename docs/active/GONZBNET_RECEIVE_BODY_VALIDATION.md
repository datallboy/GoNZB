# GoNZBNet Receive Body Validation

Status: complete

## Spec Scope

Remote events must pass envelope verification, known-type checks, typed body
schema validation, privacy-field checks, and pool authorization before they are
stored as accepted or projected.

## Implementation Plan

1. Add deterministic ReleaseCard receive validation, including identity hash,
   timestamps, counts, confidence, manifest ID shape, and group names.
2. Add a central event-body validator that checks body schema/type/version,
   rejects private user/context fields, dispatches to each typed validator, and
   verifies author-bound node IDs.
3. Invoke the validator in inbox/gossip and pull sync before accepted append.
4. Keep store-backed pool quorum and membership validation in the existing pool
   authorizer.
5. Add unit tests and a pull-sync regression test proving a correctly signed but
   malformed body is rejected and not appended.

## Implemented

- `internal/gonzbnet/eventbody` is the shared typed receive validator.
- `releasecard.Validate` recomputes the stable release identity.
- Inbox/gossip and pull sync call body validation before pool authorization and
  accepted append.
- `PoolMemberApproved` capability grants and limits are included in approval
  signatures and projection.

## Out Of Scope

- Atomic accepted append plus projection, which remains a separate storage
  cleanup.
- RFC 8785 conformance and duplicate JSON-key rejection.
- ResolutionManifest validation, which remains on the signed on-demand manifest
  response path rather than normal event gossip.
