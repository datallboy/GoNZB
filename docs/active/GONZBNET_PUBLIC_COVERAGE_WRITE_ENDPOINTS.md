# GoNZBNet Public Coverage Write Endpoints

This cleanup implements the concrete write-style coverage convenience endpoints
from the GoNZBNet addendum.

Implemented:

- `POST /gonzbnet/v1/coverage/claim`
- `POST /gonzbnet/v1/coverage/checkpoint`

Behavior:

- Requests require a valid signed GoNZBNet node `Authorization` header.
- The request body accepts a signed event or `EventBatch`, matching the inbox
  shape.
- `/coverage/claim` only accepts `RangeClaim` and `TimeWindowClaim`.
- `/coverage/checkpoint` only accepts `CoverageCheckpoint`, `RangeComplete`,
  and `RangeFailed`.
- The requesting node ID from the HTTP signature must match the signed event
  author.
- Accepted events flow through the same verification, pool authorization,
  append-only event log, and coverage projection path as `/gonzbnet/v1/inbox`.

Out of scope:

- `POST /gonzbnet/v1/validation/request`

The validation request endpoint still needs a concrete request/event schema and
task admission policy before implementation. Existing validator attestations
continue to flow through the signed inbox path.
