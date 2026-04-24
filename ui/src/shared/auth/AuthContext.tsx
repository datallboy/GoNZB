import { createContext, useContext, useEffect, useState } from 'react'
import type { PropsWithChildren } from 'react'
import { deleteSession, getSession } from '../api/auth'
import type { SessionInfo } from '../types'

type AuthContextValue = {
  loading: boolean
  session: SessionInfo
  refreshSession: () => Promise<void>
  logout: () => Promise<void>
  hasPermission: (permission: string) => boolean
}

const unauthenticatedSession: SessionInfo = {
  authenticated: false,
  permissions: [],
}

const AuthContext = createContext<AuthContextValue | null>(null)

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
    void refreshSession()
      .catch(() => setSession(unauthenticatedSession))
      .finally(() => setLoading(false))
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

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) {
    throw new Error('useAuth must be used inside AuthProvider')
  }
  return value
}
