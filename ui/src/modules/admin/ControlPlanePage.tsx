import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getCapabilities } from '../../shared/api/settings'
import type { ControlPlaneCapabilities, ModuleCapability } from '../../shared/types'

function statusLabel(cap?: ModuleCapability) {
  if (!cap || !cap.enabled) return 'Disabled'
  if (cap.ready) return 'Ready'
  if (cap.configured) return 'Configured'
  return 'Setup required'
}

export function ControlPlanePage() {
  const [capabilities, setCapabilities] = useState<ControlPlaneCapabilities | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getCapabilities()
      .then((response) => {
        setCapabilities(response as ControlPlaneCapabilities)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load capabilities'))
  }, [])

  const modules = capabilities?.modules ?? {}
  const names = ['downloader', 'aggregator', 'usenet_indexer', 'api', 'web_ui']

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Control Plane</p>
        <h1 className="page-title">Runtime settings and module readiness.</h1>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      <div className="dashboard-grid">
        {names.map((name) => {
          const cap = modules[name]
          return (
            <div className="page-card stack" key={name}>
              <p className="eyebrow">{name.replace('_', ' ')}</p>
              <h2 className="section-title">{statusLabel(cap)}</h2>
              {cap?.requirements?.length ? (
                <ul className="compact-list">
                  {cap.requirements.map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              ) : (
                <p className="muted-copy">{cap?.reason || 'No missing requirements reported.'}</p>
              )}
            </div>
          )
        })}
      </div>
      <div className="page-card toolbar-row">
        <Link className="primary-button" to="/admin/settings">
          Runtime Settings
        </Link>
      </div>
    </div>
  )
}
