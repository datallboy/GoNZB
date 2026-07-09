# GoNZBNet Security Cleanup: Explicit Key Export

## Spec Scope

Security Requirements include:

- never return private key material from admin APIs
- support key backup/export only through explicit admin action

## Implementation Plan

1. Add identity support for exporting the current node private key as an
   encrypted backup envelope using an admin-supplied backup password.
2. Add an admin API endpoint guarded by `gonzbnet.admin.keys`.
3. Require an explicit confirmation token and non-empty backup password.
4. Add a minimal admin UI action that displays the encrypted backup envelope
   only after the explicit action.
5. Document behavior under `docs/wiki/gonzbnet/`.
6. Run UI build and Go tests.

## Out Of Scope

- Raw private key export.
- Key restore/import workflows.
- Key rotation.
