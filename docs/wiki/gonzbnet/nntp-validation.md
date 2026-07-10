# NNTP Validation

GoNZBNet validation uses the shared local NNTP manager when available. Each
manifest segment is checked through the validator-scoped body-prefix fetch
operation, so peers receive only the signed aggregate attestation, never local
credentials or user context.

The attestation status is `available`, `partial`, or `missing` based on segment
results. Nodes without a local NNTP manager retain structural `unverified`
fallback behavior. Checksum and PAR2 checks are separate optional capabilities.
