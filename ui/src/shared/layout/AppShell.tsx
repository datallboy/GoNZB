import { useEffect, useMemo, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'
import { browseCategories } from '../../modules/indexer/browse'
import { getCapabilities } from '../api/settings'
import type { ControlPlaneCapabilities } from '../types'

function canOpenAdminPortal(permissions: string[]) {
  return permissions.some((permission) =>
    permission.startsWith('indexer.runtime.') ||
    permission.startsWith('aggregator.runtime.') ||
    permission.startsWith('downloader.runtime.') ||
    permission.startsWith('gonzbnet.') ||
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
            <Link className="secondary-button" to="/admin" onClick={() => setOpen(false)}>
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
  const location = useLocation()
  const [capabilities, setCapabilities] = useState<ControlPlaneCapabilities | null>(null)
  const [navOpen, setNavOpen] = useState(false)

  useEffect(() => {
    void getCapabilities()
      .then((response) => setCapabilities(response as ControlPlaneCapabilities))
      .catch(() => setCapabilities(null))
  }, [])

  const moduleVisible = (name: string) => capabilities ? Boolean(capabilities.modules?.[name]?.visible) : true
  const context = adminContext(location.pathname)

  return (
    <div className="shell-frame">
      <aside className={navOpen ? 'shell-sidebar is-open' : 'shell-sidebar'}>
        <Link className="brand-mark" to="/admin" onClick={() => setNavOpen(false)}>
          <span>GoNZB</span>
          <strong>Administration</strong>
        </Link>
        <button
          className="shell-nav-toggle"
          type="button"
          aria-expanded={navOpen}
          aria-controls="admin-navigation"
          onClick={() => setNavOpen((current) => !current)}
        >
          Menu
        </button>
        <nav
          id="admin-navigation"
          className={navOpen ? 'shell-nav is-open' : 'shell-nav'}
          aria-label="Administration"
          onClick={(event) => {
            if ((event.target as HTMLElement).closest('a')) setNavOpen(false)
          }}
        >
          <div className="shell-nav__section">
            <span className="shell-nav__heading">General</span>
            <NavLink end to="/admin">Overview</NavLink>
            {hasPermission('admin.settings.write') || hasPermission('admin.settings.read') ? (
              <NavLink to="/admin/settings">Runtime Settings</NavLink>
            ) : null}
          </div>
          {moduleVisible('gonzbnet') && hasPermission('gonzbnet.admin.read') ? (
            <div className="shell-nav__section">
              <span className="shell-nav__heading">Federation</span>
              <NavLink to="/admin/gonzbnet">GoNZBNet</NavLink>
            </div>
          ) : null}
          {moduleVisible('usenet_indexer') && hasPermission('indexer.runtime.read') ? (
            <div className="shell-nav__section">
              <span className="shell-nav__heading">Indexer</span>
              <NavLink to="/admin/indexer/dashboard">Overview</NavLink>
              <span className="shell-nav__subheading">Catalog</span>
              <NavLink to="/admin/indexer/releases">Releases</NavLink>
              <NavLink to="/admin/indexer/binaries">Binaries</NavLink>
              <span className="shell-nav__subheading">Pipeline</span>
              <NavLink to="/admin/indexer/scrape">Scrape</NavLink>
              <NavLink to="/admin/indexer/work">Work</NavLink>
              <NavLink to="/admin/indexer/cohorts">Cohorts</NavLink>
              <NavLink to="/admin/indexer/stages">Stages</NavLink>
              <NavLink to="/admin/indexer/runs">Runs</NavLink>
              <span className="shell-nav__subheading">Operations</span>
              <NavLink to="/admin/indexer/attention">Attention</NavLink>
              <NavLink to="/admin/indexer/maintenance">Maintenance</NavLink>
            </div>
          ) : null}
          {hasPermission('auth.users.read') || hasPermission('auth.roles.read') ? (
            <div className="shell-nav__section">
              <span className="shell-nav__heading">Access</span>
              {hasPermission('auth.users.read') ? <NavLink to="/admin/security/users">Users</NavLink> : null}
              {hasPermission('auth.roles.read') ? <NavLink to="/admin/security/roles">Roles</NavLink> : null}
            </div>
          ) : null}
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
            <p className="eyebrow">{context.eyebrow}</p>
            <h1 className="admin-topbar__title">{context.title}</h1>
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

function adminContext(pathname: string) {
  if (pathname.startsWith('/admin/indexer')) {
    return { eyebrow: 'Module administration', title: 'Indexer' }
  }
  if (pathname.startsWith('/admin/gonzbnet')) {
    return { eyebrow: 'Module administration', title: 'GoNZBNet' }
  }
  if (pathname.startsWith('/admin/security')) {
    return { eyebrow: 'Administration', title: 'Access control' }
  }
  if (pathname.startsWith('/admin/settings')) {
    return { eyebrow: 'Administration', title: 'Runtime settings' }
  }
  return { eyebrow: 'Administration', title: 'System overview' }
}
