# Phase 9 Moderation And Tombstones

Phase 9 adds signed moderation events that can hide or reject federated release
metadata and invalidate cached manifests. Local user identity still stays local:
admin actions create local signed node events and never send local usernames or
API keys to peers.

## Event Model

`internal/gonzbnet/moderation` defines `Tombstone` with body schema
`gonzbnet.Tombstone/1.0`.

Supported target types are:

- `release`
- `manifest`
- `event`
- `node`
- `pool_member`
- `health_attestation`
- `trust_attestation`

Supported severities are:

- `hide`
- `reject`
- `warn`
- `local_only`

Validation rejects unknown target types, unknown severities, missing target IDs,
missing reasons, invalid timestamps, and effective times too far in the future.

## Projection

Migration `010_gonzbnet_tombstones.up.sql` adds:

- `tombstone_votes`: every accepted signed tombstone event, keyed by source
  event ID.
- `tombstones`: the active or pending projection for each target and pool.

Pool tombstones use vote-based moderation. Each signed `Tombstone` event counts
as a vote. A pool tombstone becomes active when distinct active pool-admin votes
meet `trust_pools.moderation_threshold`.

Local-only tombstones activate immediately and are signed with
`visibility = local`; outbox queries now exclude local-visibility events so they
are not shared with peers.

## Effects

Active `release` tombstones with `hide`, `reject`, or `local_only` remove the
release from federated Newznab search by changing the projected release-card
status and by adding tombstone filters to federated search.

Active `release` or `manifest` tombstones with `reject` or `local_only`
invalidate cached resolution manifests and generated NZBs by setting
`validation_status = invalidated` and clearing generated NZB bytes.

Manifest resolution and direct manifest reads also check active reject/local
tombstones before serving cached data.

## Admin API

Phase 9 adds local moderation endpoints:

- `GET /api/v1/admin/gonzbnet/moderation/tombstones`
- `POST /api/v1/admin/gonzbnet/moderation/tombstones`

These routes require `gonzbnet.admin.moderation`.

When `pool_id` is omitted, `POST` defaults to a `local_only` tombstone. When
`pool_id` is provided, it defaults to `reject` and publishes a pool-scoped
signed tombstone event. Pool-scoped tombstones activate only when the configured
moderation threshold is met.

## Current Limits

This phase adds admin APIs but no web UI components. It also uses existing
active pool admins as moderation approvers; a separate moderator role can be
added later if needed.
