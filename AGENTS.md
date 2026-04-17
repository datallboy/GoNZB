# AGENTS.md

## Codex Operating Mode For This Repository
Codex may edit code directly in this repository. Unless the user says otherwise, assume implementation is allowed and make the requested changes in-place for later review.

## Primary Working Rules
1. Keep changes focused on the user's request.
2. Prefer direct edits over long plans or large speculative rewrites.
3. Keep responses concise and practical.
4. Avoid unrelated cleanup unless it is required to complete the task safely.
5. If the user references a markdown file in this repo or in the session, treat that markdown as the primary scope and source of direction for the task.
6. For current indexer foundation, hardening, or API/UI expansion work, prefer the active docs in `docs/active/` before archived planning docs.

## Active Docs Priority
- `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`
  - top-level sequence and phase gates for the next indexer era
- `docs/active/INDEXER_NORMALIZATION_AND_STORAGE_PLAN.md`
  - Phase 1 execution plan for normalization, storage, and repository-boundary cleanup
- `docs/active/RELEASE_QUALITY_AND_API_SURFACE_HARDENING_PLAN.md`
  - Phase 2 execution plan for stable release-contract hardening
- `docs/active/INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md`
  - Phase 3 execution plan for the first user-facing indexer API and web UI work
- `docs/active/INDEXER_FOUNDATION_DOCS.md`
  - docs map describing which docs are active, reference, or archived

When working on indexer foundation tasks, use these active docs as the default source of truth unless the user explicitly redirects you elsewhere.

For completed stabilization history, release-formation baseline, or prior schema-target decisions, use:

- `docs/archive/completed/indexer/INDEXER_STABILIZATION_WORKLIST.md`
- `docs/archive/completed/indexer/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`
- `docs/archive/completed/indexer/INDEXER_SCHEMA_TARGET.md`

Do not treat those archived docs as the active backlog for current execution.

## Scope And Decision Making
- Default to making the change instead of only describing it.
- Use short explanations, short snippets, and short summaries unless the user asks for more detail.
- Ask before making broad architectural changes, destructive data changes, dependency overhauls, or work that extends beyond the referenced task context.
- Surface assumptions briefly when they affect behavior or design.

## Repository Guidance
- Preserve module independence where practical.
- Do not introduce hidden hard dependencies between the aggregator, usenet-indexer, and downloader modules unless explicitly requested.
- Keep implementation compatible with these deployment shapes when relevant:
  1. downloader-only
  2. aggregator-only
  3. usenet-indexer-only
  4. all-in-one

## Preferred Response Style
1. Brief outcome summary.
2. Files changed.
3. Short validation note or next check when useful.

## Session Bootstrap Prompt
Use this prompt at the start of Codex chats when needed:

```text
- You may edit files directly in this repository unless I say otherwise.
- Keep changes focused, reviewable, and concise.
- Use any markdown file I reference in the repo or this session as primary task context.
- Do not drift into unrelated refactors or broad rewrites unless I ask for them.
- Keep explanations short and practical.
```
