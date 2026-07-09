# Profile Capacity Alignment

Node profiles now include addendum capacity metadata:

- `module_status`
- `scanner_capacity`
- `validator_capacity`
- `provider_scope`

Scanner capacity is included only when scanner mode is enabled. Validator
capacity is included only when validator mode is enabled. Provider scope is
privacy-preserving and does not publish raw provider names or credentials.

Peer sync stores these blocks in `federation_node_capabilities` alongside the
existing capability booleans. If a peer omits optional capacity blocks, the
profile upsert preserves capacity previously learned from signed
`ScannerCapacity` or `ValidatorCapacity` events.
