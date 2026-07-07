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

- [x] Record pre-change query-mix duration on a representative dev database.
- [x] Start post-change query-mix measurement on the same database.
- [x] Complete persisted post-change refresh timing after the assemble backlog query is optimized or bounded.

### Baseline Measurements Captured On 2026-05-18

Environment:

- live local Docker database: `gonzb-postgres`, database `gonzb`
- branch: `sprint/dashboard-backlog-refresh-2026-05-18`
- measurement method: temporary Go harness using `pgindex.NewStore` plus direct SQL for removed diagnostic queries
- note: the old query-mix baseline excludes old per-stat persistence overhead, so it is a lower bound for the old refresh path

Live cardinality snapshot:

| Table | Rows | Timing |
| --- | ---: | ---: |
| `article_headers` | 79,109,360 | 10,586 ms |
| `article_header_ingest_payloads` | 57,456,637 | 34,368 ms |
| `binaries` | 12,491,709 | 7,188 ms |
| `binary_grouping_evidence` | 12,475,143 | 36,735 ms |
| `release_family_readiness_summaries` | 12,236,870 | 7,658 ms |
| `release_files` | 12,531 | 11 ms |
| `releases` | 1,370 | 5 ms |
| `binary_inspections` | 4,904 | 10 ms |

Old dashboard query-mix baseline:

| Stat/query | Value | Timing |
| --- | ---: | ---: |
| `unassembled_headers` | 55,155,159 | 58,702 ms |
| `pending_media_inspection_binaries` | 0 | 137 ms |
| `pending_release_candidate_families` | 11 | 220 ms |
| `payload_rows` | 57,456,637 | 9,040 ms |
| `payload_bytes` | 18,616,180,736 | 1 ms |
| `payload_dead_tuples` | 10,271,759 | 6 ms |
| `grouping_evidence_rows` | 12,484,239 | 10,799 ms |
| `grouping_evidence_bytes` | 20,819,902,464 | 1 ms |
| `grouping_evidence_dead_tuples` | 62,348 | 1 ms |
| `readiness_rows` | 12,257,576 | 3,161 ms |
| `readiness_bytes` | 6,547,013,632 | 1 ms |
| `readiness_dead_tuples` | 1,968,608 | 1 ms |
| total query time | | 82,070 ms |

Current backlog query-mix measurement:

| Stat/query | Value | Timing |
| --- | ---: | ---: |
| assemble backlog | 55,139,599 | 92,099 ms |
| release backlog | 4,151 | 1,077 ms |
| yEnc recovery backlog | 1,000 | 814 ms |
| inspect discovery backlog | 0 | 60 ms |
| inspect PAR2 backlog | 1,000 | 11,973 ms |
| inspect NFO backlog | 0 | 456 ms |
| inspect archive backlog | 0 | 126 ms |
| inspect password backlog | 0 | 8 ms |
| inspect media backlog | 0 | 165 ms |
| total query time | | 106,778 ms |

Cached read timing after the partial measurement run:

- `SELECT ... FROM indexer_dashboard_stats ...` for current backlog keys returned in `5.902 ms`.
- Only `unassembled_headers` and `pending_release_candidate_families` were refreshed before the long persisted refresh run was stopped.

Initial findings:

- Removing storage diagnostics is still worthwhile: exact diagnostic row counts alone consumed roughly `23,000 ms` in the old query mix, and standalone cardinality checks for the same hot tables took roughly `78,762 ms`.
- The dominant remaining bottleneck is now the exact assemble backlog count on `article_headers`, which measured between `58,702 ms` and `92,099 ms`.
- The bounded PAR2 backlog selector also needs follow-up; it returned the `1,000` row cap and still took `11,973 ms`.
- A complete persisted post-change refresh timing should wait until assemble backlog is no longer an exact full-table count, otherwise it mostly remeasures the known bottleneck.

### Post-Change Measurements Captured On 2026-05-19

Implementation notes:

- assemble backlog now uses the existing `idx_article_headers_pending_assembly` partial-index planner estimate instead of exact `COUNT(*)`
- yEnc recovery backlog now uses a capped weak-family candidate estimate rather than materializing ordered recovery candidates
- PAR2 inspection backlog now uses a capped simplified selector instead of the worker's distinct PAR2 set selector
- no new indexes were added

