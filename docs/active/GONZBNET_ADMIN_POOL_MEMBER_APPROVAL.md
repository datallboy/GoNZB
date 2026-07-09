# GoNZBNet Admin: Pool Member Approval

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` sections 10.10,
15.3, and admin actions.

This cleanup adds a local admin action for signed trust-pool membership
approval:

- add an admin endpoint that signs a `PoolMemberApproved` event with the local
  node identity;
- include the local admin approval object in the event body;
- validate the approval event through existing pool-control validation;
- append the verified event to the local federation event log;
- project the event through the existing pool-member projector;
- expose the action in the GoNZBNet admin UI.

The existing direct pool-member upsert remains available for local bootstrap and
repair. Signed approval is the spec-mode path for promoting a join request into
active membership.
