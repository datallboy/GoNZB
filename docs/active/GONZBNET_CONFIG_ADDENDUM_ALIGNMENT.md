# GoNZBNet Addendum Config Alignment

This cleanup aligns the typed GoNZBNet config with the addendum configuration
keys.

Implemented:

- Scanner limits and claim behavior config:
  `scanner_max_groups`, `scanner_max_articles_per_hour`,
  `scanner_claim_ttl_minutes`, `scanner_checkpoint_interval_seconds`,
  `scanner_respect_remote_claims`, and `scanner_allow_unassigned_work`.
- Coverage config:
  `coverage_mode`, `coverage_min_trust_for_claim`,
  `coverage_validation_overlap_percent`, `coverage_stale_claim_penalty`, and
  `coverage_provider_scope_mode`.
- Validation config:
  `validation_tiers`, `validation_max_manifests_per_hour`,
  `validation_sample_percent`, `validation_allow_sample_payload_fetch`,
  `validation_allow_par2_validation`, and
  `validation_publish_provider_scope_hash`.
- Manifest-cache config:
  `manifest_cache_max_bytes`, `manifest_cache_ttl_days`, and
  `manifest_cache_serve_to_trusted_pools`.
- Direct `GONZBNET_*` environment aliases for the addendum keys.

Existing settings are reused where they already carry the same meaning:

- `GONZBNET_SCANNER_PUBLISH_RELEASE_CARDS` maps to
  `gonzbnet.publish_release_cards_enabled`.
- `GONZBNET_SCANNER_PUBLISH_MANIFEST_AVAILABILITY` maps to
  `gonzbnet.manifest_availability_enabled`.
- `GONZBNET_SCANNER_PROJECT_TO_LOCAL_INDEX` maps to
  `gonzbnet.index_projection_enabled`.

This is config plumbing only. Scanner execution, validation admission, and
manifest-cache eviction policies can consume these fields in later work.
