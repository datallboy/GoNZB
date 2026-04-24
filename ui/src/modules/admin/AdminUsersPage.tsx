import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { deleteUser, getRoles, getUsers, upsertUser } from '../../shared/api/auth'
import type { Role, User } from '../../shared/types'

export function AdminUsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [message, setMessage] = useState<string | null>(null)
  const [form, setForm] = useState({
    id: '',
    username: '',
    password: '',
    enabled: true,
    role_ids: ['viewer'],
  })

  async function refresh() {
    const [userResponse, roleResponse] = await Promise.all([getUsers(), getRoles()])
    setUsers(userResponse.items)
    setRoles(roleResponse.items)
  }

  useEffect(() => {
    void refresh().catch((err) => setMessage(err instanceof Error ? err.message : 'Failed to load users'))
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await upsertUser(form)
      setMessage('User saved.')
      setForm({ id: '', username: '', password: '', enabled: true, role_ids: ['viewer'] })
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save user')
    }
  }

  return (
    <div className="page-section stack">
      <div className="dashboard-grid">
        <form className="page-card stack" onSubmit={handleSubmit}>
          <p className="eyebrow">Security</p>
          <h1 className="page-title">Users</h1>
          <label className="field">
            <span>User ID</span>
            <input value={form.id} onChange={(event) => setForm((current) => ({ ...current, id: event.target.value }))} />
          </label>
          <label className="field">
            <span>Username</span>
            <input
              value={form.username}
              onChange={(event) => setForm((current) => ({ ...current, username: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Password</span>
            <input
              type="password"
              value={form.password}
              onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))}
            />
          </label>
          <label className="field checkbox-field">
            <span>Enabled</span>
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => setForm((current) => ({ ...current, enabled: event.target.checked }))}
            />
          </label>
          <label className="field">
            <span>Roles</span>
            <select
              multiple
              value={form.role_ids}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  role_ids: Array.from(event.target.selectedOptions).map((option) => option.value),
                }))
              }
            >
              {roles.map((role) => (
                <option key={role.id} value={role.id}>
                  {role.name}
                </option>
              ))}
            </select>
          </label>
          <button className="primary-button" type="submit">
            Save User
          </button>
          {message ? <div className="banner">{message}</div> : null}
        </form>

        <div className="page-card">
          <h2 className="section-title">Current Users</h2>
          <div className="stack">
            {users.map((user) => (
              <div className="list-row" key={user.id}>
                <div>
                  <strong>{user.username}</strong>
                  <div className="muted-row">
                    <span>{user.id}</span>
                    <span>{user.enabled ? 'enabled' : 'disabled'}</span>
                  </div>
                </div>
                <button
                  className="secondary-button"
                  onClick={() =>
                    deleteUser(user.id)
                      .then(() => refresh())
                      .catch((err) =>
                        setMessage(err instanceof Error ? err.message : 'Failed to delete user'),
                      )
                  }
                >
                  Delete
                </button>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
