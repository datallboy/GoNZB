# GoNZBNet Observability And Pool Reporting Plan

Status: complete (2026-07-21)

## Outcome

The GoNZBNet admin UI must explain what the local node does, whether each
enabled behavior is working, what useful data it exchanges, and whether its
pools have enough fresh contributions to function. Operators should not need
to infer activity from configuration flags, protocol event names, or logs.

The implementation is centered on four understandable operator jobs plus a
shared connection layer. Observability remains read-only and links to
**Settings > GoNZBNet** for changes.

## Operator Job Model

| Operator job | Underlying behavior |
| --- | --- |
| Find and use releases | Consumer, aggregator source, index projection, manifest resolution and cache |
| Contribute releases | Scanner output, release-card publication, manifest building and availability |
| Verify release health | Validator, article availability, checksum work, and health attestations |
| Coordinate scanning | Coverage assignments, claims, checkpoints, scheduling, and reassignment |
| Connection layer | Pull, push, gossip, peer exchange, relay, and admission relay |

These are presentation groups, not new protocol capabilities. Each group must
show its purpose, inputs, outputs, execution style, supported pools, last useful
work, and missing prerequisites. Enabled groups appear first. Disabled groups
remain visible as collapsed `Off` cards so an operator can learn what is
available without giving inactive functionality equal visual weight.

Scheduled workers display their cadence and next expected run. Event-driven
and on-demand components explicitly say `runs when events arrive` or `runs when
a grab needs a manifest`; lack of recent work alone must never mark them broken.

## Status Contract

The admin API uses `off`, `starting`, `ready`, `working`, `degraded`, and
`blocked`. The UI presents those as `Off`, `Starting`, `Ready`, `Working`, and
`Needs attention`, with the technical state available in expanded detail.

- A scheduled worker is `ready` after a successful pass within two configured
  intervals and `working` while a pass is active.
- Three consecutive failures or two missed intervals produce `degraded` after
  an initial grace period of the greater of two intervals or five minutes.
- An event-driven or on-demand component is `ready` while eligible and waiting;
  it degrades only after actual failures or an actionable backlog condition.
- Missing module dependencies, pool membership, pool capability grants, NNTP
  access, or indexer prerequisites produce `blocked` with a concrete remedy.
- Optional disabled components do not degrade their parent job. A parent job
  uses the most severe state among its required enabled components.
- Configuration completeness warnings remain distinct from runtime failures;
  for example, a consumer without automatic sync is usable manually but should
  show `No automatic synchronization configured`.

Advertised NodeProfile capabilities must be audited against effective runtime
behavior. In particular, manifest-builder advertisement must no longer be
hard-coded off when the configured behavior is active.

## WebUI Information Architecture

Replace `Overview` and `Advanced` with four deep-linkable views: `Overview`,
`Roles`, `Pools`, and `Activity`. Preserve every existing administration and
diagnostic operation, but move rare or dangerous controls into contextual
collapsed panels or an `Advanced tools` drawer.

### Overview

- Lead with jobs working, connected peers, active pools, and shared-data
  freshness rather than raw database row counts.
- Show a `Needs attention` panel only when actionable problems exist.
- Present compact summaries of the four jobs and connection layer.
- Describe recent useful activity in plain language.
- Display create/join onboarding prominently only when the node has no pool;
  otherwise expose it through `Add or join pool`.
- Surface pending admissions as an actionable banner.
- Remove raw configuration dumps and cumulative protocol counters from the
  primary overview.

### Roles

- Show grouped job cards with expandable technical components.
- Display local enablement and pool eligibility separately.
- Explain incomplete bundles such as `Consumer enabled, but no automatic
  synchronization is configured`.
- Show `Last useful work`, `Last checked`, and `Next run` only where meaningful.
- Provide one `Change GoNZBNet settings` link. Do not duplicate individual
  toggles or introduce hidden role presets.

### Pools

- List pools with member count, reachable peers, latest exchange, contributing
  members, and evidence freshness.
- Show member aliases first and shortened node IDs second.
- Translate technical capabilities into the operator jobs.
- Show which members contribute releases, manifests, validation, health,
  coverage, or relay service.
- Keep admissions, membership, governance, role access, moderation, and
  destructive operations in contextual management panels.

Pool health must separate two related forms of evidence:

- **Release health:** complete, repairable, incomplete, missing,
  provider-limited, or unverified, including confidence and repair evidence.
- **Article reachability:** available, partial, missing, failed, skipped, or
  unverified, including checked/available/missing article counts.

Both views show evidence age, reporting node, method, disagreement, missing
evidence, and scoped provider disclosure without exposing provider identities.

### Activity

- Provide URL-backed filters for 24 hours, 7 days, or 30 days; pool; operator
  job; and node.
- Graph accepted/rejected inbound and outbound events; releases/manifests and
  resolution/cache outcomes; health/article-availability status; and coverage
  completion/failure/stale claims.
- Every chart includes a plain-language summary, legend, hover/focus detail,
  and accessible table fallback.
