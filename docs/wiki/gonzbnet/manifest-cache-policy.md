# Manifest Cache Policy

GoNZBNet stores verified `ResolutionManifest` bodies and locally generated NZBs
in PostgreSQL. The runtime applies `gonzbnet.manifest_cache_ttl_days` and
`gonzbnet.manifest_cache_max_bytes` to that store.

Expired rows are deleted before new manifest writes and are excluded from
manifest, generated-NZB, and manifest-event reads. After a write, the oldest
rows are removed until the combined `body_blob` and `generated_nzb` payload size
fits the configured byte budget. A non-positive value disables an individual
constraint.

This policy is independent of federation authorization. Node-to-node manifest
responses still require a signed request and active membership in the relevant
trusted pool; cache limits never broaden access.
