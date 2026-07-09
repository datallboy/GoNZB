# GoNZBNet Phase N: Node Profile And Config Validation Admin

## Spec Scope

Add local admin visibility for the remaining read-only Admin UI/API
requirements:

- local node profile
- node ID and public key
- configuration validation

## Implementation Plan

1. Reuse the existing GoNZBNet node identity and profile builder from the admin
   controller.
2. Add a redacted admin config summary with validation warnings/errors for
   module dependencies and privacy-sensitive settings.
3. Add admin endpoints under `/api/v1/admin/gonzbnet` for node profile and
   config validation reads.
4. Add TypeScript types/API helpers and compact read-only panels to
   `/admin/gonzbnet`.
5. Document behavior under `docs/wiki/gonzbnet/`.
6. Run UI build and Go tests.

## Out Of Scope

- Mutating configuration from the GoNZBNet admin page.
- Enabling/disabling GoNZBNet at runtime.
- Key export, backup, or rotation.
- Force manifest resolution.
