import { useEffect, useMemo, useState } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { browseCategories } from '../../modules/indexer/browse'
import { getCapabilities } from '../api/settings'
import type { ControlPlaneCapabilities } from '../types'

function canOpenAdminPortal(permissions: string[]) {
  return permissions.some((permission) =>
    permission.startsWith('indexer.runtime.') ||
    permission.startsWith('aggregator.runtime.') ||
    permission.startsWith('downloader.runtime.') ||
    permission.startsWith('admin.settings.') ||
    permission.startsWith('auth.')
  )
}

function AccountMenu({ viewerLink }: { viewerLink?: boolean }) {
  const { session, logout } = useAuth()
  const [open, setOpen] = useState(false)

  const initials = useMemo(() => {
    const username = (session.username || 'anonymous').trim()
    return username.slice(0, 2).toUpperCase()
  }, [session.username])

  const showAdminPortal = canOpenAdminPortal(session.permissions)
  const showAccountTokens = session.authenticated

  return (
    <div className="account-menu">
      <button className="profile-button" type="button" onClick={() => setOpen((current) => !current)}>
        <span className="profile-button__badge">{initials}</span>
        <span className="profile-button__label">Profile</span>
      </button>
      {open ? (
        <div className="account-menu__panel">
          <div className="account-menu__identity">
            <strong>{session.username || 'anonymous'}</strong>
            <span>{session.permissions.length} permissions</span>
          </div>
          {viewerLink ? (
            <Link className="secondary-button" to="/indexer/releases" onClick={() => setOpen(false)}>
              Viewer
            </Link>
          ) : null}
          {showAccountTokens ? (
            <Link className="secondary-button" to="/account/tokens" onClick={() => setOpen(false)}>
              API Tokens
            </Link>
          ) : null}
          {showAdminPortal ? (
            <Link className="secondary-button" to="/admin/indexer/dashboard" onClick={() => setOpen(false)}>
              Admin Portal
            </Link>
          ) : null}
          <button className="secondary-button" type="button" onClick={() => void logout()}>
            Sign out
          </button>
        </div>
      ) : null}
    </div>
  )
}

export function PublicAppShell() {
  return (
    <div className="public-frame">
      <header className="public-topbar">
        <div className="public-topbar__brand">
          <Link className="brand-mark" to="/indexer/releases">
            <span>GoNZB</span>
            <strong>Indexer Viewer</strong>
          </Link>
          <nav className="public-nav">
            <NavLink to="/indexer/releases">Browse</NavLink>
            {browseCategories.map((category) => (
              <NavLink key={category.slug} to={`/indexer/browse/${category.slug}`}>
                {category.label}
              </NavLink>
            ))}
          </nav>
        </div>
        <AccountMenu />
      </header>
      <main className="public-main">
        <Outlet />
      </main>
    </div>
  )
}

export function AdminAppShell() {
  const { hasPermission } = useAuth()
  const [capabilities, setCapabilities] = useState<ControlPlaneCapabilities | null>(null)

  useEffect(() => {
    void getCapabilities()
      .then((response) => setCapabilities(response as ControlPlaneCapabilities))
      .catch(() => setCapabilities(null))
  }, [])

  const moduleVisible = (name: string) => Boolean(capabilities?.modules?.[name]?.visible)

  return (
    <div className="shell-frame">
      <aside className="shell-sidebar">
        <Link className="brand-mark" to="/admin">
          <span>GoNZB</span>
          <strong>Control Plane</strong>
        </Link>
        <nav className="shell-nav">
          <NavLink to="/admin">Overview</NavLink>
          {moduleVisible('usenet_indexer') && hasPermission('indexer.runtime.read') ? (
            <>
              <NavLink to="/admin/indexer/dashboard">Dashboard</NavLink>
              <NavLink to="/admin/indexer/scrape">Scrape</NavLink>
              <NavLink to="/admin/indexer/stages">Stages</NavLink>
              <NavLink to="/admin/indexer/maintenance">Maintenance</NavLink>
              <NavLink to="/admin/indexer/runs">Runs</NavLink>
              <NavLink to="/admin/indexer/releases">Releases</NavLink>
            </>
          ) : null}
          {hasPermission('admin.settings.write') || hasPermission('admin.settings.read') ? (
            <NavLink to="/admin/settings">Runtime Settings</NavLink>
          ) : null}
          {hasPermission('auth.users.read') ? <NavLink to="/admin/security/users">Users</NavLink> : null}
          {hasPermission('auth.roles.read') ? <NavLink to="/admin/security/roles">Roles</NavLink> : null}
        </nav>
        <div className="sidebar-footer">
          <Link className="secondary-button" to="/indexer/releases">
            Back to Viewer
          </Link>
        </div>
      </aside>
      <div className="admin-main">
        <header className="admin-topbar">
          <div>
            <p className="eyebrow">Admin Portal</p>
            <h1 className="admin-topbar__title">Unified runtime settings, operations, and security controls.</h1>
          </div>
          <AccountMenu viewerLink />
        </header>
        <main className="shell-main">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