Optimized dashboard query mix:

| Stat/query | Value | Timing |
| --- | ---: | ---: |
| assemble backlog estimate | 57,642,904 | 10.55 ms |
| release backlog | 0 | 4,469.21 ms |
| yEnc recovery backlog | 1,000 | 898.74 ms |
| inspect discovery backlog | 0 | 63.86 ms |
| inspect PAR2 backlog | 1,000 | 8,163.30 ms |
| inspect NFO backlog | 0 | 102.92 ms |
| inspect archive backlog | 0 | 105.82 ms |
| inspect password backlog | 0 | 5.61 ms |
| inspect media backlog | 0 | 134.99 ms |
| total query time | | 13,955.00 ms |

Optimized persisted refresh timings:

| Run | Returned stats | Timing |
| --- | ---: | ---: |
| `RefreshIndexerDashboardStats` run 1 | 9 | 9,929.59 ms |
| `RefreshIndexerDashboardStats` run 2 | 9 | 2,749.12 ms |
| `RefreshIndexerDashboardStats` run 3 | 9 | 565.58 ms |

Optimized cached read timing:

- `GetIndexerDashboardStats` returned 9 stats in `0.45 ms`.

Post-change findings:

- The old dashboard query mix baseline was `82,070 ms`; the optimized query mix is `13,955 ms`, an approximately `83%` reduction.
- Persisted refresh is now bounded by release and PAR2 estimate cache warmth rather than the assemble full-table count.
- The assemble backlog card is intentionally estimated because exact counting over `article_headers` is too expensive for an admin refresh.
- PAR2 remains the slowest estimated inspection backlog on a cold run; no index was added in this pass because repeat persisted refresh runs are now acceptable and the plan deferred indexes unless measurements required them.

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

- [x] Current API routes, functions, and SQL/query sources are recorded in this document.
- [x] Current query-mix latency is measured on representative dev data.
- [x] Post-change refresh latency is measured on the same data.
- Sign-off: [x] baseline captured, refresh timing recorded.

### Backend Backlog API Simplification

- [x] Dashboard stats definitions contain only operator-useful backlog stats.
- [x] Cached GET remains fast and route-compatible.
- [x] Refresh recomputes only backlog stats.
- [x] Expensive inspection and yEnc backlog counts are marked as estimated when bounded candidate queries are used.
- Sign-off: [x] backend contract reviewed, old callers still work.

### Query Performance Pass

- [x] Release backlog count remains exact.
- [x] Assemble backlog uses a measured partial-index estimate because exact counting is too slow.
- [x] yEnc recovery and inspect subcommand backlogs use bounded candidate queries unless exact counts are proven cheap.
- [x] Rework assemble backlog away from an exact full-table `COUNT(*)` or add a measured index/estimate strategy.
- [x] Rework or further bound PAR2 backlog selection so the capped estimate returns quickly enough for refresh.
- [x] Index additions are deferred unless measurements show they are needed.
- Sign-off: [x] query timings improved, no new obvious table-scan regressions.

### Admin UI Cleanup

- [x] Dashboard backlog section is renamed for operational use.
- [x] Assemble/release/yEnc backlog cards are visually separated from inspect subcommand backlog cards.
- [x] Storage-maintenance cards are absent from the default dashboard backlog section.
- Sign-off: [x] UI reviewed for operator usefulness.

### Regression And Improvement Testing

- [x] Go tests cover stat definitions, refresh persistence, route compatibility, and backlog behavior.
- [x] Frontend build or tests cover the dashboard contract if UI rendering changes.
- [x] Before/after refresh timing is recorded.
- Sign-off: [x] tests pass, performance improvement documented, regressions checked.

## Assumptions

- Sprint starts from local `dev`.
- Existing admin stats and backlog routes stay available for compatibility.
- Default backlog refresh favors fast operator visibility over exact storage diagnostics.
- Estimated backlog values are acceptable when they avoid slow full-table candidate counts.
