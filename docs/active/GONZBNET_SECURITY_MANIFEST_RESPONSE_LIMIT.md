# GoNZBNet Security Manifest Response Limit

## Scope

This cleanup enforces the spec requirement to limit manifest size when a local
node fetches a signed manifest from a trusted peer during Newznab get
resolution.

## Implementation

- Add `MaxManifestBytes` to `manifestresolver.Options`.
- Default resolver reads to 10 MiB when no explicit option is provided.
- Wire `gonzbnet.max_manifest_bytes` into runtime and admin manifest resolver
  construction.
- Reject oversized peer manifest responses before JSON decoding, event
  verification, manifest caching, or NZB generation.
- Record the manifest source failure through the existing resolver failure path.

## Out Of Scope

- Streaming signed manifest parsing.
- Changing the existing inbound federation route body limit behavior.
