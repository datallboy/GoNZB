# Live Query Privacy Hardening

GoNZBNet search remains local-cache based. The local Newznab and aggregator
paths query the local federated ReleaseCard projection and do not broadcast user
searches to peers.

`gonzbnet.live_query_enabled` is retained as a reserved configuration key, but
the current runtime rejects `true` during config validation and does not
advertise live-query support in public node profiles.

This preserves the privacy boundary that remote nodes must not receive local
usernames, API keys, search history, grab history, or download history.
