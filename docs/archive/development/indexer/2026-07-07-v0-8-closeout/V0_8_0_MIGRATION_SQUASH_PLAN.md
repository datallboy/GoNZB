# v0.8.0 Migration Squash Plan

Snapshot date: 2026-06-17

Status: implemented in-tree on 2026-06-17.

This records the release plan and validation notes for replacing the prior migration chains with clean v0.8.0 baselines.

## Goals

- Replace the PostgreSQL chain `001` through `062` with one v0.8.0 baseline.
- Replace the settings SQLite chain `001` through `008` with one v0.8.0 baseline.
- Keep the job SQLite store as a single baseline unless validation finds drift.
- Remove migration-history wording that treats the v2 binary projection work as in-progress.
- Omit the retired legacy `public.binaries` table from the clean v0.8 PostgreSQL baseline.

## PostgreSQL Baseline Steps

1. Create a fresh database from the current migration chain.
2. Dump schema only with stable options: no owner, no privileges, no data, deterministic ordering.
3. Remove migration-history artifacts that should not exist in a clean install, including compatibility comments that describe old lane names as live writers.
4. Add `062_drop_retired_binaries.up.sql` before the squash to document the pre-baseline transition.
5. Rename current PostgreSQL migrations into `internal/store/pgindex/migrations_archive/pre_v0_8_0_squash/`.
6. Add `internal/store/pgindex/migrations/001_v0_8_0_baseline.up.sql`.
7. Add a fresh-baseline migration smoke test that builds a clean schema from only the baseline and verifies `public.binaries` is absent.

## SQLite Settings Baseline Steps

1. Create a fresh settings database from migrations `001` through `008`.
2. Dump schema for runtime settings, auth users/roles/sessions/tokens, external indexers, ARR integrations, scoped NNTP servers, and runtime metadata.
3. Archive the old settings migrations under `internal/store/settings/migrations_archive/pre_v0_8_0_squash/`.
4. Add `internal/store/settings/migrations/001_v0_8_0_baseline.up.sql`.
5. Preserve user-level API tokens; they are now the API-key mechanism and are RBAC-scoped through the owning user's roles.

## Validation

- `go test ./...`
- `npm --prefix ui run build`
- Fresh PostgreSQL migration from the new baseline.
- Fresh settings SQLite migration from the new baseline.
- Serve startup with no existing settings DB, initial admin setup, role creation, user token creation, Newznab search via `apikey`, and `/nzb/:id` download via the same user token.
- Fresh indexer scrape/assemble/release/inspect/archive soak using final defaults.

## Validation Status

- Fresh PostgreSQL migration from the new baseline: passed against a throwaway database in `gonzb-postgres`.
- Fresh settings SQLite migration from the new baseline: covered by `internal/store/settings` tests.
- `public.binaries` decision: omitted from the PostgreSQL baseline; `binary_core` is the canonical binary anchor.

Remaining release validation before tagging:

- `go test ./...`
- `npm --prefix ui run build`
- Serve startup with no existing settings DB, initial admin setup, role creation, user token creation, Newznab search via `apikey`, and `/nzb/:id` download via the same user token.
- Fresh indexer scrape/assemble/release/inspect/archive soak using final defaults.
