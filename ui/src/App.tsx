import { Navigate, Route, Routes } from 'react-router-dom'
import { LoginPage } from './modules/auth/LoginPage'
import { AdminDashboardPage } from './modules/admin/AdminDashboardPage'
import { AdminReleaseDetailPage } from './modules/admin/AdminReleaseDetailPage'
import { AdminReleasesPage } from './modules/admin/AdminReleasesPage'
import { AdminRolesPage } from './modules/admin/AdminRolesPage'
import { AdminRunsPage } from './modules/admin/AdminRunsPage'
import { AdminSettingsPage } from './modules/admin/AdminSettingsPage'
import { AdminStagesPage } from './modules/admin/AdminStagesPage'
import { AdminTokensPage } from './modules/admin/AdminTokensPage'
import { AdminUsersPage } from './modules/admin/AdminUsersPage'
import { IndexerReleaseDetailPage } from './modules/indexer/IndexerReleaseDetailPage'
import { IndexerReleaseListPage } from './modules/indexer/IndexerReleaseListPage'
import { AuthProvider } from './shared/auth/AuthContext'
import { RequireAuth } from './shared/auth/RequireAuth'
import { AppShell } from './shared/layout/AppShell'

function NotFoundPage() {
  return (
    <div className="page-section">
      <div className="page-card">
        <p className="eyebrow">Missing Route</p>
        <h1 className="page-title">That page is not here.</h1>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <AppShell />
            </RequireAuth>
          }
        >
          <Route index element={<Navigate to="/indexer/releases" replace />} />
          <Route path="indexer/releases" element={<IndexerReleaseListPage />} />
          <Route path="indexer/releases/:id" element={<IndexerReleaseDetailPage />} />
          <Route
            path="admin/indexer/dashboard"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminDashboardPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/indexer/releases"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminReleasesPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/indexer/releases/:id"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminReleaseDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/indexer/stages"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminStagesPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/indexer/runs"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminRunsPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/indexer/settings"
            element={
              <RequireAuth permission="indexer.runtime.configure">
                <AdminSettingsPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/security/users"
            element={
              <RequireAuth permission="auth.users.read">
                <AdminUsersPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/security/roles"
            element={
              <RequireAuth permission="auth.roles.read">
                <AdminRolesPage />
              </RequireAuth>
            }
          />
          <Route
            path="admin/security/tokens"
            element={
              <RequireAuth permission="auth.tokens.read">
                <AdminTokensPage />
              </RequireAuth>
            }
          />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
