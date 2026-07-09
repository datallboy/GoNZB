# GoNZBNet Addendum Phase C: Scan-Without-Index Contribution

## Spec Scope

Add scanner contribution mode where `scanner=true` and
`index_projection=false` can still publish signed `ReleaseCard` events from scan
output, without exposing those releases through the local Newznab/search path.

## Implementation Plan

1. Add a separate GoNZBNet scan-output table for public-ready scanner releases.
   This table is not used by the local aggregator/Newznab search source.
2. Add strongly typed scan-output DTOs and store methods:
   - upsert scan output
   - list pending scan-output candidates for ReleaseCard publication
   - mark scan output as published
3. Extend the publisher to read scan-output candidates first, then existing
   indexer-cache candidates. This preserves Phase 2 behavior while enabling
   scanner-only nodes.
4. Add optional `ManifestAvailability` event schema/projection for scanner
   manifest availability announcements.
5. Keep `index_projection_enabled=false` behavior from Phase A: the GoNZBNet
   aggregator source is not registered, so local Newznab search does not expose
   scan-output releases.
6. Add tests for scan-output ReleaseCard publication and projection isolation.

## Assumptions

- Phase C adds the scan-output ingestion/storage path and publisher integration.
  It does not implement a full NNTP scanning engine; future scanner modules can
  write to this table.
- Scan output uses the existing `releasecard.LocalRelease` shape so the stable
  ReleaseCard mapper remains shared.
- Optional `ManifestAvailability` announces whether a scan output has a
  manifest ID and source event; it does not fetch or build manifests.

## Out Of Scope

- Scheduler assignment of scan work.
- Live NNTP article scanning.
- UI for entering or reviewing scan output.
