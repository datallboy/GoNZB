# Phase N: Node Profile And Config Admin

Phase N adds local read-only admin visibility for the node profile, public node
identity, and GoNZBNet configuration validation.

## Admin API

The admin API adds these local endpoints under `/api/v1/admin/gonzbnet`:

- `GET /node/profile`
- `GET /config/validation`

`/node/profile` returns the same public identity material used by federation
node profile discovery:

- deterministic node ID
- public key
- public node profile with capabilities, endpoints, limits, and policy

It never returns the private node key.

`/config/validation` returns a redacted configuration summary plus validation
issues. The summary intentionally reports counts and booleans instead of
sensitive values. It does not return key passwords, relay API keys, manual peer
URLs, local usernames, API keys, search history, grab history, or download
history.

## Validation

The validation response marks privacy-sensitive or structurally invalid settings
as errors. Advisory conditions such as missing advertised URL, live query mode,
or enabled features with missing module dependencies are warnings.

This phase does not mutate runtime configuration and does not implement key
export, backup, or rotation.

## Admin UI

`/admin/gonzbnet` now shows:

- local node profile details
- node ID and public key
- configuration validation issues
- module enablement
- limits and privacy flags
- publisher, validation, sync, and gossip settings
- names of redacted sensitive config fields

The UI reads local admin endpoints only and does not change Newznab search or
grab behavior.
