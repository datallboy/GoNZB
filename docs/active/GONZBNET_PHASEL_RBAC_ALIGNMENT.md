# GoNZBNet Phase L: RBAC Alignment

## Spec Scope

Add the remaining local GoNZBNet RBAC permission constants from the core spec
and optional participation addendum:

- `gonzbnet.view_trust_score`
- `gonzbnet.view_source_node`
- `gonzbnet.admin.keys`
- `gonzbnet.admin.coverage`
- `gonzbnet.admin.scanner`
- `gonzbnet.admin.validator`
- `gonzbnet.admin.scheduler`
- `gonzbnet.view.coverage`

## Implementation Plan

1. Add permission constants and include them in the built-in admin role.
2. Allow the GoNZBNet admin route/sidebar to open for any GoNZBNet admin
   permission while retaining backend endpoint-specific RBAC.
3. Document which permissions are currently enforced and which are reserved for
   future narrower key/scanner/validator/scheduler actions.
4. Run UI build and Go tests.

## Out Of Scope

- Node key rotation/export implementation.
- Splitting the GoNZBNet admin page into per-permission sub-routes.
- New backend key/scanner/validator/scheduler mutation endpoints.
