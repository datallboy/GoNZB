import { useEffect, useState } from 'react'
import type { PropsWithChildren } from 'react'
import { deleteSession, getSession } from '../api/auth'
import type { SessionInfo } from '../types'
import { AuthContext } from './AuthState'

const unauthenticatedSession: SessionInfo = {
  authenticated: false,
  setup_required: false,
  permissions: [],
}

export function AuthProvider({ children }: PropsWithChildren) {
  const [loading, setLoading] = useState(true)
  const [session, setSession] = useState<SessionInfo>(unauthenticatedSession)

  async function refreshSession() {
    const response = await getSession()
    setSession(response.session)
  }

  async function logout() {
    await deleteSession()
    setSession(unauthenticatedSession)
  }

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void refreshSession()
        .catch(() => setSession(unauthenticatedSession))
        .finally(() => setLoading(false))
    }, 0)
    return () => window.clearTimeout(timer)
  }, [])

  function hasPermission(permission: string) {
    return session.permissions.includes(permission)
  }

  return (
    <AuthContext.Provider value={{ loading, session, refreshSession, logout, hasPermission }}>
      {children}
    </AuthContext.Provider>
  )
}
