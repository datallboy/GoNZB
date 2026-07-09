# Security: Manifest Response Limit

On-demand manifest resolution now caps peer manifest responses with
`gonzbnet.max_manifest_bytes`. Oversized responses are rejected before JSON
decoding, event verification, manifest caching, or NZB generation.

The resolver records the source failure using the existing manifest-source
failure path. This complements the inbound federation body limit and covers the
client side of `Newznab get -> signed ManifestRequest -> signed
ResolutionManifest`.
