# GoNZBNet Test Coverage Cleanup

## Scope

This cleanup strengthens evidence for the final implementation checklist items
that require tests for non-member events and RBAC denial.

## Implementation

- Add a pull-sync test proving a signed remote ReleaseCard from a non-member
  pool author is rejected and is not stored or projected.
- Add aggregator manager coverage proving GoNZBNet `get` permission is checked
  before a GoNZBNet source is called.
- Add GoNZBNet aggregator-source coverage proving manifest resolution requires
  `gonzbnet.resolve_manifest` in addition to `gonzbnet.get`.

## Out Of Scope

- New federation behavior. These tests pin behavior that already belongs to the
  spec implementation.
