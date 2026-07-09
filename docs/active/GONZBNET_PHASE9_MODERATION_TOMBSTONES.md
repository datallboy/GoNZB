# GoNZBNet Phase 9: Moderation And Tombstones

## Spec Scope

Phase 9 adds signed `Tombstone` events, local blocklists, pool moderation
thresholds, projection hiding/rejecting tombstoned targets, and admin APIs for
moderation.

## Current Codebase Context

- Federation inbox projection lives in `internal/api/controllers/gonzbnet.go`.
- Pool policies already include `moderation_threshold`.
- Local admin pool APIs live in `internal/api/controllers/gonzbnet_admin.go`.
- Federated search reads `federated_release_cards`,
  `federated_release_sources`, and cached manifests through pgindex store
  methods.

## Phase 9 Plan

1. Add `internal/gonzbnet/moderation` with the typed `Tombstone` schema,
   allowed target types, allowed severities, validation, and body hashing.
2. Add migration `010_gonzbnet_tombstones.up.sql` with tombstone vote storage,
   active tombstone projections, and default pool accepted event types including
   `Tombstone`.
3. Add pgindex projection methods:
   - store each signed tombstone as a vote;
   - activate local-only tombstones immediately;
   - activate pool tombstones once distinct active pool-admin votes meet
     `moderation_threshold`;
   - mark target release cards hidden/rejected;
   - invalidate cached manifests/NZBs for active manifest/release tombstones.
4. Extend inbox projection to process accepted `Tombstone` events.
5. Add local admin moderation endpoints to list and create tombstones.
6. Add `gonzbnet.admin.moderation` RBAC permission.
7. Update federated search and manifest cache reads to exclude invalidated
   tombstoned targets.
8. Add unit tests for tombstone validation and admin/local event creation where
   feasible without a live PostgreSQL instance.

## Out Of Scope

- Web UI components for moderation.
- Pool moderator role expansion beyond existing active pool admins.
- Automatic moderation from reputation heuristics.
