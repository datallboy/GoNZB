# GoNZBNet Addendum Phase E: Dedup-Aware Local Scheduler

## Spec Scope

Add local scheduler read paths that help scanner nodes avoid duplicate work:

- local work suggestion endpoint
- claim/completion lookup
- skip duplicate primary work
- preserve validation overlap
- coverage score computation

## Implementation Plan

1. Add store DTOs and methods for coverage work suggestions.
2. Suggestions read local coverage projections and exclude assignments blocked by
   trusted active claims or completed ranges.
3. Add a mode flag:
   - `scanner`: skip trusted active and completed work
   - `validator`: allow overlap of completed work for validation/recheck
4. Low-trust claims do not block suggestions. Trust comes from
   `federation_nodes.local_trust_score`; unknown/zero trust does not block.
5. Extend coverage dashboard reads with gap, duplicate, and coverage score
   summary fields.
6. Add `GET /api/v1/admin/gonzbnet/coverage/suggestions`.

## Assumptions

- This is still local scheduling guidance, not automatic assignment creation.
- The endpoint uses projected signed claims/outcomes; it does not live-query
  peers.
- High-priority assignments remain suggestible when only low-trust claims
  overlap them.

## Out Of Scope

- Automated global optimizer.
- Claim creation from suggestions.
- UI components beyond the API read model.
