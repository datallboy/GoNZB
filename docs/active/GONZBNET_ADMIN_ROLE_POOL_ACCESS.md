# GoNZBNet Admin: Role Pool Access

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` Phase 5 and
admin actions.

This cleanup adds local admin management for role-level federation pool access:

- list `role_federation_pool_access` grants for a pool;
- set a local role's `can_search`, `can_get`, and `can_resolve_manifest`
  permissions for a pool;
- delete a role grant from a pool;
- expose the workflow in the GoNZBNet admin UI.

This completes the explicit `set role pool access` admin action. Direct
user-specific grants remain out of scope for this cleanup.
