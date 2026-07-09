# Phase C Scan-Without-Index Contribution

Phase C lets a scanner-capable node publish signed `ReleaseCard` metadata from
scan output without adding those releases to the local user-facing index.

## Scan Output

Migration `013_gonzbnet_scan_output.up.sql` adds `gonzbnet_scan_outputs`.
Scanner modules can write public-ready `releasecard.LocalRelease` records with
`UpsertGoNZBNetScanOutput`. This table is separate from the existing indexer
catalog and from the local Newznab search path.

The GoNZBNet publisher reads scan-output candidates before existing indexer-cache
candidates. Scan-output cards use `source.kind = local_scan_output`; after a
signed `ReleaseCard` is appended, the scan-output row is marked `published`.

## Search Isolation

`gonzbnet.index_projection_enabled=false` still prevents the GoNZBNet aggregator
source from being registered. Scan-output releases can be signed and sent to
trusted peers, but local Newznab search does not expose them unless the local
GoNZBNet aggregator source and index projection are enabled.

## Manifest Availability

Phase C adds an optional signed `ManifestAvailability` event. It is controlled
by `gonzbnet.manifest_availability_enabled`, which defaults to false. When
enabled, scan-output releases with a stable manifest ID emit availability
attestations that consuming nodes project into manifest-confidence scoring.

Pool policy requires `scanner`, `manifest_builder`, or `manifest_cache` for
`ManifestAvailability`.
