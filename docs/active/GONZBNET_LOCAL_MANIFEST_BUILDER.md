# GoNZBNet Local Manifest Builder

Status: complete

## Spec Scope

When `gonzbnet.manifest_builder_enabled` is active, local indexed releases with
complete segment Message-IDs must produce and cache a validated
`ResolutionManifest` and generated NZB. ReleaseCard and manifest IDs must use
the same canonical manifest core.

## Implementation Plan

1. Replace the ReleaseCard manifest-ID helper with `manifest.ComputeID` over the
   complete shared manifest core.
2. Map `releasecard.LocalRelease` files and segments into the existing manifest
   types, preserving groups, posters, dates, PAR2 presence, and NZB metadata.
3. Store the validated manifest, canonical core, and generated NZB through the
   existing `StoreResolutionManifest` cache path.
4. Make the publisher invoke this path only when the builder switch is enabled,
   including already-published ReleaseCards that need local projection.
5. Add deterministic ID, builder validation, publisher, and PostgreSQL migration
   coverage, then document the cache behavior.

## Out Of Scope

- Remote manifest federation, which already uses the resolver path.
- NNTP retrieval or reconstruction; the builder uses indexed local segments.
- Changing Newznab response behavior beyond making the existing local cache
  available to its current GoNZBNet source.

## Implementation Notes

The publisher maps complete local release files and segments through
`releasecard.ManifestCoreForLocalRelease`, computes the manifest ID with the
shared `manifest.ComputeID` implementation, validates the resulting
`ResolutionManifest`, generates an NZB locally, and stores both canonical core
and generated NZB through `StoreResolutionManifest`. Existing manifests are
rebuilt idempotently when the release-card publisher revisits an unchanged
candidate. Incomplete releases do not produce a manifest. Remote federation is
unchanged.
