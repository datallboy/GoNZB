# GoNZBNet Admin: Pool Join Request

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` sections 10.9,
15.3, and admin actions.

This cleanup implements the local admin action for requesting trust-pool
membership:

- add an admin endpoint that signs a `PoolJoinRequest` event for the local node;
- persist the signed event in the local append-only federation event log;
- keep membership unchanged until pool admins approve the request with a
  `PoolMemberApproved` event;
- expose the action in the GoNZBNet admin UI;
- do not introduce cross-node user identity or user login.

The join request is a node-authenticated federation event. It advertises the
local candidate node ID, requested roles, optional message, and target pool ID.
