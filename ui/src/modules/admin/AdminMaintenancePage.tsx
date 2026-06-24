import { useEffect, useState } from "react"
import { Link } from "react-router-dom"
import {
  dryRunAdminMaintenanceTask,
  getAdminMaintenanceStorageAudit,
  getAdminMaintenanceTasks,
  patchAdminMaintenanceTask,
  runAdminMaintenanceTask,
} from "../../shared/api/admin"
import {
  formatBytes,
  formatDateTime,
  formatNumber,
} from "../../shared/lib/format"
import type {
  AdminMaintenanceTask,
  AdminMaintenanceTaskRun,
  AdminMaintenanceStorageSnapshot,
  AdminStorageAuditReport,
} from "../../shared/types"

function formatRows(rows?: Record<string, number>) {
  if (!rows || Object.keys(rows).length === 0) {
    return "No rows estimated."
  }
  return Object.entries(rows)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([table, count]) => `${table}: ${count.toLocaleString()}`)
    .join(" · ")
}

function firstTableSnapshot(snapshot?: AdminMaintenanceStorageSnapshot) {
  const tableBytes = snapshot?.table_total_bytes_by_table || {}
  const table = Object.keys(tableBytes).sort()[0]
  if (!snapshot || !table) {
    return null
  }
  return {
    table,
    totalBytes: tableBytes[table],
    liveRows: snapshot.table_live_rows_by_table?.[table],
    deadRows: snapshot.table_dead_rows_by_table?.[table],
  }
}

function formatStorageDelta(before?: number, after?: number) {
  if (before == null || after == null) {
    return "n/a"
  }
  const delta = after - before
  if (delta === 0) {
    return "no change"
  }
  const prefix = delta > 0 ? "+" : "-"
  return `${prefix}${formatBytes(Math.abs(delta))}`
}

function riskClassName(risk?: string) {
  const normalized = (risk || "unknown").toLowerCase()
  if (normalized === "low") {
    return "risk-pill risk-pill--low"
  }
  if (normalized === "medium") {
    return "risk-pill risk-pill--medium"
  }
  if (normalized === "high" || normalized === "blocker") {
    return "risk-pill risk-pill--high"
  }
  return "risk-pill"
}

function RiskPill({ risk }: { risk?: string }) {
  return <span className={riskClassName(risk)}>{risk || "unknown"}</span>
}

function MaintenanceStorageSnapshotView({
  before,
  after,
}: {
  before?: AdminMaintenanceStorageSnapshot
  after?: AdminMaintenanceStorageSnapshot
}) {
  if (!before && !after) {
    return null
  }
  const beforeTable = firstTableSnapshot(before)
  const afterTable = firstTableSnapshot(after)
  const tableName = beforeTable?.table || afterTable?.table
  return (
    <div className="banner">
      <strong>Storage snapshot</strong>
      <div className="muted-copy">
        Database: {before ? formatBytes(before.database_bytes) : "n/a"}
        {after
          ? ` -> ${formatBytes(after.database_bytes)} (${formatStorageDelta(
              before?.database_bytes,
              after.database_bytes,
            )})`
          : ""}
      </div>
      {before?.filesystem_visible || after?.filesystem_visible ? (
        <div className="muted-copy">
          Filesystem free:{" "}
          {before ? formatBytes(before.filesystem_free_bytes || 0) : "n/a"}
          {after
            ? ` -> ${formatBytes(
                after.filesystem_free_bytes || 0,
              )} (${formatStorageDelta(
                before?.filesystem_free_bytes,
                after.filesystem_free_bytes,
              )})`
            : ""}
        </div>
      ) : (
        <div className="muted-copy">Filesystem free space was not visible.</div>
      )}
      {tableName ? (
        <div className="muted-copy">
          {tableName}:{" "}
          {beforeTable ? formatBytes(beforeTable.totalBytes) : "n/a"}
          {afterTable
            ? ` -> ${formatBytes(afterTable.totalBytes)} (${formatStorageDelta(
                beforeTable?.totalBytes,
                afterTable.totalBytes,
              )})`
            : ""}
          {afterTable?.deadRows != null
            ? ` · dead rows ${formatNumber(afterTable.deadRows)}`
            : ""}
        </div>
      ) : null}
    </div>
  )
}

