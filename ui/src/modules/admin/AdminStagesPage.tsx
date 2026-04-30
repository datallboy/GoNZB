import { useEffect, useState } from 'react'
import { getAdminStages, patchAdminStage, runStageAction } from '../../shared/api/admin'
import type { AdminStage, AdminStageConfigPatch } from '../../shared/types'

function stagePatchFromView(stage: AdminStage): AdminStageConfigPatch {
  const patch: AdminStageConfigPatch = {
    enabled: stage.enabled,
    interval_minutes: stage.interval_seconds > 0 ? stage.interval_seconds / 60 : 0,
    batch_size: stage.batch_size,
    backoff_seconds: stage.backoff_seconds,
  }
  if (stage.supports_concurrency) {
    patch.concurrency = stage.concurrency ?? 1
  }
  return patch
}

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
  const [patch, setPatch] = useState<AdminStageConfigPatch>(() => stagePatchFromView(stage))
  const [message, setMessage] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setPatch(stagePatchFromView(stage))
  }, [stage])

  async function savePatch() {
    setMessage(null)
    setSaving(true)
    try {
      await patchAdminStage(stage.stage_name, patch)
      setMessage('Saved runtime settings.')
      await onRefresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save stage settings')
    } finally {
      setSaving(false)
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
            Runtime settings stored in the app settings DB. `config.yaml` is bootstrap only.
          </p>
          <p className="muted-copy">
            Status: {stageRuntimeStatus(stage)} · lease owner: {stage.lease_owner || 'none'} · last error:{' '}
            {stage.last_error || 'none'}
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
            onChange={(event) => setPatch((current) => ({ ...current, batch_size: Number(event.target.value) }))}
          />
        </label>
        {stage.supports_concurrency ? (
          <label className="field">
            <span>Concurrency</span>
            <input
              type="number"
              value={patch.concurrency ?? 1}
              onChange={(event) => setPatch((current) => ({ ...current, concurrency: Number(event.target.value) }))}
            />
          </label>
        ) : null}
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
        <div className="button-row align-end">
          <button className="secondary-button" type="button" onClick={() => setPatch(stagePatchFromView(stage))}>
            Reset
          </button>
          <button className="primary-button" type="button" disabled={saving} onClick={() => void savePatch()}>
            {saving ? 'Saving...' : 'Save Stage Settings'}
          </button>
        </div>
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
