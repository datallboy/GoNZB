import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  dryRunAdminMaintenanceTask,
  getAdminMaintenanceTasks,
  patchAdminMaintenanceTask,
  runAdminMaintenanceTask,
} from '../../shared/api/admin'
import type { AdminMaintenanceTask, AdminMaintenanceTaskRun } from '../../shared/types'

function formatRows(rows?: Record<string, number>) {
  if (!rows || Object.keys(rows).length === 0) {
    return 'No rows estimated.'
  }
  return Object.entries(rows)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([table, count]) => `${table}: ${count.toLocaleString()}`)
    .join(' · ')
}

function MaintenanceTaskCard({ task, onRefresh }: { task: AdminMaintenanceTask; onRefresh: () => Promise<void> }) {
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
      setMessage('Dry-run completed.')
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Dry-run failed')
    }
  }

  async function run() {
    setMessage(null)
    try {
      const next = await runAdminMaintenanceTask(task.task_key)
      setResult(next)
      setMessage('Run completed.')
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Run failed')
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
      setMessage('Schedule saved.')
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save schedule')
    }
  }

  const hasFreshDryRun = task.last_dry_run_at && Date.now() - Date.parse(task.last_dry_run_at) < 30 * 60 * 1000
  const rows = result?.estimated_rows_by_table || result?.deleted_rows_by_table

  return (
    <div className="page-card stack">
      <div className="page-header">
        <div>
          <h2 className="section-title">{task.label}</h2>
          <p className="muted-copy">{task.purpose}</p>
          <p className="muted-copy">
            {task.destructive ? 'Destructive' : 'Non-destructive'} · schedule {task.schedule_enabled ? 'enabled' : 'disabled'}
            {task.uses_batch_size ? ` · batch ${task.batch_size}` : ''}
          </p>
          <p className="muted-copy">
            Last dry-run: {task.last_dry_run_at ? new Date(task.last_dry_run_at).toLocaleString() : 'never'} · latest run:{' '}
            {task.last_run ? <Link className="table-link" to={`/admin/indexer/runs/${task.last_run.id}`}>#{task.last_run.id}</Link> : 'never'}
          </p>
        </div>
        <div className="button-row">
          <button className="primary-button" type="button" onClick={() => void dryRun()}>
            Dry Run
          </button>
          <button className="secondary-button" type="button" onClick={() => void run()} disabled={task.destructive && !hasFreshDryRun}>
            Run
          </button>
        </div>
      </div>

      {task.warnings?.length ? <div className="banner">{task.warnings.join(' · ')}</div> : null}
      {task.destructive ? <div className="banner">Run requires a dry-run completed within the last 30 minutes.</div> : null}
      {rows ? <div className="banner">{formatRows(rows)}</div> : null}
      {result?.blockers?.length ? <div className="banner error">{result.blockers.join(' · ')}</div> : null}

      <div className="form-grid">
        <label className="field checkbox-field">
          <input type="checkbox" checked={scheduleEnabled} onChange={(event) => setScheduleEnabled(event.target.checked)} />
          <span>Schedule enabled</span>
        </label>
        <label className="field">
          <span>Interval hours</span>
          <input type="number" min={task.min_interval_hours || 6} value={intervalHours} onChange={(event) => setIntervalHours(Number(event.target.value))} />
        </label>
        {task.uses_batch_size ? (
          <label className="field">
            <span>Batch size</span>
            <input type="number" min={1} value={batchSize} onChange={(event) => setBatchSize(Number(event.target.value))} />
          </label>
        ) : null}
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void saveSchedule()}>
            Save
          </button>
        </div>
      </div>
      {message ? <div className="banner">{message}</div> : null}
    </div>
  )
}

export function AdminMaintenancePage() {
  const [tasks, setTasks] = useState<AdminMaintenanceTask[]>([])
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      setTasks(await getAdminMaintenanceTasks())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load maintenance tasks')
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Indexer Maintenance</p>
        <h1 className="page-title">Manual cleanup and guarded schedules.</h1>
        <p className="muted-copy">
          Operational release and archive stages stay on the Stages page. Destructive cleanup runs here with dry-runs and opt-in long intervals.
        </p>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      {tasks.map((task) => (
        <MaintenanceTaskCard key={task.task_key} task={task} onRefresh={refresh} />
      ))}
    </div>
  )
}
