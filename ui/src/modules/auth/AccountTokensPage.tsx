import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { createCurrentUserToken, getCurrentUserTokens, revokeCurrentUserToken } from '../../shared/api/auth'
import { formatDateTime } from '../../shared/lib/format'
import type { Token } from '../../shared/types'

export function AccountTokensPage() {
  const [tokens, setTokens] = useState<Token[]>([])
  const [name, setName] = useState('')
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const response = await getCurrentUserTokens()
      setTokens(response.items ?? [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load API tokens')
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => void refresh(), 0)
    return () => window.clearTimeout(timer)
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createCurrentUserToken({ name })
      setMessage(`Token created. Secret: ${response.secret}`)
      setName('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create token')
    }
  }

  async function handleRevoke(id: string) {
    try {
      await revokeCurrentUserToken(id)
      setMessage('Token revoked.')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke token')
    }
  }

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <p className="eyebrow">Account</p>
        <h1 className="page-title">API tokens</h1>
        <p className="muted-copy">Create personal API tokens here. The secret is only shown once.</p>
      </div>
      <form className="page-card stack" onSubmit={handleSubmit}>
        <label className="field">
          <span>Token name</span>
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="CLI automation" />
        </label>
        <div className="button-row">
          <button className="primary-button" type="submit">
            Create Token
          </button>
        </div>
        {message ? <div className="banner">{message}</div> : null}
        {error ? <div className="banner error">{error}</div> : null}
      </form>
      <div className="page-card">
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Prefix</th>
                <th>Created</th>
                <th>Last Used</th>
                <th>Status</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {tokens.map((token) => (
                <tr key={token.id}>
                  <td>{token.name}</td>
                  <td><code>{token.prefix}</code></td>
                  <td>{formatDateTime(token.created_at)}</td>
                  <td>{formatDateTime(token.last_used_at)}</td>
                  <td>{token.revoked_at ? 'revoked' : 'active'}</td>
                  <td>
                    {!token.revoked_at ? (
                      <button className="secondary-button" onClick={() => void handleRevoke(token.id)}>
                        Revoke
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
              {tokens.length === 0 ? (
                <tr>
                  <td colSpan={6}>
                    <div className="empty-state">No API tokens yet.</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
