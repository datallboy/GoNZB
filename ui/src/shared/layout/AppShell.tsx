import { Link, NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

function ShellNav() {
  const { session, hasPermission, logout } = useAuth()

  return (
    <aside className="shell-sidebar">
      <Link className="brand-mark" to="/indexer/releases">
        <span>GoNZB</span>
        <strong>Indexer UI</strong>
      </Link>
      <nav className="shell-nav">
        <NavLink to="/indexer/releases">Catalog</NavLink>
        {hasPermission('indexer.runtime.read') ? (
          <>
            <NavLink to="/admin/indexer/dashboard">Dashboard</NavLink>
            <NavLink to="/admin/indexer/stages">Stages</NavLink>
            <NavLink to="/admin/indexer/runs">Runs</NavLink>
            <NavLink to="/admin/indexer/releases">Moderation</NavLink>
          </>
        ) : null}
        {hasPermission('indexer.runtime.configure') ? (
          <NavLink to="/admin/indexer/settings">Runtime Settings</NavLink>
        ) : null}
        {hasPermission('auth.users.read') ? <NavLink to="/admin/security/users">Users</NavLink> : null}
        {hasPermission('auth.roles.read') ? <NavLink to="/admin/security/roles">Roles</NavLink> : null}
        {hasPermission('auth.tokens.read') ? <NavLink to="/admin/security/tokens">Tokens</NavLink> : null}
      </nav>
      <div className="sidebar-footer">
        <div>
          <strong>{session.username || 'anonymous'}</strong>
          <div className="muted-copy">{session.permissions.length} permissions</div>
        </div>
        <button className="secondary-button" onClick={() => void logout()}>
          Sign out
        </button>
      </div>
    </aside>
  )
}

export function AppShell() {
  return (
    <div className="shell-frame">
      <ShellNav />
      <main className="shell-main">
        <Outlet />
      </main>
    </div>
  )
}
