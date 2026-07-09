# GoNZBNet Admin: Recompute Scores

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` admin actions.

This cleanup adds the local admin action to recompute federated scores:

- add a store method that recomputes federated release-source availability,
  validation, checksum, trust, and aggregate release-card scores for a pool;
- add an admin endpoint under `/api/v1/admin/gonzbnet/scores/recompute`;
- add a small admin UI action for the selected pool;
- keep the action local-only and avoid sending user data or search context to
  peers.

Out of scope:

- changing the scoring formula;
- changing reputation event generation;
- scheduling automatic recomputation.
