import { apiRequest } from './http'
import type {
  RoleListResponse,
  SetupStatusResponse,
  SessionResponse,
  TokenCreateRequest,
  TokenCreateResponse,
  TokenListResponse,
  UpsertRoleRequest,
  UpsertUserRequest,
  UserDetailResponse,
  UserListResponse,
} from '../types'

export function getSession() {
  return apiRequest<SessionResponse>('/api/v1/auth/session')
}

export function getSetupStatus() {
  return apiRequest<SetupStatusResponse>('/api/v1/auth/setup')
}

export function createInitialUser(body: { username: string; password: string; bootstrap_token?: string }) {
  return apiRequest<SessionResponse>('/api/v1/auth/setup', { method: 'POST', body })
}

export function createSession(body: { username: string; password: string }) {
  return apiRequest<SessionResponse>('/api/v1/auth/session', { method: 'POST', body })
}

export function deleteSession() {
  return apiRequest<void>('/api/v1/auth/session', { method: 'DELETE' })
}

export function getUsers() {
  return apiRequest<UserListResponse>('/api/v1/admin/auth/users')
}

export function upsertUser(body: UpsertUserRequest) {
  return apiRequest<{ user: unknown }>('/api/v1/admin/auth/users', { method: 'POST', body })
}

export function deleteUser(id: string) {
  return apiRequest<void>(`/api/v1/admin/auth/users/${id}`, { method: 'DELETE' })
}

export function getUserDetail(id: string) {
  return apiRequest<UserDetailResponse>(`/api/v1/admin/auth/users/${id}`)
}

export function getRoles() {
  return apiRequest<RoleListResponse>('/api/v1/admin/auth/roles')
}

export function upsertRole(body: UpsertRoleRequest) {
  return apiRequest<{ role: unknown }>('/api/v1/admin/auth/roles', { method: 'POST', body })
}

export function deleteRole(id: string) {
  return apiRequest<void>(`/api/v1/admin/auth/roles/${id}`, { method: 'DELETE' })
}

export function getTokens() {
  return apiRequest<TokenListResponse>('/api/v1/admin/auth/tokens')
}

export function createToken(body: TokenCreateRequest) {
  return apiRequest<TokenCreateResponse>('/api/v1/admin/auth/tokens', { method: 'POST', body })
}

export function revokeToken(id: string) {
  return apiRequest<void>(`/api/v1/admin/auth/tokens/${id}`, { method: 'DELETE' })
}

export function getCurrentUserTokens() {
  return apiRequest<TokenListResponse>('/api/v1/auth/tokens')
}

export function createCurrentUserToken(body: { name: string }) {
  return apiRequest<TokenCreateResponse>('/api/v1/auth/tokens', { method: 'POST', body })
}

export function revokeCurrentUserToken(id: string) {
  return apiRequest<void>(`/api/v1/auth/tokens/${id}`, { method: 'DELETE' })
}
