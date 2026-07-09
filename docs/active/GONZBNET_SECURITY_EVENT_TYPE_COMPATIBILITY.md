# GoNZBNet Security Event Type Compatibility

## Scope

This cleanup enforces the spec requirement to reject unknown signed event types
unless compatibility is explicitly declared.

## Implementation

- Add a runtime-supported event-type whitelist in `internal/gonzbnet/pools`.
- Reject unsupported event types in public inbox acceptance before pool
  authorization or append-only event storage.
- Reject unsupported event types in pull sync before accepted-event storage or
  projection.
- Store rejected unsupported events in `federation_rejected_events` with reason
  `unsupported event_type`.

## Out Of Scope

- Negotiating compatibility extensions for future event families.
- Accepting opaque future event bodies into the trusted accepted-event log.
