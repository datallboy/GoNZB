# GoNZBNet Phase M: Source And Health Diagnostics

## Spec Scope

Add read-only local admin diagnostics for remaining Admin UI/API Requirements:

- release source details
- manifest source details
- health attestations
- node reputation / trust score visibility

## Implementation Plan

1. Add pgindex diagnostic structs and list methods over existing GoNZBNet
   projection tables.
2. Add admin diagnostics endpoints under `/api/v1/admin/gonzbnet/diagnostics/*`.
3. Add TypeScript types/API helpers and compact tables to `/admin/gonzbnet`.
4. Document behavior under `docs/wiki/gonzbnet/`.
5. Run UI build and Go tests.

## Out Of Scope

- Mutating trust scores.
- Force manifest resolution.
- New health scoring formulas.
- Exposing remote diagnostics through federation endpoints.
