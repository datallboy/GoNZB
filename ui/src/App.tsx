import { Route, Routes } from 'react-router-dom'
import { LoginPage } from './modules/auth/LoginPage'
import { SetupPage } from './modules/auth/SetupPage'
import { AdminDashboardPage } from './modules/admin/AdminDashboardPage'
import { AdminMaintenancePage } from './modules/admin/AdminMaintenancePage'
import { AdminReleaseDetailPage } from './modules/admin/AdminReleaseDetailPage'
import { AdminReleasesPage } from './modules/admin/AdminReleasesPage'
import { AdminRolesPage } from './modules/admin/AdminRolesPage'
import { AdminRunDetailPage } from './modules/admin/AdminRunDetailPage'
import { AdminRunsPage } from './modules/admin/AdminRunsPage'
import { AdminScrapePage } from './modules/admin/AdminScrapePage'
import { AdminSettingsPage } from './modules/admin/AdminSettingsPage'
import { AdminStagesPage } from './modules/admin/AdminStagesPage'
import { AdminUserDetailPage } from './modules/admin/AdminUserDetailPage'
import { AdminUsersPage } from './modules/admin/AdminUsersPage'
import { ControlPlanePage } from './modules/admin/ControlPlanePage'
import { IndexerReleaseDetailPage } from './modules/indexer/IndexerReleaseDetailPage'
import { IndexerReleaseListPage } from './modules/indexer/IndexerReleaseListPage'
import { AccountTokensPage } from './modules/auth/AccountTokensPage'
import { RootRedirect } from './modules/RootRedirect'
import { AuthProvider } from './shared/auth/AuthContext'
import { RequireAuth } from './shared/auth/RequireAuth'
import { AdminAppShell, PublicAppShell } from './shared/layout/AppShell'

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
        <Route path="/setup" element={<SetupPage />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <PublicAppShell />
            </RequireAuth>
          }
        >
          <Route index element={<RootRedirect />} />
          <Route path="indexer/releases" element={<IndexerReleaseListPage />} />
          <Route path="indexer/browse/:category" element={<IndexerReleaseListPage />} />
          <Route path="indexer/browse/:category/:subcategory" element={<IndexerReleaseListPage />} />
          <Route path="indexer/releases/:id" element={<IndexerReleaseDetailPage />} />
          <Route path="account/tokens" element={<AccountTokensPage />} />
        </Route>
        <Route
          path="/admin"
          element={
            <RequireAuth>
              <AdminAppShell />
            </RequireAuth>
          }
        >
          <Route index element={<ControlPlanePage />} />
          <Route
            path="indexer/dashboard"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminDashboardPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/releases"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminReleasesPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/releases/:id"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminReleaseDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/stages"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminStagesPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/maintenance"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminMaintenancePage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/runs"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminRunsPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/runs/:id"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminRunDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/scrape"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminScrapePage />
              </RequireAuth>
            }
          />
          <Route
            path="settings"
            element={
              <RequireAuth permission="admin.settings.read">
                <AdminSettingsPage />
              </RequireAuth>
            }
          />
          <Route
            path="security/users"
            element={
              <RequireAuth permission="auth.users.read">
                <AdminUsersPage />
              </RequireAuth>
            }
          />
          <Route
            path="security/users/:id"
            element={
              <RequireAuth permission="auth.users.read">
                <AdminUserDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="security/roles"
            element={
              <RequireAuth permission="auth.roles.read">
                <AdminRolesPage />
              </RequireAuth>
            }
          />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </AuthProvider>
  )
}
