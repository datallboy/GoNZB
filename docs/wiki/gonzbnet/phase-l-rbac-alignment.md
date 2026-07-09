# Phase L RBAC Alignment

Phase L adds the remaining local GoNZBNet permissions named by the core
implementation spec and optional-participation addendum.

## Added Permissions

- `gonzbnet.view_trust_score`
- `gonzbnet.view_source_node`
- `gonzbnet.view.coverage`
- `gonzbnet.admin.keys`
- `gonzbnet.admin.coverage`
- `gonzbnet.admin.scanner`
- `gonzbnet.admin.validator`
- `gonzbnet.admin.scheduler`

The built-in admin role now receives these permissions along with the previously
implemented GoNZBNet permissions.

## UI Access

The `/admin/gonzbnet` route is guarded by `gonzbnet.admin.read`. The sidebar
shows GoNZBNet when the local user has any GoNZBNet admin permission.

Backend endpoints still enforce narrower permissions:

- peer management: `gonzbnet.admin.peers`
- pool and coverage administration: `gonzbnet.admin.pools`
- tombstone moderation: `gonzbnet.admin.moderation`

Key, scanner, validator, scheduler, trust-score, source-node, and coverage-view
permissions are now available for future narrower APIs and UI gating.
