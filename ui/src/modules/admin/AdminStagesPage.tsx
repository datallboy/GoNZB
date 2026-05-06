import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getAdminStages, runStageAction } from '../../shared/api/admin'
import type { AdminStage } from '../../shared/types'

function stageRuntimeStatus(stage: AdminStage) {
  if (!stage.enabled) {
    return 'disabled'
  }
  if (stage.paused) {
    return 'paused'
  }
  if (stage.lease_owner) {
    return 'running'
  }
  return 'idle'
}

function StageCard({ stage, onRefresh }: { stage: AdminStage; onRefresh: () => Promise<void> }) {
  const [message, setMessage] = useState<string | null>(null)

  async function runAction(action: 'run' | 'pause' | 'resume') {
    setMessage(null)
    try {
      await runStageAction(stage.stage_name, action)
      setMessage(`${action} accepted.`)
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : `Failed to ${action} stage`)
    }
  }

  return (
    <div className="page-card stack">
      <div className="page-header">
        <div>
          <h2 className="section-title">{stage.stage_name}</h2>
          <p className="muted-copy">
            Stage configuration lives in <Link className="table-link" to="/admin/settings">Runtime Settings</Link>.
          </p>
          <p className="muted-copy">
            Status: {stageRuntimeStatus(stage)} · enabled: {stage.enabled ? 'yes' : 'no'} · paused: {stage.paused ? 'yes' : 'no'}
          </p>
          <p className="muted-copy">
            Lease owner: {stage.lease_owner || 'none'} · last error: {stage.last_error || 'none'}
          </p>
          <p className="muted-copy">
            Current runtime: every {stage.interval_seconds}s · batch {stage.batch_size}
            {stage.supports_concurrency ? ` · concurrency ${stage.concurrency ?? 1}` : ''}
            {' · '}backoff {stage.backoff_seconds}s
          </p>
        </div>
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void runAction('run')}>
            Run
          </button>
          <button className="secondary-button" type="button" onClick={() => void runAction('pause')}>
            Pause
          </button>
          <button className="secondary-button" type="button" onClick={() => void runAction('resume')}>
            Resume
          </button>
        </div>
      </div>
      {stage.latest_run ? (
        <div className="banner">
          Latest run: <Link className="table-link" to={`/admin/indexer/runs/${stage.latest_run.id}`}>#{stage.latest_run.id}</Link> · {stage.latest_run.status}
        </div>
      ) : null}
      {message ? <div className="banner">{message}</div> : null}
    </div>
  )
}

export function AdminStagesPage() {
  const [stages, setStages] = useState<AdminStage[]>([])
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      setStages(await getAdminStages())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load stages')
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Runtime Stages</p>
        <h1 className="page-title">Stage status and manual controls.</h1>
        <p className="muted-copy">
          Stage settings are no longer edited here. Use the Runtime Settings page for batch size, interval, concurrency, and backoff changes.
        </p>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      {stages.map((stage) => (
        <StageCard key={stage.stage_name} stage={stage} onRefresh={refresh} />
      ))}
    </div>
  )
}
