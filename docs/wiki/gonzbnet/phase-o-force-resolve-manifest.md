# Phase O: Force Resolve Manifest

Phase O adds a local admin action for resolving a federated release manifest on
demand.

## Admin API

The admin API adds:

- `POST /api/v1/admin/gonzbnet/manifests/resolve`

Request body:

```json
{
  "release_id": "rel_example"
}
```

The endpoint invokes the existing GoNZBNet manifest resolver. If an NZB is
already cached locally, the resolver returns it from cache. If it is not cached,
the resolver selects a trusted manifest source, sends the existing signed
node-to-node manifest request, verifies the returned signed
`ResolutionManifest`, stores it, generates an NZB, and reports local action
status.

The admin response includes only status, release ID, resolved flag, and NZB byte
count. It does not return the NZB payload through the admin endpoint.

## Privacy

The action uses the same privacy-preserving resolver path as Newznab get:

- remote peers receive node-authenticated manifest request metadata only
- local usernames, API keys, search history, grab history, and download history
  are not sent
- remote peers authorize the requesting node, not a local user

## Admin UI

`/admin/gonzbnet` includes a compact manifest resolve form. It can use an
operator-entered release ID or a suggested release ID from current validation
gap/source diagnostics.
