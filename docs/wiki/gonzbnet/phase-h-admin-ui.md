# Phase H Admin UI

Phase H adds a local WebUI surface for GoNZBNet operator workflows that were
already exposed through the admin API.

## Route

`/admin/gonzbnet` is registered behind the local `gonzbnet.admin.pools`
permission. The navigation link is shown only when the GoNZBNet module is
visible in the control-plane capability response and the signed-in local user
has that permission.

The admin portal entry check now treats `gonzbnet.*` permissions as sufficient
for opening the portal. This does not add cross-node login or change federation
auth; all access remains local RBAC.

## Data Sources

The page reads the existing local admin endpoints:

- `GET /api/v1/admin/gonzbnet/nodes/capabilities`
- `GET /api/v1/admin/gonzbnet/coverage`
- `GET /api/v1/admin/gonzbnet/coverage/groups`
- `GET /api/v1/admin/gonzbnet/coverage/validation-gaps`
- `GET /api/v1/admin/gonzbnet/coverage/suggestions`
- `GET /api/v1/admin/gonzbnet/coverage/plan`

It shows node capability snapshots, coverage score, assignments, active and
stale claims, outcomes, duplicate ranges, group observations, validation gaps,
dedup-aware suggestions, and the read-only scheduler plan.

## Local Signed Actions

The page can create the existing local signed coverage events:

- coverage assignment
- range claim
- range complete
- range failed
- stale-claim penalty materialization

These actions call the local admin API, which signs events with the local node
identity, appends them to the local federation event log, and updates local
coverage projections.

## Privacy Boundary

The page does not expose local usernames, API keys, searches, grabs, or download
history. It only displays local GoNZBNet node, coverage, validation, and group
projection state that was already available through GoNZBNet admin endpoints.