function MaintenanceTaskCard({
  task,
  onRefresh,
}: {
  task: AdminMaintenanceTask
  onRefresh: () => Promise<void>
}) {
  const [message, setMessage] = useState<string | null>(null)
  const [result, setResult] = useState<AdminMaintenanceTaskRun | null>(null)
  const [scheduleEnabled, setScheduleEnabled] = useState(task.schedule_enabled)
  const [intervalHours, setIntervalHours] = useState(task.interval_hours || 24)
  const [batchSize, setBatchSize] = useState(task.batch_size || 100)

  useEffect(() => {
    setScheduleEnabled(task.schedule_enabled)
    setIntervalHours(task.interval_hours || 24)
    setBatchSize(task.batch_size || 100)
  }, [task])

  async function dryRun() {
    setMessage(null)
    try {
      const next = await dryRunAdminMaintenanceTask(task.task_key)
      setResult(next)
      setMessage("Dry-run completed.")
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Dry-run failed")
    }
  }

  async function run() {
    setMessage(null)
    try {
      const next = await runAdminMaintenanceTask(task.task_key)
      setResult(next)
      setMessage("Run completed.")
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Run failed")
    }
  }

  async function saveSchedule() {
    setMessage(null)
    try {
      await patchAdminMaintenanceTask(task.task_key, {
        schedule_enabled: scheduleEnabled,
        interval_hours: intervalHours,
        ...(task.uses_batch_size ? { batch_size: batchSize } : {}),
      })
      setMessage("Schedule saved.")
      await onRefresh()
    } catch (err) {
      setMessage(
        err instanceof Error ? err.message : "Failed to save schedule",
      )
    }
  }

  const hasFreshDryRun =
    task.last_dry_run_at &&
    Date.now() - Date.parse(task.last_dry_run_at) < 30 * 60 * 1000
  const rows = result?.estimated_rows_by_table || result?.deleted_rows_by_table

  return (
    <div className="page-card stack">
      <div className="page-header">
        <div>
          <h2 className="section-title">{task.label}</h2>
          <p className="muted-copy">{task.purpose}</p>
          <p className="muted-copy">
            {task.destructive ? "Destructive" : "Non-destructive"} ·{" "}
            <RiskPill risk={task.risk} /> · schedule{" "}
            {task.schedule_enabled ? "enabled" : "disabled"}
            {task.uses_batch_size ? ` · batch ${task.batch_size}` : ""}
          </p>
          <p className="muted-copy">
            Last dry-run:{" "}
            {task.last_dry_run_at
              ? new Date(task.last_dry_run_at).toLocaleString()
              : "never"}{" "}
            · latest run:{" "}
            {task.last_run ? (
              <Link
                className="table-link"
                to={`/admin/indexer/runs/${task.last_run.id}`}
              >
                #{task.last_run.id}
              </Link>
            ) : (
              "never"
            )}
          </p>
        </div>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            onClick={() => void dryRun()}
          >
            Dry Run
          </button>
          <button
            className="secondary-button"
            type="button"
            onClick={() => void run()}
            disabled={task.destructive && !hasFreshDryRun}
          >
            Run
          </button>
        </div>
      </div>

      <div className="form-grid">
        <div>
          <strong>Space</strong>
          <p className="muted-copy">{task.space_effect || "Not documented."}</p>
        </div>
        <div>
          <strong>Supervisor</strong>
          <p className="muted-copy">
            {task.supervisor_effect || "Not documented."}
          </p>
        </div>
        <div>
          <strong>Data</strong>
          <p className="muted-copy">{task.data_effect || "Not documented."}</p>
        </div>
        <div>
          <strong>Release safety</strong>
          <p className="muted-copy">
            {task.release_safety || "Not documented."}
          </p>
        </div>
      </div>

      {task.warnings?.length ? (
        <div className="banner">{task.warnings.join(" · ")}</div>
      ) : null}
      {task.destructive ? (
        <div className="banner">
          Run requires a dry-run completed within the last 30 minutes.
        </div>
      ) : null}
      {rows ? <div className="banner">{formatRows(rows)}</div> : null}
      {result?.vacuumed_tables?.length ? (
        <div className="banner">
          VACUUM (ANALYZE): {result.vacuumed_tables.join(", ")}. Space is
          reusable inside PostgreSQL; OS disk space is not returned without
          VACUUM FULL, pg_repack, CLUSTER, or a table rewrite.
        </div>
      ) : null}
      <MaintenanceStorageSnapshotView
        before={result?.before_storage}
        after={result?.after_storage}
      />
      {result?.blockers?.length ? (
        <div className="banner error">{result.blockers.join(" · ")}</div>
      ) : null}

      <div className="form-grid">
        <label className="field checkbox-field">
          <input
            type="checkbox"
            checked={scheduleEnabled}
            onChange={(event) => setScheduleEnabled(event.target.checked)}
          />
          <span>Schedule enabled</span>
        </label>
        <label className="field">
          <span>Interval hours</span>
          <input
            type="number"
            min={task.min_interval_hours || 6}
            value={intervalHours}
            onChange={(event) => setIntervalHours(Number(event.target.value))}
          />
        </label>
        {task.uses_batch_size ? (
          <label className="field">
            <span>Batch size</span>
            <input
              type="number"
              min={1}
              value={batchSize}
              onChange={(event) => setBatchSize(Number(event.target.value))}
            />
          </label>
        ) : null}
        <div className="button-row">
          <button
            className="secondary-button"
            type="button"
            onClick={() => void saveSchedule()}
          >
            Save
          </button>
        </div>
      </div>
      {message ? <div className="banner">{message}</div> : null}
    </div>
  )
}

