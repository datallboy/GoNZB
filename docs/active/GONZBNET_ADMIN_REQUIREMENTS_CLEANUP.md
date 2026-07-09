# GoNZBNet Admin Requirements Cleanup

## Spec Scope

This cleanup covers admin requirements listed in the implementation spec outside
the named implementation phases.

Current work:

- block node
- unblock node

## Implementation Plan

1. Add pgindex store support for updating local federation node status to
   `blocked` or back to `known`.
2. Add local admin API endpoints under `/api/v1/admin/gonzbnet/nodes/:node_id`.
3. Add TypeScript API helpers and actions to the node capabilities table.
4. Document behavior under `docs/wiki/gonzbnet/`.
5. Run UI build and Go tests.

## Out Of Scope

- Publishing block actions as pool moderation events.
- Removing node identity records, event logs, or pool memberships.
- Applying release/manifest tombstone semantics to node blocking.
