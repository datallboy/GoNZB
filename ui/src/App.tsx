import { lazy, Suspense } from 'react'
import { Route, Routes } from 'react-router-dom'
import { AuthProvider } from './shared/auth/AuthContext'
import { RequireAuth } from './shared/auth/RequireAuth'
import { AdminAppShell, PublicAppShell } from './shared/layout/AppShell'

const LoginPage = lazy(() => import('./modules/auth/LoginPage').then((module) => ({ default: module.LoginPage })))
const SetupPage = lazy(() => import('./modules/auth/SetupPage').then((module) => ({ default: module.SetupPage })))
const AccountTokensPage = lazy(() => import('./modules/auth/AccountTokensPage').then((module) => ({ default: module.AccountTokensPage })))
const RootRedirect = lazy(() => import('./modules/RootRedirect').then((module) => ({ default: module.RootRedirect })))
const IndexerReleaseDetailPage = lazy(() => import('./modules/indexer/IndexerReleaseDetailPage').then((module) => ({ default: module.IndexerReleaseDetailPage })))
const IndexerReleaseListPage = lazy(() => import('./modules/indexer/IndexerReleaseListPage').then((module) => ({ default: module.IndexerReleaseListPage })))
const AdminAttentionPage = lazy(() => import('./modules/admin/AdminAttentionPage').then((module) => ({ default: module.AdminAttentionPage })))
const AdminArticleCohortsPage = lazy(() => import('./modules/admin/AdminArticleCohortsPage').then((module) => ({ default: module.AdminArticleCohortsPage })))
const AdminDashboardPage = lazy(() => import('./modules/admin/AdminDashboardPage').then((module) => ({ default: module.AdminDashboardPage })))
const AdminBinaryDetailPage = lazy(() => import('./modules/admin/AdminBinaryDetailPage').then((module) => ({ default: module.AdminBinaryDetailPage })))
const AdminBinariesPage = lazy(() => import('./modules/admin/AdminBinariesPage').then((module) => ({ default: module.AdminBinariesPage })))
const AdminGoNZBNetPage = lazy(() => import('./modules/admin/AdminGoNZBNetPage').then((module) => ({ default: module.AdminGoNZBNetPage })))
const AdminIndexerWorkPage = lazy(() => import('./modules/admin/AdminIndexerWorkPage').then((module) => ({ default: module.AdminIndexerWorkPage })))
const AdminMaintenancePage = lazy(() => import('./modules/admin/AdminMaintenancePage').then((module) => ({ default: module.AdminMaintenancePage })))
const AdminReleaseDetailPage = lazy(() => import('./modules/admin/AdminReleaseDetailPage').then((module) => ({ default: module.AdminReleaseDetailPage })))
const AdminReleasesPage = lazy(() => import('./modules/admin/AdminReleasesPage').then((module) => ({ default: module.AdminReleasesPage })))
const AdminRolesPage = lazy(() => import('./modules/admin/AdminRolesPage').then((module) => ({ default: module.AdminRolesPage })))
const AdminRunDetailPage = lazy(() => import('./modules/admin/AdminRunDetailPage').then((module) => ({ default: module.AdminRunDetailPage })))
const AdminRunsPage = lazy(() => import('./modules/admin/AdminRunsPage').then((module) => ({ default: module.AdminRunsPage })))
const AdminScrapePage = lazy(() => import('./modules/admin/AdminScrapePage').then((module) => ({ default: module.AdminScrapePage })))
const AdminSettingsPage = lazy(() => import('./modules/admin/AdminSettingsPage').then((module) => ({ default: module.AdminSettingsPage })))
const AdminStagesPage = lazy(() => import('./modules/admin/AdminStagesPage').then((module) => ({ default: module.AdminStagesPage })))
const AdminUserDetailPage = lazy(() => import('./modules/admin/AdminUserDetailPage').then((module) => ({ default: module.AdminUserDetailPage })))
const AdminUsersPage = lazy(() => import('./modules/admin/AdminUsersPage').then((module) => ({ default: module.AdminUsersPage })))
const ControlPlanePage = lazy(() => import('./modules/admin/ControlPlanePage').then((module) => ({ default: module.ControlPlanePage })))

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
      <Suspense fallback={<div className="banner"><span className="loading-dot" /> Loading page...</div>}>
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
            path="indexer/attention"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminAttentionPage />
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
            path="indexer/binaries"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminBinariesPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/binaries/:id"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminBinaryDetailPage />
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
            path="indexer/work"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminIndexerWorkPage />
              </RequireAuth>
            }
          />
          <Route
            path="indexer/cohorts"
            element={
              <RequireAuth permission="indexer.runtime.read">
                <AdminArticleCohortsPage />
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
            path="gonzbnet"
            element={
              <RequireAuth permission="gonzbnet.admin.read">
                <AdminGoNZBNetPage />
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
      </Suspense>
    </AuthProvider>
  )
}
