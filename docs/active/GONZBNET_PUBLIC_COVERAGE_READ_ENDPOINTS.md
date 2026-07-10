# GoNZBNet Public Coverage Read Endpoints

This cleanup implements the read-only convenience endpoints from the GoNZBNet
addendum.

Implemented:

- `GET /gonzbnet/v1/coverage/groups`
- `GET /gonzbnet/v1/coverage/plan`
- `GET /gonzbnet/v1/coverage/work`
- `GET /gonzbnet/v1/capabilities/nodes`

Behavior:

- All endpoints require a signed GoNZBNet node request.
- `pool_id` is required.
- The requesting node must be an active member of the requested pool.
- Coverage plan/work responses are scoped to the requesting node ID.
- Node capabilities are filtered to active members of the requested pool.

Implemented separately:

- `POST /gonzbnet/v1/coverage/claim`
- `POST /gonzbnet/v1/coverage/checkpoint`
- `POST /gonzbnet/v1/validation/request`
