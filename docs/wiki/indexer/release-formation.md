# Indexer Release Formation

Release formation depends on binary projections and release-family summaries.

## Inputs

- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `binary_lifecycle`
- `binary_completion_keys`
- release dirty/summary queues

## Summary Refresh

`release_summary_refresh` aggregates binary projections into:

- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`

This stage is the heavy writer for release readiness. It should not use source
tables as progress state.

## Formation

Release formation consumes ready candidates and writes durable release catalog
and lineage tables. It may form releases from binaries that span daily
partitions because durable identity keys and release-family keys are not scoped
to a single source day.

## Cross-Day Behavior

Daily partitions are retention and scan boundaries, not release-family
boundaries. Binaries and releases may span adjacent or non-adjacent days.
Retention must preserve any day still referenced by active, incomplete, or
non-archived release work.
