# GoNZBNet Profile Capacity Alignment

This cleanup closes the addendum checklist item for NodeProfile capability and
capacity advertisement.

Implemented:

- `NodeProfile.module_status` advertises enabled/disabled status for scanner,
  index projection, manifest builder/cache, validator, health checker,
  coverage, scheduler, and relay.
- `NodeProfile.scanner_capacity` advertises local scanner capacity when scanner
  mode is enabled.
- `NodeProfile.validator_capacity` advertises local validation capacity when
  validator mode is enabled.
- `NodeProfile.provider_scope` advertises privacy-preserving provider scope
  metadata, defaulting to `hash_only` disclosure and `provider_local` article
  numbering.
- When `gonzbnet.share_provider_backbone_hash` is enabled, provider scope
  includes a deterministic hash of configured indexer NNTP server scope values.
- Peer sync persists module status, scanner capacity, validator capacity, and
  provider scope into `federation_node_capabilities`.

Profile upserts do not erase capacity learned from signed capacity events when
an older peer profile omits optional capacity blocks.
