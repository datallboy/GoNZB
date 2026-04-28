import type { PropsWithChildren } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useAuth } from './AuthContext'

type RequireAuthProps = PropsWithChildren<{
  permission?: string
}>

export function RequireAuth({ children, permission }: RequireAuthProps) {
  const { loading, session, hasPermission } = useAuth()
  const location = useLocation()

  if (loading) {
    return <div className="banner">Loading session...</div>
  }
  if (!session.authenticated) {
    if (session.setup_required) {
      return <Navigate to="/setup" replace />
    }
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  if (permission && !hasPermission(permission)) {
    return <div className="banner error">You do not have permission to view this page.</div>
  }
  return <>{children}</>
}
