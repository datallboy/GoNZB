# Phase A Capability Registry

GoNZBNet capability policy separates node identity from user identity. Local
users, API keys, searches, grabs, and history remain local; trust pools approve
what a remote node may contribute.

## Node Capabilities

Node profiles advertise module switches for consumer, scanner, indexer,
manifest builder, manifest cache, validator, health checker, coverage, and
scheduler participation. Discovery stores the advertised capability snapshot in
`federation_node_capabilities` whenever a peer profile is upserted.

These fields are descriptive. A node advertising `scanner=true` is not trusted
to publish scanner-derived events unless a local pool grants that capability.

## Pool Grants

Pool members now have:

- `allowed_capabilities`: explicit contribution capabilities approved by the
  pool.
- `limits_json`: reserved per-member limits for future scanner, validator, and
  relay quotas.

Admins are treated as capability-unrestricted for bootstrap and moderation
compatibility. Ordinary members with no `allowed_capabilities` are consumer-only.

Contribution gates currently require:

- `ReleaseCard`: `scanner` or `indexer`
- `ResolutionManifest`: `manifest_builder` or `manifest_cache`
- `HealthAttestation`: `validator` or `health_checker`
- `Tombstone`: admin role

## Module Switches

The local `gonzbnet` config exposes module switches. Optional contribution
workers no-op when their module is disabled:

- Release-card publishing requires both `publish_release_cards_enabled` and
  `scanner_enabled`.
- Health-attestation publishing requires both `health_attestations_enabled` and
  `health_checker_enabled`.
- The local GoNZBNet aggregator source is only registered when
  `index_projection_enabled` is true.

Disabling scanner does not prevent remote search or manifest fetch. Disabling
index projection allows a node to scan/publish without exposing the local
federated cache through search.

Addendum config cleanup also exposes scanner limits, coverage policy knobs,
validation limits, and manifest-cache bounds as typed config fields with
`GONZBNET_*` environment aliases.
