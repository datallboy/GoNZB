import { createContext } from 'react'
import type { SessionInfo } from '../types'

export type AuthContextValue = {
  loading: boolean
  session: SessionInfo
  refreshSession: () => Promise<void>
  logout: () => Promise<void>
  hasPermission: (permission: string) => boolean
}

export const AuthContext = createContext<AuthContextValue | null>(null)