- Every summary links to filtered, bounded diagnostic rows.
- Prometheus, configuration validation, raw events, manual manifest resolve,
  score recomputation, peer controls, key management, and other rare tools live
  under `Advanced tools`.

Charts use small reusable native React/SVG components with responsive
`viewBox` geometry and CSS styling. No production chart dependency is added.

## Reporting Model

Keep three evidence classes distinct in API responses and UI labels:

- **Local runtime facts:** worker state, attempts, success/failure, next run,
  work count, backlog, duration, and recent sanitized error.
- **Signed pool facts:** accepted events, memberships/capabilities, health and
  article-availability attestations, coverage claims/outcomes, and manifest
  availability attributed to their signing node and pool.
- **Derived pool health:** freshness, success ratios, disagreement, coverage
  gaps, peer reachability, and resolvability calculated locally from signed
  facts and explicitly labelled as derived.

`HealthAttestation` and `ArticleAvailabilityAttestation` remain protocol data,
not settings. The WebUI summarizes their existing projections and provides
filtered drill-down without requiring raw JSON.

## Backend Activity Recording

Instrument actual scheduled, event-driven, and on-demand paths through a shared
activity recorder. Cover admission polling, release/manifest publication,
health publication, validation, pull/push/gossip, projection, manifest
resolution/cache, scanner coordination, coverage reassignment, relay, and peer
exchange.

Each record may contain component/job key, execution mode, pool, attempt and
completion timestamps, success/failure, items, bytes in/out, duration, backlog,
and sanitized error. The recorder updates memory on hot paths and never inserts
one row per article, event, or worker item.

## Low-Write History

Add a bounded generic activity-rollup table in migration `024`:

- flush local activity in one database batch every five minutes;
- derive signed pool history from durable federation tables rather than
  treating process counters as source evidence;
- retain five-minute buckets for 48 hours and hourly buckets for 90 days;
- compact and expire buckets in the same coarse maintenance pass;
- combine persisted last activity with live registry state after restart;
- expose collection freshness, retention, and partial-history warnings.

The existing process-local Prometheus registry remains available. New metrics
must use bounded labels and must not place pool IDs, node IDs, release IDs, or
manifest IDs in unbounded Prometheus labels.

## Admin API

Add authenticated, read-only reporting endpoints shaped for the WebUI:

- `GET /api/v1/admin/gonzbnet/overview`
- `GET /api/v1/admin/gonzbnet/roles`
- `GET /api/v1/admin/gonzbnet/activity`
- `GET /api/v1/admin/gonzbnet/pools/{pool_id}/health`
- `GET /api/v1/admin/gonzbnet/diagnostics/article-availability`

Responses include `generated_at`, data freshness, component execution mode,
configured/eligible state, status and reason, supported pools, last/next
timestamps, bounded series, retention metadata, and partial-data warnings.
Existing detailed diagnostic and Prometheus endpoints remain compatible.

## Access, Privacy, And Safety

- Reuse granular local GoNZBNet admin-read permissions and pool scoping.
- Never expose local usernames, searches, grabs, download history, NNTP
  credentials, raw provider names, or unapproved provider/source identifiers.
- Label local observations separately from signed remote claims.
- Do not add a public federation endpoint for local worker internals.
- Bound reporting queries by pool, time window, pagination, and indexed order.
- Truncate and sanitize stored/displayed errors.

## Implementation Workstreams

1. Define the activity/status contract, audit advertised capabilities, and
   instrument runtime paths.
2. Add rollup persistence, compaction, aggregate health/contribution queries,
   and reporting APIs.
3. Refactor the current large admin component into the four views and shared
   reporting components.
4. Add native accessible chart components and filtered diagnostic drill-down.
5. Move all existing controls into their appropriate pool-management or
   advanced-tools locations without removing behavior.
6. Update maintained documentation and the four-node E2E fixture.

## Acceptance Criteria

- An operator can identify what every enabled job does, whether it is working,
  what it last accomplished, and why it is blocked without logs or raw JSON.
- Healthy inactivity for event-driven/on-demand work is displayed as `Ready`,
  not failure.
- A pool admin can see which members contribute releases, manifests,
  validation/health evidence, coverage outcomes, and relay service.
- Health and article availability are understandable, visibly distinct, and
  show freshness/disagreement.
- Every summary can be traced to signed data or a clearly labelled local fact.
- Graph history survives restart, remains bounded, and adds only coarse batched
  writes.
- Backend tests cover state derivation, grants/prerequisites, rollups,
  retention, restart, pool scoping, aggregation, and authorization.
- The production TypeScript build, focused lint, Go state/API tests, and
  four-node E2E fixture cover grouped roles, disabled/empty states, warnings,
  pool selection, accessible chart fallbacks, filters, degraded behavior, and
  cross-node contribution reporting. The repository does not currently carry
  a browser-unit test runner.
- The existing four-node E2E topology demonstrates scanner, validator,
  consumer, and relay contributions under the correct jobs and pools.

External alert delivery and user-configurable alert thresholds remain future
work until the measurements and default presentation have been validated by
operators.
