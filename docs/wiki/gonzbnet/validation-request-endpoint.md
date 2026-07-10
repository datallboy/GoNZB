# Validation Request Endpoint

`POST /gonzbnet/v1/validation/request` lets a trusted pool member ask the local
node to validate a manifest that is already cached locally.

The request is authenticated with the existing GoNZBNet node-to-node HTTP
signature. It is not a user-authenticated endpoint and it is not a signed
append-only federation event.

Request admission requires:

- `schema_version: "1.0"` and `type: "ValidationRequest"`;
- `request_id`, `release_id`, `manifest_id`, `pool_id`, `requesting_node_id`,
  and `created_at`;
- a valid request signature whose node ID matches `requesting_node_id`;
- active pool membership for the requesting node;
- optional `target_node_id` matching the local node;
- local manifest cache presence for the requested manifest;
- `release_id` matching the cached manifest.

When admitted, the request enqueues or refreshes a
`federation_validation_tasks` row. Completed tasks are not reopened by a later
request.

The endpoint does not fetch remote manifests, accept remote user identity,
expose API keys or local history, or bypass the existing validator module
switch. Validation attestations remain signed events accepted through the
standard inbox path.
