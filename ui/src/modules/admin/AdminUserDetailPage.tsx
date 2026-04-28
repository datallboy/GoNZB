import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { createToken, getUserDetail, revokeToken } from '../../shared/api/auth'
import { formatDateTime } from '../../shared/lib/format'
import type { Token, User } from '../../shared/types'

export function AdminUserDetailPage() {
  const { id = '' } = useParams()
  const [user, setUser] = useState<User | null>(null)
  const [tokens, setTokens] = useState<Token[]>([])
  const [name, setName] = useState('')
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const response = await getUserDetail(id)
      setUser(response.user)
      setTokens(response.tokens ?? [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load user detail')
    }
  }

  useEffect(() => {
    void refresh()
  }, [id])

  async function handleCreateToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createToken({ user_id: id, name })
      setMessage(`Token created. Secret: ${response.secret}`)
      setName('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create token')
    }
  }

  async function handleRevokeToken(tokenID: string) {
    try {
      await revokeToken(tokenID)
      setMessage('Token revoked.')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke token')
    }
  }

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Security</p>
            <h1 className="page-title">{user?.username || 'User detail'}</h1>
            <p className="muted-copy">
              {user?.enabled ? 'Enabled' : 'Disabled'} · roles: {(user?.role_ids ?? []).join(', ') || 'none'}
            </p>
          </div>
          <Link className="secondary-button" to="/admin/security/users">
            Back to Users
          </Link>
        </div>
        {error ? <div className="banner error">{error}</div> : null}
        {message ? <div className="banner">{message}</div> : null}
      </div>
      <div className="dashboard-grid">
        <div className="page-card stack">
          <h2 className="section-title">User summary</h2>
          <div className="detail-grid">
            <div><strong>User ID</strong><div className="muted-copy">{user?.id || 'n/a'}</div></div>
            <div><strong>Created</strong><div className="muted-copy">{formatDateTime(user?.created_at)}</div></div>
            <div><strong>Updated</strong><div className="muted-copy">{formatDateTime(user?.updated_at)}</div></div>
            <div><strong>Permissions</strong><div className="muted-copy">{(user?.permissions ?? []).join(', ') || 'none'}</div></div>
          </div>
        </div>
        <form className="page-card stack" onSubmit={handleCreateToken}>
          <h2 className="section-title">Create API token</h2>
          <label className="field">
            <span>Token name</span>
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Automation token" />
          </label>
          <button className="primary-button" type="submit">
            Create Token
          </button>
        </form>
      </div>
      <div className="page-card">
        <h2 className="section-title">Issued tokens</h2>
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
                      <button className="secondary-button" onClick={() => void handleRevokeToken(token.id)}>
                        Revoke
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
              {tokens.length === 0 ? (
                <tr>
                  <td colSpan={6}>
                    <div className="empty-state">No tokens issued for this user.</div>
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
