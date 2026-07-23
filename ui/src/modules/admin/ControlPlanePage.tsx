import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getCapabilities } from '../../shared/api/settings'
import { useAuth } from '../../shared/auth/useAuth'
import type { ControlPlaneCapabilities, ModuleCapability } from '../../shared/types'

type ModuleDefinition = {
  key: string
  label: string
  route?: string
  permission?: string
}

const managedModules: ModuleDefinition[] = [
  { key: 'usenet_indexer', label: 'Indexer', route: '/admin/indexer/dashboard', permission: 'indexer.runtime.read' },
  { key: 'gonzbnet', label: 'GoNZBNet', route: '/admin/gonzbnet', permission: 'gonzbnet.admin.read' },
  { key: 'aggregator', label: 'Aggregator', route: '/admin/settings?tab=aggregator', permission: 'admin.settings.read' },
  { key: 'downloader', label: 'Downloader', route: '/admin/settings?tab=downloader', permission: 'admin.settings.read' },
]

const runtimeModules: ModuleDefinition[] = [
  { key: 'api', label: 'API' },
  { key: 'web_ui', label: 'Web UI' },
]

function statusLabel(capability?: ModuleCapability) {
  if (!capability) return 'Unavailable'
  if (!capability.enabled) return 'Disabled'
  if (capability.ready) return 'Ready'
  if (capability.configured) return 'Configured'
  return 'Setup required'
}

function statusClass(capability?: ModuleCapability) {
  if (capability?.ready) return 'module-status module-status--ready'
  if (capability?.enabled) return 'module-status module-status--attention'
  return 'module-status'
}

function ModuleCard({
  definition,
  capability,
  canOpen,
}: {
  definition: ModuleDefinition
  capability?: ModuleCapability
  canOpen: boolean
}) {
  const requirements = capability?.requirements ?? []

  return (
    <article className="module-card">
      <div className="module-card__header">
        <h3>{definition.label}</h3>
        <span className={statusClass(capability)}>{statusLabel(capability)}</span>
      </div>
      {requirements.length ? (
        <ul className="module-card__requirements">
          {requirements.map((requirement) => <li key={requirement}>{requirement}</li>)}
        </ul>
      ) : capability?.reason ? (
        <p className="muted-copy">{capability.reason}</p>
      ) : (
        <p className="module-card__state">No readiness issues</p>
      )}
      {definition.route && capability?.visible && canOpen ? (
        <Link className="module-card__action" to={definition.route}>Open {definition.label}</Link>
      ) : null}
    </article>
  )
}

export function ControlPlanePage() {
  const { hasPermission } = useAuth()
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
  const enabledCount = Object.values(modules).filter((module) => module.enabled).length
  const readyCount = Object.values(modules).filter((module) => module.ready).length

  return (
    <div className="page-section admin-overview">
      <header className="admin-overview__header">
        <div>
          <p className="eyebrow">System</p>
          <h1 className="page-title">Administration</h1>
        </div>
        <div className="admin-overview__summary" aria-label="Module status summary">
          <span><strong>{enabledCount}</strong> enabled</span>
          <span><strong>{readyCount}</strong> ready</span>
          <span><strong>{capabilities?.revision ?? 0}</strong> revision</span>
        </div>
      </header>

      {error ? <div className="banner error">{error}</div> : null}

      <section className="admin-overview__section">
        <div className="section-heading">
          <p className="eyebrow">Modules</p>
          <h2 className="section-title">Managed modules</h2>
        </div>
        <div className="module-grid">
          {managedModules.map((definition) => (
            <ModuleCard
              key={definition.key}
              definition={definition}
              capability={modules[definition.key]}
              canOpen={!definition.permission || hasPermission(definition.permission)}
            />
          ))}
        </div>
      </section>

      <section className="admin-overview__section">
        <div className="section-heading">
          <p className="eyebrow">Runtime</p>
          <h2 className="section-title">Application services</h2>
        </div>
        <div className="module-grid module-grid--compact">
          {runtimeModules.map((definition) => (
            <ModuleCard key={definition.key} definition={definition} capability={modules[definition.key]} canOpen={false} />
          ))}
        </div>
      </section>

      <section className="admin-overview__section">
        <div className="section-heading">
          <p className="eyebrow">Administration</p>
          <h2 className="section-title">System controls</h2>
        </div>
        <div className="admin-shortcuts">
          {hasPermission('admin.settings.read') ? <Link to="/admin/settings">Runtime settings</Link> : null}
          {hasPermission('auth.users.read') ? <Link to="/admin/security/users">Users</Link> : null}
          {hasPermission('auth.roles.read') ? <Link to="/admin/security/roles">Roles</Link> : null}
        </div>
      </section>
    </div>
  )
}
