# GoNZBNet Validation Request Endpoint

This cleanup implements the addendum-listed
`POST /gonzbnet/v1/validation/request` endpoint without introducing remote user
authentication or live search fanout.

## Implementation

- The endpoint accepts a signed GoNZBNet node-to-node HTTP request.
- The body uses `schema_version: "1.0"` and `type: "ValidationRequest"`.
- The request includes `request_id`, `release_id`, `manifest_id`, `pool_id`,
  `requesting_node_id`, optional `target_node_id`, optional `priority`,
  optional `due_at`, and `created_at`.
- The HTTP signature node ID must match `requesting_node_id`.
- If present, `target_node_id` must match the local node ID.
- The requester must be an active member of the requested pool and authorized
  to fetch the referenced manifest.
- The referenced manifest must already be cached locally and match the supplied
  `release_id`.
- Accepted requests enqueue or refresh a local `federation_validation_tasks`
  row.

## Boundary

This endpoint does not fetch remote manifests, accept user credentials, expose
local user activity, or create a new signed append-only event type. Validator
capacity and validation attestations continue to use the signed event inbox.
