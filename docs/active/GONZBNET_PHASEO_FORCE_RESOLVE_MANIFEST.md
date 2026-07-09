# GoNZBNet Phase O: Force Resolve Manifest Admin Action

## Spec Scope

Add the remaining local admin action:

- force resolve manifest

## Implementation Plan

1. Add a local admin endpoint that accepts a federated `release_id` and invokes
   the existing GoNZBNet manifest resolver.
2. Return a compact action response containing status, release ID, byte count,
   and whether the resolver returned an NZB.
3. Add TypeScript types/API helper and a small form on `/admin/gonzbnet`.
4. Keep the existing resolver behavior: use local cache when available,
   otherwise request a signed manifest from trusted sources, verify it, cache it,
   generate an NZB, and return only local action status to the admin UI.
5. Document behavior under `docs/wiki/gonzbnet/`.
6. Run UI build and Go tests.

## Out Of Scope

- Bypassing existing resolver trust/source selection.
- Sending local user identity, API keys, search history, grab history, or
  download history to remote nodes.
- Rebuilding manifests from local article data.
- Adding new remote federation endpoints.
