# GoNZBNet ManifestAvailability Wire Alignment

Status: complete

## Spec Scope

`ManifestAvailability` identifies which node can serve a manifest to which pool,
whether it is available, the fetch policy, compressed size, and update time. It
is not a health-confidence attestation.

## Implementation Plan

1. Replace the non-spec `checked_at`, `status`, `confidence`, and `method` body
   fields with the source-of-truth wire fields.
2. Validate source author, pool envelope membership, fetch policy, size, and
   timestamp before append.
3. Extend the PostgreSQL projection without destroying existing rows and scope
   release-source availability updates to the author and pool.
4. Publish the aligned body from local scan output and add deterministic body,
   validation, and migration tests.
5. Update the wiki and completion audit; coverage wire alignment remains the
   other half of the current protocol-body audit item.

## Implemented

- The signed body now carries `source_node_id`, `pool_id`, `available`,
  `fetch_policy`, `compressed_size_bytes`, and `updated_at` as specified.
- Typed receive validation binds source and pool fields to the signed envelope
  and validates policy, size, and time.
- Migration 018 backfills wire columns while retaining legacy projection
  columns for existing installations.
- Projection updates only the matching release source node and pool and can
  mark that source unavailable as well as available.
- The scan-output publisher emits deterministic aligned bodies; focused tests
  and a fresh PostgreSQL migration test pass.
