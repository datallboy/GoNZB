# Public Coverage Read Endpoints

GoNZBNet exposes read-only coverage coordination endpoints for trusted nodes:

- `GET /gonzbnet/v1/coverage/groups`
- `GET /gonzbnet/v1/coverage/plan`
- `GET /gonzbnet/v1/coverage/work`
- `GET /gonzbnet/v1/capabilities/nodes`

Each request must include a valid `Authorization: GoNZBNet ...` signature and a
`pool_id` query parameter. The requesting node must be an active member of that
pool.

The group endpoint returns the local coverage group catalog. Plan and work
responses are scoped to the requesting node ID from the signature. Node
capabilities are filtered to active members of the requested pool.

These endpoints do not expose local users, API keys, search history, grab
history, download history, or NNTP credentials.

The concrete addendum write-style coverage convenience endpoints are implemented
separately:

- `POST /gonzbnet/v1/coverage/claim`
- `POST /gonzbnet/v1/coverage/checkpoint`
- `POST /gonzbnet/v1/validation/request`
