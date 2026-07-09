# Config Addendum Alignment

GoNZBNet now exposes the scanner, coverage, validation, and manifest-cache
settings named in the implementation addendum as typed config fields.

The loader accepts both normal project-prefixed env vars, such as
`GONZB_GONZBNET_SCANNER_MAX_GROUPS`, and the addendum shorthand aliases, such as
`GONZBNET_SCANNER_MAX_GROUPS`.

Some addendum aliases map to existing fields:

- `GONZBNET_SCANNER_PUBLISH_RELEASE_CARDS` -> `gonzbnet.publish_release_cards_enabled`
- `GONZBNET_SCANNER_PUBLISH_MANIFEST_AVAILABILITY` -> `gonzbnet.manifest_availability_enabled`
- `GONZBNET_SCANNER_PROJECT_TO_LOCAL_INDEX` -> `gonzbnet.index_projection_enabled`

Validation rejects out-of-range trust percentages, coverage overlap
percentages, unknown validation tiers, invalid coverage modes, and negative
scanner/cache limits.
