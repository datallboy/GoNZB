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

type GuideStep = {
  label: string
  detail: string
  complete: boolean
  completeLabel?: string
  route?: string
  action?: string
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

function GuideCard({ step, index }: { step: GuideStep; index: number }) {
  return (
    <article className="module-card">
      <div className="module-card__header">
        <h3>{index + 1}. {step.label}</h3>
        <span className={step.complete ? 'module-status module-status--ready' : 'module-status module-status--attention'}>
          {step.complete ? (step.completeLabel ?? 'Complete') : 'Next'}
        </span>
      </div>
      <p className="muted-copy">{step.detail}</p>
      {step.route ? <Link className="module-card__action" to={step.route}>{step.action ?? 'Open'}</Link> : null}
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
  const enabledManagedModules = managedModules
    .map((definition) => modules[definition.key])
    .filter((module): module is ModuleCapability => Boolean(module?.enabled))
  const enabledModulesReady = enabledManagedModules.length > 0 && enabledManagedModules.every((module) => module.ready)
  const setupSteps: GuideStep[] = [
    {
      label: 'Create the administrator',
      detail: 'The first account exists and this authenticated control plane is available.',
      complete: true,
    },
    {
      label: 'Configure providers and modules',
      detail: 'Add NNTP providers, choose provider roles, select newsgroups, and configure aggregator sources. Settings remain visible after applying them.',
      complete: Boolean(capabilities?.settings.runtime_configured),
      route: '/admin/settings',
      action: 'Open settings',
    },
    {
      label: 'Resolve readiness requirements',
      detail: enabledModulesReady
        ? 'Every enabled managed module has the configuration it needs. Open its dashboard to confirm live work and first output.'
        : 'Use the module cards below to resolve each concrete requirement before expecting background work.',
      complete: enabledModulesReady,
      route: modules.usenet_indexer?.enabled ? '/admin/indexer/dashboard' : '/admin/settings',
      action: modules.usenet_indexer?.enabled ? 'Verify indexer activity' : 'Review modules',
    },
    {
      label: 'Connect a Newznab client',
      detail: 'Create a least-privilege account token, then use the exact URL and API-key instructions on the token page.',
      complete: Boolean(modules.aggregator?.ready),
      completeLabel: 'Ready',
      route: '/account/tokens',
      action: 'Open client connection',
    },
  ]

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
          <p className="eyebrow">Getting started</p>
          <h2 className="section-title">From setup to a usable result</h2>
        </div>
        <div className="module-grid">
          {setupSteps.map((step, index) => <GuideCard key={step.label} step={step} index={index} />)}
        </div>
      </section>

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
