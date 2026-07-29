# Indexer Recovery Profiles

Status: completed 2026-07-29
Branch: `feat/indexer-recovery-profiles`
Depends on: `audit/indexer-sustained-workload`

## Goal

Give operators one understandable runtime choice for how much NNTP BODY work
the indexer spends recovering weak or obfuscated article headers. Preserve the
existing detailed tuning controls, but make the normal behavior selectable with
three explicit profiles.

## Profiles

### Header only

- Assemble only from XOVER/HEAD evidence.
- Do not run `recover_yenc`, issue recovery BODY requests, seed yEnc work, or
  let dormant yEnc rows apply scrape backpressure.
- Keep unresolved articles and binaries provisional until normal outcome
  retention classifies them. Do not fabricate grouping from weak proximity,
  poster, Message-ID, or `(part/total)` evidence.
- Preserve queued evidence so switching to a deeper profile can resume recovery.

Best for inexpensive discovery of clear and stable multipart Subjects.

### Balanced

- Default profile.
- Run yEnc recovery only for priority-0 work that is likely to unlock an
  incomplete multipart binary or release.
- Admit scheduler-ranked multipart pressure and productive opaque cohorts.
- Do not run generic priority-1/2 backlog seeding or claim lower-priority work.
- Keep normal retry/backoff, queue caps, target windows, and terminal-attempt
  policy for admitted priority-0 work.

Best for normal unattended indexing where BODY traffic should produce a likely
release benefit.

### Exhaustive

- Preserve the current full recovery behavior.
- Process priority-0, priority-1, and priority-2 eligible work.
- Allow generic weak/provisional backlog seeding after priority work drains.
- Continue through the configured retry and terminal-attempt policy.

Best for targeted historical work, investigation, and operators willing to
trade NNTP traffic and time for maximum recoverable coverage.

Exhaustive does not guess an identity for fully randomized posts. If Subject,
yEnc name, and other evidence are independently randomized, the work remains
unclassifiable unless a trusted manifest or equivalent evidence becomes
available.

## Runtime Contract

- Add `indexing.recovery_profile` with values `header_only`, `balanced`, and
  `exhaustive`.
- Default new and existing configurations without an explicit value to
  `balanced`.
- Expose the setting in the Indexer runtime-settings UI with a plain-language
  description of cost, expected coverage, and behavior.
- Keep `recover_yenc.enabled` as the stage on/off switch. The effective stage
  runs only when it is enabled and the selected profile permits BODY recovery.
- Record the effective profile in recovery-stage metrics and gate reasons.

## Implementation

1. Add runtime/config types, normalization, validation, defaults, and UI.
2. Pass the profile into yEnc selection.
3. Make balanced selection priority-0-only and suppress generic seeding.
4. Disable recovery execution and opaque yEnc cohort scheduling in header-only
   mode.
5. Make admission/backpressure and terminal outcome accounting ignore work that
   the active profile cannot execute.
6. Preserve queued work across profile changes so moving to a deeper profile
   resumes rather than rebuilding source state.
7. Update maintained indexer wiki pages and add focused unit/PostgreSQL tests.

## Acceptance Criteria

- Header-only runs issue zero yEnc recovery BODY requests.
- Balanced claims only `priority_rank = 0` and never invokes generic seeding.
- Exhaustive retains existing priority and generic-seeding behavior.
- Header-only and balanced are not paused by lower-priority dormant yEnc rows.
- Switching from balanced to exhaustive makes existing lower-priority ready
  work eligible without destructive queue rewrites.
- Invalid profile values are rejected by runtime-settings validation and
  normalized safely when reading legacy configuration.
- `go test ./...`, UI tests/build, migration tests, and PostgreSQL repository
  tests pass.

## Completion

- Runtime settings, YAML configuration, validation, and the Web UI expose all
  three profiles with Balanced as the default.
- Selection, admission capacity, NNTP contention gates, cohort scheduling, and
  outcome retention count only work executable by the active profile.
- Profile changes preserve dormant lower-priority work and refresh the
  operator-visible capacity snapshot immediately.
- Focused unit tests, the full PostgreSQL repository suite on a disposable
  checksummed PostgreSQL 17 database, UI lint, and the production UI build
  passed.
