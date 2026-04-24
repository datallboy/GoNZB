import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { createToken, getTokens, getUsers, revokeToken } from '../../shared/api/auth'
import type { Token, User } from '../../shared/types'

export function AdminTokensPage() {
  const [users, setUsers] = useState<User[]>([])
  const [tokens, setTokens] = useState<Token[]>([])
  const [form, setForm] = useState({ user_id: '', name: '' })
  const [message, setMessage] = useState<string | null>(null)

  async function refresh() {
    const [userResponse, tokenResponse] = await Promise.all([getUsers(), getTokens()])
    setUsers(userResponse.items)
    setTokens(tokenResponse.items)
    if (!form.user_id && userResponse.items[0]) {
      setForm((current) => ({ ...current, user_id: userResponse.items[0].id }))
    }
  }

  useEffect(() => {
    void refresh().catch((err) => setMessage(err instanceof Error ? err.message : 'Failed to load tokens'))
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const response = await createToken(form)
      setMessage(`Token created. Secret: ${response.secret}`)
      setForm((current) => ({ ...current, name: '' }))
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to create token')
    }
  }

  return (
    <div className="page-section stack">
      <div className="dashboard-grid">
        <form className="page-card stack" onSubmit={handleSubmit}>
          <p className="eyebrow">Security</p>
          <h1 className="page-title">API Tokens</h1>
          <label className="field">
            <span>User</span>
            <select
              value={form.user_id}
              onChange={(event) => setForm((current) => ({ ...current, user_id: event.target.value }))}
            >
              {users.map((user) => (
                <option key={user.id} value={user.id}>
                  {user.username}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Token Name</span>
            <input
              value={form.name}
              onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
            />
          </label>
          <button className="primary-button" type="submit">
            Create Token
          </button>
          {message ? <div className="banner">{message}</div> : null}
        </form>

        <div className="page-card">
          <h2 className="section-title">Current Tokens</h2>
          <div className="stack">
            {tokens.map((token) => (
              <div className="list-row" key={token.id}>
                <div>
                  <strong>{token.name}</strong>
                  <div className="muted-row">
                    <span>{token.prefix}</span>
                    <span>{token.user_id}</span>
                  </div>
                </div>
                <button
                  className="secondary-button"
                  onClick={() =>
                    revokeToken(token.id)
                      .then(() => refresh())
                      .catch((err) =>
                        setMessage(err instanceof Error ? err.message : 'Failed to revoke token'),
                      )
                  }
                >
                  Revoke
                </button>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
