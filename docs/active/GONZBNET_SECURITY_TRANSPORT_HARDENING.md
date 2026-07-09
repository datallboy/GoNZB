# GoNZBNet Transport Security Cleanup

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` sections 23.2,
23.4, and 27.

This cleanup closes public federation transport gaps that were not represented
as a named implementation phase:

- enforce a GoNZBNet-specific JSON body cap for `/gonzbnet/v1/*` routes;
- rate-limit protected inbox and manifest routes;
- expose `gonzbnet.rate_limit_events_per_minute`;
- align node profile rate-limit advertisement with the runtime limiter;
- return stable machine-readable federation error codes from public federation
  HTTP handlers;
- normalize visible inbox/gossip rejection codes to the spec error vocabulary.

This work does not add new federation features, remote login, live remote
search, cross-node user identity, or changes to local Newznab behavior.
