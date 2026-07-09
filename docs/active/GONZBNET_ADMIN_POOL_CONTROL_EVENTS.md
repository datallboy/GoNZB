# GoNZBNet Admin: Pool Control Event Views

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` admin views.

This cleanup adds a local admin view for accepted pool-control workflow events:

- join requests;
- member approval events;
- member revocation events.

The view reads accepted events from the local append-only federation event log.
It does not create a new projection table and does not expose local usernames,
API keys, search history, grab history, or download history.
