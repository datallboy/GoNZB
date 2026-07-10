# Phase 7 Resolution Manifests

Phase 7 implements the local resolution path from a federated `ReleaseCard` to
a generated NZB without broadcasting user searches or leaking local user
identity to peers.

## Manifest Model

`internal/gonzbnet/manifest` defines `ResolutionManifest` events. A manifest
contains a `manifest_core` with groups, files, segments, optional PAR2 metadata,
hash hints, and NZB generation metadata.

The manifest ID is deterministic:

```text
man_<lower-base32-sha256(canonical-json(manifest_core))>
```

`manifest.Validate` recomputes that ID and rejects mismatches, missing release
IDs, empty file lists, empty segment lists, and invalid segment references.

`manifest.GenerateNZB` validates the manifest and emits Newzbin-compatible XML
from the manifest file and segment list.

## Storage

Migration `008_gonzbnet_resolution_manifests.up.sql` adds:

- `resolution_manifests`: accepted manifests, canonical manifest bytes,
  verified signed event ID, generated NZB bytes, and validation metadata.
- `federated_manifest_sources`: source-node and pool tracking for advertised
  manifest IDs seen on federated `ReleaseCard` projections.

When a `ReleaseCard` with a `manifest_id` is projected, the source node, pool,
and manifest ID are recorded as a candidate manifest source.

## Peer Fetch Flow

`internal/gonzbnet/manifestresolver` resolves NZBs for federated releases:

1. Return the cached generated NZB when present.
2. Pick the highest-trust advertised manifest source for the release.
3. Send a signed node-to-node `ManifestRequest` to
   `/gonzbnet/v1/manifests/{manifest_id}/request`.
4. Verify the returned signed `ResolutionManifest` event.
5. Recompute and compare the manifest ID.
6. Generate an NZB, append the verified event, cache the manifest and NZB, and
   record source success or failure.

The request body includes the local node ID, release ID, manifest ID, pool ID,
request ID, and reason. It does not include local usernames, API keys, search
history, grab history, or download history.

## API Endpoints

Phase 7 adds two signed federation endpoints:

- `POST /gonzbnet/v1/manifests/:manifest_id/request`
- `GET /gonzbnet/v1/manifests/:manifest_id`

Both endpoints use GoNZBNet node request authentication. The request endpoint
returns a signed manifest event when the requesting node is authorized for a
manifest source pool. The direct GET endpoint returns the cached manifest body
only to an authorized active pool member.

## Newznab Integration

The GoNZBNet aggregator source now supports `GetNZB` through the manifest
resolver. Local Newznab authentication and RBAC remain unchanged:

- search still reads from the local federated cache;
- get requires local `gonzbnet.get`;
- remote manifest resolution additionally requires local
  `gonzbnet.resolve_manifest`;
- peers authenticate the local node only, not the local user.

## Current Limits

The original Phase 7 resolver path does not build local manifests from indexed
articles. The optional manifest-builder module now builds and caches complete
local manifests; the resolver path continues to define, verify, fetch, store,
and generate NZBs from signed manifests available through trusted peers or
local cache.
