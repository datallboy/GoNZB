# 2026-05-18 Dashboard Backlog Refresh Sprint

Sprint branch: `sprint/dashboard-backlog-refresh-2026-05-18`

## Summary

Simplify the admin dashboard backlog stats so the default view is useful to operators and faster to refresh. The dashboard should focus on stage backlogs instead of mixing queue counts with storage diagnostics.

## Baseline

Current dashboard API calls:

- `GET /api/v1/admin/indexer/overview`
- `GET /api/v1/admin/indexer/overview/stats`
- `POST /api/v1/admin/indexer/overview/stats/actions/refresh`
- `GET /api/v1/admin/indexer/overview/backlog`
- `POST /api/v1/admin/indexer/overview/backlog/actions/refresh`

Current UI callers:

- `getAdminDashboardStats()`
- `refreshAdminDashboardStats()`

Current refresh behavior:

- `RefreshIndexerDashboardStats` loops over every `indexerDashboardStatDefinitions` entry.
- Each stat is recomputed serially, persisted to `indexer_dashboard_stats`, and then the cached stats are reloaded.
- The old stat set included table row counts, table byte counts, and dead tuple counters alongside actual backlog counts.

Current cached read:

```sql
SELECT stat_key, int_value, updated_at, refresh_attempted_at, last_error
FROM indexer_dashboard_stats
WHERE stat_key = ANY($1)
ORDER BY stat_key;
```

Current exact assemble backlog query:

```sql
SELECT COUNT(*)
FROM article_headers
WHERE assembled_at IS NULL;
```

Current exact release backlog query:

```sql
SELECT COUNT(*)
FROM release_family_readiness_summaries
WHERE updated_at > COALESCE(processed_at, updated_at);
```

The prior media inspection backlog used the full `inspect_media` candidate filter against `binaries`, `release_files`, `releases`, and `binary_inspections`, including rerun and error predicates.

The removed storage/admin diagnostic stats used:

```sql
SELECT COUNT(*) FROM article_header_ingest_payloads;
SELECT COUNT(*) FROM binary_grouping_evidence;
SELECT COUNT(*) FROM release_family_readiness_summaries;
SELECT COALESCE(pg_total_relation_size($1::regclass), 0);
SELECT COALESCE(n_dead_tup, 0) FROM pg_stat_user_tables WHERE relname = $1;
```

Baseline measurement:

- [ ] Record pre-change refresh duration on a representative dev database.
- [ ] Record post-change refresh duration on the same database.

## Backlog Model

Default dashboard stats should include only:

- assemble backlog
- release backlog
- yEnc recovery backlog
- inspect discovery backlog
- PAR2 inspection backlog
- NFO inspection backlog
- archive inspection backlog
- password inspection backlog
- media inspection backlog

Storage row counts, table sizes, and dead tuple diagnostics are intentionally out of scope for the dashboard backlog section. If still needed, they should move to a dedicated diagnostics or maintenance view.

## Sprint Sections And Sign-Offs

### Baseline And Measurement

- [ ] Current API routes, functions, and SQL/query sources are recorded in this document.
- [ ] Current refresh latency is measured on representative dev data.
- [ ] Post-change refresh latency is measured on the same data.
- Sign-off: [ ] baseline captured, refresh timing recorded.

### Backend Backlog API Simplification

- [ ] Dashboard stats definitions contain only operator-useful backlog stats.
- [ ] Cached GET remains fast and route-compatible.
- [ ] Refresh recomputes only backlog stats.
- [ ] Expensive inspection and yEnc backlog counts are marked as estimated when bounded candidate queries are used.
- Sign-off: [ ] backend contract reviewed, old callers still work.

### Query Performance Pass

- [ ] Assemble and release backlog counts remain exact.
- [ ] yEnc recovery and inspect subcommand backlogs use bounded candidate queries unless exact counts are proven cheap.
- [ ] Index additions are deferred unless measurements show they are needed.
- Sign-off: [ ] query timings improved, no new obvious table-scan regressions.

### Admin UI Cleanup

- [ ] Dashboard backlog section is renamed for operational use.
- [ ] Assemble/release/yEnc backlog cards are visually separated from inspect subcommand backlog cards.
- [ ] Storage-maintenance cards are absent from the default dashboard backlog section.
- Sign-off: [ ] UI reviewed for operator usefulness.

### Regression And Improvement Testing

- [ ] Go tests cover stat definitions, refresh persistence, route compatibility, and backlog behavior.
- [ ] Frontend build or tests cover the dashboard contract if UI rendering changes.
- [ ] Before/after refresh timing is recorded.
- Sign-off: [ ] tests pass, performance improvement documented, regressions checked.

## Assumptions

- Sprint starts from local `dev`.
- Existing admin stats and backlog routes stay available for compatibility.
- Default backlog refresh favors fast operator visibility over exact storage diagnostics.
- Estimated backlog values are acceptable when they avoid slow full-table candidate counts.