function StorageAuditPanel({
  audit,
  loading,
  onRefresh,
}: {
  audit: AdminStorageAuditReport | null
  loading: boolean
  onRefresh: () => Promise<void>
}) {
  const topTables = audit?.tables.slice(0, 10) || []
  const topIndexes = audit?.indexes.slice(0, 10) || []
  const sourceAges = audit?.source_ages || []
  const sourceWindows = audit?.source_windows || []
  const yencBacklog = audit?.yenc_backlog || []
  const guardCounts = audit?.guard_counts || []
  const cleanupMatrix = audit?.cleanup_matrix || []

  return (
    <>
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Read-only storage audit</p>
            <h2 className="section-title">Database size and cleanup impact.</h2>
            <p className="muted-copy">
              This report reads PostgreSQL catalog/stat views and count
              estimates only. It does not drop indexes or delete rows.
            </p>
            <p className="muted-copy">
              Generated:{" "}
              {audit?.generated_at
                ? formatDateTime(audit.generated_at)
                : "not loaded"}
            </p>
          </div>
          <button
            className="secondary-button"
            type="button"
            onClick={() => void onRefresh()}
            disabled={loading}
          >
            {loading ? "Refreshing..." : "Refresh Audit"}
          </button>
        </div>
        {loading ? (
          <div className="banner">
            Generating PostgreSQL storage audit. Large indexer databases can
            take about a minute to summarize.
          </div>
        ) : null}
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Largest tables</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Table</th>
              <th>Rows</th>
              <th>Total</th>
              <th>Table</th>
              <th>Indexes</th>
              <th>Dead tuples</th>
              <th>Vacuum</th>
            </tr>
          </thead>
          <tbody>
            {topTables.map((item) => (
              <tr key={item.table_name}>
                <td>{item.table_name}</td>
                <td>{formatNumber(item.row_estimate)}</td>
                <td>{formatBytes(item.total_bytes)}</td>
                <td>{formatBytes(item.table_bytes)}</td>
                <td>{formatBytes(item.index_bytes)}</td>
                <td>{formatNumber(item.dead_tuples)}</td>
                <td>
                  {item.last_autovacuum || item.last_vacuum
                    ? formatDateTime(item.last_autovacuum || item.last_vacuum)
                    : "Unknown"}
                </td>
              </tr>
            ))}
            {!topTables.length ? (
              <tr>
                <td colSpan={7}>No audit data loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Largest indexes</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Index</th>
              <th>Table</th>
              <th>Size</th>
              <th>Scans</th>
              <th>Tuples read</th>
              <th>Flags</th>
            </tr>
          </thead>
          <tbody>
            {topIndexes.map((item) => (
              <tr key={item.index_name}>
                <td>{item.index_name}</td>
                <td>{item.table_name}</td>
                <td>{formatBytes(item.index_bytes)}</td>
                <td>{formatNumber(item.scans)}</td>
                <td>{formatNumber(item.tuples_read)}</td>
                <td>
                  {[item.primary ? "primary" : "", item.unique ? "unique" : ""]
                    .filter(Boolean)
                    .join(", ") || "secondary"}
                </td>
              </tr>
            ))}
            {!topIndexes.length ? (
              <tr>
                <td colSpan={6}>No audit data loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Source age windows</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Scope</th>
              <th>Age</th>
              <th>Rows</th>
              <th>Risk</th>
              <th>Use</th>
              <th>Purge note</th>
            </tr>
          </thead>
          <tbody>
            {sourceAges.map((item) => (
              <tr key={`${item.scope}-${item.bucket}`}>
                <td>{item.scope}</td>
                <td>{item.bucket}</td>
                <td>{formatNumber(item.rows)}</td>
                <td>
                  <RiskPill risk={item.risk} />
                </td>
                <td>{item.data_use}</td>
                <td>{item.purge_note}</td>
              </tr>
            ))}
            {!sourceAges.length ? (
              <tr>
                <td colSpan={6}>No source age data loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Purge guard counts</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Guard</th>
              <th>Rows</th>
              <th>Risk</th>
              <th>Note</th>
            </tr>
          </thead>
          <tbody>
            {guardCounts.map((item) => (
              <tr key={item.key}>
                <td>{item.label}</td>
                <td>{formatNumber(item.rows)}</td>
                <td>
                  <RiskPill risk={item.risk} />
                </td>
                <td>{item.notes}</td>
              </tr>
            ))}
            {!guardCounts.length ? (
              <tr>
                <td colSpan={4}>No guard counts loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Source window audit</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Age</th>
              <th>Headers</th>
              <th>Payloads</th>
              <th>Assembly queue</th>
              <th>Binary parts</th>
              <th>yEnc work</th>
              <th>Archive lineage</th>
              <th>Orphan audit</th>
              <th>Risk</th>
            </tr>
          </thead>
          <tbody>
            {sourceWindows.map((item) => (
              <tr key={item.bucket}>
                <td>{item.bucket}</td>
                <td>{formatNumber(item.headers)}</td>
                <td>{formatNumber(item.payloads)}</td>
                <td>{formatNumber(item.assembly_queue)}</td>
                <td>{formatNumber(item.binary_parts)}</td>
                <td>{formatNumber(item.yenc_work_items)}</td>
                <td>{formatNumber(item.archive_lineage)}</td>
                <td title={item.notes}>{formatNumber(item.orphan_headers)}</td>
                <td>
                  <RiskPill risk={item.risk} />
                </td>
              </tr>
            ))}
            {!sourceWindows.length ? (
              <tr>
                <td colSpan={9}>No source window audit data loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">yEnc backlog audit</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Age</th>
              <th>Status</th>
              <th>Priority</th>
              <th>Readiness</th>
              <th>Rows</th>
              <th>Blocking</th>
              <th>Oldest</th>
              <th>Newest</th>
            </tr>
          </thead>
          <tbody>
            {yencBacklog.map((item) => (
              <tr
                key={`${item.bucket}-${item.status}-${item.priority_rank}-${item.readiness_bucket}`}
              >
                <td>{item.bucket}</td>
                <td>{item.status}</td>
                <td>{item.priority_rank}</td>
                <td>{item.readiness_bucket}</td>
                <td>{formatNumber(item.rows)}</td>
                <td title={item.notes}>{formatNumber(item.blocking_rows)}</td>
                <td>
                  {item.oldest_date ? formatDateTime(item.oldest_date) : ""}
                </td>
                <td>
                  {item.newest_date ? formatDateTime(item.newest_date) : ""}
                </td>
              </tr>
            ))}
            {!yencBacklog.length ? (
              <tr>
                <td colSpan={8}>No yEnc backlog audit data loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div className="page-card stack">
        <h2 className="section-title">Cleanup decision matrix</h2>
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <th>Cleanup</th>
              <th>Risk</th>
              <th>Rows</th>
              <th>Supervisor effect</th>
              <th>Data effect</th>
              <th>Release safety</th>
            </tr>
          </thead>
          <tbody>
            {cleanupMatrix.map((item) => (
              <tr key={item.task_key}>
                <td>
                  {item.label}
                  <br />
                  <span className="muted-copy">
                    {item.implemented ? "implemented" : "not implemented"}
                  </span>
                </td>
                <td>
                  <RiskPill risk={item.risk} />
                </td>
                <td>{formatRows(item.estimated_rows_by_table)}</td>
                <td>{item.supervisor_effect}</td>
                <td>{item.data_effect}</td>
                <td>{item.release_safety}</td>
              </tr>
            ))}
            {!cleanupMatrix.length ? (
              <tr>
                <td colSpan={6}>No cleanup audit data loaded.</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </>
  )
}

export function AdminMaintenancePage() {
  const [tasks, setTasks] = useState<AdminMaintenanceTask[]>([])
  const [audit, setAudit] = useState<AdminStorageAuditReport | null>(null)
  const [auditLoading, setAuditLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [auditError, setAuditError] = useState<string | null>(null)

  async function refresh() {
    try {
      setTasks(await getAdminMaintenanceTasks())
      setError(null)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to load maintenance tasks",
      )
    }
  }

  async function refreshAudit() {
    setAuditLoading(true)
    try {
      setAudit(await getAdminMaintenanceStorageAudit())
      setAuditError(null)
    } catch (err) {
      setAuditError(
        err instanceof Error ? err.message : "Failed to load storage audit",
      )
    } finally {
      setAuditLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
    void refreshAudit()
  }, [])

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Indexer Maintenance</p>
        <h1 className="page-title">Manual cleanup and guarded schedules.</h1>
        <p className="muted-copy">
          Operational release and archive stages stay on the Stages page.
          Destructive cleanup runs here with dry-runs and opt-in long intervals.
        </p>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      {auditError ? <div className="banner error">{auditError}</div> : null}
      <StorageAuditPanel
        audit={audit}
        loading={auditLoading}
        onRefresh={refreshAudit}
      />
      {tasks.map((task) => (
        <MaintenanceTaskCard
          key={task.task_key}
          task={task}
          onRefresh={refresh}
        />
      ))}
    </div>
  )
}
