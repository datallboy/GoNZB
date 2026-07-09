# GoNZBNet Peer TLS Policy Cleanup

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` section 23.2.

This cleanup enforces the spec requirement that production peer federation uses
TLS:

- add `gonzbnet.allow_insecure_peer_http`, default `false`;
- reject outbound non-local `http://` peer URLs by default;
- allow insecure HTTP only for explicit local development loopback addresses;
- apply the policy to manual peers, peer exchange, pull sync, push sync,
  websocket gossip, and manifest resolution;
- surface policy issues in admin config validation.

This does not change local user authentication, Newznab behavior, or federation
node authentication semantics.
