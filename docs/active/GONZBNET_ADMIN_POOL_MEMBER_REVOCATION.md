# GoNZBNet Admin: Pool Member Revocation

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` sections 10.11,
15.4, and admin actions.

This cleanup adds a local admin action for signed trust-pool member revocation:

- add an admin endpoint that signs a `PoolMemberRevoked` event with the local
  node identity;
- include the local admin revocation approval object in the event body;
- validate the revocation event through existing pool-control validation;
- append the verified event to the local federation event log;
- project the event through the existing pool-member revocation projector;
- expose the action in the GoNZBNet admin UI.

The existing direct pool-member revoke endpoint remains available for local
repair. Signed revocation is the spec-mode path for pool-auditable membership
removal.
