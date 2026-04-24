import { useEffect, useState } from 'react'
import { getAdminStages, patchAdminStage, runStageAction } from '../../shared/api/admin'
import type { AdminStage, AdminStageConfigPatch } from '../../shared/types'

function StageCard({ stage, onRefresh }: { stage: AdminStage; onRefresh: () => Promise<void> }) {
  const [patch, setPatch] = useState<AdminStageConfigPatch>({
    enabled: stage.enabled,
    interval_minutes: stage.interval_seconds > 0 ? stage.interval_seconds / 60 : 0,
    batch_size: stage.batch_size,
    concurrency: stage.concurrency,
    backoff_seconds: stage.backoff_seconds,
  })
  const [message, setMessage] = useState<string | null>(null)

  async function savePatch() {
    setMessage(null)
    try {
      await patchAdminStage(stage.stage_name, patch)
      setMessage('Saved runtime settings.')
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save stage settings')
    }
  }

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
            Lease owner: {stage.lease_owner || 'none'} · paused: {stage.paused ? 'yes' : 'no'} ·
            last error: {stage.last_error || 'none'}
          </p>
        </div>
        <div className="button-row">
          <button className="secondary-button" onClick={() => runAction('run')}>
            Run
          </button>
          <button className="secondary-button" onClick={() => runAction('pause')}>
            Pause
          </button>
          <button className="secondary-button" onClick={() => runAction('resume')}>
            Resume
          </button>
        </div>
      </div>
      <div className="toolbar-grid">
        <label className="field checkbox-field">
          <span>Enabled</span>
          <input
            type="checkbox"
            checked={Boolean(patch.enabled)}
            onChange={(event) => setPatch((current) => ({ ...current, enabled: event.target.checked }))}
          />
        </label>
        <label className="field">
          <span>Interval Minutes</span>
          <input
            type="number"
            value={patch.interval_minutes ?? 0}
            onChange={(event) =>
              setPatch((current) => ({ ...current, interval_minutes: Number(event.target.value) }))
            }
          />
        </label>
        <label className="field">
          <span>Batch Size</span>
          <input
            type="number"
            value={patch.batch_size ?? 0}
            onChange={(event) =>
              setPatch((current) => ({ ...current, batch_size: Number(event.target.value) }))
            }
          />
        </label>
        <label className="field">
          <span>Concurrency</span>
          <input
            type="number"
            value={patch.concurrency ?? 0}
            onChange={(event) =>
              setPatch((current) => ({ ...current, concurrency: Number(event.target.value) }))
            }
          />
        </label>
        <label className="field">
          <span>Backoff Seconds</span>
          <input
            type="number"
            value={patch.backoff_seconds ?? 0}
            onChange={(event) =>
              setPatch((current) => ({ ...current, backoff_seconds: Number(event.target.value) }))
            }
          />
        </label>
        <button className="primary-button align-end" onClick={savePatch}>
          Save Stage Settings
        </button>
      </div>
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
      <div className="page-card">
        <p className="eyebrow">Runtime Stages</p>
        <h1 className="page-title">Scheduler control stays in-process and stage-driven.</h1>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      {stages.map((stage) => (
        <StageCard key={stage.stage_name} stage={stage} onRefresh={refresh} />
      ))}
    </div>
  )
}
