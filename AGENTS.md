# AGENTS.md

## Codex Operating Mode For This Repository
Codex may edit code directly in this repository. Unless the user says otherwise, assume implementation is allowed and make the requested changes in-place for later review.

## Primary Working Rules
1. Keep changes focused on the user's request.
2. Prefer direct edits over long plans or large speculative rewrites.
3. Keep responses concise and practical.
4. Avoid unrelated cleanup unless it is required to complete the task safely.
5. If the user references a markdown file in this repo or in the session, treat that markdown as the primary scope and source of direction for the task.
6. For current indexer foundation, hardening, refinement, or API/UI expansion work, prefer the active docs in `docs/active/` before archived planning docs.
7. Prefer incremental, trackable changes that can be committed in reviewable chunks instead of letting large, mixed, uncommitted diffs accumulate.

## Active Docs Priority
- Start with `docs/active/INDEXER_FOUNDATION_DOCS.md`.
  - It defines which docs in `docs/active/` are the current execution guides and which docs are now historical or reference-only.
- Then use the most relevant active plan(s) in `docs/active/` for the current phase.
  - Example categories include roadmap, refinement/stabilization, and API/UI expansion.

When working on indexer foundation tasks, use the active docs in `docs/active/` as the default source of truth unless the user explicitly redirects you elsewhere.

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
