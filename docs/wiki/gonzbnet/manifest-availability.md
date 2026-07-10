# Manifest Availability

`ManifestAvailability` is a signed routing statement, not a health attestation.
It says that a source node can or cannot serve a particular manifest to a pool.
The body contains:

- release and manifest IDs;
- source node and pool IDs;
- an `available` boolean;
- `trusted_peers_only` or `local_only` fetch policy;
- compressed size metadata;
- the RFC3339 update time.

Receive validation requires `source_node_id` to equal the signed event author.
The generic pool-body check requires `pool_id` to appear in the event envelope.
Negative sizes, unknown fetch policies, and future timestamps are rejected
before append.

The PostgreSQL projection stores each signed statement and updates only the
matching `federated_release_sources` row for the source node, pool, release, and
manifest. An unavailable statement clears that source's resolvability and
manifest-confidence score without affecting other trusted sources.

Local scan-output publishing emits this event only when
`gonzbnet.manifest_availability_enabled` is enabled and a stable manifest ID is
present. Local scan cards currently use `local_only`; remotely fetchable cached
sources can use `trusted_peers_only`.
