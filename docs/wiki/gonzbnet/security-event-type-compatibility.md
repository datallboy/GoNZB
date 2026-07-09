# Security: Event Type Compatibility

Remote events must use event types this runtime understands. The inbox and pull
sync paths reject validly signed but unsupported event types before append-only
storage or projection.

Rejected unsupported events are written to `federation_rejected_events` with
reason `unsupported event_type` and return/report the stable
`unsupported_event_type` code where the transport exposes item-level codes.

Future compatibility negotiation can extend the whitelist, but unsupported
events are not accepted as opaque trusted state.
