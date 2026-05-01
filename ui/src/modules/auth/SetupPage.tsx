import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { createInitialUser, getSetupStatus } from '../../shared/api/auth'
import { useAuth } from '../../shared/auth/AuthContext'

export function SetupPage() {
  const { session, refreshSession } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [loading, setLoading] = useState(true)
  const [setupRequired, setSetupRequired] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void getSetupStatus()
      .then((response) => {
        setSetupRequired(response.setup_required)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load setup status'))
      .finally(() => setLoading(false))
  }, [])

  if (session.authenticated) {
    return <Navigate to="/indexer/releases" replace />
  }
  if (!loading && !setupRequired) {
    return <Navigate to="/login" replace />
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password !== confirmPassword) {
      setError('passwords do not match')
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      await createInitialUser({ username, password })
      await refreshSession()
      navigate('/admin', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create initial user')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="auth-screen">
      <div className="auth-panel">
        <p className="eyebrow">Initial Setup</p>
        <h1 className="auth-title">Create the first administrator account.</h1>
        <p className="auth-copy">
          No default admin exists. This step is available only while the auth database has no users.
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
              autoComplete="new-password"
            />
          </label>
          <label className="field">
            <span>Confirm password</span>
            <input
              type="password"
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
              autoComplete="new-password"
            />
          </label>
          <p className="muted-copy">Passwords must be at least 12 characters.</p>
          {error ? <div className="banner error">{error}</div> : null}
          <button className="primary-button" type="submit" disabled={submitting || loading}>
            {submitting ? 'Creating account...' : 'Create administrator'}
          </button>
        </form>
      </div>
    </main>
  )
}
