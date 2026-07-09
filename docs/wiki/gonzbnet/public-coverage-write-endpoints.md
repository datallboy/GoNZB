# Public Coverage Write Endpoints

GoNZBNet exposes two signed node-to-node coverage write convenience endpoints:

- `POST /gonzbnet/v1/coverage/claim`
- `POST /gonzbnet/v1/coverage/checkpoint`

Both endpoints accept a single signed federation event or an `EventBatch`, then
reuse the normal inbox verification and projection path.

Accepted event types:

- `/coverage/claim`: `RangeClaim`, `TimeWindowClaim`
- `/coverage/checkpoint`: `CoverageCheckpoint`, `RangeComplete`, `RangeFailed`

The HTTP request must be signed by the same node that authored each event. Pool
membership and capability checks are enforced by the existing event acceptance
pipeline before events are appended or projected.

`POST /gonzbnet/v1/validation/request` remains pending until the implementation
has a concrete validation-request schema and task admission policy.
