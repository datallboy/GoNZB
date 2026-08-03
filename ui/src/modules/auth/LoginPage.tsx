import { useState } from 'react'
import type { FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { createSession } from '../../shared/api/auth'
import { useAuth } from '../../shared/auth/useAuth'

function safePostLoginPath(value: unknown) {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) {
    return '/indexer/releases'
  }
  return value
}

export function LoginPage() {
  const { session, refreshSession } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (session.authenticated) {
    return <Navigate to="/indexer/releases" replace />
  }
  if (session.setup_required) {
    return <Navigate to="/setup" replace />
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await createSession({ username, password })
      await refreshSession()
      const next = safePostLoginPath((location.state as { from?: string } | null)?.from)
      navigate(next, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="auth-screen">
      <div className="auth-panel">
        <p className="eyebrow">Indexer Control</p>
        <h1 className="auth-title">Sign in to the new indexer workspace.</h1>
        <p className="auth-copy">
          Browse indexed releases, operate enabled modules, and manage integrations from one place.
        </p>
        <form className="stack" onSubmit={handleSubmit}>
          <label className="field">
            <span>Username</span>
            <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
          </label>
          <label className="field">
            <span>Password</span>
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
            />
          </label>
          {error ? <div className="banner error">{error}</div> : null}
          <button className="primary-button" type="submit" disabled={submitting}>
            {submitting ? 'Signing in...' : 'Sign in'}
          </button>
        </form>
      </div>
    </main>
  )
}
