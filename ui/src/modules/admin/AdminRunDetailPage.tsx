import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getAdminRun } from '../../shared/api/admin'
import { formatDateTime, formatNumber } from '../../shared/lib/format'
import type { AdminRun } from '../../shared/types'

function prettyJSON(value?: string) {
  if (!value) {
    return '{}'
  }
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

export function AdminRunDetailPage() {
  const { id = '' } = useParams()
  const [run, setRun] = useState<AdminRun | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getAdminRun(id)
      .then((response) => {
        setRun(response.run)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load run'))
  }, [id])

  if (error) {
    return <div className="banner error">{error}</div>
  }
  if (!run) {
    return <div className="banner">Loading run...</div>
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <div className="page-header">
          <div>
            <p className="eyebrow">Run Detail</p>
            <h1 className="page-title">
              {run.stage_name} · run {formatNumber(run.id)}
            </h1>
            <p className="muted-copy">
              {run.status} · {run.trigger_kind} · owner {run.claimed_by || 'n/a'}
            </p>
          </div>
          <Link className="secondary-button" to="/admin/indexer/runs">
            Back to Runs
          </Link>
        </div>
      </div>

      <div className="dashboard-grid">
        <div className="page-card">
          <h2 className="section-title">Run summary</h2>
          <dl className="detail-grid">
            <div>
              <dt>Run ID</dt>
              <dd>{formatNumber(run.id)}</dd>
            </div>
            <div>
              <dt>Stage</dt>
              <dd>{run.stage_name}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{run.status}</dd>
            </div>
            <div>
              <dt>Trigger</dt>
              <dd>{run.trigger_kind}</dd>
            </div>
            <div>
              <dt>Owner</dt>
              <dd>{run.claimed_by || 'n/a'}</dd>
            </div>
            <div>
              <dt>Started</dt>
              <dd>{formatDateTime(run.started_at)}</dd>
            </div>
            <div>
              <dt>Heartbeat</dt>
              <dd>{formatDateTime(run.heartbeat_at)}</dd>
            </div>
            <div>
              <dt>Finished</dt>
              <dd>{formatDateTime(run.finished_at)}</dd>
            </div>
          </dl>
        </div>
        <div className="page-card">
          <h2 className="section-title">Error text</h2>
          <pre className="json-block">{run.error_text || 'None'}</pre>
        </div>
      </div>

      <div className="page-card">
        <h2 className="section-title">Metrics JSON</h2>
        <pre className="json-block">{prettyJSON(run.metrics_json)}</pre>
      </div>
    </div>
  )
}
